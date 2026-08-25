// Package storage provides a file storage abstraction layer.
// Supports local file system, S3/MinIO/OSS via aws-sdk-go-v2,
// and multiple named profiles with runtime default switching.
package storage

import (
	"context"
	"io"
	"time"
)

// FileInfo 文件/目录元信息
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	ModTime     time.Time `json:"mod_time"`
	ContentType string    `json:"content_type"`
	ETag        string    `json:"etag,omitempty"`
}

// TotalMode 总数统计模式
type TotalMode string

const (
	TotalExact   TotalMode = "exact"
	TotalUnknown TotalMode = "unknown"
)

// ListOptions 列表/搜索选项
type ListOptions struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Search    string `json:"search"`
	Recursive bool   `json:"recursive"`
	NextToken string `json:"next_token"`
}

// ListResult 列表返回结果
type ListResult struct {
	Files     []FileInfo `json:"files"`
	Total     int64      `json:"total"`
	TotalMode TotalMode  `json:"total_mode"`
	Page      int        `json:"page"`
	PageSize  int        `json:"page_size"`
	NextToken string     `json:"next_token,omitempty"`
}

// TreeNode 目录树节点
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Size     int64       `json:"size,omitempty"`
	Children []*TreeNode `json:"children,omitempty"`
}

// FileStorage 统一文件存储接口
type FileStorage interface {
	Save(ctx context.Context, path string, data io.Reader, contentType string) (*FileInfo, error)
	Read(ctx context.Context, path string) ([]byte, error)
	ReadStream(ctx context.Context, path string) (io.ReadCloser, error)
	Delete(ctx context.Context, path string) error
	RemoveDir(ctx context.Context, path string) error
	Exists(ctx context.Context, path string) (bool, error)
	Stat(ctx context.Context, path string) (*FileInfo, error)
	Mkdir(ctx context.Context, path string) error
	List(ctx context.Context, dir string, opts *ListOptions) (*ListResult, error)
	Tree(ctx context.Context, dir string, depth int) ([]*TreeNode, error)
	Rename(ctx context.Context, oldPath, newPath string) error
	DeleteBatch(ctx context.Context, paths []string) error
}

// ProfileInfo Profile 的简要信息
type ProfileInfo struct {
	Name      string            `json:"name"`
	Code      string            `json:"code"`
	Backend   string            `json:"backend"`
	Enabled   bool              `json:"enabled"`
	IsDefault bool              `json:"is_default"`
	Summary   map[string]string `json:"summary,omitempty"`
}
