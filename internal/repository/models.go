package repository

import (
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// User represents a system user
type User struct {
	ID        string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Username  string     `gorm:"type:varchar(50);uniqueIndex;not null" json:"username"`
	Password  string     `gorm:"type:varchar(255);not null" json:"-"`
	Email     string     `gorm:"type:varchar(100)" json:"email"`
	Role      string     `gorm:"type:varchar(20);not null;default:viewer" json:"role"` // admin, operator, developer, viewer
	Status    int        `gorm:"type:tinyint;default:1" json:"status"`                // 0=disabled, 1=enabled
	TenantID  string     `gorm:"type:varchar(36);index" json:"tenant_id"`
	LastLogin *time.Time `json:"last_login"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (User) TableName() string {
	return "users"
}

// DataSource represents a database connection configuration
type DataSource struct {
	ID          string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name        string    `gorm:"type:varchar(100);not null" json:"name"`
	Type        string    `gorm:"type:varchar(20);not null" json:"type"` // mysql, postgres, oracle, sqlserver, sqlite
	Host        string    `gorm:"type:varchar(100);not null" json:"host"`
	Port        int       `gorm:"type:int;not null" json:"port"`
	Database    string    `gorm:"type:varchar(100)" json:"database"`
	Username    string    `gorm:"type:varchar(50);not null" json:"username"`
	Password    string    `gorm:"type:text;not null" json:"-"` // AES-GCM encrypted, never returned in JSON
	SSLMode     string    `gorm:"type:varchar(20)" json:"ssl_mode"`
	ExtraConfig string    `gorm:"type:text" json:"extra_config"`
	Tags        string    `gorm:"type:varchar(200);default:''" json:"tags"` // comma-separated: "database_management,data_query"
	Env         string    `gorm:"type:varchar(20);default:'dev'" json:"env"` // dev/test/prod
	// IsSystem 标记系统数据源（指向系统自身底层数据库）；系统数据源不可删除、type 不可变更。
	// 显式 tinyint(1) 而非 boolean：tinyint(1) 在 sqlite/mysql/postgres 上语义一致。
	IsSystem  bool      `gorm:"type:tinyint(1);not null;default:0;index" json:"is_system"`
	TenantID  string    `gorm:"type:varchar(36);index" json:"tenant_id"`
	CreatedBy string    `gorm:"type:varchar(36)" json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (DataSource) TableName() string {
	return "data_sources"
}

// SyncTask represents a data synchronization task
type SyncTask struct {
	ID           string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name         string     `gorm:"type:varchar(100);not null" json:"name"`
	SourceDS     string     `gorm:"type:varchar(36);not null" json:"source_ds"`
	TargetDS     string     `gorm:"type:varchar(36);not null" json:"target_ds"`
	SourceTable  string     `gorm:"type:varchar(100);not null" json:"source_table"`
	TargetTable  string     `gorm:"type:varchar(100);not null" json:"target_table"`
	SyncMode     string     `gorm:"type:varchar(20);not null;default:full" json:"sync_mode"` // full, incremental, ddl
	Status       string     `gorm:"type:varchar(20);not null;default:pending" json:"status"` // pending, running, completed, failed, stopped
	Progress     float64    `gorm:"default:0" json:"progress"`                               // 0-100
	LastSyncTime *time.Time `json:"last_sync_time"`
	ErrorMessage string     `gorm:"type:text" json:"error_message"`
	TenantID     string     `gorm:"type:varchar(36);index" json:"tenant_id"`
	CreatedBy    string     `gorm:"type:varchar(36)" json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (SyncTask) TableName() string {
	return "sync_tasks"
}

// AuditLog records all user operations
type AuditLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    string    `gorm:"type:varchar(36);index:idx_user_time,priority:1" json:"user_id"`
	TenantID  string    `gorm:"type:varchar(36);index:idx_tenant_time_module,priority:1" json:"tenant_id"`
	Module    string    `gorm:"type:varchar(50);default:'';index:idx_tenant_time_module,priority:3" json:"module"`
	Operation string    `gorm:"type:varchar(50);not null;index:idx_tenant_time_module,priority:4" json:"operation"`
	TargetID  string    `gorm:"type:varchar(64);default:''" json:"target_id"`
	Result    string    `gorm:"type:varchar(20);default:''" json:"result"`
	Details   string    `gorm:"type:text" json:"details"`
	IP        string    `gorm:"type:varchar(45)" json:"ip"`
	UserAgent string    `gorm:"type:text" json:"user_agent"`
	Username  string    `gorm:"type:varchar(100);default:''" json:"username"`
	CreatedAt time.Time `gorm:"index:idx_tenant_time_module,priority:2;index:idx_user_time,priority:2" json:"created_at"`
}

func (AuditLog) TableName() string {
	return "audit_logs"
}

// AuditPurgeLog records each audit purge execution
type AuditPurgeLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Deleted   int64     `json:"deleted"`
	Duration  int64     `json:"duration_ms"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (AuditPurgeLog) TableName() string {
	return "audit_purge_logs"
}

// Setting stores system configuration
type Setting struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Key       string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"key"`
	Value     string    `gorm:"type:text;not null" json:"value"`
	Category  string    `gorm:"type:varchar(50);not null" json:"category"` // ai, sync, system, security
	IsSecret  bool      `gorm:"type:boolean;default:false" json:"is_secret"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Setting) TableName() string {
	return "settings"
}

// LockRecord tracks distributed lock ownership
type LockRecord struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	LockKey   string    `gorm:"type:varchar(255);uniqueIndex;not null" json:"lock_key"`
	Owner     string    `gorm:"type:varchar(36);not null" json:"owner"`
	Level     string    `gorm:"type:varchar(20);not null" json:"level"` // database, schema, table
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func (LockRecord) TableName() string {
	return "lock_records"
}

// ─── BeforeCreate hooks ─────────────────────────────────────────────────────

func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == "" {
		u.ID = generateUUID()
	}
	u.CreatedAt = time.Now()
	u.UpdatedAt = time.Now()
	return nil
}

func (ds *DataSource) BeforeCreate(tx *gorm.DB) error {
	if ds.ID == "" {
		ds.ID = generateUUID()
	}
	ds.CreatedAt = time.Now()
	ds.UpdatedAt = time.Now()
	return nil
}

func (st *SyncTask) BeforeCreate(tx *gorm.DB) error {
	if st.ID == "" {
		st.ID = generateUUID()
	}
	st.CreatedAt = time.Now()
	st.UpdatedAt = time.Now()
	return nil
}

func (al *AuditLog) BeforeCreate(tx *gorm.DB) error {
	al.CreatedAt = time.Now()
	return nil
}

func (s *Setting) BeforeCreate(tx *gorm.DB) error {
	s.CreatedAt = time.Now()
	s.UpdatedAt = time.Now()
	return nil
}

func (l *LockRecord) BeforeCreate(tx *gorm.DB) error {
	l.CreatedAt = time.Now()
	return nil
}

// ─── BeforeUpdate hooks ─────────────────────────────────────────────────────

func (u *User) BeforeUpdate(tx *gorm.DB) error {
	u.UpdatedAt = time.Now()
	return nil
}

func (ds *DataSource) BeforeUpdate(tx *gorm.DB) error {
	ds.UpdatedAt = time.Now()
	return nil
}

func (st *SyncTask) BeforeUpdate(tx *gorm.DB) error {
	st.UpdatedAt = time.Now()
	return nil
}

func (s *Setting) BeforeUpdate(tx *gorm.DB) error {
	s.UpdatedAt = time.Now()
	return nil
}

// ─── Storage Models ─────────────────────────────────────────────────────────

type StorageInstance struct {
	ID         string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name       string    `gorm:"type:varchar(100);not null" json:"name"`
	Code       string    `gorm:"type:varchar(50);uniqueIndex;not null" json:"code"`
	Backend    string    `gorm:"type:varchar(20);not null;default:local" json:"backend"`
	Enabled    bool      `gorm:"type:tinyint(1);default:1" json:"enabled"`
	ConfigJSON string    `gorm:"type:text;not null" json:"config_json"`
	SortOrder  int       `gorm:"type:int;default:0" json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (StorageInstance) TableName() string { return "storage_instances" }

type StorageBinding struct {
	ID          string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	ModuleCode  string    `gorm:"type:varchar(50);uniqueIndex;not null" json:"module_code"`
	ModuleName  string    `gorm:"type:varchar(100);not null" json:"module_name"`
	ProfileCode string    `gorm:"type:varchar(50);not null" json:"profile_code"`
	BasePath    string    `gorm:"type:varchar(500);not null;default:''" json:"base_path"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (StorageBinding) TableName() string { return "storage_bindings" }

// ─── Import/Export Task Models ──────────────────────────────────────────────

type Task struct {
	ID           string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name         string    `gorm:"type:varchar(200);not null" json:"name"`
	TaskType     string    `gorm:"type:varchar(20);not null" json:"task_type"`
	DataSourceID string    `gorm:"type:varchar(36);not null" json:"data_source_id"`
	DatabaseName string    `gorm:"type:varchar(100)" json:"database_name"`
	SchemaName   string    `gorm:"type:varchar(100)" json:"schema_name"`
	Config       string    `gorm:"type:text;not null" json:"config"`
	Status       string    `gorm:"-" json:"status,omitempty"`
	TenantID     string    `gorm:"type:varchar(36)" json:"tenant_id"`
	CreatedBy    string    `gorm:"type:varchar(36)" json:"created_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (Task) TableName() string { return "tasks" }

func (t *Task) BeforeCreate(tx *gorm.DB) error {
	if t.ID == "" {
		t.ID = generateUUID()
	}
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	return nil
}

func (t *Task) BeforeUpdate(tx *gorm.DB) error {
	t.UpdatedAt = time.Now()
	return nil
}

type TaskExecution struct {
	ID             string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	TaskID         string     `gorm:"type:varchar(36);not null;index" json:"task_id"`
	RunNumber      int        `gorm:"default:1" json:"run_number"`
	Status         string     `gorm:"type:varchar(20);default:pending" json:"status"`
	Progress       int        `gorm:"default:0" json:"progress"`
	StartedAt      *time.Time `json:"started_at"`
	FinishedAt     *time.Time `json:"finished_at"`
	ResultFilePath string     `gorm:"type:varchar(500)" json:"result_file_path"`
	ResultFileName string     `gorm:"type:varchar(200)" json:"result_file_name"`
	ResultFileSize int64      `gorm:"default:0" json:"result_file_size"`
	LogFilePath    string     `gorm:"type:varchar(500)" json:"log_file_path"`
	LogText        string     `gorm:"type:longtext" json:"log_text"`
	ErrorMsg       string     `gorm:"type:text" json:"error_msg"`
	CreatedAt      time.Time  `json:"created_at"`
}

func (TaskExecution) TableName() string { return "task_executions" }

func (e *TaskExecution) BeforeCreate(tx *gorm.DB) error {
	if e.ID == "" {
		e.ID = generateUUID()
	}
	e.CreatedAt = time.Now()
	return nil
}

type ExportConfig struct {
	ExportScope     string   `json:"export_scope"`
	ExportContent   string   `json:"export_content"`
	ExportFormat    string   `json:"export_format"`
	ExportBatchSize int      `json:"export_batch_size"`
	ExportTables    []string `json:"export_tables"`
	StorageProfile  string   `json:"storage_profile"`
	StoragePath     string   `json:"storage_path"`
}

type ImportConfig struct {
	ImportSource    string   `json:"import_source"`
	ImportContent   string   `json:"import_content"`
	ImportStrategy  string   `json:"import_strategy"`
	SkipSafetyCheck bool     `json:"skip_safety_check"`
	ImportFileName  string   `json:"import_file_name"`
	ImportFilePath  string   `json:"import_file_path"`
	CompressedImport bool   `json:"compressed_import"`
	StorageProfile  string   `json:"storage_profile"`
	SourceDSID     string   `json:"source_ds_id"`
	SourceDatabase string   `json:"source_database"`
	SourceSchema   string   `json:"source_schema"`
	SourceTables   []string `json:"source_tables"`
	TargetSchema   string   `json:"target_schema"`
	TargetDatabase string   `json:"target_database"`
	TargetTable    string   `json:"target_table"`
}

type TaskConfig struct {
	Export *ExportConfig `json:"export,omitempty"`
	Import *ImportConfig `json:"import,omitempty"`
}

func (t *Task) GetExportConfig() (*ExportConfig, error) {
	var cfg TaskConfig
	if t.Config == "" {
		return nil, fmt.Errorf("task config is empty")
	}
	if err := json.Unmarshal([]byte(t.Config), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse task config: %w", err)
	}
	if cfg.Export == nil {
		return nil, fmt.Errorf("export config not found in task")
	}
	return cfg.Export, nil
}

func (t *Task) GetImportConfig() (*ImportConfig, error) {
	var cfg TaskConfig
	if t.Config == "" {
		return nil, fmt.Errorf("task config is empty")
	}
	if err := json.Unmarshal([]byte(t.Config), &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse task config: %w", err)
	}
	if cfg.Import == nil {
		return nil, fmt.Errorf("import config not found in task")
	}
	return cfg.Import, nil
}

func (t *Task) SetExportConfig(cfg *ExportConfig) error {
	taskCfg := TaskConfig{Export: cfg}
	b, err := json.Marshal(taskCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal export config: %w", err)
	}
	t.Config = string(b)
	return nil
}

func (t *Task) SetImportConfig(cfg *ImportConfig) error {
	taskCfg := TaskConfig{Import: cfg}
	b, err := json.Marshal(taskCfg)
	if err != nil {
		return fmt.Errorf("failed to marshal import config: %w", err)
	}
	t.Config = string(b)
	return nil
}

// ─── Mask Rule Config ───────────────────────────────────────────────────────

type MaskRuleConfig struct {
	ID           string    `gorm:"primaryKey" json:"id"`
	DataSourceID string    `json:"data_source_id"`
	Database     string    `json:"database"`
	Schema       string    `json:"schema"`
	TablePattern string    `json:"table_name"`
	ColumnName   string    `json:"column_name"`
	MaskFunc     string    `json:"mask_func"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
}

func (MaskRuleConfig) TableName() string { return "mask_rule_configs" }

// ─── AI Skill Models ────────────────────────────────────────────────────────

type AISkill struct {
	ID             string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Slug           string    `gorm:"type:varchar(100);index" json:"slug"`
	Name           string    `gorm:"type:varchar(100);not null" json:"name"`
	Description    string    `gorm:"type:text" json:"description"`
	Icon           string    `gorm:"type:varchar(50);default:thunderbolt" json:"icon"`
	Category       string    `gorm:"type:varchar(50);default:general" json:"category"`
	PromptTemplate string    `gorm:"type:text;not null" json:"prompt_template"`
	SystemPrompt   string    `gorm:"type:text" json:"system_prompt"`
	Tools          string    `gorm:"type:text" json:"tools"`
	ScriptFiles    string    `gorm:"type:text" json:"script_files"`
	Resources      string    `gorm:"type:text" json:"resources"`
	InputVars      string    `gorm:"type:text" json:"input_vars"`
	OutputFormat   string    `gorm:"type:varchar(20);default:markdown" json:"output_format"`
	ScriptSandbox  string    `gorm:"type:text" json:"script_sandbox"`
	IsBuiltin      bool      `gorm:"default:false" json:"is_builtin"`
	IsActive       bool      `gorm:"default:true" json:"is_active"`
	Version        int       `gorm:"default:1" json:"version"`
	AuthorID       string    `gorm:"type:varchar(36)" json:"author_id"`
	TenantID       string    `gorm:"type:varchar(36);index" json:"tenant_id"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (AISkill) TableName() string { return "ai_skills" }

type AISkillVersion struct {
	ID             string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	SkillID        string    `gorm:"type:varchar(36);not null;index" json:"skill_id"`
	Version        int       `gorm:"not null" json:"version"`
	Name           string    `gorm:"type:varchar(100)" json:"name"`
	Description    string    `gorm:"type:text" json:"description"`
	PromptTemplate string    `gorm:"type:text" json:"prompt_template"`
	SystemPrompt   string    `gorm:"type:text" json:"system_prompt"`
	Tools          string    `gorm:"type:text" json:"tools"`
	ScriptFiles    string    `gorm:"type:text" json:"script_files"`
	Resources      string    `gorm:"type:text" json:"resources"`
	AuthorID       string    `gorm:"type:varchar(36)" json:"author_id"`
	CreatedAt      time.Time `json:"created_at"`
}

func (AISkillVersion) TableName() string { return "ai_skill_versions" }

// ─── Agent Models ───────────────────────────────────────────────────────────

type Agent struct {
	ID           string         `gorm:"primaryKey;type:varchar(36)" json:"id"`
	Name         string         `gorm:"type:varchar(100);not null" json:"name"`
	Description  string         `gorm:"type:text" json:"description"`
	Icon         string         `gorm:"type:varchar(50);default:robot" json:"icon"`
	SystemPrompt string         `gorm:"type:text" json:"system_prompt"`
	Model        string         `gorm:"type:varchar(50);default:deepseek-v4-flash" json:"model"`
	Temperature  float64        `gorm:"type:double;default:0.7" json:"temperature"`
	MaxTokens    int            `gorm:"default:4096" json:"max_tokens"`
	Tools        string         `gorm:"type:text" json:"tools"`
	Skills       string         `gorm:"type:text" json:"skills"`
	SubAgents    string         `gorm:"type:text" json:"sub_agents"`
	IsDefault    bool           `gorm:"default:false" json:"is_default"`
	IsBuiltin    bool           `gorm:"default:false" json:"is_builtin"`
	OutputFormat string         `gorm:"type:varchar(20);default:markdown" json:"output_format"`
	ResultFormat string         `gorm:"type:text" json:"result_format"`
	Scope        string         `gorm:"type:varchar(20);default:user" json:"scope"`
	UserID       string         `gorm:"index;type:varchar(36)" json:"user_id"`
	TenantID     string         `gorm:"index;type:varchar(36)" json:"tenant_id"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (Agent) TableName() string { return "agents" }

type AgentDataSource struct {
	ID         string `gorm:"primaryKey;type:varchar(36)" json:"id"`
	AgentID    string `gorm:"index;type:varchar(36);not null" json:"agent_id"`
	DSID       string `gorm:"type:varchar(36);not null" json:"ds_id"`
	Database   string `gorm:"type:varchar(100)" json:"database"`
	SchemaName string `gorm:"type:varchar(100)" json:"schema"`
	Permission string `gorm:"type:varchar(20);default:read" json:"permission"`
}

func (AgentDataSource) TableName() string { return "agent_datasources" }

// ─── Semantic Cube Model ────────────────────────────────────────────────────

type SemanticCube struct {
	ID             string    `gorm:"type:varchar(36);primaryKey" json:"id"`
	Name           string    `gorm:"type:varchar(100);uniqueIndex;not null" json:"name"`
	DisplayName    string    `gorm:"type:varchar(200);not null" json:"display_name"`
	Description    string    `gorm:"type:text" json:"description"`
	DataSourceID   string    `gorm:"type:varchar(36);not null" json:"data_source_id"`
	Database       string    `gorm:"type:varchar(100);default:''" json:"database"`
	SchemaName     string    `gorm:"type:varchar(100);default:''" json:"schema_name"`
	SQLTable       string    `gorm:"type:varchar(200);not null" json:"sql_table"`
	SQLQuery       string    `gorm:"type:text" json:"sql_query"`
	MeasuresJSON   string    `gorm:"type:longtext" json:"measures"`
	DimensionsJSON string    `gorm:"type:longtext" json:"dimensions"`
	JoinsJSON      string    `gorm:"type:longtext" json:"joins"`
	PreAggsJSON    string    `gorm:"type:longtext" json:"pre_aggregations"`
	Version        int       `gorm:"type:int;default:1" json:"version"`
	UpstreamTables string    `gorm:"type:text" json:"upstream_tables"`
	TenantID       string    `gorm:"type:varchar(36);index" json:"tenant_id"`
	CreatedBy      string    `gorm:"type:varchar(36)" json:"created_by"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (SemanticCube) TableName() string { return "semantic_cubes" }

func (sc *SemanticCube) BeforeCreate(tx *gorm.DB) error {
	if sc.ID == "" {
		sc.ID = generateUUID()
	}
	sc.CreatedAt = time.Now()
	sc.UpdatedAt = time.Now()
	return nil
}

func (sc *SemanticCube) BeforeUpdate(tx *gorm.DB) error {
	sc.UpdatedAt = time.Now()
	return nil
}
