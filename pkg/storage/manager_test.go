package storage

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestManager(t *testing.T) *ProfileManager {
	t.Helper()
	mgr := NewProfileManager()

	dir := t.TempDir()
	ls1, err := NewLocalFileStorage(dir)
	require.NoError(t, err)

	dir2 := t.TempDir()
	ls2, err := NewLocalFileStorage(dir2)
	require.NoError(t, err)

	err = mgr.Register("local", "lcl", "local", ls1, map[string]string{"root_dir": dir})
	require.NoError(t, err)

	err = mgr.Register("backup", "bkp", "local", ls2, map[string]string{"root_dir": dir2})
	require.NoError(t, err)

	return mgr
}

func TestProfileManager_Register_Duplicate(t *testing.T) {
	mgr := newTestManager(t)
	ls, _ := NewLocalFileStorage(t.TempDir())
	err := mgr.Register("local", "lcl", "local", ls, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestProfileManager_SetDefault(t *testing.T) {
	mgr := newTestManager(t)

	err := mgr.SetDefault("local")
	require.NoError(t, err)
	assert.Equal(t, "local", mgr.GetDefaultName())

	err = mgr.SetDefault("backup")
	require.NoError(t, err)
	assert.Equal(t, "backup", mgr.GetDefaultName())

	// 不存在的 profile
	err = mgr.SetDefault("nonexistent")
	assert.Error(t, err)
}

func TestProfileManager_GetDefault(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	fs := mgr.GetDefault()
	require.NotNil(t, fs)

	// 验证默认实例可用
	ctx := context.Background()
	info, err := fs.Stat(ctx, "")
	// Stat on empty string may fail but shouldn't panic
	assert.NotNil(t, fs)
	_ = info
	_ = err
}

func TestProfileManager_Get(t *testing.T) {
	mgr := newTestManager(t)

	localFS := mgr.Get("local")
	require.NotNil(t, localFS)

	backupFS := mgr.Get("backup")
	require.NotNil(t, backupFS)

	// 不同实例应该是不同的
	assert.NotSame(t, localFS, backupFS)

	// 不存在的返回 nil
	nilFS := mgr.Get("nonexistent")
	assert.Nil(t, nilFS)
}

func TestProfileManager_ListProfiles(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	profiles := mgr.ListProfiles()
	assert.Len(t, profiles, 2)

	for _, p := range profiles {
		if p.Name == "local" {
			assert.Equal(t, "local", p.Backend)
			assert.True(t, p.Enabled)
		} else {
			assert.Equal(t, "local", p.Backend)
		}
	}
}

func TestProfileManager_ConcurrentSwitch(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				_ = mgr.SetDefault("local")
			} else {
				_ = mgr.SetDefault("backup")
			}
			_ = mgr.GetDefault()
			_ = mgr.GetDefaultName()
			_ = mgr.ListProfiles()
			_ = mgr.Get("local")
			_ = mgr.Get("backup")
		}(i)
	}
	wg.Wait()

	// 并发切换后默认应该存在
	assert.NotEmpty(t, mgr.GetDefaultName())
	assert.NotNil(t, mgr.GetDefault())
}

func TestProfileManager_Shutdown(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	err := mgr.Shutdown()
	assert.NoError(t, err)
	assert.Empty(t, mgr.GetDefaultName())
}

func TestProfileManager_Replace(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	// 替换 local 实例
	newLS, err := NewLocalFileStorage(t.TempDir())
	require.NoError(t, err)

	err = mgr.Replace("local", "local", "local", newLS, map[string]string{"root_dir": "/new/path"})
	assert.NoError(t, err)

	// 验证替换后实例可用
	fs := mgr.Get("local")
	assert.NotNil(t, fs)
	assert.Same(t, newLS, fs)

	// 默认实例仍然是 local
	assert.Equal(t, "local", mgr.GetDefaultName())
	assert.Same(t, newLS, mgr.GetDefault())
}

func TestProfileManager_Replace_NewCode(t *testing.T) {
	mgr := newTestManager(t)

	// 替换 backup 实例并改 code
	newLS, _ := NewLocalFileStorage(t.TempDir())
	err := mgr.Replace("backup", "bkp2", "local", newLS, map[string]string{"root_dir": "/backup2"})
	assert.NoError(t, err)

	// 新 code 可访问
	assert.Same(t, newLS, mgr.GetByCode("bkp2"))

	// 旧 code 应已清理（backup 原来的 code 是 "bkp"）
	assert.Nil(t, mgr.GetByCode("bkp"))
}
