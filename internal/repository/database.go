package repository

import (
	"fmt"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/service/drivers"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var DB *gorm.DB
var systemDialect drivers.DatabaseDriver

// systemDBCfg 保存系统库配置，供初始化阶段识别/创建系统数据源使用。
// 仅在 Init 时写入一次（main.go 启动流程中仅调用一次）。
var systemDBCfg config.DatabaseConfig

// GetDialect returns the system SQL dialect driver
func GetDialect() drivers.DatabaseDriver {
	if systemDialect == nil {
		systemDialect = drivers.NewSQLiteDriver()
	}
	return systemDialect
}

// SystemDBConnection 返回系统库的关键连接信息（类型 + 定位字段 + 用户名 + 明文密码）。
// 供 EnsureSystemDataSource 识别/创建系统数据源时使用。
func SystemDBConnection() (dbType, host, database, username, password string, port int) {
	switch systemDBCfg.Type {
	case "mysql", "mariadb":
		return systemDBCfg.Type, systemDBCfg.MySQL.Host, systemDBCfg.MySQL.Database,
			systemDBCfg.MySQL.Username, systemDBCfg.MySQL.Password, systemDBCfg.MySQL.Port
	case "postgres", "postgresql":
		return systemDBCfg.Type, systemDBCfg.PostgreSQL.Host, systemDBCfg.PostgreSQL.Database,
			systemDBCfg.PostgreSQL.Username, systemDBCfg.PostgreSQL.Password, systemDBCfg.PostgreSQL.Port
	case "sqlite":
		return "sqlite", systemDBCfg.SQLite.Path, "", "", "", 0
	default:
		return systemDBCfg.Type, "", "", "", "", 0
	}
}

func Init(cfg config.DatabaseConfig) error {
	// 保存系统库配置（仅在启动初始化时调用一次）
	systemDBCfg = cfg

	var dialector gorm.Dialector
	var dsn string

	switch cfg.Type {
	case "mysql", "mariadb":
		my := cfg.MySQL
		charset := my.Charset
		if charset == "" {
			charset = "utf8mb4"
		}
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=Local",
			my.Username, my.Password, my.Host, my.Port, my.Database, charset)
		dialector = mysql.Open(dsn)
	case "postgresql", "postgres":
		pg := cfg.PostgreSQL
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=UTC",
			pg.Host, pg.Port, pg.Username, pg.Password, pg.Database, pg.SSLMode)
		dialector = postgres.Open(dsn)
	case "sqlite":
		dsn = cfg.SQLite.Path
		dialector = sqlite.Open(dsn)
	default:
		return fmt.Errorf("unsupported database type: %s", cfg.Type)
	}

	gormConfig := &gorm.Config{}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return fmt.Errorf("failed to connect database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	// Set connection pool
	switch cfg.Type {
	case "mysql", "mariadb":
		my := cfg.MySQL
		sqlDB.SetMaxOpenConns(my.MaxOpenConns)
		sqlDB.SetMaxIdleConns(my.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(my.ConnMaxLifetime)
		sqlDB.SetConnMaxIdleTime(my.ConnMaxIdleTime)
	case "postgresql", "postgres":
		pg := cfg.PostgreSQL
		sqlDB.SetMaxOpenConns(pg.MaxOpenConns)
		sqlDB.SetMaxIdleConns(pg.MaxIdleConns)
		sqlDB.SetConnMaxLifetime(pg.ConnMaxLifetime)
		sqlDB.SetConnMaxIdleTime(pg.ConnMaxIdleTime)
	default:
		sqlDB.SetMaxOpenConns(1) // SQLite doesn't support concurrent writes
	}

	// Test connection
	if err := sqlDB.Ping(); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	DB = db
	systemDialect = drivers.NewSQLiteDriver()
	switch cfg.Type {
	case "mysql", "mariadb":
		systemDialect = &drivers.MySQLDriver{}
	case "postgres", "postgresql":
		systemDialect = &drivers.PostgresDriver{}
	case "sqlserver", "mssql":
		systemDialect = &drivers.SQLServerDriver{}
	case "oracle":
		systemDialect = &drivers.OracleDriver{}
	}
	return nil
}

func Close() error {
	if DB != nil {
		sqlDB, err := DB.DB()
		if err != nil {
			return err
		}
		return sqlDB.Close()
	}
	return nil
}

func GetDB() *gorm.DB {
	return DB
}

// AutoMigrate runs migration for all models
func AutoMigrate() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}

	return DB.AutoMigrate(
		&User{},
		&DataSource{},
		&SyncTask{},
		&AuditLog{},
		&AuditPurgeLog{},
		&Setting{},
		&LockRecord{},
	)
}

// SeedDefaultAdmin creates a default admin user if no users exist.
func SeedDefaultAdmin() error {
	if DB == nil {
		return fmt.Errorf("database not initialized")
	}
	var count int64
	DB.Model(&User{}).Count(&count)
	if count > 0 {
		return nil
	}

	hash, err := bcryptHash("admin123")
	if err != nil {
		return err
	}

	admin := User{
		Username: "admin",
		Password: hash,
		Role:     "admin",
		TenantID: "default",
	}
	return DB.Create(&admin).Error
}

func bcryptHash(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
