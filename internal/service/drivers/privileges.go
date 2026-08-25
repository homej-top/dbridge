package drivers

// PrivilegeEntry represents a single privilege grant on an object
type PrivilegeEntry struct {
	Database   string   `json:"database"`
	Schema     string   `json:"schema,omitempty"`
	ObjectType string   `json:"object_type"` 
	ObjectName string   `json:"object_name"`
	Privileges []string `json:"privileges"`
	Grantable  bool     `json:"grantable"`
	IsSystem   bool     `json:"is_system"`
}

// ParentRoleInfo describes a direct parent role of the current role
type ParentRoleInfo struct {
	Name           string `json:"name"`
	IsSuperuser    bool   `json:"is_superuser"`
	CanLogin       bool   `json:"can_login"`
	CanCreatedb    bool   `json:"can_createdb"`
	CanCreaterole  bool   `json:"can_createrole"`
	CanReplication bool   `json:"can_replication"`
	CanBypassrls   bool   `json:"can_bypassrls"`
	AdminOption    bool   `json:"admin_option"`
}

// MemberRoleInfo describes a direct child role (member) of the current role
type MemberRoleInfo struct {
	Name           string `json:"name"`
	IsSuperuser    bool   `json:"is_superuser"`
	CanLogin       bool   `json:"can_login"`
	CanCreatedb    bool   `json:"can_createdb"`
	CanCreaterole  bool   `json:"can_createrole"`
	CanReplication bool   `json:"can_replication"`
	CanBypassrls   bool   `json:"can_bypassrls"`
	AdminOption    bool   `json:"admin_option"`
}

// PrivilegeDelta represents a change to apply
type PrivilegeDelta struct {
	Database   string   `json:"database"`
	ObjectType string   `json:"object_type"`
	ObjectName string   `json:"object_name"`
	Grant      []string `json:"grant"`
	Revoke     []string `json:"revoke"`
	DryRun     bool     `json:"-"`
}

// ChangeResult is the outcome of applying privilege changes
type ChangeResult struct {
	Statements []string `json:"statements"`
	Executed   int      `json:"executed"`
	Errors     []string `json:"errors,omitempty"`
}

// ─── MSSQL Login ────────────────────────────────────────────────────────

// MSSQLLogin 实例级登录名
type MSSQLLogin struct {
	Name            string   `json:"name"`
	Type            string   `json:"type"`             // "SQL Login" | "Windows Login" | "Windows Group"
	IsDisabled      bool     `json:"is_disabled"`
	DefaultDatabase string   `json:"default_database"`
	DefaultLanguage string   `json:"default_language"`
	IsSysadmin      bool     `json:"is_sysadmin"`
	ServerRoles     []string `json:"server_roles"`
	MappedDatabases []string `json:"mapped_databases"`
	CreatedAt       string   `json:"created_at"`
	ModifiedAt      string   `json:"modified_at"`
}

// CreateLoginRequest 创建 Login 的请求体
type CreateLoginRequest struct {
	Name           string          `json:"name" binding:"required"`
	Password       string          `json:"password"`
	LoginType      string          `json:"login_type"`       // "SQL" | "Windows"
	DefaultDatabase string         `json:"default_database"`
	DefaultLanguage string         `json:"default_language"`
	ServerRoles    []string        `json:"server_roles"`
	EnforcePolicy  bool            `json:"enforce_policy"`
	DBUserMappings []DBUserMapping `json:"db_user_mappings"`
}

// DBUserMapping 单库用户映射配置
type DBUserMapping struct {
	Database      string   `json:"database"`
	UserName      string   `json:"user_name"`
	DefaultSchema string   `json:"default_schema"`
	DatabaseRoles []string `json:"database_roles"`
}

// AlterLoginRequest 修改 Login 属性的请求体
type AlterLoginRequest struct {
	NewPassword     *string `json:"new_password,omitempty"`
	Disable         *bool   `json:"disable,omitempty"`
	DefaultDatabase *string `json:"default_database,omitempty"`
	Unlock          *bool   `json:"unlock,omitempty"`
	RenameTo        *string `json:"rename_to,omitempty"`
}

// LoginDetail 登录名详情
type LoginDetail struct {
	Login          MSSQLLogin             `json:"login"`
	ServerRoles    []ServerRoleInfo       `json:"server_roles"`
	DBUserMappings []DBUserMappingDetail  `json:"db_user_mappings"`
}

// ServerRoleInfo 服务器角色信息
type ServerRoleInfo struct {
	Name        string `json:"name"`
	IsFixedRole bool   `json:"is_fixed_role"`
}

// DBUserMappingDetail 数据库用户映射详情
type DBUserMappingDetail struct {
	Database      string   `json:"database"`
	UserName      string   `json:"user_name"`
	DefaultSchema string   `json:"default_schema"`
	DBRoles       []string `json:"db_roles"`
	IsOrphaned    bool     `json:"is_orphaned"`
}

// DropLoginResult 删除 Login 的结果
type DropLoginResult struct {
	LoginDropped bool     `json:"login_dropped"`
	CascadeUsers bool     `json:"cascade_users"`
	DroppedUsers []string `json:"dropped_users"`
	Warnings     []string `json:"warnings,omitempty"`
}

// ─── MSSQL Database User ────────────────────────────────────────────────

// MSSQLDatabaseUser 数据库用户
type MSSQLDatabaseUser struct {
	Name               string   `json:"name"`
	LoginName          string   `json:"login_name"`
	Type               string   `json:"type"` // "SQL User" | "Windows User" | "Contained"
	DefaultSchema      string   `json:"default_schema"`
	DatabaseRoles      []string `json:"database_roles"`
	IsOrphaned         bool     `json:"is_orphaned"`
	IsSystem           bool     `json:"is_system"`
	HasSchemaOwnership bool     `json:"has_schema_ownership"`
	CreatedAt          string   `json:"created_at"`
}

// CreateDBUserRequest 创建数据库用户的请求体
type CreateDBUserRequest struct {
	UserName      string   `json:"user_name" binding:"required"`
	LoginName     string   `json:"login_name"`
	DefaultSchema string   `json:"default_schema"`
	DatabaseRoles []string `json:"database_roles"`
	IsContained   bool     `json:"is_contained"`
}

// ─── MSSQL Orphaned Users ──────────────────────────────────────────────

// OrphanedUser 孤用用户信息
type OrphanedUser struct {
	UserName     string `json:"user_name"`
	Database     string `json:"database"`
	UserSID      string `json:"user_sid"`
	MatchedLogin string `json:"matched_login,omitempty"`
	SuggestedFix string `json:"suggested_fix"`
}

// ─── MSSQL Effective Permissions ───────────────────────────────────────

// EffectivePermission 有效权限计算结果
type EffectivePermission struct {
	PrincipalName string             `json:"principal_name"`
	ObjectType    string             `json:"object_type"`
	ObjectName    string             `json:"object_name"`
	Permissions   []PermissionDetail `json:"permissions"`
	Source        string             `json:"source"`
}

// PermissionDetail 单条权限详情
type PermissionDetail struct {
	Permission string `json:"permission"`
	State      string `json:"state"`
	Source     string `json:"source"`
	Grantable  bool   `json:"grantable"`
}

// ─── MSSQL Guest Compliance ────────────────────────────────────────────

// GuestStatus guest 用户状态
type GuestStatus struct {
	Database   string `json:"database"`
	HasGuest   bool   `json:"has_guest"`
	IsDisabled bool   `json:"is_disabled"`
	Warning    string `json:"warning,omitempty"`
}

// ─── Permission Presets ────────────────────────────────────────────────

// CommonGrantPresets maps preset names to permission lists
var CommonGrantPresets = map[string][]string{
	"只读 (SELECT)":         {"SELECT"},
	"读写 (CRUD)":           {"SELECT", "INSERT", "UPDATE", "DELETE"},
	"只读+DML+EXEC":         {"SELECT", "INSERT", "UPDATE", "DELETE", "EXECUTE"},
	"DDL (结构管理)":        {"SELECT", "INSERT", "UPDATE", "DELETE", "ALTER", "CREATE TABLE", "CREATE VIEW", "CREATE PROCEDURE"},
	"完全控制 (CONTROL)":    {"CONTROL"},
}

// DBUserInfo holds database+username pair for login-user mappings
type DBUserInfo struct {
	Database string `json:"database"`
	UserName string `json:"user_name"`
}

// SchemaOwnership holds schema ownership info for drop-user pre-check
type SchemaOwnership struct {
	Database   string `json:"database"`
	SchemaName string `json:"schema_name"`
	OwnerName  string `json:"owner_name"`
}
