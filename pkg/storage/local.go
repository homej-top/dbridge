package storage

import (
	"context"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// LocalFileStorage 本地文件系统存储实现
type LocalFileStorage struct {
	BasePath string
}

// NewLocalFileStorage 创建本地文件存储实例
func NewLocalFileStorage(basePath string) (*LocalFileStorage, error) {
	absPath, err := filepath.Abs(basePath)
	if err != nil {
		return nil, fmt.Errorf("storage: resolve base path: %w", err)
	}
	if err := os.MkdirAll(absPath, 0755); err != nil {
		return nil, fmt.Errorf("storage: create base dir: %w", err)
	}
	return &LocalFileStorage{BasePath: absPath}, nil
}

// HealthCheck 实现 HealthChecker 接口，检查根目录是否可访问
func (s *LocalFileStorage) HealthCheck(ctx context.Context) error {
	_, err := os.Stat(s.BasePath)
	return err
}

// Resolve 将相对路径解析为安全的绝对路径，公开给 SQLite 连接等场景使用
func (s *LocalFileStorage) Resolve(path string) (string, error) {
	return s.resolve(path)
}

// resolve 路径安全校验：软链接解析 + 大小写归一 + 父目录链校验 + 前后缀双重确认
func (s *LocalFileStorage) resolve(path string) (string, error) {
	cleanPath := filepath.Clean(path)
	if filepath.IsAbs(cleanPath) || strings.HasPrefix(cleanPath, "..") {
		return "", fmt.Errorf("storage: invalid path: %s", path)
	}

	// 解析基础路径的软链接
	basePath, err := filepath.EvalSymlinks(s.BasePath)
	if err != nil {
		return "", fmt.Errorf("storage: resolve base path: %w", err)
	}

	fullPath := filepath.Join(basePath, cleanPath)
	
	// 对已存在的路径解析软链接
	resolvedPath, evalErr := filepath.EvalSymlinks(fullPath)
	if evalErr == nil {
		fullPath = resolvedPath
	} else if !os.IsNotExist(evalErr) {
		return "", fmt.Errorf("storage: resolve path: %w", evalErr)
	}
	// 对于尚不存在的路径（如 Save 新建文件），使用原始路径继续校验

	// 逐级校验父目录链均在 BasePath 内
	normalizedBase := strings.ToLower(filepath.ToSlash(basePath))
	current := filepath.Dir(fullPath)
	for current != basePath && strings.HasPrefix(current, basePath) {
		normalized := strings.ToLower(filepath.ToSlash(current))
		if !strings.HasPrefix(normalized, normalizedBase) {
			return "", fmt.Errorf("storage: path traversal detected: %s", path)
		}
		if current == "/" || current == "." {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	// 最终前缀校验
	normalizedFull := strings.ToLower(filepath.ToSlash(fullPath))
	if !strings.HasPrefix(normalizedFull, normalizedBase) {
		return "", fmt.Errorf("storage: path traversal detected: %s", path)
	}
	return fullPath, nil
}

// Save 保存文件
func (s *LocalFileStorage) Save(ctx context.Context, path string, data io.Reader, contentType string) (*FileInfo, error) {
	if data == nil {
		return nil, fmt.Errorf("storage: data reader is nil")
	}
	fullPath, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	if info, statErr := os.Stat(fullPath); statErr == nil && info.IsDir() {
		return nil, fmt.Errorf("storage: path is a directory: %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return nil, fmt.Errorf("storage: mkdir: %w", err)
	}

	f, err := os.Create(fullPath)
	if err != nil {
		return nil, fmt.Errorf("storage: create file: %w", err)
	}

	// ctxReader 在每次 Read 前检查 ctx 是否取消，不泄漏 goroutine
	cr := &ctxReader{ctx: ctx, r: data}
	written, copyErr := io.Copy(f, cr)
	f.Close()

	if copyErr != nil {
		os.Remove(fullPath)
		return nil, fmt.Errorf("storage: write file: %w", copyErr)
	}

	return &FileInfo{
		Name:        filepath.Base(path),
		Path:        path,
		IsDir:       false,
		Size:        written,
		ModTime:     time.Now(),
		ContentType: contentType,
	}, nil
}

// Read 读取文件全部内容
func (s *LocalFileStorage) Read(ctx context.Context, path string) ([]byte, error) {
	fullPath, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(fullPath)
}

// ReadStream 流式读取文件
func (s *LocalFileStorage) ReadStream(ctx context.Context, path string) (io.ReadCloser, error) {
	fullPath, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	return os.Open(fullPath)
}

// Delete 仅删除文件，目录返回错误
func (s *LocalFileStorage) Delete(ctx context.Context, path string) error {
	fullPath, err := s.resolve(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("storage: path is a directory, use RemoveDir: %s", path)
	}
	return os.Remove(fullPath)
}

// RemoveDir 删除空目录，非空报错
func (s *LocalFileStorage) RemoveDir(ctx context.Context, path string) error {
	fullPath, err := s.resolve(path)
	if err != nil {
		return err
	}
	return os.Remove(fullPath)
}

// Exists 检查路径是否存在
func (s *LocalFileStorage) Exists(ctx context.Context, path string) (bool, error) {
	fullPath, err := s.resolve(path)
	if err != nil {
		return false, err
	}
	_, err = os.Stat(fullPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	return err == nil, err
}

// Stat 获取文件/目录元信息
func (s *LocalFileStorage) Stat(ctx context.Context, path string) (*FileInfo, error) {
	fullPath, err := s.resolve(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(fullPath)
	if err != nil {
		return nil, err
	}
	contentType := ""
	if !info.IsDir() {
		contentType = mime.TypeByExtension(filepath.Ext(info.Name()))
	}
	return &FileInfo{
		Name:        info.Name(),
		Path:        path,
		IsDir:       info.IsDir(),
		Size:        info.Size(),
		ModTime:     info.ModTime(),
		ContentType: contentType,
	}, nil
}

// Mkdir 创建目录（递归创建父目录）
func (s *LocalFileStorage) Mkdir(ctx context.Context, path string) error {
	fullPath, err := s.resolve(path)
	if err != nil {
		return err
	}
	return os.MkdirAll(fullPath, 0755)
}

// List 列出目录内容（支持分页、搜索、递归）
func (s *LocalFileStorage) List(ctx context.Context, dir string, opts *ListOptions) (*ListResult, error) {
	if opts == nil {
		opts = &ListOptions{Page: 1, PageSize: 50}
	}
	if opts.Page < 1 {
		opts.Page = 1
	}
	if opts.PageSize < 1 {
		opts.PageSize = 50
	}

	fullDir, err := s.resolve(dir)
	if err != nil {
		return nil, err
	}

	var allFiles []FileInfo

	if opts.Recursive {
		err = filepath.Walk(fullDir, func(p string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, _ := filepath.Rel(s.BasePath, p)
			rel = filepath.ToSlash(rel)
			if rel == "." {
				return nil
			}
			if opts.Search != "" && !strings.Contains(strings.ToLower(info.Name()), strings.ToLower(opts.Search)) {
				if info.IsDir() {
					return nil // 继续深入搜索子目录
				}
				return nil
			}
			contentType := ""
			if !info.IsDir() {
				contentType = mime.TypeByExtension(filepath.Ext(info.Name()))
			}
			allFiles = append(allFiles, FileInfo{
				Name:        info.Name(),
				Path:        rel,
				IsDir:       info.IsDir(),
				Size:        info.Size(),
				ModTime:     info.ModTime(),
				ContentType: contentType,
			})
			return nil
		})
	} else {
		entries, readErr := os.ReadDir(fullDir)
		if readErr != nil {
			return nil, readErr
		}
		for _, entry := range entries {
			if opts.Search != "" && !strings.Contains(strings.ToLower(entry.Name()), strings.ToLower(opts.Search)) {
				continue
			}
			info, _ := entry.Info()
			if info == nil {
				continue
			}
			rel := filepath.ToSlash(filepath.Join(dir, entry.Name()))
			contentType := ""
			if !entry.IsDir() {
				contentType = mime.TypeByExtension(filepath.Ext(entry.Name()))
			}
			allFiles = append(allFiles, FileInfo{
				Name:        entry.Name(),
				Path:        rel,
				IsDir:       entry.IsDir(),
				Size:        info.Size(),
				ModTime:     info.ModTime(),
				ContentType: contentType,
			})
		}
	}
	if err != nil {
		return nil, err
	}

	// 排序：目录在前，然后按名称字母排序
	sort.Slice(allFiles, func(i, j int) bool {
		if allFiles[i].IsDir != allFiles[j].IsDir {
			return allFiles[i].IsDir
		}
		return strings.ToLower(allFiles[i].Name) < strings.ToLower(allFiles[j].Name)
	})

	total := int64(len(allFiles))
	start := (opts.Page - 1) * opts.PageSize
	end := start + opts.PageSize
	if start > len(allFiles) {
		start = len(allFiles)
	}
	if end > len(allFiles) {
		end = len(allFiles)
	}

	return &ListResult{
		Files:     allFiles[start:end],
		Total:     total,
		TotalMode: TotalExact,
		Page:      opts.Page,
		PageSize:  opts.PageSize,
	}, nil
}

// Tree 获取目录树，depth 控制递归深度
func (s *LocalFileStorage) Tree(ctx context.Context, dir string, depth int) ([]*TreeNode, error) {
	fullDir, err := s.resolve(dir)
	if err != nil {
		return nil, err
	}
	if depth == 0 {
		depth = 5
	}
	if depth == -1 {
		return nil, fmt.Errorf("storage: depth=-1 is forbidden")
	}
	return s.buildTree(fullDir, dir, depth)
}

func (s *LocalFileStorage) buildTree(fullPath, relPath string, depth int) ([]*TreeNode, error) {
	if depth == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return nil, err
	}

	var nodes []*TreeNode
	for _, entry := range entries {
		childRel := filepath.ToSlash(filepath.Join(relPath, entry.Name()))
		node := &TreeNode{
			Name:  entry.Name(),
			Path:  childRel,
			IsDir: entry.IsDir(),
		}
		if entry.IsDir() {
			info, _ := entry.Info()
			if info != nil {
				node.Size = info.Size()
			}
			children, err := s.buildTree(filepath.Join(fullPath, entry.Name()), childRel, depth-1)
			if err == nil {
				node.Children = children
			}
		} else {
			info, _ := entry.Info()
			if info != nil {
				node.Size = info.Size()
			}
		}
		nodes = append(nodes, node)
	}

	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	return nodes, nil
}

// Rename 重命名/移动文件
func (s *LocalFileStorage) Rename(ctx context.Context, oldPath, newPath string) error {
	oldFull, err := s.resolve(oldPath)
	if err != nil {
		return err
	}
	newFull, err := s.resolve(newPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(newFull), 0755); err != nil {
		return fmt.Errorf("storage: mkdir for rename: %w", err)
	}
	return os.Rename(oldFull, newFull)
}

// DeleteBatch 批量删除文件
func (s *LocalFileStorage) DeleteBatch(ctx context.Context, paths []string) error {
	for _, p := range paths {
		if err := s.Delete(ctx, p); err != nil {
			return fmt.Errorf("storage: delete %s: %w", p, err)
		}
	}
	return nil
}

// ─── ctxReader: 上下文感知的 io.Reader ──────────────────────────────

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (cr *ctxReader) Read(p []byte) (int, error) {
	select {
	case <-cr.ctx.Done():
		return 0, cr.ctx.Err()
	default:
	}
	return cr.r.Read(p)
}
