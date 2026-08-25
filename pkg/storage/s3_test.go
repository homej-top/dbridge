package storage

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// s3TestConfig returns S3 config if MinIO is available via env vars
func s3TestConfig(t *testing.T) (S3Config, bool) {
	t.Helper()

	// 检查环境变量
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		endpoint = "http://localhost:9000"
	}
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		bucket = "dbridge-test"
	}
	accessKey := os.Getenv("S3_TEST_ACCESS_KEY")
	if accessKey == "" {
		accessKey = "minioadmin"
	}
	secretKey := os.Getenv("S3_TEST_SECRET_KEY")
	if secretKey == "" {
		secretKey = "minioadmin"
	}

	cfg := S3Config{
		Endpoint:        endpoint,
		Region:          "us-east-1",
		Bucket:          bucket,
		AccessKeyID:     accessKey,
		SecretAccessKey: secretKey,
		UsePathStyle:    true,
		DisableSSL:      true,
		Prefix:          "test/" + strings.ToLower(t.Name()) + "/",
		MaxRemoveObjs:   100,
	}

	// 尝试连接
	s3fs, err := NewS3FileStorage(cfg)
	if err != nil {
		t.Skipf("S3/MinIO not available: %v", err)
		return cfg, false
	}
	s3fs.Close()
	return cfg, true
}

func TestS3FileStorage_SaveAndRead(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()
	content := "hello from s3 test"
	info, err := fs.Save(ctx, "test-save-read.txt", strings.NewReader(content), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "test-save-read.txt", info.Name)
	assert.Greater(t, info.Size, int64(0))

	data, err := fs.Read(ctx, "test-save-read.txt")
	require.NoError(t, err)
	assert.Equal(t, content, string(data))

	// 清理
	_ = fs.Delete(ctx, "test-save-read.txt")
}

func TestS3FileStorage_Exists(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	exists, err := fs.Exists(ctx, "nonexistent-file-12345.txt")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = fs.Save(ctx, "exists-test.txt", strings.NewReader("data"), "")
	require.NoError(t, err)

	exists, err = fs.Exists(ctx, "exists-test.txt")
	require.NoError(t, err)
	assert.True(t, exists)

	_ = fs.Delete(ctx, "exists-test.txt")
}

func TestS3FileStorage_List(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	// 上传测试文件
	_, _ = fs.Save(ctx, "list/a.txt", strings.NewReader("a"), "text/plain")
	_, _ = fs.Save(ctx, "list/b.txt", strings.NewReader("b"), "text/plain")
	defer func() {
		_ = fs.Delete(ctx, "list/a.txt")
		_ = fs.Delete(ctx, "list/b.txt")
	}()

	// 给 S3 一点时间确保索引一致
	time.Sleep(100 * time.Millisecond)

	result, err := fs.List(ctx, "list", &ListOptions{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Files), 1)

	// S3 后端应是游标分页模式
	if result.NextToken == "" && result.Total >= 0 {
		assert.Equal(t, TotalExact, result.TotalMode)
	} else {
		assert.Equal(t, TotalUnknown, result.TotalMode)
	}
}

func TestS3FileStorage_MkdirAndRemoveDir(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	err = fs.Mkdir(ctx, "rmdir-test/")
	require.NoError(t, err)

	// 上传一个文件到目录
	_, err = fs.Save(ctx, "rmdir-test/file.txt", strings.NewReader("data"), "")
	require.NoError(t, err)

	// 删除目录
	err = fs.RemoveDir(ctx, "rmdir-test/")
	require.NoError(t, err)

	// 文件应不存在
	exists, _ := fs.Exists(ctx, "rmdir-test/file.txt")
	assert.False(t, exists)
}

func TestS3FileStorage_Stat(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	_, err = fs.Save(ctx, "stat-test.txt", strings.NewReader("hello world"), "text/plain")
	require.NoError(t, err)
	defer fs.Delete(ctx, "stat-test.txt")

	info, err := fs.Stat(ctx, "stat-test.txt")
	require.NoError(t, err)
	assert.Equal(t, int64(11), info.Size)
	assert.False(t, info.IsDir)
}

func TestS3FileStorage_Rename(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	_, err = fs.Save(ctx, "rename-old.txt", strings.NewReader("rename-me"), "")
	require.NoError(t, err)

	err = fs.Rename(ctx, "rename-old.txt", "rename-new.txt")
	require.NoError(t, err)

	// 旧路径不存在
	exists, _ := fs.Exists(ctx, "rename-old.txt")
	assert.False(t, exists)

	// 新路径存在
	data, err := fs.Read(ctx, "rename-new.txt")
	require.NoError(t, err)
	assert.Equal(t, "rename-me", string(data))

	_ = fs.Delete(ctx, "rename-new.txt")
}

func TestS3FileStorage_Tree_DepthLimit(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	// depth > 3 应报错
	_, err = fs.Tree(ctx, "", 10)
	assert.Error(t, err)
	var treeErr *ErrTreeDepthExceeded
	assert.ErrorAs(t, err, &treeErr)

	// depth = -1 应报错
	_, err = fs.Tree(ctx, "", -1)
	assert.Error(t, err)
}

func TestS3FileStorage_DeleteBatch(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	_, _ = fs.Save(ctx, "batch/1.txt", strings.NewReader("1"), "")
	_, _ = fs.Save(ctx, "batch/2.txt", strings.NewReader("2"), "")

	err = fs.DeleteBatch(ctx, []string{"batch/1.txt", "batch/2.txt"})
	require.NoError(t, err)

	exists1, _ := fs.Exists(ctx, "batch/1.txt")
	exists2, _ := fs.Exists(ctx, "batch/2.txt")
	assert.False(t, exists1)
	assert.False(t, exists2)
}

func TestS3FileStorage_HealthCheck(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err = fs.HealthCheck(ctx)
	assert.NoError(t, err)
}

func TestS3FileStorage_Vendor(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	vendor := fs.Vendor()
	assert.Contains(t, []string{"aws", "minio", "oss"}, vendor)
}

func TestS3FileStorage_RemoveDir_ThresholdProtection(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	cfg.MaxRemoveObjs = 1 // 极低阈值用于测试

	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx := context.Background()

	// 创建目录 + 多个文件
	_ = fs.Mkdir(ctx, "threshold-test/")
	_, _ = fs.Save(ctx, "threshold-test/a.txt", strings.NewReader("a"), "")
	_, _ = fs.Save(ctx, "threshold-test/b.txt", strings.NewReader("b"), "")

	// 应该因超过阈值而失败
	err = fs.RemoveDir(ctx, "threshold-test/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds max objects")

	// 清理
	cfg.MaxRemoveObjs = 100
	_ = fs.RemoveDir(ctx, "threshold-test/")
}

func TestS3FileStorage_AbortOldMultipartUploads(t *testing.T) {
	cfg, ok := s3TestConfig(t)
	if !ok {
		return
	}
	fs, err := NewS3FileStorage(cfg)
	require.NoError(t, err)
	defer fs.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 清理超过 1 天的未完成分片（应该为 0，因为测试文件不会挂这么久）
	aborted, err := fs.AbortOldMultipartUploads(ctx, 24*time.Hour)
	require.NoError(t, err)
	// 测试环境下应该是干净的
	assert.GreaterOrEqual(t, aborted, 0)
	t.Logf("aborted %d old multipart uploads", aborted)
}
