package repository

import (
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
