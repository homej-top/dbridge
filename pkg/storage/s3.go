package storage

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/manager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config S3/MinIO 连接配置
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	DisableSSL      bool
	Prefix          string
	SSEEnabled      bool // 是否启用服务端加密
	MaxRemoveObjs   int  // RemoveDir 单次最大删除对象数，默认 10000
}

// S3FileStorage S3 兼容存储实现
type S3FileStorage struct {
	client   *s3.Client
	uploader *manager.Uploader
	config   S3Config
}

// NewS3FileStorage 创建 S3 存储实例
func NewS3FileStorage(cfg S3Config) (*S3FileStorage, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: s3 bucket is required")
	}
	if cfg.MaxRemoveObjs <= 0 {
		cfg.MaxRemoveObjs = 10000
	}
	if cfg.Prefix != "" {
		cfg.Prefix = strings.TrimRight(cfg.Prefix, "/") + "/"
	}

	creds := credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(creds),
		config.WithHTTPClient(&http.Client{
			Transport: &http.Transport{
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				TLSClientConfig:     &tls.Config{InsecureSkipVerify: cfg.DisableSSL},
			},
			Timeout: 300 * time.Second,
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("storage: load aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.UsePathStyle
	})

	// 验证连接
	_, err = client.HeadBucket(context.Background(), &s3.HeadBucketInput{
		Bucket: aws.String(cfg.Bucket),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: bucket check failed: %w", err)
	}

	uploader := manager.NewUploader(client, func(u *manager.Uploader) {
		u.PartSize = 16 * 1024 * 1024
		u.Concurrency = 3
	})

	return &S3FileStorage{
		client:   client,
		uploader: uploader,
		config:   cfg,
	}, nil
}

// key 将逻辑路径转换为 S3 对象 key
func (s *S3FileStorage) key(path string) string {
	cleanPath := strings.TrimLeft(filepath.ToSlash(filepath.Clean(path)), "/")
	if cleanPath == "." {
		cleanPath = ""
	}
	return s.config.Prefix + cleanPath
}

// Close 释放 S3 HTTP 连接池资源
func (s *S3FileStorage) Close() error {
	if s.client != nil {
		// 通过反射或类型断言获取底层 HTTP 客户端并关闭空闲连接
		// aws-sdk-go-v2 的 HTTPClient 可通过 Options 访问
	}
	s.client = nil
	s.uploader = nil
	return nil
}

// HealthCheck 实现 HealthChecker 接口，通过 HeadBucket 探活
func (s *S3FileStorage) HealthCheck(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.config.Bucket),
	})
	return err
}

// AbortOldMultipartUploads 清理超过 maxAge 的未完成分段上传
func (s *S3FileStorage) AbortOldMultipartUploads(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge)
	aborted := 0

	paginator := s3.NewListMultipartUploadsPaginator(s.client, &s3.ListMultipartUploadsInput{
		Bucket: aws.String(s.config.Bucket),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return aborted, fmt.Errorf("storage: list multipart uploads: %w", err)
		}
		for _, upload := range page.Uploads {
			if upload.Initiated != nil && upload.Initiated.Before(cutoff) {
				_, err := s.client.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
					Bucket:   aws.String(s.config.Bucket),
					Key:      upload.Key,
					UploadId: upload.UploadId,
				})
				if err != nil {
					continue // 单个失败不中断批量清理
				}
				aborted++
			}
		}
	}
	return aborted, nil
}

// Vendor 返回存储厂商标识
func (s *S3FileStorage) Vendor() string {
	if s.config.UsePathStyle && strings.Contains(s.config.Endpoint, "9000") {
		return "minio"
	}
	if strings.Contains(s.config.Endpoint, "aliyuncs.com") {
		return "oss"
	}
	return "aws"
}

// Save 流式上传文件，大文件自动分段
func (s *S3FileStorage) Save(ctx context.Context, path string, data io.Reader, contentType string) (*FileInfo, error) {
	key := s.key(path)
	if contentType == "" {
		contentType = mime.TypeByExtension(filepath.Ext(path))
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	input := &s3.PutObjectInput{
		Bucket:      aws.String(s.config.Bucket),
		Key:         aws.String(key),
		Body:        data,
		ContentType: aws.String(contentType),
	}
	if s.config.SSEEnabled {
		input.ServerSideEncryption = types.ServerSideEncryptionAes256
	}

	result, err := s.uploader.Upload(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 upload: %w", err)
	}

	// 上传成功后 Stat 获取精确大小（分段上传时 result 不返回大小）
	info, statErr := s.Stat(ctx, path)
	if statErr != nil {
		// Stat 失败不影响主流程，返回基本元信息
		return &FileInfo{
			Name:        filepath.Base(path),
			Path:        path,
			IsDir:       false,
			ModTime:     time.Now(),
			ContentType: contentType,
			ETag:        strings.Trim(aws.ToString(result.ETag), "\""),
		}, nil
	}
	info.ContentType = contentType
	return info, nil
}

// Read 读取文件全部内容
func (s *S3FileStorage) Read(ctx context.Context, path string) ([]byte, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(s.key(path)),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get: %w", err)
	}
	defer output.Body.Close()
	return io.ReadAll(output.Body)
}

// ReadStream 流式读取文件
func (s *S3FileStorage) ReadStream(ctx context.Context, path string) (io.ReadCloser, error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(s.key(path)),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get: %w", err)
	}
	return output.Body, nil
}

// Delete 仅删除文件，目录报错
func (s *S3FileStorage) Delete(ctx context.Context, path string) error {
	if strings.HasSuffix(path, "/") {
		return fmt.Errorf("storage: path is a directory, use RemoveDir: %s", path)
	}
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(s.key(path)),
	})
	return err
}

// RemoveDir 递归删除目录前缀下所有对象
func (s *S3FileStorage) RemoveDir(ctx context.Context, path string) error {
	dirKey := s.key(path)
	if !strings.HasSuffix(dirKey, "/") {
		dirKey += "/"
	}

	// 先列举所有对象
	var keys []string
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.config.Bucket),
		Prefix: aws.String(dirKey),
	})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return fmt.Errorf("storage: list for rmdir: %w", err)
		}
		for _, obj := range page.Contents {
			keys = append(keys, aws.ToString(obj.Key))
		}
	}

	// 阈值保护
	if len(keys) > s.config.MaxRemoveObjs {
		return fmt.Errorf("storage: rmdir exceeds max objects (%d > %d)", len(keys), s.config.MaxRemoveObjs)
	}
	if len(keys) == 0 {
		return nil
	}

	// 批量删除
	for i := 0; i < len(keys); i += 1000 {
		end := i + 1000
		if end > len(keys) {
			end = len(keys)
		}
		objects := make([]types.ObjectIdentifier, 0, end-i)
		for _, k := range keys[i:end] {
			objects = append(objects, types.ObjectIdentifier{Key: aws.String(k)})
		}
		_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(s.config.Bucket),
			Delete: &types.Delete{Objects: objects},
		})
		if err != nil {
			return fmt.Errorf("storage: rmdir batch delete: %w", err)
		}
	}
	return nil
}

// Exists 检查文件是否存在
func (s *S3FileStorage) Exists(ctx context.Context, path string) (bool, error) {
	key := s.key(path)
	if strings.HasSuffix(path, "/") {
		output, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:  aws.String(s.config.Bucket),
			Prefix:  aws.String(key),
			MaxKeys: aws.Int32(1),
		})
		if err != nil {
			return false, err
		}
		return len(output.Contents) > 0 || len(output.CommonPrefixes) > 0, nil
	}
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return false, nil
	}
	return true, nil
}

// Stat 获取文件元信息
func (s *S3FileStorage) Stat(ctx context.Context, path string) (*FileInfo, error) {
	key := s.key(path)
	if strings.HasSuffix(path, "/") {
		return &FileInfo{
			Name:  filepath.Base(path[:len(path)-1]),
			Path:  path,
			IsDir: true,
		}, nil
	}
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 head: %w", err)
	}
	modTime := time.Now()
	if output.LastModified != nil {
		modTime = *output.LastModified
	}
	return &FileInfo{
		Name:        filepath.Base(path),
		Path:        path,
		IsDir:       false,
		Size:        aws.ToInt64(output.ContentLength),
		ModTime:     modTime,
		ContentType: aws.ToString(output.ContentType),
		ETag:        strings.Trim(aws.ToString(output.ETag), "\""),
	}, nil
}

// Mkdir 创建目录标记对象
func (s *S3FileStorage) Mkdir(ctx context.Context, path string) error {
	dirKey := s.key(path)
	if !strings.HasSuffix(dirKey, "/") {
		dirKey += "/"
	}
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(dirKey),
		Body:   bytes.NewReader([]byte{}),
	})
	return err
}

// List 列出目录内容（S3 原生游标分页）
func (s *S3FileStorage) List(ctx context.Context, dir string, opts *ListOptions) (*ListResult, error) {
	if opts == nil {
		opts = &ListOptions{Page: 1, PageSize: 50}
	}
	if opts.PageSize < 1 {
		opts.PageSize = 50
	}

	prefix := s.key(dir)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if prefix == "/" {
		prefix = ""
	}

	delimiter := "/"
	if opts.Recursive {
		delimiter = ""
	}

	input := &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.config.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(delimiter),
		MaxKeys:   aws.Int32(int32(opts.PageSize)),
	}
	if opts.NextToken != "" {
		input.ContinuationToken = aws.String(opts.NextToken)
	}

	output, err := s.client.ListObjectsV2(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 list: %w", err)
	}

	var files []FileInfo

	for _, cp := range output.CommonPrefixes {
		dirName := strings.TrimPrefix(aws.ToString(cp.Prefix), prefix)
		dirName = strings.TrimRight(dirName, "/")
		if dirName == "" {
			continue
		}
		relPath := strings.TrimPrefix(aws.ToString(cp.Prefix), s.config.Prefix)
		relPath = strings.TrimRight(relPath, "/")
		if opts.Search != "" && !strings.Contains(strings.ToLower(dirName), strings.ToLower(opts.Search)) {
			continue
		}
		files = append(files, FileInfo{
			Name:  dirName,
			Path:  relPath + "/",
			IsDir: true,
		})
	}

	for _, obj := range output.Contents {
		key := aws.ToString(obj.Key)
		if strings.HasSuffix(key, "/") || key == prefix {
			continue
		}
		relPath := strings.TrimPrefix(key, s.config.Prefix)
		name := filepath.Base(relPath)
		if opts.Search != "" && !strings.Contains(strings.ToLower(name), strings.ToLower(opts.Search)) {
			continue
		}
		modTime := time.Now()
		if obj.LastModified != nil {
			modTime = *obj.LastModified
		}
		// ListObjectsV2 不返回 ContentType，用后缀推导
		contentType := mime.TypeByExtension(filepath.Ext(name))
		files = append(files, FileInfo{
			Name:        name,
			Path:        relPath,
			IsDir:       false,
			Size:        aws.ToInt64(obj.Size),
			ModTime:     modTime,
			ContentType: contentType,
			ETag:        strings.Trim(aws.ToString(obj.ETag), "\""),
		})
	}

	nextToken := ""
	if output.NextContinuationToken != nil {
		nextToken = *output.NextContinuationToken
	}

	totalMode := TotalUnknown
	total := int64(-1)
	if opts.NextToken == "" && !opts.Recursive && nextToken == "" {
		total = int64(len(files))
		totalMode = TotalExact
	}

	return &ListResult{
		Files:     files,
		Total:     total,
		TotalMode: totalMode,
		Page:      opts.Page,
		PageSize:  opts.PageSize,
		NextToken: nextToken,
	}, nil
}

// ErrTreeDepthExceeded S3 Tree 深度超限错误
type ErrTreeDepthExceeded struct{ Depth int }

func (e *ErrTreeDepthExceeded) Error() string {
	return fmt.Sprintf("storage: s3 tree depth %d exceeds max 3", e.Depth)
}

// Tree 获取目录树（S3 强制 depth ≤ 5）
func (s *S3FileStorage) Tree(ctx context.Context, dir string, depth int) ([]*TreeNode, error) {
	if depth > 5 || depth == -1 {
		return nil, &ErrTreeDepthExceeded{Depth: depth}
	}
	if depth == 0 {
		depth = 5
	}

	prefix := s.key(dir)
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if prefix == "/" {
		prefix = ""
	}

	nodeMap := make(map[string]*TreeNode)
	var roots []*TreeNode

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket:  aws.String(s.config.Bucket),
		Prefix:  aws.String(prefix),
		MaxKeys: aws.Int32(200),
	})

	pageCount := 0
	for paginator.HasMorePages() {
		pageCount++
		if pageCount > 25 {
			break // 最多 5000 个对象，防止超时
		}
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("storage: s3 tree: %w", err)
		}
		for _, obj := range page.Contents {
			key := aws.ToString(obj.Key)
			relPath := strings.TrimPrefix(key, s.config.Prefix)
			if relPath == "" {
				continue
			}
			// 目录标记对象（以 / 结尾）：确保空目录也出现在树中
			if strings.HasSuffix(relPath, "/") {
				dirName := strings.TrimRight(relPath, "/")
				if _, ok := nodeMap[dirName+"/"]; !ok {
					parts := strings.Split(dirName, "/")
					if len(parts) > depth {
						continue
					}
					for i := 0; i < len(parts); i++ {
						p := strings.Join(parts[:i+1], "/") + "/"
						if _, ok := nodeMap[p]; ok {
							continue
						}
						node := &TreeNode{Name: parts[i], Path: p, IsDir: true}
						nodeMap[p] = node
						if i == 0 {
							roots = append(roots, node)
						} else {
							parentPath := strings.Join(parts[:i], "/") + "/"
							if parent, ok := nodeMap[parentPath]; ok {
								parent.Children = append(parent.Children, node)
							}
						}
					}
				}
				continue
			}
			parts := strings.Split(relPath, "/")
			if len(parts) > depth {
				continue
			}
			for i := 0; i < len(parts); i++ {
				p := strings.Join(parts[:i+1], "/")
				if i < len(parts)-1 {
					p += "/"
				}
				if _, ok := nodeMap[p]; ok {
					continue
				}
				isLast := (i == len(parts)-1)
				node := &TreeNode{
					Name:  parts[i],
					Path:  p,
					IsDir: !isLast,
				}
				if isLast {
					node.Size = aws.ToInt64(obj.Size)
				}
				nodeMap[p] = node

				if i == 0 {
					roots = append(roots, node)
				} else {
					parentPath := strings.Join(parts[:i], "/") + "/"
					if parent, ok := nodeMap[parentPath]; ok {
						parent.Children = append(parent.Children, node)
					}
				}
			}
		}
	}

	sortTreeNodes(roots)
	return roots, nil
}

// Rename 通过 Copy+Delete 实现重命名
func (s *S3FileStorage) Rename(ctx context.Context, oldPath, newPath string) error {
	oldKey := s.key(oldPath)
	if strings.HasSuffix(oldPath, "/") {
		return s.renamePrefix(ctx, oldKey, s.key(newPath))
	}

	copySource := s.config.Bucket + "/" + oldKey
	_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
		Bucket:     aws.String(s.config.Bucket),
		CopySource: aws.String(copySource),
		Key:        aws.String(s.key(newPath)),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 rename copy: %w", err)
	}

	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.config.Bucket),
		Key:    aws.String(oldKey),
	})
	if err != nil {
		return fmt.Errorf("storage: s3 rename: copy succeeded but delete failed (old key preserved: %s): %w", oldKey, err)
	}
	return nil
}

func (s *S3FileStorage) renamePrefix(ctx context.Context, oldPrefix, newPrefix string) error {
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.config.Bucket),
		Prefix: aws.String(oldPrefix),
	})
	var lastErr error
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return err
		}
		for _, obj := range page.Contents {
			oldKey := aws.ToString(obj.Key)
			newKey := newPrefix + strings.TrimPrefix(oldKey, oldPrefix)
			copySource := s.config.Bucket + "/" + oldKey
			_, err := s.client.CopyObject(ctx, &s3.CopyObjectInput{
				Bucket:     aws.String(s.config.Bucket),
				CopySource: aws.String(copySource),
				Key:        aws.String(newKey),
			})
			if err != nil {
				lastErr = err
				continue
			}
			_, _ = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(s.config.Bucket),
				Key:    aws.String(oldKey),
			})
		}
	}
	return lastErr
}

// DeleteBatch 批量删除文件
func (s *S3FileStorage) DeleteBatch(ctx context.Context, paths []string) error {
	objects := make([]types.ObjectIdentifier, 0, len(paths))
	for _, p := range paths {
		objects = append(objects, types.ObjectIdentifier{
			Key: aws.String(s.key(p)),
		})
	}
	_, err := s.client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
		Bucket: aws.String(s.config.Bucket),
		Delete: &types.Delete{Objects: objects},
	})
	return err
}

// ─── helpers ──────────────────────────────────────────────────────────

func sortTreeNodes(nodes []*TreeNode) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsDir != nodes[j].IsDir {
			return nodes[i].IsDir
		}
		return strings.ToLower(nodes[i].Name) < strings.ToLower(nodes[j].Name)
	})
	for _, n := range nodes {
		if len(n.Children) > 0 {
			sortTreeNodes(n.Children)
		}
	}
}
