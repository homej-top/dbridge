package service

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service/drivers"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	"gorm.io/gorm"
)

// SystemDataSourceID 系统数据源固定 ID（长度 36 字符，与 data_sources.id 列兼容）。
const SystemDataSourceID = "00000000-0000-0000-0000-000000000001"

// 系统数据源保护错误（供 handler 区分并返回 400）。
var (
	ErrSystemDataSourceProtected  = errors.New("系统数据源不可删除")
	ErrSystemDataSourceTypeLocked = errors.New("系统数据源不可变更数据库类型")
)

type DataSourceService struct {
	db *gorm.DB
}

func NewDataSourceService(db *gorm.DB) *DataSourceService {
	return &DataSourceService{db: db}
}

// systemDSFromConfig 把 config.database 映射为系统数据源连接信息。
// 系统数据源代表系统自身的底层数据库，供查询/报表/同步等业务模块使用。
func systemDSFromConfig(cfg config.DatabaseConfig) (repository.DataSource, error) {
	ds := repository.DataSource{
		ID:        SystemDataSourceID,
		Name:      "系统数据源",
		TenantID:  "",     // 全局，对全体租户可见
		CreatedBy: "system",
		IsSystem:  true,
		Tags:      "system",
		Env:       "system",
	}

	switch cfg.Type {
	case "sqlite":
		ds.Type = "sqlite"
		ds.Host = cfg.SQLite.Path
		ds.Port = 0
		ds.Username = ""
		// sqlite 无密码，但同样加密空字符串，与普通数据源 Create 行为一致，
		// 避免后续连接时 Decrypt("") 失败。
		encryptedPwd, err := cryptoPkg.Encrypt("")
		if err != nil {
			return ds, fmt.Errorf("failed to encrypt empty password: %w", err)
		}
		ds.Password = encryptedPwd
	case "mysql", "mariadb":
		ds.Type = cfg.Type
		ds.Host = cfg.MySQL.Host
		ds.Port = cfg.MySQL.Port
		ds.Database = cfg.MySQL.Database
		ds.Username = cfg.MySQL.Username
		encryptedPwd, err := cryptoPkg.Encrypt(cfg.MySQL.Password)
		if err != nil {
			return ds, fmt.Errorf("failed to encrypt mysql password: %w", err)
		}
		ds.Password = encryptedPwd
	case "postgres", "postgresql":
		ds.Type = "postgres"
		ds.Host = cfg.PostgreSQL.Host
		ds.Port = cfg.PostgreSQL.Port
		ds.Database = cfg.PostgreSQL.Database
		ds.Username = cfg.PostgreSQL.Username
		ds.SSLMode = cfg.PostgreSQL.SSLMode
		encryptedPwd, err := cryptoPkg.Encrypt(cfg.PostgreSQL.Password)
		if err != nil {
			return ds, fmt.Errorf("failed to encrypt postgres password: %w", err)
		}
		ds.Password = encryptedPwd
	default:
		return ds, fmt.Errorf("unsupported system database type: %s", cfg.Type)
	}
	return ds, nil
}

// ensureSystemDSMu 保护系统数据源的"检查-创建"流程，避免并发下产生多条。
// 采用互斥锁而非 sync.Once：Once 会缓存首次错误且不可重试，锁则每次重新检查、可自愈。
// 锁为包级，跨 DataSourceService 实例共享；串行化仅影响初始化阶段，成本可忽略。
var ensureSystemDSMu sync.Mutex

// EnsureSystemDataSource 初始化时确保系统数据源存在（幂等、并发安全）。
// 识别优先级：is_system=true 字段 → 连接信息精确匹配（Type+Host+Port+Database）→ 回填标记 → 新建。
// 注意：连接信息匹配到非系统数据源时会隐式回填 is_system=true（自动修复旧数据），
// 详见连接匹配分支注释。
func (s *DataSourceService) EnsureSystemDataSource(cfg config.DatabaseConfig) error {
	ensureSystemDSMu.Lock()
	defer ensureSystemDSMu.Unlock()

	// 1. 优先按 is_system 字段查找（最明确、不受用户操作影响）
	var existing repository.DataSource
	if err := s.db.Where("is_system = ?", true).Order("created_at ASC").First(&existing).Error; err == nil {
		return nil // 已存在，跳过
	}

	// 2. 按连接信息精确匹配（Type + Host + Port + Database）
	ds, err := systemDSFromConfig(cfg)
	if err != nil {
		return err
	}
	var matched repository.DataSource
	matchQuery := s.db.Where("type = ? AND host = ?", ds.Type, ds.Host)
	if ds.Port > 0 {
		matchQuery = matchQuery.Where("port = ?", ds.Port)
	}
	if ds.Database != "" {
		matchQuery = matchQuery.Where("database = ?", ds.Database)
	}
	if err := matchQuery.Order("created_at ASC").First(&matched).Error; err == nil {
		// 自动修复旧数据：将指向系统库的业务数据源标记为系统数据源。
		// 这是隐式变更用户数据的操作，已在此显式注释，便于维护者理解。
		if !matched.IsSystem {
			s.db.Model(&repository.DataSource{}).Where("id = ?", matched.ID).Update("is_system", true)
		}
		return nil
	}

	// 3. 创建系统数据源（固定 ID），捕获唯一键冲突（并发下另一 goroutine 已创建）
	if err := s.db.Create(&ds).Error; err != nil {
		if isDuplicateKeyError(err) {
			return nil // 冲突 = 已存在，幂等成功
		}
		return err
	}
	return nil
}

// isDuplicateKeyError 判断是否为唯一键/主键冲突错误。
// 覆盖 MySQL(1062)、PostgreSQL(duplicate key)、SQLite(UNIQUE constraint) 三类后端。
func isDuplicateKeyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "duplicate") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "1062")
}

type CreateDSInput struct {
	Name              string `json:"name" binding:"required"`
	Type              string `json:"type" binding:"required"`
	Host              string `json:"host"`
	Port              int    `json:"port"`
	Database          string `json:"database"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	SSLMode           string `json:"ssl_mode"`
	ExtraConfig       string `json:"extra_config"`
	Tags              string `json:"tags"`
	Env               string `json:"env"`
	// Oracle-specific fields (also merged into extra_config)
	OracleService     string `json:"oracle_service"`
	OracleConnectMode string `json:"oracle_connect_mode"`
	OracleRole        string `json:"oracle_role"`
	TenantID          string `json:"tenant_id"`
	CreatedBy         string `json:"created_by"`
}

type DSOutput struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Type              string    `json:"type"`
	Host              string    `json:"host"`
	Port              int       `json:"port"`
	Database          string    `json:"database"`
	Username          string    `json:"username"`
	SSLMode           string    `json:"ssl_mode"`
	ExtraConfig       string    `json:"extra_config"`
	Tags              string    `json:"tags"`
	Env               string    `json:"env"`
	IsSystem          bool      `json:"is_system"`
	OracleService     string    `json:"oracle_service,omitempty"`
	OracleConnectMode string    `json:"oracle_connect_mode,omitempty"`
	OracleRole        string    `json:"oracle_role,omitempty"`
	TenantID          string    `json:"tenant_id"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func toDSOutput(ds repository.DataSource) DSOutput {
	out := DSOutput{
		ID:          ds.ID,
		Name:        ds.Name,
		Type:        ds.Type,
		Host:        ds.Host,
		Port:        ds.Port,
		Database:    ds.Database,
		Username:    ds.Username,
		SSLMode:     ds.SSLMode,
		ExtraConfig: ds.ExtraConfig,
		Tags:        ds.Tags,
		Env:         ds.Env,
		IsSystem:    ds.IsSystem,
		TenantID:    ds.TenantID,
		CreatedBy:   ds.CreatedBy,
		CreatedAt:   ds.CreatedAt,
		UpdatedAt:   ds.UpdatedAt,
	}
	// Extract Oracle fields from extra_config for frontend convenience
	if ds.Type == "oracle" && ds.ExtraConfig != "" {
		var extra map[string]string
		if json.Unmarshal([]byte(ds.ExtraConfig), &extra) == nil {
			if v, ok := extra["oracle_service"]; ok {
				out.OracleService = v
			}
			if v, ok := extra["connect_mode"]; ok {
				out.OracleConnectMode = v
			}
			if v, ok := extra["role"]; ok {
				out.OracleRole = v
			}
		}
	}
	return out
}

// mergeOracleConfig merges Oracle-specific top-level fields into the extra_config JSON string.
// This allows the frontend to send oracle_service, oracle_connect_mode, oracle_role
// as top-level fields, and the backend automatically stores them in extra_config.
func mergeOracleConfig(existingConfig, oracleService, connectMode, oracleRole string) string {
	extra := make(map[string]string)

	// Parse existing config
	if existingConfig != "" {
		json.Unmarshal([]byte(existingConfig), &extra)
	}

	// Override with explicitly provided values
	if oracleService != "" {
		extra["oracle_service"] = oracleService
	}
	if connectMode != "" {
		extra["connect_mode"] = connectMode
	}
	if oracleRole != "" {
		extra["role"] = oracleRole
	}

	if len(extra) == 0 {
		return ""
	}

	data, _ := json.Marshal(extra)
	return string(data)
}

func (s *DataSourceService) List(tenantID string) ([]DSOutput, error) {
	return s.ListByTag(tenantID, "")
}

func (s *DataSourceService) ListByTag(tenantID, tag string) ([]DSOutput, error) {
	if s.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	var dataSources []repository.DataSource
	// 系统数据源（is_system=true）和未分配租户的数据源（tenant_id=""）为全局资源，对全体租户可见。
	q := s.db.Where("tenant_id = ? OR tenant_id = '' OR is_system = ?", tenantID, true)
	if tag != "" {
		q = q.Where("(tags LIKE ? OR is_system = ?) AND (tenant_id = ? OR tenant_id = '' OR is_system = ?)", "%"+tag+"%", true, tenantID, true)
	}
	if err := q.Order("created_at DESC").Find(&dataSources).Error; err != nil {
		return nil, err
	}

	result := make([]DSOutput, len(dataSources))
	for i, ds := range dataSources {
		result[i] = toDSOutput(ds)
	}
	return result, nil
}

func (s *DataSourceService) Get(id, tenantID string) (*DSOutput, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ? AND (tenant_id = ? OR is_system = ?)", id, tenantID, true).First(&ds).Error; err != nil {
		return nil, err
	}
	out := toDSOutput(ds)
	return &out, nil
}

func (s *DataSourceService) Create(input CreateDSInput) (*DSOutput, error) {
	if input.Password == "" && input.Type != "sqlite" {
		return nil, fmt.Errorf("password is required")
	}
	encryptedPwd, err := cryptoPkg.Encrypt(input.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt password: %w", err)
	}

	// Merge Oracle top-level fields into extra_config
	input.ExtraConfig = mergeOracleConfig(input.ExtraConfig, input.OracleService, input.OracleConnectMode, input.OracleRole)

	ds := repository.DataSource{
		Name:        input.Name,
		Type:        input.Type,
		Host:        input.Host,
		Port:        input.Port,
		Database:    input.Database,
		Username:    input.Username,
		Password:    encryptedPwd,
		SSLMode:     input.SSLMode,
		ExtraConfig: input.ExtraConfig,
		Tags:        input.Tags,
		Env:         input.Env,
		TenantID:    input.TenantID,
		CreatedBy:   input.CreatedBy,
	}

	if err := s.db.Create(&ds).Error; err != nil {
		return nil, err
	}

	out := toDSOutput(ds)
	return &out, nil
}

func (s *DataSourceService) Update(id, tenantID string, input CreateDSInput) (*DSOutput, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ? AND (tenant_id = ? OR is_system = ?)", id, tenantID, true).First(&ds).Error; err != nil {
		return nil, err
	}

	// 系统数据源：数据库类型不可变更（与前端只读一致），防止条目与实际底层库脱节。
	if ds.IsSystem && input.Type != ds.Type {
		return nil, fmt.Errorf("%w（当前类型: %s）", ErrSystemDataSourceTypeLocked, ds.Type)
	}

	// Merge Oracle top-level fields into extra_config
	input.ExtraConfig = mergeOracleConfig(input.ExtraConfig, input.OracleService, input.OracleConnectMode, input.OracleRole)

	ds.Name = input.Name
	ds.Type = input.Type
	ds.Host = input.Host
	ds.Port = input.Port
	ds.Database = input.Database
	ds.Username = input.Username
	ds.SSLMode = input.SSLMode
	ds.ExtraConfig = input.ExtraConfig
	ds.Tags = input.Tags
	ds.Env = input.Env

	if input.Password != "" {
		encryptedPwd, err := cryptoPkg.Encrypt(input.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to encrypt password: %w", err)
		}
		ds.Password = encryptedPwd
	}

	if err := s.db.Save(&ds).Error; err != nil {
		return nil, err
	}

	out := toDSOutput(ds)
	return &out, nil
}

func (s *DataSourceService) Delete(id, tenantID string) error {
	var ds repository.DataSource
	if err := s.db.Where("id = ? AND (tenant_id = ? OR is_system = ?)", id, tenantID, true).First(&ds).Error; err != nil {
		return err
	}
	if ds.IsSystem {
		return ErrSystemDataSourceProtected
	}
	return s.db.Where("id = ?", ds.ID).Delete(&repository.DataSource{}).Error
}

// TestConnection tests database connectivity without saving
func (s *DataSourceService) TestConnection(input CreateDSInput) (string, error) {
	// Validate type against allowlist to prevent driver injection
	if !isSupportedDBType(input.Type) {
		return "", fmt.Errorf("unsupported database type for test: %s", input.Type)
	}

	// Merge Oracle top-level fields into extra_config
	input.ExtraConfig = mergeOracleConfig(input.ExtraConfig, input.OracleService, input.OracleConnectMode, input.OracleRole)

	ds := repository.DataSource{
		Type:        input.Type,
		Host:        input.Host,
		Port:        input.Port,
		Database:    input.Database,
		Username:    input.Username,
		ExtraConfig: input.ExtraConfig,
	}
	db, err := s.openDBConnection(ds, input.Password)
	if err != nil {
		return "", err
	}
	defer db.Close()

	return "connected", nil
}

// GetDecryptedPassword returns the decrypted password for a data source
func (s *DataSourceService) GetDecryptedPassword(id string) (string, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return "", err
	}
	return cryptoPkg.Decrypt(ds.Password)
}

// connectDB opens and pings a connection to the given data source
func (s *DataSourceService) connectDB(id string) (*sql.DB, *repository.DataSource, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return nil, nil, fmt.Errorf("data source not found")
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt password")
	}

	if !isSupportedDBType(ds.Type) {
		return nil, nil, fmt.Errorf("unsupported database type: %s", ds.Type)
	}

	conn, err := openDBConn(ds, pwd)
	if err != nil {
		return nil, nil, err
	}

	return conn, &ds, nil
}

// connectDriver creates a DatabaseDriver for the given data source ID.
// The caller is responsible for closing the driver.
// Connect creates a driver connection for server management operations
func (s *DataSourceService) Connect(id string) (drivers.DatabaseDriver, *repository.DataSource, error) {
	return s.connectDriver(id)
}

// ConnectForDB connects to a specific database on the data source
func (s *DataSourceService) ConnectForDB(id, database string) (drivers.DatabaseDriver, error) {
	return s.connectDriverForDB(id, database)
}

func (s *DataSourceService) connectDriver(id string) (drivers.DatabaseDriver, *repository.DataSource, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return nil, nil, fmt.Errorf("data source not found")
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decrypt password")
	}

	if !isSupportedDBType(ds.Type) {
		return nil, nil, fmt.Errorf("unsupported database type: %s", ds.Type)
	}

	driver, _, err := s.buildDriver(ds, pwd)
	if err != nil {
		return nil, nil, err
	}

	return driver, &ds, nil
}

// buildDriver creates a DatabaseDriver from a data source record and decrypted password.
func (s *DataSourceService) buildDriver(ds repository.DataSource, pwd string) (drivers.DatabaseDriver, *sql.DB, error) {
	cfg := drivers.DriverConfig{
		Host:           ds.Host,
		Port:           ds.Port,
		Username:       ds.Username,
		Password:       pwd,
		Database:       ds.Database,
		MaxConnections: 10,
	}

	// Parse Oracle-specific extra config
	if ds.Type == "oracle" && ds.ExtraConfig != "" {
		var extra map[string]string
		if json.Unmarshal([]byte(ds.ExtraConfig), &extra) == nil {
			if v, ok := extra["oracle_service"]; ok {
				cfg.OracleService = v
			}
			if v, ok := extra["connect_mode"]; ok {
				cfg.OracleConnectMode = v
			}
			if v, ok := extra["role"]; ok {
				cfg.OracleRole = v
			}
		}
	}

	return drivers.CreateDriver(ds.Type, cfg)
}

// connectDriverForDB creates a DatabaseDriver connected to a specific database.
// Used when the tree has database → schema hierarchy (PG/MSSQL) and the
// schema is within a different database than the data source's default.
func (s *DataSourceService) connectDriverForDB(id, database string) (drivers.DatabaseDriver, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found")
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password")
	}

	if !isSupportedDBType(ds.Type) {
		return nil, fmt.Errorf("unsupported database type: %s", ds.Type)
	}

	// Override the database in the config
	cfg := drivers.DriverConfig{
		Host:           ds.Host,
		Port:           ds.Port,
		Username:       ds.Username,
		Password:       pwd,
		Database:       database,
		MaxConnections: 10,
	}

	// Parse Oracle-specific extra config
	if ds.Type == "oracle" && ds.ExtraConfig != "" {
		var extra map[string]string
		if json.Unmarshal([]byte(ds.ExtraConfig), &extra) == nil {
			if v, ok := extra["oracle_service"]; ok {
				cfg.OracleService = v
			}
			if v, ok := extra["connect_mode"]; ok {
				cfg.OracleConnectMode = v
			}
			if v, ok := extra["role"]; ok {
				cfg.OracleRole = v
			}
		}
	}

	driver, _, err := drivers.CreateDriver(ds.Type, cfg)
	return driver, err
}

// openDBConnection is a thin wrapper for backward compatibility
func (s *DataSourceService) openDBConnection(ds repository.DataSource, pwd string) (*sql.DB, error) {
	return openDBConn(ds, pwd)
}

// ListSchemaNames returns only the schema/database names for a data source (no tables/views)
func (s *DataSourceService) ListSchemaNames(id string) ([]string, error) {
	driver, ds, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	// For MySQL/MariaDB/OceanBase with a specific database configured,
	// return just that database (it's always the only visible "schema")
	if (ds.Type == "mysql" || ds.Type == "mariadb" || ds.Type == "oceanbase") && ds.Database != "" {
		return []string{ds.Database}, nil
	}

	return driver.ListSchemaNames()
}

// GetSchemaObjects returns tables and views for a single schema (no columns)
func (s *DataSourceService) GetSchemaObjects(id string, schemaName string) (*drivers.SchemaInfo, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListObjects(schemaName)
}

// ExportItem represents a data source in the export file
type ExportItem struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Database    string `json:"database,omitempty"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	SSLMode     string `json:"ssl_mode,omitempty"`
	ExtraConfig string `json:"extra_config,omitempty"`
}

// ImportResult summarizes the outcome of an import operation
type ImportResult struct {
	Total   int      `json:"total"`
	Success int      `json:"success"`
	Skip    int      `json:"skip"`
	Errors  []string `json:"errors"`
}

// ExportDataSources exports all data sources for a tenant, with passwords encrypted by the export password
func (s *DataSourceService) ExportDataSources(tenantID, exportPassword string) ([]ExportItem, error) {
	var dataSources []repository.DataSource
	if err := s.db.Where("tenant_id = ?", tenantID).Order("created_at DESC").Find(&dataSources).Error; err != nil {
		return nil, err
	}

	items := make([]ExportItem, 0, len(dataSources))
	for _, ds := range dataSources {
		plainPwd, err := cryptoPkg.Decrypt(ds.Password)
		if err != nil {
			continue
		}
		encryptedPwd, err := cryptoPkg.EncryptWithPassword(plainPwd, exportPassword)
		if err != nil {
			continue
		}
		items = append(items, ExportItem{
			Name:        ds.Name,
			Type:        ds.Type,
			Host:        ds.Host,
			Port:        ds.Port,
			Database:    ds.Database,
			Username:    ds.Username,
			Password:    encryptedPwd,
			SSLMode:     ds.SSLMode,
			ExtraConfig: ds.ExtraConfig,
		})
	}
	return items, nil
}

// ImportDataSources imports data sources, decrypting passwords with the export password and storing with system encryption
func (s *DataSourceService) ImportDataSources(tenantID, createdBy string, items []ExportItem, exportPassword string) (*ImportResult, error) {
	result := &ImportResult{Total: len(items)}

	for i, item := range items {
		if item.Name == "" || item.Type == "" || item.Host == "" || item.Port == 0 || item.Username == "" {
			result.Skip++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 条: 缺少必填字段 (name/type/host/port/username)", i+1))
			continue
		}

		plainPwd, err := cryptoPkg.DecryptWithPassword(item.Password, exportPassword)
		if err != nil {
			result.Skip++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 条 (%s): 密码解密失败，导出密码可能不正确", i+1, item.Name))
			continue
		}

		input := CreateDSInput{
			Name:        item.Name,
			Type:        item.Type,
			Host:        item.Host,
			Port:        item.Port,
			Database:    item.Database,
			Username:    item.Username,
			Password:    plainPwd,
			SSLMode:     item.SSLMode,
			ExtraConfig: item.ExtraConfig,
			TenantID:    tenantID,
			CreatedBy:   createdBy,
		}
		if _, err := s.Create(input); err != nil {
			result.Skip++
			result.Errors = append(result.Errors, fmt.Sprintf("第 %d 条 (%s): %s", i+1, item.Name, err.Error()))
		} else {
			result.Success++
		}
	}
	return result, nil
}

// GetSchema connects to the user data source and returns schema metadata
func (s *DataSourceService) GetSchema(id string) ([]drivers.SchemaInfo, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListSchemas()
}

// SchemaDetailList returns a summary list of schemas with table/view counts (no nested objects)
func (s *DataSourceService) SchemaDetailList(id string) ([]drivers.SchemaDetailItem, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListSchemaDetail()
}

// TableList returns tables and views for a given schema with metadata
func (s *DataSourceService) TableList(id string, schemaName string, database string) ([]drivers.TableListItem, error) {
	if database != "" {
		driver, err := s.connectDriverForDB(id, database)
		if err != nil {
			return nil, err
		}
		defer driver.Close()
		return driver.ListTables(schemaName)
	}

	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListTables(schemaName)
}

// GetDDL retrieves the CREATE TABLE/VIEW DDL for the given table
func (s *DataSourceService) GetDDL(id string, schemaName, tableName string) (string, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return "", err
	}
	defer driver.Close()

	return driver.GetDDL(schemaName, tableName)
}

// ─── Tree Metadata ─────────────────────────────────────────────────────────

// withStaticDriver executes a function on a static (nil) driver of the given type.
// This is used for operations that return static metadata and don't require a connection.
func withStaticDriver[T any](dbType string, fn func(drivers.DatabaseDriver) T) T {
	switch dbType {
	case "mysql", "mariadb", "oceanbase":
		return fn(&drivers.MySQLDriver{})
	case "postgres", "postgresql":
		return fn(&drivers.PostgresDriver{})
	case "oracle":
		return fn(&drivers.OracleDriver{})
	case "sqlserver":
		return fn(&drivers.SQLServerDriver{})
	default:
		return fn(&drivers.MySQLDriver{})
	}
}

// getTreeMetadataForDB returns the tree metadata for a given database type (no connection needed).
func getTreeMetadataForDB(dbType string) drivers.TreeMetadata {
	return withStaticDriver(dbType, func(d drivers.DatabaseDriver) drivers.TreeMetadata {
		return d.GetTreeMetadata()
	})
}

// resolveContextForDB resolves a schema-like string into the correct DatabaseContext for a given DB type.
func resolveContextForDB(dbType, arg string) drivers.DatabaseContext {
	return withStaticDriver(dbType, func(d drivers.DatabaseDriver) drivers.DatabaseContext {
		return d.ResolveContext(arg)
	})
}

// getColumnTypesForDB returns the column type list for a given database type.
func getColumnTypesForDB(dbType string) []drivers.ColumnTypeInfo {
	return withStaticDriver(dbType, func(d drivers.DatabaseDriver) []drivers.ColumnTypeInfo {
		return d.GetColumnTypes()
	})
}

// GetColumnTypes returns column type definitions for a given data source
func (s *DataSourceService) GetColumnTypes(dsID string) ([]drivers.ColumnTypeInfo, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		// If data source not found, return types based on the string type
		return getColumnTypesForDB(dsID), nil
	}
	return getColumnTypesForDB(ds.Type), nil
}

// getIndexTypesForDB returns the index type list for a given database type.
func getIndexTypesForDB(dbType string) []drivers.IndexTypeInfo {
	return withStaticDriver(dbType, func(d drivers.DatabaseDriver) []drivers.IndexTypeInfo {
		return d.GetIndexTypes()
	})
}

// GetIndexTypes returns index type definitions for a given data source
func (s *DataSourceService) GetIndexTypes(dsID string) ([]drivers.IndexTypeInfo, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return getIndexTypesForDB(dsID), nil
	}
	return getIndexTypesForDB(ds.Type), nil
}

// GetTreeMetadata returns hierarchy metadata for the given data source.
func (s *DataSourceService) GetTreeMetadata(id string) (drivers.TreeMetadata, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return drivers.TreeMetadata{}, err
	}
	return getTreeMetadataForDB(ds.Type), nil
}

// ListDatabases returns database list with metadata
func (s *DataSourceService) ListDatabases(id string) ([]drivers.DatabaseInfo, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListDatabases()
}

// ResolveContext resolves a schema-like string into the correct DatabaseContext for the given data source.
func (s *DataSourceService) ResolveContext(id, arg string) (drivers.DatabaseContext, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", id).First(&ds).Error; err != nil {
		return drivers.DatabaseContext{}, err
	}
	return resolveContextForDB(ds.Type, arg), nil
}

// ListDatabaseSchemas returns schema names within a specific database (PG/MSSQL).
func (s *DataSourceService) ListDatabaseSchemas(id, database string) ([]string, error) {
	driver, _, err := s.connectDriver(id)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	return driver.ListDatabaseSchemas(database)
}


