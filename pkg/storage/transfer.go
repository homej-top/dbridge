package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"time"
)

const (
	maxDirDepth      = 50
	copyBufferSize   = 32 * 1024
	maxSyncFiles     = 100000
	maxSyncTotalSize = 100 << 30
)

type ConflictStrategy string

const (
	ConflictOverwrite ConflictStrategy = "overwrite"
	ConflictSkip    ConflictStrategy = "skip"
	ConflictRename  ConflictStrategy = "rename"
)

type SyncMode string

const (
	SyncCopy SyncMode = "copy"
	SyncMove SyncMode = "move"
)

type SyncOptions struct {
	Conflict ConflictStrategy `json:"conflict"`
	Mode     SyncMode         `json:"mode"`
	DryRun   bool             `json:"dry_run"`
	Filter   *SyncFilter      `json:"filter"`
}

type SyncFilter struct {
	Patterns []string `json:"patterns"`
	MinSize  int64    `json:"min_size"`
	MaxSize  int64    `json:"max_size"`
}

type SyncPreview struct {
	DryRun   bool               `json:"dry_run"`
	Mode     SyncMode           `json:"mode"`
	ToCreate []PreviewItem      `json:"to_create"`
	ToUpdate []PreviewItem      `json:"to_update"`
	ToDelete []PreviewItem      `json:"to_delete"`
	Summary  SyncPreviewSummary `json:"summary"`
}

type PreviewItem struct {
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Conflict string `json:"conflict,omitempty"`
}

type SyncPreviewSummary struct {
	NewFiles            int   `json:"new_files"`
	UpdatedFiles        int   `json:"updated_files"`
	DeletedFiles        int   `json:"deleted_files"`
	SourceFilesToDelete int   `json:"source_files_to_delete"`
	TotalBytes          int64 `json:"total_bytes"`
	IsMoveMode          bool  `json:"is_move_mode"`
}

type TransferError struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}

type TransferResult struct {
	TotalFiles       int64           `json:"total_files"`
	Transferred      int64           `json:"transferred"`
	Skipped          int64           `json:"skipped"`
	Failed           int64           `json:"failed"`
	TotalBytes       int64           `json:"total_bytes"`
	TransferredBytes int64           `json:"transferred_bytes"`
	TotalErrors      int64           `json:"total_errors"`
	Errors           []TransferError `json:"errors,omitempty"`
	ErrorsTruncated  bool            `json:"errors_truncated"`
	Duration         time.Duration   `json:"duration"`
}

func (r *TransferResult) addError(filePath, message string) {
	r.TotalErrors++
	if len(r.Errors) < 100 {
		r.Errors = append(r.Errors, TransferError{Path: filePath, Message: message})
	} else {
		r.ErrorsTruncated = true
	}
}

func (r *TransferResult) merge(other *TransferResult) {
	if other == nil {
		return
	}
	r.TotalFiles += other.TotalFiles
	r.Transferred += other.Transferred
	r.Skipped += other.Skipped
	r.Failed += other.Failed
	r.TotalBytes += other.TotalBytes
	r.TransferredBytes += other.TransferredBytes
	r.TotalErrors += other.TotalErrors
	for _, e := range other.Errors {
		if len(r.Errors) < 100 {
			r.Errors = append(r.Errors, e)
		} else {
			r.ErrorsTruncated = true
		}
	}
	if other.ErrorsTruncated {
		r.ErrorsTruncated = true
	}
}

type TransferService struct{}

func NewTransferService() *TransferService {
	return &TransferService{}
}

func (t *TransferService) Close() error { return nil }

func (t *TransferService) CopyFile(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, overwrite bool) error {
	if !overwrite {
		if exists, _ := dst.Exists(ctx, dstPath); exists {
			return fmt.Errorf("transfer: target already exists: %s (set overwrite=true to replace)", dstPath)
		}
	}

	if isSameStorage(src, dst) {
		return t.copySameStorage(ctx, src, srcPath, dstPath)
	}

	reader, err := src.ReadStream(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("transfer: read source: %w", err)
	}
	defer reader.Close()

	stat, err := src.Stat(ctx, srcPath)
	if err != nil {
		return fmt.Errorf("transfer: stat source: %w", err)
	}

	contentType := stat.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err = dst.Save(ctx, dstPath, reader, contentType)
	if err != nil {
		return fmt.Errorf("transfer: save target: %w", err)
	}
	return nil
}

func (t *TransferService) CopyDir(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, overwrite bool) (*TransferResult, error) {
	start := time.Now()
	result := &TransferResult{}
	err := t.recursiveCopy(ctx, src, srcPath, dst, dstPath, overwrite, 0, result)
	result.Duration = time.Since(start)
	return result, err
}

func (t *TransferService) recursiveCopy(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, overwrite bool, depth int, result *TransferResult) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxDirDepth {
		return fmt.Errorf("transfer: max directory depth (%d) exceeded at %s", maxDirDepth, srcPath)
	}

	listResult, err := src.List(ctx, srcPath, &ListOptions{Page: 1, PageSize: 10000})
	if err != nil {
		return fmt.Errorf("transfer: list source %s: %w", srcPath, err)
	}

	for _, f := range listResult.Files {
		if err := ctx.Err(); err != nil {
			return err
		}

		if f.IsDir {
			childDstPath := path.Join(dstPath, f.Name)
			if err := t.recursiveCopy(ctx, src, f.Path, dst, childDstPath, overwrite, depth+1, result); err != nil {
				return err
			}
			continue
		}

		result.TotalFiles++
		result.TotalBytes += f.Size

		childDstPath := path.Join(dstPath, f.Name)
		if err := t.CopyFile(ctx, src, f.Path, dst, childDstPath, overwrite); err != nil {
			result.Failed++
			result.addError(f.Path, err.Error())
			continue
		}
		result.Transferred++
		result.TransferredBytes += f.Size
	}
	return nil
}

func (t *TransferService) MoveFile(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, overwrite bool) error {
	if !overwrite {
		if exists, _ := dst.Exists(ctx, dstPath); exists {
			return fmt.Errorf("transfer: target already exists: %s (set overwrite=true to replace)", dstPath)
		}
	}

	if isSameStorage(src, dst) {
		return src.Rename(ctx, srcPath, dstPath)
	}

	if err := t.CopyFile(ctx, src, srcPath, dst, dstPath, true); err != nil {
		return err
	}
	return src.Delete(ctx, srcPath)
}

func (t *TransferService) MoveDir(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, overwrite bool) (*TransferResult, error) {
	start := time.Now()
	result, copyErr := t.CopyDir(ctx, src, srcPath, dst, dstPath, overwrite)
	if copyErr != nil && result == nil {
		return nil, copyErr
	}

	if result.Transferred > 0 {
		t.deleteTransferredSources(ctx, src, srcPath, dst, dstPath, result)
	}

	_ = src.RemoveDir(ctx, srcPath)

	result.Duration = time.Since(start)
	return result, copyErr
}

func (t *TransferService) deleteTransferredSources(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, result *TransferResult) {
	listResult, err := src.List(ctx, srcPath, &ListOptions{Page: 1, PageSize: 10000})
	if err != nil {
		return
	}
	for _, f := range listResult.Files {
		if err := ctx.Err(); err != nil {
			return
		}
		if f.IsDir {
			childDstPath := path.Join(dstPath, f.Name)
			t.deleteTransferredSources(ctx, src, f.Path, dst, childDstPath, result)
			continue
		}
		childDstPath := path.Join(dstPath, f.Name)
		if exists, _ := dst.Exists(ctx, childDstPath); exists {
			if delErr := src.Delete(ctx, f.Path); delErr != nil {
				result.addError(f.Path, "delete source after copy: "+delErr.Error())
			}
		}
	}
}

func isSameStorage(src, dst FileStorage) bool {
	return src == dst
}

func (t *TransferService) copySameStorage(ctx context.Context, fs FileStorage, srcPath, dstPath string) error {
	switch s := fs.(type) {
	case *LocalFileStorage:
		return s.copyLocal(ctx, srcPath, dstPath)
	case *S3FileStorage:
		return s.copyObject(ctx, srcPath, dstPath)
	default:
		reader, err := fs.ReadStream(ctx, srcPath)
		if err != nil {
			return err
		}
		defer reader.Close()
		_, err = fs.Save(ctx, dstPath, reader, "")
		return err
	}
}

func copyStream(ctx context.Context, src FileStorage, srcPath string, dst io.Writer) (int64, error) {
	reader, err := src.ReadStream(ctx, srcPath)
	if err != nil {
		return 0, err
	}
	defer reader.Close()
	buf := make([]byte, copyBufferSize)
	return io.CopyBuffer(dst, reader, buf)
}

func (t *TransferService) SyncDir(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, opts SyncOptions) (*TransferResult, error) {
	if opts.Mode == "" {
		opts.Mode = SyncCopy
	}
	if opts.Conflict == "" {
		opts.Conflict = ConflictSkip
	}

	srcFiles, err := listAllFiles(ctx, src, srcPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: list source: %w", err)
	}
	if len(srcFiles) > maxSyncFiles {
		return nil, fmt.Errorf("transfer: source has %d files, exceeds limit %d, please sync in batches", len(srcFiles), maxSyncFiles)
	}

	dstFiles, err := listAllFiles(ctx, dst, dstPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: list target: %w", err)
	}

	dstMap := make(map[string]FileInfo, len(dstFiles))
	for _, f := range dstFiles {
		rel := relPath(dstPath, f.Path)
		dstMap[rel] = f
	}

	var toCreate []FileInfo
	var toUpdate []FileInfo
	for _, sf := range srcFiles {
		if !matchesFilter(sf, opts.Filter) {
			continue
		}
		rel := relPath(srcPath, sf.Path)
		if df, ok := dstMap[rel]; !ok {
			toCreate = append(toCreate, sf)
		} else if sf.Size != df.Size || sf.ModTime.Unix() != df.ModTime.Unix() {
			toUpdate = append(toUpdate, sf)
		}
	}

	start := time.Now()
	result := &TransferResult{}

	for _, sf := range toCreate {
		if err := ctx.Err(); err != nil {
			result.Duration = time.Since(start)
			return result, err
		}
		rel := relPath(srcPath, sf.Path)
		dstFile := path.Join(dstPath, rel)
		result.TotalFiles++
		result.TotalBytes += sf.Size

		if err := t.CopyFile(ctx, src, sf.Path, dst, dstFile, true); err != nil {
			result.Failed++
			result.addError(sf.Path, err.Error())
			continue
		}
		result.Transferred++
		result.TransferredBytes += sf.Size

		if opts.Mode == SyncMove {
			if delErr := src.Delete(ctx, sf.Path); delErr != nil {
				result.addError(sf.Path, "delete source: "+delErr.Error())
			}
		}
	}

	for _, sf := range toUpdate {
		if err := ctx.Err(); err != nil {
			result.Duration = time.Since(start)
			return result, err
		}
		rel := relPath(srcPath, sf.Path)
		dstFile := path.Join(dstPath, rel)

		switch opts.Conflict {
		case ConflictSkip:
			result.TotalFiles++
			result.TotalBytes += sf.Size
			result.Skipped++
			continue
		case ConflictRename:
			dstFile = generateRename(dstFile)
		}

		result.TotalFiles++
		result.TotalBytes += sf.Size

		if err := t.CopyFile(ctx, src, sf.Path, dst, dstFile, true); err != nil {
			result.Failed++
			result.addError(sf.Path, err.Error())
			continue
		}
		result.Transferred++
		result.TransferredBytes += sf.Size

		if opts.Mode == SyncMove {
			if delErr := src.Delete(ctx, sf.Path); delErr != nil {
				result.addError(sf.Path, "delete source: "+delErr.Error())
			}
		}
	}

	result.Duration = time.Since(start)
	return result, nil
}

func (t *TransferService) ComputeSyncPreview(ctx context.Context, src FileStorage, srcPath string, dst FileStorage, dstPath string, opts SyncOptions) (*SyncPreview, error) {
	if opts.Mode == "" {
		opts.Mode = SyncCopy
	}
	if opts.Conflict == "" {
		opts.Conflict = ConflictSkip
	}

	srcFiles, err := listAllFiles(ctx, src, srcPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: list source: %w", err)
	}
	if len(srcFiles) > maxSyncFiles {
		return nil, fmt.Errorf("transfer: source has %d files, exceeds limit %d, please sync in batches", len(srcFiles), maxSyncFiles)
	}

	dstFiles, err := listAllFiles(ctx, dst, dstPath)
	if err != nil {
		return nil, fmt.Errorf("transfer: list target: %w", err)
	}

	dstMap := make(map[string]FileInfo, len(dstFiles))
	for _, f := range dstFiles {
		rel := relPath(dstPath, f.Path)
		dstMap[rel] = f
	}

	preview := &SyncPreview{
		DryRun:   true,
		Mode:     opts.Mode,
		ToCreate: []PreviewItem{},
		ToUpdate: []PreviewItem{},
		ToDelete: []PreviewItem{},
	}

	for _, sf := range srcFiles {
		if !matchesFilter(sf, opts.Filter) {
			continue
		}
		rel := relPath(srcPath, sf.Path)
		dstFile := path.Join(dstPath, rel)

		if _, ok := dstMap[rel]; !ok {
			preview.ToCreate = append(preview.ToCreate, PreviewItem{Path: dstFile, Size: sf.Size})
			preview.Summary.NewFiles++
			preview.Summary.TotalBytes += sf.Size
		} else {
			df := dstMap[rel]
			if sf.Size != df.Size || sf.ModTime.Unix() != df.ModTime.Unix() {
				preview.ToUpdate = append(preview.ToUpdate, PreviewItem{
					Path:     dstFile,
					Size:     sf.Size,
					Conflict: string(opts.Conflict),
				})
				preview.Summary.UpdatedFiles++
				if opts.Conflict != ConflictSkip {
					preview.Summary.TotalBytes += sf.Size
				}
			}
		}
	}

	if opts.Mode == SyncMove {
		preview.Summary.IsMoveMode = true
		preview.Summary.SourceFilesToDelete = preview.Summary.NewFiles
		if opts.Conflict == ConflictOverwrite || opts.Conflict == ConflictRename {
			preview.Summary.SourceFilesToDelete += preview.Summary.UpdatedFiles
		}
	}

	return preview, nil
}

func listAllFiles(ctx context.Context, fs FileStorage, dir string) ([]FileInfo, error) {
	var all []FileInfo
	var listRecursive func(currentDir string) error
	listRecursive = func(currentDir string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		result, err := fs.List(ctx, currentDir, &ListOptions{Page: 1, PageSize: 10000})
		if err != nil {
			return err
		}
		for _, f := range result.Files {
			if len(all) > maxSyncFiles {
				return fmt.Errorf("too many files")
			}
			if f.IsDir {
				if err := listRecursive(f.Path); err != nil {
					return err
				}
			} else {
				all = append(all, f)
			}
		}
		return nil
	}
	if err := listRecursive(dir); err != nil {
		return nil, err
	}
	return all, nil
}

func relPath(base, fullPath string) string {
	if base == "" {
		return fullPath
	}
	r := fullPath
	if len(r) > len(base) && r[:len(base)] == base {
		r = r[len(base):]
	}
	if len(r) > 0 && r[0] == '/' {
		r = r[1:]
	}
	return r
}

func matchesFilter(f FileInfo, filter *SyncFilter) bool {
	if filter == nil {
		return true
	}
	if len(filter.Patterns) > 0 {
		matched := false
		name := path.Base(f.Path)
		for _, p := range filter.Patterns {
			if ok, _ := path.Match(p, name); ok {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if filter.MinSize > 0 && f.Size < filter.MinSize {
		return false
	}
	if filter.MaxSize > 0 && f.Size > filter.MaxSize {
		return false
	}
	return true
}

func generateRename(dstPath string) string {
	ext := path.Ext(dstPath)
	base := dstPath[:len(dstPath)-len(ext)]
	return fmt.Sprintf("%s(1)%s", base, ext)
}
