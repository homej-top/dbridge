package service

import (
	"errors"
	"os"
	"sync"
	"testing"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/repository"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// TestMain 统一初始化加密 key（密码加密依赖）。
func TestMain(m *testing.M) {
	_ = cryptoPkg.InitKey([]byte("0123456789abcdef0123456789abcdef"))
	os.Exit(m.Run())
}

// newTestDataSourceService 创建基于内存 sqlite 的 DataSourceService（含 data_sources 表迁移）。
func newTestDataSourceService(t *testing.T) *DataSourceService {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&repository.DataSource{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})
	return NewDataSourceService(db)
}

func TestSystemDSFromConfigSQLite(t *testing.T) {
	cfg := config.DatabaseConfig{
		Type:   "sqlite",
		SQLite: config.SQLiteConfig{Path: "./data/dbbridge.db"},
	}
	ds, err := systemDSFromConfig(cfg)
	if err != nil {
		t.Fatalf("systemDSFromConfig: %v", err)
	}
	if ds.ID != SystemDataSourceID {
		t.Errorf("ID = %q, want %q", ds.ID, SystemDataSourceID)
	}
	if !ds.IsSystem {
		t.Errorf("IsSystem should be true")
	}
	if ds.Type != "sqlite" || ds.Host != "./data/dbbridge.db" {
		t.Errorf("unexpected mapping: type=%q host=%q", ds.Type, ds.Host)
	}
	// sqlite 密码应加密（空密码也加密），且可解密为空字符串
	if ds.Password == "" {
		t.Errorf("sqlite password should be encrypted (non-empty), got empty")
	}
	if plain, err := cryptoPkg.Decrypt(ds.Password); err != nil || plain != "" {
		t.Errorf("sqlite password should decrypt to empty, got %q err=%v", plain, err)
	}
}

func TestSystemDSFromConfigMySQL(t *testing.T) {
	cfg := config.DatabaseConfig{
		Type: "mysql",
		MySQL: config.MySQLConfig{
			Host: "192.168.1.105", Port: 3306, Database: "dbbridge",
			Username: "root", Password: "123456",
		},
	}
	ds, err := systemDSFromConfig(cfg)
	if err != nil {
		t.Fatalf("systemDSFromConfig: %v", err)
	}
	if ds.Type != "mysql" || ds.Host != "192.168.1.105" || ds.Port != 3306 {
		t.Errorf("unexpected mapping: %+v", ds)
	}
	if ds.Database != "dbbridge" || ds.Username != "root" {
		t.Errorf("unexpected mapping: db=%q user=%q", ds.Database, ds.Username)
	}
	if ds.Password == "" || ds.Password == "123456" {
		t.Errorf("password should be encrypted, got %q", ds.Password)
	}
}

func TestSystemDSFromConfigPostgres(t *testing.T) {
	cfg := config.DatabaseConfig{
		Type: "postgresql",
		PostgreSQL: config.PostgreSQLConfig{
			Host: "localhost", Port: 5432, Database: "dbbridge",
			Username: "postgres", Password: "secret", SSLMode: "disable",
		},
	}
	ds, err := systemDSFromConfig(cfg)
	if err != nil {
		t.Fatalf("systemDSFromConfig: %v", err)
	}
	if ds.Type != "postgres" || ds.Host != "localhost" || ds.Port != 5432 {
		t.Errorf("unexpected mapping: %+v", ds)
	}
	if ds.SSLMode != "disable" {
		t.Errorf("ssl_mode = %q, want disable", ds.SSLMode)
	}
	if ds.Password == "" || ds.Password == "secret" {
		t.Errorf("password should be encrypted")
	}
}

func TestSystemDSFromConfigUnsupported(t *testing.T) {
	cfg := config.DatabaseConfig{Type: "oracle"}
	if _, err := systemDSFromConfig(cfg); err == nil {
		t.Fatalf("expected error for unsupported type")
	}
}

func TestEnsureSystemDataSourceIdempotent(t *testing.T) {
	svc := newTestDataSourceService(t)
	cfg := config.DatabaseConfig{
		Type:   "sqlite",
		SQLite: config.SQLiteConfig{Path: "./data/dbbridge.db"},
	}
	if err := svc.EnsureSystemDataSource(cfg); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	if err := svc.EnsureSystemDataSource(cfg); err != nil {
		t.Fatalf("second ensure: %v", err)
	}
	var count int64
	if err := svc.db.Model(&repository.DataSource{}).Where("is_system = ?", true).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 system data source, got %d", count)
	}
}

// TestEnsureSystemDataSourceBackfillsIsSystem 验证连接信息匹配到非系统数据源时回填 is_system=true。
func TestEnsureSystemDataSourceBackfillsIsSystem(t *testing.T) {
	svc := newTestDataSourceService(t)
	cfg := sqliteCfg()

	// 预先创建一条连接信息与系统库一致、但 is_system=false 的旧数据（模拟历史数据）
	legacy := repository.DataSource{
		ID:       SystemDataSourceID,
		Name:     "旧数据源",
		Type:     "sqlite",
		Host:     "./data/dbbridge.db",
		IsSystem: false,
		TenantID: "tenantA",
	}
	if err := svc.db.Create(&legacy).Error; err != nil {
		t.Fatalf("create legacy ds: %v", err)
	}

	if err := svc.EnsureSystemDataSource(cfg); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	// 验证回填
	var updated repository.DataSource
	if err := svc.db.Where("id = ?", SystemDataSourceID).First(&updated).Error; err != nil {
		t.Fatalf("query: %v", err)
	}
	if !updated.IsSystem {
		t.Fatalf("is_system should be backfilled to true")
	}

	// 验证未新建第二条
	var count int64
	svc.db.Model(&repository.DataSource{}).Where("is_system = ?", true).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 system data source after backfill, got %d", count)
	}
}

// TestEnsureSystemDataSourceConcurrent 验证并发调用只产生一条系统数据源。
func TestEnsureSystemDataSourceConcurrent(t *testing.T) {
	svc := newTestDataSourceService(t)
	cfg := sqliteCfg()

	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = svc.EnsureSystemDataSource(cfg)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: %v", i, err)
		}
	}

	var count int64
	svc.db.Model(&repository.DataSource{}).Where("is_system = ?", true).Count(&count)
	if count != 1 {
		t.Fatalf("expected exactly 1 system data source after concurrent ensure, got %d", count)
	}
}

func sqliteCfg() config.DatabaseConfig {
	return config.DatabaseConfig{
		Type:   "sqlite",
		SQLite: config.SQLiteConfig{Path: "./data/dbbridge.db"},
	}
}

func TestDeleteSystemDataSourceProtected(t *testing.T) {
	svc := newTestDataSourceService(t)
	if err := svc.EnsureSystemDataSource(sqliteCfg()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if err := svc.Delete(SystemDataSourceID, "default"); !errors.Is(err, ErrSystemDataSourceProtected) {
		t.Fatalf("expected ErrSystemDataSourceProtected, got %v", err)
	}
	// 记录仍在
	var count int64
	svc.db.Model(&repository.DataSource{}).Where("id = ?", SystemDataSourceID).Count(&count)
	if count != 1 {
		t.Fatalf("system data source should still exist, count=%d", count)
	}
}

func TestUpdateSystemDataSourceTypeLocked(t *testing.T) {
	svc := newTestDataSourceService(t)
	if err := svc.EnsureSystemDataSource(sqliteCfg()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	_, err := svc.Update(SystemDataSourceID, "default", CreateDSInput{
		Name: "系统数据源", Type: "mysql", Host: "localhost", Port: 3306, Username: "root",
	})
	if !errors.Is(err, ErrSystemDataSourceTypeLocked) {
		t.Fatalf("expected ErrSystemDataSourceTypeLocked, got %v", err)
	}
}

func TestUpdateSystemDataSourceKeepsIsSystem(t *testing.T) {
	svc := newTestDataSourceService(t)
	if err := svc.EnsureSystemDataSource(sqliteCfg()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	out, err := svc.Update(SystemDataSourceID, "default", CreateDSInput{
		Name: "系统数据源(改)", Type: "sqlite", Host: "./data/new.db",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !out.IsSystem {
		t.Fatalf("is_system should remain true after update")
	}
	if out.Name != "系统数据源(改)" || out.Host != "./data/new.db" {
		t.Fatalf("unexpected update result: name=%q host=%q", out.Name, out.Host)
	}
}

func TestListIncludesSystemDataSource(t *testing.T) {
	svc := newTestDataSourceService(t)
	// 普通租户数据源
	if err := svc.db.Create(&repository.DataSource{
		Name: "普通", Type: "sqlite", Host: "./x.db", Port: 0, Username: "", TenantID: "tenantA",
	}).Error; err != nil {
		t.Fatalf("create normal ds: %v", err)
	}
	if err := svc.EnsureSystemDataSource(sqliteCfg()); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	list, err := svc.List("tenantA")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var foundSystem, foundNormal bool
	for _, ds := range list {
		if ds.IsSystem {
			foundSystem = true
		}
		if ds.Name == "普通" {
			foundNormal = true
		}
	}
	if !foundSystem {
		t.Fatalf("system data source should be visible to tenant")
	}
	if !foundNormal {
		t.Fatalf("normal data source should be visible to its tenant")
	}
}
