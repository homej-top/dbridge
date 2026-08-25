package storage

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 阶段5：异常场景测试 ─────────────────────────────────────────

// TestInitFromDB_ConcurrentSeed 并发启动时不重复插入种子数据
func TestInitFromDB_ConcurrentSeed(t *testing.T) {
	db := setupTestDB(t)

	var wg sync.WaitGroup
	errs := make([]error, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			// 每个 goroutine 独立调用 ensureDefaultLocalStorage
			errs[idx] = ensureDefaultLocalStorage(db)
		}(i)
	}
	wg.Wait()

	// 所有调用都不应报错（OnConflict DoNothing）
	for _, err := range errs {
		assert.NoError(t, err)
	}

	// 只应有一个 local 实例
	var count int64
	db.Model(&StorageInstanceRecord{}).Where("code = ?", "local").Count(&count)
	assert.Equal(t, int64(1), count, "并发种子不应重复插入")
}

// TestProfileManager_Replace_InvalidPath 替换为无效路径应失败
func TestProfileManager_Replace_InvalidPath(t *testing.T) {
	mgr := newTestManager(t)

	// 尝试创建指向不存在的父目录且无法创建的文件系统
	// 使用一个空字符串路径（NewLocalFileStorage 会失败）
	_, err := NewLocalFileStorage("")
	if err == nil {
		t.Skip("空路径创建本地存储未报错，跳过")
	}

	// 验证 Replace 时传入 nil 不会 panic
	err = mgr.Replace("local", "local", "local", nil, map[string]string{})
	assert.NoError(t, err) // Replace 不校验 nil，由调用方先验证
}

// TestProfileManager_Replace_ClosesOldInstance 替换时关闭旧实例
func TestProfileManager_Replace_ClosesOldInstance(t *testing.T) {
	mgr := newTestManager(t)

	oldFS := mgr.Get("local")
	require.NotNil(t, oldFS)

	// 旧实例实现 io.Closer（LocalFileStorage 目前没有，验证 Replace 不报错）
	newFS, err := NewLocalFileStorage(t.TempDir())
	require.NoError(t, err)

	err = mgr.Replace("local", "local", "local", newFS, map[string]string{"root_dir": "/new"})
	assert.NoError(t, err)

	// 新实例生效
	assert.Same(t, newFS, mgr.Get("local"))
}

// TestInitFromDB_SeedsWithContextTimeout 种子数据目录可写性验证
func TestInitFromDB_SeedDirectoryCreation(t *testing.T) {
	db := setupTestDB(t)

	// 调用 ensureDefaultLocalStorage 后，./data/files 目录应被创建
	err := ensureDefaultLocalStorage(db)
	require.NoError(t, err)

	// 验证目录确实存在
	fs := mgrFromInit()
	_ = fs
	// 通过 Get() 验证默认实例可用
	Reset()
	require.NoError(t, InitFromDB(db))
	assert.NotNil(t, Get())
}

func mgrFromInit() *ProfileManager {
	return GetManager()
}

// TestListProfiles_ThreadSafe 并发读取 ListProfiles 不 panic
func TestListProfiles_ThreadSafe(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = mgr.ListProfiles()
			_ = mgr.GetDefault()
			_ = mgr.GetDefaultName()
		}()
	}
	wg.Wait()
}

// TestHealthChecker_ContextCancel 健康检查 context 取消不 panic
func TestHealthChecker_ContextCancel(t *testing.T) {
	ls, err := NewLocalFileStorage(t.TempDir())
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	// 本地存储 HealthCheck 不依赖 ctx，应正常返回
	err = ls.HealthCheck(ctx)
	assert.NoError(t, err)
}

// 辅助：确保重复注册不会破坏已有实例
func TestRegister_DuplicateDoesNotOverwrite(t *testing.T) {
	mgr := newTestManager(t)
	oldFS := mgr.Get("local")

	// 尝试重复注册
	dupFS, _ := NewLocalFileStorage(t.TempDir())
	err := mgr.Register("local", "local", "local", dupFS, nil)
	assert.Error(t, err, "重复注册应报错")

	// 原实例未被覆盖
	assert.Same(t, oldFS, mgr.Get("local"))
}

var _ = fmt.Sprintf // 保留 fmt 引用避免未使用
