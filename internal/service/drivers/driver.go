package drivers

import (
	"context"
	"database/sql"
	"encoding/json"
	"unicode/utf16"
	"fmt"
	"strings"
	"time"
)

// ─── Shared Types ──────────────────────────────────────────────────────────

// TreeMetadata describes the hierarchical organization of a database type.
// Used by the frontend to render the correct tree structure, labels, and actions.
type TreeMetadata struct {
	DBType       string            `json:"db_type"`                 // mysql, postgres, oracle, sqlserver
	Levels       []TreeLevel       `json:"levels"`                  // hierarchy from root to leaf
	AllowCreate  map[string]bool   `json:"allow_create"`           // e.g. {"schema":true, "database":false}
	SystemFilter *SystemFilter     `json:"system_filter,omitempty"` // rules for excluding system objects
}

// TreeLevel describes one level in the database hierarchy
type TreeLevel struct {
	Key            string `json:"key"`                       // server, database, schema, user, tables_folder, views_folder
	Label          string `json:"label"`                      // "Database", "Schema", "User"
	LabelKey       string `json:"label_key"`                  // i18n: "tree.database", "tree.schema", "tree.user"
	PlaceholderKey string `json:"placeholder_key,omitempty"`  // i18n: "tree.create_db_hint"
	Icon           string `json:"icon,omitempty"`             // ant-design icon component name
}

// SystemFilter defines rules for excluding system-internal objects
type SystemFilter struct {
	ExcludeNames    []string `json:"exclude_names"`    // exact names to hide
	ExcludePrefixes []string `json:"exclude_prefixes"` // prefix patterns
	ExcludePatterns []string `json:"exclude_patterns"` // LIKE patterns
}

// DatabaseContext unifies schema/database/user selection across DB types
type DatabaseContext struct {
	Database string `json:"database,omitempty"` // MySQL
	Schema   string `json:"schema,omitempty"`   // PG, MSSQL
	User     string `json:"user,omitempty"`     // Oracle
}

// ─── Shared Types (continued) ──────────────────────────────────────────────

// QueryResult represents the result of a SQL query
type QueryResult struct {
	Columns      []string        `json:"columns"`
	Rows         [][]interface{} `json:"rows"`
	TotalRows    int64           `json:"total_rows"`
	Duration     int64           `json:"duration"` // milliseconds
	IsSelect     bool            `json:"is_select"`
	AffectedRows int64           `json:"affected_rows"`
}

// TableDataResult represents paginated table data
type TableDataResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

// ColumnDetail represents column metadata
type ColumnDetail struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Length   string `json:"length"`
	Nullable string `json:"nullable"`
	Default  string `json:"default"`
	Comment  string `json:"comment"`
	Key      string `json:"key"`
}

// AlterColumn represents a column for ALTER TABLE operations
// AlterColumnChange mirrors service.ColumnChange for use in driver DDL builders.
type AlterColumnChange struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Length   string `json:"length"`
	Nullable *bool  `json:"nullable"`
	Default  string `json:"default"`
	HasDef   *bool  `json:"has_default"`
	Comment  string `json:"comment"`
	After    string `json:"after"`
	NewName  string `json:"new_name"`
}

// AlterIndexChange mirrors service.IndexChange for use in driver DDL builders.
type AlterIndexChange struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
	Comment string   `json:"comment"`
}

type AlterColumn struct {
	Name       string `json:"name"`
	Type       string `json:"col_type"`
	Nullable   bool   `json:"nullable"`
	DefaultVal string `json:"default_val"`
}

// SchemaDetailItem represents a schema summary
type SchemaDetailItem struct {
	Name       string `json:"name"`
	TableCount int64  `json:"table_count"`
	ViewCount  int64  `json:"view_count"`
	Charset    string `json:"charset"`
	Collation  string `json:"collation"`
}

// TableListItem represents a table or view with metadata
type TableListItem struct {
	Name       string  `json:"name"`
	Type       string  `json:"type"` // "table" or "view"
	Engine     *string `json:"engine"`
	RowCount   *int64  `json:"row_count"`
	Comment    string  `json:"comment"`
	CreateTime *string `json:"create_time"`
	UpdateTime *string `json:"update_time"`
}

// SchemaInfo represents database schema metadata
type SchemaInfo struct {
	Name   string       `json:"name"`
	Tables []ObjectInfo `json:"tables"`
	Views  []ObjectInfo `json:"views"`
}

// ObjectInfo represents a table or view
type ObjectInfo struct {
	Name    string       `json:"name"`
	Columns []ColumnInfo `json:"columns,omitempty"`
}

// ColumnInfo represents a column
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key"`
}

// DriverConfig holds connection parameters
type DriverConfig struct {
	Host              string
	Port              int
	Username          string
	Password          string
	Database          string
	MaxConnections    int
	OracleConnectMode string // "service_name" or "sid"
	OracleRole        string // "default", "sysdba", "sysoper"
	OracleService     string // service name or SID value
	DB                *sql.DB // pre-built *sql.DB from pool manager (optional)
}

// ─── DatabaseDriver Interface ──────────────────────────────────────────────

// DatabaseDriver is the unified interface for all database types.
// All database-specific implementations must satisfy this interface.
// RoleInfo represents a database role
type RoleInfo struct {
	Name          string   `json:"name"`
	IsSystem      bool     `json:"is_system"`
	Members       []string `json:"members"`
	CanLogin      string   `json:"can_login,omitempty"`
	IsSuperuser   string   `json:"is_superuser,omitempty"`
	CanCreatedb   string   `json:"can_createdb,omitempty"`
	CanCreaterole string   `json:"can_createrole,omitempty"`
	CanReplication string  `json:"can_replication,omitempty"`
	CanBypassrls  string   `json:"can_bypassrls,omitempty"`
	CanInherit    string   `json:"can_inherit,omitempty"`
	ConnLimit     string   `json:"conn_limit,omitempty"`
	ValidUntil    string   `json:"valid_until,omitempty"`
}

// DatabaseInfo holds basic information about a database on a server
type DatabaseInfo struct {
	Name    string `json:"name"`
	Charset string `json:"charset,omitempty"`
	SizeMB  int64  `json:"size_mb,omitempty"`
	Tables  int    `json:"tables,omitempty"`
}

type DatabaseDriver interface {
	// Connection management
	Ping() error
	Close() error

	// Schema discovery
	ListSchemas() ([]SchemaInfo, error)
	ListSchemaNames() ([]string, error)
	ListObjects(schema string) (*SchemaInfo, error)
	ListSchemaDetail() ([]SchemaDetailItem, error)
	ListTables(schema string) ([]TableListItem, error)

	// Database listing with metadata
	ListDatabases() ([]DatabaseInfo, error)
	// ServerInfo returns basic server information (version, uptime, settings)
	GetServerInfo(ctx context.Context) (map[string]interface{}, error)
	// GetMetrics returns current server metrics
	GetMetrics(ctx context.Context) (map[string]interface{}, error)
	GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error)
	DetectCapability() (*CapabilitySet, error)
	ListProcesses(dbType string) ([]map[string]interface{}, error)
	ListUsers(dbType string) ([]map[string]interface{}, error)
	ListTablespaces(dbType string) ([]map[string]interface{}, error)
	CreateDatabase(name string) error
	DropDatabase(name string) error
	CreateUser(username, password string) error
	DropUser(username string) error
	AlterUserPassword(username, newPassword string) error
	AlterUserLock(username string, lock bool) error
	AlterUserRename(oldName, newName string) error
	AlterUserDefaultSchema(username, schema string) error
	GrantPrivileges(username, database string, privileges []string) error
	GetUserPrivileges(username string) ([]PrivilegeEntry, error)
	GetUserRoles(username string) ([]string, error)
	GetRolePrivileges(roleName string) ([]PrivilegeEntry, error)
	GetParentRoles(roleName string) ([]ParentRoleInfo, error)
	GetRoleMembers(roleName string) ([]MemberRoleInfo, error)
	GetRoleInherit(roleName string) (bool, error)
	AlterRoleAttribute(roleName, attribute, value string) error
	GetRoleMemberships(roleName string) ([]string, error)
	ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error)
	ListRoles() ([]RoleInfo, error)
	CreateRole(name string) error
	DropRole(name string) error
	AddRoleMember(role, member string) error
	RemoveRoleMember(role, member string) error

	// ListDatabaseSchemas returns schema names within a specific database.
	// PG: requires reconnection to the target database.
	// MSSQL: switches database context with USE statement then queries schemas.
	// MySQL & Oracle: not applicable (return nil).
	ListDatabaseSchemas(database string) ([]string, error)

	// Tree metadata - provides frontend with hierarchy labels, levels and filters
	GetTreeMetadata() TreeMetadata

	// ResolveContext normalizes a schema/user/database selection into the driver-native context.
	ResolveContext(arg string) DatabaseContext

	// Column metadata
	GetColumns(schema, table string) ([]ColumnDetail, error)
	GetColumnDetails(schema, table string) ([]map[string]interface{}, error)
	GetDDL(schema, table string) (string, error)
	GetViewDefinition(schema, view string) (string, error)

	// Query execution
	ExecuteQuery(sql string, schema string) (*QueryResult, error)
	GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error)

	// DDL execution
	ExecuteDDL(ddl string) (int64, error)
	ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error)

	// DDL helpers - low level (deprecated, use Build* methods instead)
	AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string
	DropIndexDDL(qualifiedTable, schema, indexName string) string
	AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string
	DropColumnDDL(qualifiedTable, columnName string) string
	AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string

	// DDL builder - high level (preferred): takes full change context, returns DDL+rollback+warnings+risk+error
	BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error)
	BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error)
	BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error)
	BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error)
	BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error)
	BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error)
	BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error)
	BuildDropConstraint(table string, constraintName string) (string, string, error)
	BuildTableComment(table, newComment, oldComment string) (string, string, error)

	// Index and constraint metadata
	GetIndexes(schema, table string) ([]map[string]interface{}, error)
	GetConstraints(schema, table string) ([]map[string]interface{}, error)
	GetTableMeta(schema, table string) (map[string]interface{}, error)

	// Cross-database helpers
	BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string
	SetTableCommentDDL(qualifiedTable, schema, table, comment string) string
	FormatSQLValue(val interface{}) string
	AlterColumnClause(columnName, colType, columnDef string) string
	BuildInsertSQL(tableName, colList string, rowValues []string) string
	RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string
	ListColumnsForAlter(schema, table string) ([]AlterColumn, error)

	// Full structure returns complete table metadata: columns, indexes, constraints, DDL
	GetFullStructure(schema, table string) (*FullStructure, error)

	// Dialect info
	DBType() string
	Dialect() string

	// ─── System SQL Dialect Methods ──────────────────────────────────────
	// These methods generate SQL fragments compatible with the driver's
	// database type, for use in the system database queries.
	SQLFormatTime(col string) string
	SQLConcat(parts ...string) string
	SQLIsNull(col, defaultVal string) string
	SQLCurrentTimestamp() string
	SQLQuoteIdent(name string) string

	// Column type definitions (for frontend dropdown)
	GetColumnTypes() []ColumnTypeInfo

	// Index type definitions (for frontend dropdown)
	GetIndexTypes() []IndexTypeInfo

	// ─── MSSQL Login Management ──────────────────────────────────────
	ListLogins() ([]MSSQLLogin, error)
	CreateLogin(req CreateLoginRequest) error
	DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error)
	AlterLogin(loginName string, req AlterLoginRequest) error
	GetLoginDetail(loginName string) (*LoginDetail, error)

	// ─── MSSQL Database User Management ───────────────────────────────
	ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error)
	CreateDatabaseUser(database string, req CreateDBUserRequest) error
	DropDatabaseUser(database, userName string) error
	BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error

	// ─── MSSQL Orphaned Users ────────────────────────────────────────
	DetectOrphanedUsers(database string) ([]OrphanedUser, error)
	FixOrphanedUser(database, userName, loginName string) error

	// ─── MSSQL Effective Permissions ─────────────────────────────────
	GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error)

	// ─── MSSQL Guest Compliance ──────────────────────────────────────
	CheckGuestStatus(database string) (*GuestStatus, error)
	DisableGuest(database string) error
}

// IndexTypeInfo describes an index type supported by a database.
type IndexTypeInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// ColumnTypeInfo describes a column type for UI dropdown rendering
type ColumnTypeInfo struct {
	Name        string `json:"name"`
	NeedsLength bool   `json:"needs_length"`
	NeedsScale  bool   `json:"needs_scale"`
	Description string `json:"description,omitempty"`
}

// ─── Full Structure Types (returned by GetFullStructure) ────────────────────

// TableColumn represents a single column in a table
type TableColumn struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Length    string `json:"length"`
	Nullable  bool   `json:"nullable"`
	Default   string `json:"default"`
	HasDef    bool   `json:"has_default"`
	Comment   string `json:"comment"`
	Key       string `json:"key"`
	Extra     string `json:"extra"`
	Charset   string `json:"charset"`
	Collation string `json:"collation"`
}

// TableIndex represents an index on a table
type TableIndex struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
	Comment string   `json:"comment"`
}

// TableConstraint represents a constraint (PK, FK, UNIQUE, CHECK)
type TableConstraint struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"`
	Columns    []string `json:"columns"`
	RefTable   string   `json:"ref_table"`
	RefColumns []string `json:"ref_columns"`
	OnDelete   string   `json:"on_delete"`
	OnUpdate   string   `json:"on_update"`
}

// TableMeta contains table-level metadata
type TableMeta struct {
	Engine     string `json:"engine"`
	Charset    string `json:"charset"`
	Collation  string `json:"collation"`
	RowFormat  string `json:"row_format"`
	Comment    string `json:"comment"`
	RowCount   int64  `json:"row_count"`
	CreateTime string `json:"create_time"`
	UpdateTime string `json:"update_time"`
}

// FullStructure contains the complete structure of a table or view
type FullStructure struct {
	Columns     []TableColumn     `json:"columns"`
	Indexes     []TableIndex      `json:"indexes"`
	Constraints []TableConstraint `json:"constraints"`
	TableMeta   TableMeta         `json:"table_meta"`
	DDL         string            `json:"ddl"`
	IsView      bool              `json:"is_view"`
}

// ─── Common Functions ─────────────────────────────────────────────────────

// QuoteIdent quotes an identifier for the given dialect
func QuoteIdent(name, dialect string) string {
	switch dialect {
	case "mysql":
		return "`" + strings.ReplaceAll(name, "`", "``") + "`"
	case "postgres", "oracle":
		return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
	case "sqlserver":
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	default:
		return name
	}
}

// FormatSQLTime 格式化 time.Time 为标准 SQL 日期时间字面量，各数据库兼容
func FormatSQLTime(t time.Time) string {
	return "'" + t.Format("2006-01-02 15:04:05") + "'"
}

// FormatSQLBytes 格式化 []byte 为带引号的 SQL 字符串字面量
func FormatSQLBytes(b []byte) string {
	return "'" + strings.ReplaceAll(string(b), "'", "''") + "'"
}

// DialectOf returns the canonical dialect name for a database type
func DialectOf(dsType string) string {
	switch strings.ToLower(dsType) {
	case "mysql", "mariadb", "oceanbase":
		return "mysql"
	case "postgres", "postgresql":
		return "postgres"
	case "sqlserver":
		return "sqlserver"
	case "oracle":
		return "oracle"
	default:
		return "unknown"
	}
}

// QuoteTableName returns a fully qualified, quoted table name
func QuoteTableName(schema, table, dialect string) string {
	return QuoteIdent(schema, dialect) + "." + QuoteIdent(table, dialect)
}

// ─── Row Scanning Helpers ──────────────────────────────────────────────────

// bytesToString converts bytes to string, handling UTF-16LE encoding from SQL Server NVARCHAR.
func bytesToString(b []byte) string {
	// Detect UTF-16LE: even length, every odd byte is 0x00 for ASCII-range chars
	if len(b) >= 2 && len(b)%2 == 0 {
		isUTF16 := true
		for i := 1; i < len(b); i += 2 {
			if b[i] != 0 {
				isUTF16 = false
				break
			}
		}
		if isUTF16 {
			u16 := make([]uint16, len(b)/2)
			for i := range u16 {
				u16[i] = uint16(b[i*2]) | uint16(b[i*2+1])<<8
			}
			return string(utf16.Decode(u16))
		}
	}
	return string(b)
}

// ScanQueryResult scans a *sql.Rows into [][]interface{}
func ScanQueryResult(rows *sql.Rows) ([]string, [][]interface{}, error) {
	colNames, err := rows.Columns()
	if err != nil {
		return nil, nil, err
	}

	var resultRows [][]interface{}
	for rows.Next() {
		values := make([]interface{}, len(colNames))
		valuePtrs := make([]interface{}, len(colNames))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		row := make([]interface{}, len(colNames))
		for i, v := range values {
			if v == nil {
				row[i] = nil
			} else if b, ok := v.([]byte); ok {
				row[i] = bytesToString(b)
			} else {
				row[i] = v
			}
		}
		resultRows = append(resultRows, row)
	}

	if resultRows == nil {
		resultRows = make([][]interface{}, 0)
	}
	return colNames, resultRows, nil
}

// ScanMapRows scans rows into []map[string]interface{}
func ScanMapRows(rows *sql.Rows) ([]map[string]interface{}, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{}, len(cols))
		for i, c := range cols {
			row[c] = vals[i]
		}
		results = append(results, row)
	}
	if results == nil {
		results = make([]map[string]interface{}, 0)
	}
	return results, nil
}

// ToJSON marshals a value to JSON string
func ToJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// ─── Type Conversion - MySQL → PostgreSQL ────────────────────────────────

func MySQLToPGType(dtype string, charLen string, colType string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "int", "integer", "mediumint":
		if strings.Contains(strings.ToLower(colType), "unsigned") {
			return "BIGINT"
		}
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "smallint":
		return "SMALLINT"
	case "tinyint":
		if strings.Contains(colType, "1") {
			return "BOOLEAN"
		}
		return "SMALLINT"
	case "float":
		return "REAL"
	case "double":
		return "DOUBLE PRECISION"
	case "decimal", "numeric":
		return "NUMERIC"
	case "varchar", "character varying":
		if charLen != "" && charLen != "-" && charLen != "0" {
			return fmt.Sprintf("VARCHAR(%s)", charLen)
		}
		return "TEXT"
	case "char", "character":
		if charLen != "" && charLen != "-" && charLen != "0" {
			return fmt.Sprintf("CHAR(%s)", charLen)
		}
		return "CHAR(1)"
	case "text", "mediumtext", "longtext", "tinytext":
		return "TEXT"
	case "blob", "mediumblob", "longblob", "tinyblob", "binary", "varbinary":
		return "BYTEA"
	case "datetime", "timestamp":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time":
		return "TIME"
	case "json":
		return "JSONB"
	case "enum", "set":
		return "VARCHAR(255)"
	case "bit":
		return "BOOLEAN"
	default:
		return strings.ToUpper(dtype)
	}
}

// ─── Type Conversion - PostgreSQL → MySQL ────────────────────────────────

func PGToMySQLType(dtype string, charLen string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "integer", "int", "int4":
		return "INT"
	case "bigint", "int8":
		return "BIGINT"
	case "smallint", "int2":
		return "SMALLINT"
	case "serial":
		return "INT AUTO_INCREMENT"
	case "bigserial":
		return "BIGINT AUTO_INCREMENT"
	case "real", "float4":
		return "FLOAT"
	case "double precision", "float8":
		return "DOUBLE"
	case "numeric", "decimal":
		return "DECIMAL"
	case "boolean", "bool":
		return "TINYINT(1)"
	case "character varying", "varchar":
		if charLen != "" && charLen != "-" && charLen != "0" {
			return fmt.Sprintf("VARCHAR(%s)", charLen)
		}
		return "TEXT"
	case "character", "char":
		if charLen != "" && charLen != "-" && charLen != "0" {
			return fmt.Sprintf("CHAR(%s)", charLen)
		}
		return "CHAR(1)"
	case "text":
		return "TEXT"
	case "bytea":
		return "BLOB"
	case "timestamp", "timestamptz", "timestamp without time zone", "timestamp with time zone":
		return "DATETIME"
	case "date":
		return "DATE"
	case "time", "time without time zone", "time with time zone":
		return "TIME"
	case "json", "jsonb":
		return "JSON"
	case "uuid":
		return "VARCHAR(36)"
	case "inet", "cidr", "macaddr":
		return "VARCHAR(50)"
	default:
		return strings.ToUpper(dtype)
	}
}

// ─── Type Conversion - SQL Server → MySQL ────────────────────────────────

func SQLServerToMySQLType(dtype, charLen string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "int":
		return "INT"
	case "bigint":
		return "BIGINT"
	case "smallint":
		return "SMALLINT"
	case "tinyint":
		return "TINYINT"
	case "bit":
		return "TINYINT(1)"
	case "float":
		return "DOUBLE"
	case "real":
		return "FLOAT"
	case "decimal", "numeric":
		return "DECIMAL"
	case "money", "smallmoney":
		return "DECIMAL(19,4)"
	case "varchar", "nvarchar":
		if charLen == "" || charLen == "-" {
			return "TEXT"
		}
		return fmt.Sprintf("VARCHAR(%s)", charLen)
	case "char", "nchar":
		if charLen == "" || charLen == "-" {
			return "CHAR(1)"
		}
		return fmt.Sprintf("CHAR(%s)", charLen)
	case "text", "ntext":
		return "LONGTEXT"
	case "varbinary", "binary":
		if charLen == "" || charLen == "-" {
			return "BLOB"
		}
		return fmt.Sprintf("VARBINARY(%s)", charLen)
	case "image":
		return "LONGBLOB"
	case "datetime", "datetime2", "smalldatetime":
		return "DATETIME"
	case "date":
		return "DATE"
	case "time":
		return "TIME"
	case "datetimeoffset":
		return "VARCHAR(50)"
	case "uniqueidentifier":
		return "VARCHAR(36)"
	case "xml":
		return "LONGTEXT"
	default:
		return strings.ToUpper(dtype)
	}
}

// ─── Cross-Database DDL Type Conversion ────────────────────────────────────

// ConvertDDLType converts a column type from source to target database
func ConvertDDLType(sourceType, targetType, dtype, charLen, colType string) string {
	if sourceType == targetType {
		return dtype
	}
	switch {
	case sourceType == "mysql" && targetType != "mysql":
		return MySQLToPGType(dtype, charLen, colType)
	case sourceType == "postgres" && targetType == "mysql":
		return PGToMySQLType(dtype, charLen)
	case sourceType == "sqlserver" && targetType == "mysql":
		return SQLServerToMySQLType(dtype, charLen)
	case sourceType == "sqlserver" && targetType == "postgres":
		return SQLServerToPGType(dtype, charLen)
	case sourceType == "oracle" && targetType == "mysql":
		return oracleToMySQLType(dtype, charLen)
	case sourceType == "oracle" && targetType == "postgres":
		return oracleToPGType(dtype, charLen)
	default:
		return dtype
	}
}

func SQLServerToPGType(dtype, charLen string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "int":
		return "INTEGER"
	case "bigint":
		return "BIGINT"
	case "smallint":
		return "SMALLINT"
	case "tinyint":
		return "SMALLINT"
	case "bit":
		return "BOOLEAN"
	case "float":
		return "DOUBLE PRECISION"
	case "real":
		return "REAL"
	case "decimal", "numeric":
		return "NUMERIC"
	case "money", "smallmoney":
		return "NUMERIC(19,4)"
	case "varchar", "nvarchar":
		if charLen == "" || charLen == "-" {
			return "TEXT"
		}
		return fmt.Sprintf("VARCHAR(%s)", charLen)
	case "char", "nchar":
		if charLen == "" || charLen == "-" {
			return "CHAR(1)"
		}
		return fmt.Sprintf("CHAR(%s)", charLen)
	case "text", "ntext":
		return "TEXT"
	case "varbinary", "binary", "image":
		return "BYTEA"
	case "datetime", "datetime2", "smalldatetime":
		return "TIMESTAMP"
	case "date":
		return "DATE"
	case "time":
		return "TIME"
	case "datetimeoffset":
		return "TIMESTAMPTZ"
	case "uniqueidentifier":
		return "UUID"
	case "xml":
		return "XML"
	default:
		return strings.ToUpper(dtype)
	}
}

func oracleToMySQLType(dtype, charLen string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "number":
		return "DECIMAL"
	case "varchar2", "nvarchar2":
		if charLen == "" || charLen == "-" {
			return "TEXT"
		}
		return fmt.Sprintf("VARCHAR(%s)", charLen)
	case "clob":
		return "TEXT"
	case "blob":
		return "BLOB"
	case "date":
		return "DATETIME"
	case "timestamp", "timestamp with time zone", "timestamp with local time zone":
		return "DATETIME"
	default:
		return strings.ToUpper(dtype)
	}
}

func oracleToPGType(dtype, charLen string) string {
	dt := strings.ToLower(dtype)
	switch dt {
	case "number":
		return "NUMERIC"
	case "varchar2", "nvarchar2":
		if charLen == "" || charLen == "-" {
			return "TEXT"
		}
		return fmt.Sprintf("VARCHAR(%s)", charLen)
	case "clob":
		return "TEXT"
	case "blob":
		return "BYTEA"
	case "date":
		return "TIMESTAMP"
	case "timestamp", "timestamp with time zone", "timestamp with local time zone":
		return "TIMESTAMP"
	default:
		return strings.ToUpper(dtype)
	}
}

// ─── Factory Function ──────────────────────────────────────────────────────

// CreateDriver creates the appropriate driver for the given database type.
// Returns the driver and an optional underlying *sql.DB for connection pooling.
func CreateDriver(dsType string, cfg DriverConfig) (DatabaseDriver, *sql.DB, error) {
	switch strings.ToLower(dsType) {
	case "mysql", "mariadb", "oceanbase":
		return NewMySQLDriver(cfg)
	case "postgres", "postgresql":
		return NewPostgresDriver(cfg)
	case "sqlserver":
		return NewSQLServerDriver(cfg)
	case "oracle":
		return NewOracleDriver(cfg)
	case "sqlite":
		drv := NewSQLiteDriver()
		// Host stores the file path
		path := cfg.Host
		if err := drv.(*SQLiteDriver).Open(path, false); err != nil {
			return nil, nil, fmt.Errorf("open sqlite: %w", err)
		}
		return drv, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database type: %s", dsType)
	}
}

// SplitDDL splits a DDL string into individual statements
func SplitDDL(ddl string) []string {
	return SplitDDLWithDialect(ddl, "mysql")
}

// SplitDDLWithDialect splits DDL with explicit dialect handling
func SplitDDLWithDialect(ddl, dialect string) []string {
	// Simple semicolon-based split with basic string literal awareness
	var statements []string
	var current strings.Builder
	inString := false
	stringChar := byte(0)

	for i := 0; i < len(ddl); i++ {
		ch := ddl[i]

		if inString {
			current.WriteByte(ch)
			if ch == stringChar {
				// Check for escape (doubled quote)
				if i+1 < len(ddl) && ddl[i+1] == stringChar {
					current.WriteByte(ddl[i+1])
					i++
					continue
				}
				inString = false
			}
			continue
		}

		if ch == '\'' || ch == '"' {
			inString = true
			stringChar = ch
			current.WriteByte(ch)
			continue
		}

		// Handle -- comments
		if ch == '-' && i+1 < len(ddl) && ddl[i+1] == '-' {
			for i < len(ddl) && ddl[i] != '\n' {
				current.WriteByte(ddl[i])
				i++
			}
			if i < len(ddl) {
				current.WriteByte(ddl[i])
			}
			continue
		}

		// Handle /* comments */
		if ch == '/' && i+1 < len(ddl) && ddl[i+1] == '*' {
			current.WriteByte(ch)
			current.WriteByte(ddl[i+1])
			i += 2
			for i < len(ddl)-1 {
				if ddl[i] == '*' && ddl[i+1] == '/' {
					current.WriteByte('*')
					current.WriteByte('/')
					i += 2
					break
				}
				current.WriteByte(ddl[i])
				i++
			}
			continue
		}

		if ch == ';' {
			s := strings.TrimSpace(current.String())
			if s != "" {
				statements = append(statements, s)
			}
			current.Reset()
		} else {
			current.WriteByte(ch)
		}
	}

	// Last statement (without trailing semicolon)
	s := strings.TrimSpace(current.String())
	if s != "" {
		statements = append(statements, s)
	}

	return statements
}

// IsSelectStatement checks if a SQL query is a SELECT-like statement
func IsSelectStatement(sql string) bool {
	lower := strings.ToLower(strings.TrimSpace(sql))
	return strings.HasPrefix(lower, "select") ||
		strings.HasPrefix(lower, "show ") ||
		strings.HasPrefix(lower, "describe ") ||
		strings.HasPrefix(lower, "desc ") ||
		strings.HasPrefix(lower, "explain ") ||
		strings.HasPrefix(lower, "with ")
}

// ExecuteWithTiming wraps a DB operation and tracks execution duration
func ExecuteWithTiming(fn func() error) (time.Duration, error) {
	start := time.Now()
	err := fn()
	return time.Since(start), err
}

// ─── Server Management Helpers ────────────────────────────────────────────

var processQueries = map[string]string{
	"mysql":     "SELECT ID AS pid, USER AS username, HOST, DB AS database_name, COMMAND, TIME AS seconds, STATE, IFNULL(INFO, '') AS query FROM information_schema.PROCESSLIST ORDER BY TIME DESC",
	"postgres":  "SELECT pid, usename AS username, application_name, client_addr, state, COALESCE(query, '') AS query, EXTRACT(EPOCH FROM NOW() - query_start)::int AS seconds FROM pg_stat_activity ORDER BY seconds DESC NULLS LAST",
	"oracle":    "SELECT SID AS pid, SERIAL# AS serial, USERNAME AS username, STATUS AS state, OSUSER, MACHINE, PROGRAM, SQL_ID, LAST_CALL_ET AS seconds FROM V$SESSION WHERE TYPE!='BACKGROUND' ORDER BY LAST_CALL_ET DESC",
	"sqlserver": "SELECT s.session_id AS pid, s.login_name AS username, s.status, s.host_name, s.program_name, COALESCE(r.text, '') AS query, DATEDIFF(SECOND, s.last_request_start_time, GETDATE()) AS seconds FROM sys.dm_exec_sessions s LEFT JOIN sys.dm_exec_requests rq ON s.session_id=rq.session_id OUTER APPLY sys.dm_exec_sql_text(rq.sql_handle) r WHERE s.is_user_process=1 ORDER BY s.session_id",
}

var userQueries = map[string]string{
	"mysql":     "SELECT User AS username, Host AS host FROM mysql.user ORDER BY User",
	"postgres":  "SELECT u.usename AS username, u.usesuper::text AS is_superuser, u.usecreatedb::text AS can_createdb, (SELECT rolcreaterole::text FROM pg_authid WHERE rolname=u.usename) AS can_createrole, u.userepl::text AS can_replication, (SELECT rolbypassrls::text FROM pg_authid WHERE rolname=u.usename) AS can_bypassrls, u.valuntil::text AS valid_until FROM pg_user u ORDER BY u.usename",
	"oracle":    "SELECT USERNAME AS username, ACCOUNT_STATUS AS account_status, DEFAULT_TABLESPACE AS default_tablespace, CREATED AS created FROM DBA_USERS ORDER BY USERNAME",
	"sqlserver": "SELECT username, MAX(type_desc) AS type_desc, MAX(created) AS created, MAX(default_schema_name) AS default_schema_name, MAX(account_status) AS account_status, MAX(CAST(is_superuser AS INT)) AS is_superuser FROM (SELECT p.name AS username, p.type_desc, p.create_date AS created, s.name AS default_schema_name, CASE WHEN p.is_fixed_role=1 THEN 'SYSTEM' ELSE 'ACTIVE' END AS account_status, IS_SRVROLEMEMBER('sysadmin', p.name) AS is_superuser, 1 AS priority FROM sys.database_principals p LEFT JOIN sys.schemas s ON p.default_schema_name=s.name WHERE p.type IN ('S','U','G') AND p.name NOT IN ('dbo','guest','INFORMATION_SCHEMA','sys') UNION ALL SELECT sp.name AS username, sp.type_desc, sp.create_date AS created, 'N/A' AS default_schema_name, CASE WHEN sp.is_disabled=1 THEN 'DISABLED' ELSE 'ACTIVE' END AS account_status, IS_SRVROLEMEMBER('sysadmin', sp.name) AS is_superuser, 0 AS priority FROM sys.server_principals sp WHERE sp.type IN ('S','U') AND sp.name NOT IN ('NT AUTHORITY\\SYSTEM','NT SERVICE\\MSSQLSERVER')) t GROUP BY username ORDER BY username",
}

func queryToList(db *sql.DB, sql string) ([]map[string]interface{}, error) {
	if sql == "" {
		return []map[string]interface{}{}, nil
	}
	rows, err := db.Query(sql)
	if err != nil {
		return []map[string]interface{}{}, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	var result []map[string]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, c := range cols {
			v := vals[i]
			key := strings.ToLower(c)  // normalize column names to lowercase
			if b, ok := v.([]byte); ok {
				row[key] = string(b)
			} else {
				row[key] = v
			}
		}
		result = append(result, row)
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	return result, nil
}

var tablespaceQueries = map[string]string{
	"oracle":    "SELECT TABLESPACE_NAME AS name, ROUND(SUM(BYTES)/1024/1024, 2) AS size_mb, ROUND(SUM(DECODE(AUTOEXTENSIBLE, 'YES', MAXBYTES, BYTES))/1024/1024, 2) AS max_size_mb FROM DBA_DATA_FILES GROUP BY TABLESPACE_NAME",
	"postgres":  "SELECT spcname AS name, pg_tablespace_size(spcname)/1024/1024 AS size_mb FROM pg_tablespace",
}

func parseIntStr(s string) int {
	var v int
	fmt.Sscanf(s, "%d", &v)
	return v
}
