package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLocalStorage(t *testing.T) *LocalFileStorage {
	t.Helper()
	dir := t.TempDir()
	ls, err := NewLocalFileStorage(dir)
	require.NoError(t, err)
	return ls
}

func TestLocalFileStorage_SaveAndRead(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	content := "hello, dbridge storage"
	info, err := ls.Save(ctx, "test.txt", strings.NewReader(content), "text/plain")
	require.NoError(t, err)
	assert.Equal(t, "test.txt", info.Name)
	assert.Equal(t, "test.txt", info.Path)
	assert.False(t, info.IsDir)
	assert.Equal(t, int64(len(content)), info.Size)
	assert.Equal(t, "text/plain", info.ContentType)

	data, err := ls.Read(ctx, "test.txt")
	require.NoError(t, err)
	assert.Equal(t, content, string(data))
}

func TestLocalFileStorage_Exists(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	exists, err := ls.Exists(ctx, "no-such-file.txt")
	require.NoError(t, err)
	assert.False(t, exists)

	_, err = ls.Save(ctx, "hello.txt", strings.NewReader("hi"), "")
	require.NoError(t, err)

	exists, err = ls.Exists(ctx, "hello.txt")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLocalFileStorage_Delete(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, err := ls.Save(ctx, "delete-me.txt", strings.NewReader("data"), "")
	require.NoError(t, err)

	err = ls.Delete(ctx, "delete-me.txt")
	require.NoError(t, err)

	exists, _ := ls.Exists(ctx, "delete-me.txt")
	assert.False(t, exists)
}

func TestLocalFileStorage_Delete_DirectoryReturnsError(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	err := ls.Mkdir(ctx, "subdir")
	require.NoError(t, err)

	err = ls.Delete(ctx, "subdir")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "use RemoveDir")
}

func TestLocalFileStorage_RemoveDir(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	err := ls.Mkdir(ctx, "empty-dir")
	require.NoError(t, err)

	err = ls.RemoveDir(ctx, "empty-dir")
	require.NoError(t, err)

	exists, _ := ls.Exists(ctx, "empty-dir")
	assert.False(t, exists)
}

func TestLocalFileStorage_Mkdir(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	err := ls.Mkdir(ctx, "a/b/c")
	require.NoError(t, err)

	exists, _ := ls.Exists(ctx, "a/b/c")
	assert.True(t, exists) // directory exists as a path

	// Verify parent dirs too
	info, err := os.Stat(filepath.Join(ls.BasePath, "a", "b", "c"))
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLocalFileStorage_List(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, _ = ls.Save(ctx, "a.txt", strings.NewReader("a"), "")
	_, _ = ls.Save(ctx, "b.txt", strings.NewReader("b"), "")
	_ = ls.Mkdir(ctx, "sub")

	result, err := ls.List(ctx, "", &ListOptions{Page: 1, PageSize: 10})
	require.NoError(t, err)
	assert.Equal(t, TotalExact, result.TotalMode)
	assert.Equal(t, int64(3), result.Total)
	assert.Len(t, result.Files, 3)

	// sub 应该排在文件前
	assert.True(t, result.Files[0].IsDir)
	assert.Equal(t, "sub", result.Files[0].Name)
}

func TestLocalFileStorage_List_Search(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, _ = ls.Save(ctx, "apple.txt", strings.NewReader("a"), "")
	_, _ = ls.Save(ctx, "banana.txt", strings.NewReader("b"), "")
	_, _ = ls.Save(ctx, "cherry.txt", strings.NewReader("c"), "")

	result, err := ls.List(ctx, "", &ListOptions{Page: 1, PageSize: 10, Search: "ban"})
	require.NoError(t, err)
	assert.Len(t, result.Files, 1)
	assert.Equal(t, "banana.txt", result.Files[0].Name)
}

func TestLocalFileStorage_Tree(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_ = ls.Mkdir(ctx, "parent")
	_, _ = ls.Save(ctx, "parent/child.txt", strings.NewReader("child"), "")

	nodes, err := ls.Tree(ctx, "", 10)
	require.NoError(t, err)

	// 应该有一个目录节点 "parent"
	var parent *TreeNode
	for _, n := range nodes {
		if n.Name == "parent" {
			parent = n
			break
		}
	}
	require.NotNil(t, parent)
	assert.True(t, parent.IsDir)
	require.Len(t, parent.Children, 1)
	assert.Equal(t, "child.txt", parent.Children[0].Name)
}

func TestLocalFileStorage_Tree_DepthForbidden(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, err := ls.Tree(ctx, "", -1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "forbidden")
}

func TestLocalFileStorage_Rename(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, _ = ls.Save(ctx, "old.txt", strings.NewReader("old"), "")

	err := ls.Rename(ctx, "old.txt", "new.txt")
	require.NoError(t, err)

	exists, _ := ls.Exists(ctx, "old.txt")
	assert.False(t, exists)

	exists, _ = ls.Exists(ctx, "new.txt")
	assert.True(t, exists)

	data, _ := ls.Read(ctx, "new.txt")
	assert.Equal(t, "old", string(data))
}

func TestLocalFileStorage_PathTraversal(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	// 测试 .. 绕过
	_, err := ls.Save(ctx, "../escape.txt", strings.NewReader("x"), "")
	assert.Error(t, err)

	// 测试绝对路径
	_, err = ls.Save(ctx, "/etc/passwd", strings.NewReader("x"), "")
	assert.Error(t, err)

	// 测试中间 .. 
	_, err = ls.Save(ctx, "subdir/../../escape.txt", strings.NewReader("x"), "")
	assert.Error(t, err)
}

func TestLocalFileStorage_DeleteBatch(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, _ = ls.Save(ctx, "1.txt", strings.NewReader("1"), "")
	_, _ = ls.Save(ctx, "2.txt", strings.NewReader("2"), "")
	_, _ = ls.Save(ctx, "3.txt", strings.NewReader("3"), "")

	err := ls.DeleteBatch(ctx, []string{"1.txt", "2.txt"})
	require.NoError(t, err)

	exists, _ := ls.Exists(ctx, "1.txt")
	assert.False(t, exists)
	exists, _ = ls.Exists(ctx, "2.txt")
	assert.False(t, exists)
	exists, _ = ls.Exists(ctx, "3.txt")
	assert.True(t, exists)
}

func TestLocalFileStorage_Stat(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	_, err := ls.Save(ctx, "stat.txt", strings.NewReader("hello world"), "text/plain")
	require.NoError(t, err)

	info, err := ls.Stat(ctx, "stat.txt")
	require.NoError(t, err)
	assert.Equal(t, "stat.txt", info.Name)
	assert.Equal(t, int64(11), info.Size)
	assert.False(t, info.IsDir)
}

func TestLocalFileStorage_HealthCheck(t *testing.T) {
	ls := newTestLocalStorage(t)
	ctx := context.Background()

	err := ls.HealthCheck(ctx)
	assert.NoError(t, err)
}

func TestCtxReader_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cr := &ctxReader{ctx: ctx, r: strings.NewReader("data")}
	buf := make([]byte, 10)
	_, err := cr.Read(buf)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}
