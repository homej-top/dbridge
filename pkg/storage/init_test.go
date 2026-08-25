package storage

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	// 使用唯一命名的共享内存数据库（支持并发 goroutine，且测试间隔离）
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	require.NoError(t, err)

	// 创建 storage_instances 表
	err = db.Exec(`CREATE TABLE storage_instances (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		code TEXT NOT NULL UNIQUE,
		backend TEXT NOT NULL DEFAULT 'local',
		enabled INTEGER DEFAULT 1,
		config_json TEXT NOT NULL,
		sort_order INTEGER DEFAULT 0,
		created_at DATETIME,
		updated_at DATETIME
	)`).Error
	require.NoError(t, err)
	return db
}

func TestInitFromDB_SeedsDefaultLocal(t *testing.T) {
	db := setupTestDB(t)

	err := InitFromDB(db)
	require.NoError(t, err)

	// 验证种子实例已创建
	var count int64
	db.Model(&StorageInstanceRecord{}).Count(&count)
	assert.Equal(t, int64(1), count)

	// 验证默认实例可用
	mgr := GetManager()
	require.NotNil(t, mgr)
	assert.Equal(t, "local", mgr.GetDefaultName())
	assert.NotNil(t, mgr.GetDefault())
}

func TestInitFromDB_DoesNotDuplicateSeed(t *testing.T) {
	db := setupTestDB(t)

	// 首次初始化
	require.NoError(t, InitFromDB(db))
	Reset()

	// 第二次初始化（表已有数据，不应重复插入）
	require.NoError(t, InitFromDB(db))

	var count int64
	db.Model(&StorageInstanceRecord{}).Count(&count)
	assert.Equal(t, int64(1), count, "种子数据不应重复插入")
}

func TestInitFromDB_LoadsExistingInstances(t *testing.T) {
	db := setupTestDB(t)

	// 手动插入两个本地实例
	db.Create(&StorageInstanceRecord{Name: "local", Code: "local", Backend: "local", Enabled: true, ConfigJSON: `{"root_dir":"/tmp/a"}`})
	db.Create(&StorageInstanceRecord{Name: "backup", Code: "backup", Backend: "local", Enabled: true, ConfigJSON: `{"root_dir":"/tmp/b"}`})

	// 重新初始化
	Reset()
	require.NoError(t, InitFromDB(db))

	mgr := GetManager()
	require.NotNil(t, mgr)

	profiles := mgr.ListProfiles()
	assert.Len(t, profiles, 2)

	// 第一个实例（local）是默认
	assert.Equal(t, "local", mgr.GetDefaultName())
}

func TestInitFromDB_NilDB(t *testing.T) {
	// 无数据库时也能初始化（内存 local 实例）
	Reset()
	err := InitFromDB(nil)
	require.NoError(t, err)

	mgr := GetManager()
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.GetDefault())
}

func TestProfileManager_ListProfiles_IsDefault(t *testing.T) {
	mgr := newTestManager(t)
	_ = mgr.SetDefault("local")

	profiles := mgr.ListProfiles()
	for _, p := range profiles {
		if p.Name == "local" {
			assert.True(t, p.IsDefault, "local 应标记为默认")
		} else {
			assert.False(t, p.IsDefault)
		}
	}
}
