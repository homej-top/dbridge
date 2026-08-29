package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	_ "modernc.org/sqlite"
)

// SQLiteDriver implements DatabaseDriver for SQLite file databases
type SQLiteDriver struct {
	db       *sql.DB
	path     string
	readonly bool
}

// NewSQLiteDriver creates a legacy stub (for SQL dialect use only)
func NewSQLiteDriver() DatabaseDriver {
	return &SQLiteDriver{}
}

// NewSQLiteDriverWithDB creates a real SQLite driver connected to a file
func NewSQLiteDriverWithDB(db *sql.DB, path string, readonly bool) DatabaseDriver {
	return &SQLiteDriver{db: db, path: path, readonly: readonly}
}

// ─── Connection ─────────────────────────────────────────────────────────

func (d *SQLiteDriver) Open(path string, readonly bool) error {
	mode := "rwc"
	if readonly {
		mode = "ro"
	}
	conn, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=%s&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", path, mode))
	if err != nil {
		return err
	}
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	// Integrity check (lightweight: first page only)
	var result string
	if err := conn.QueryRow("PRAGMA integrity_check(1)").Scan(&result); err != nil || result != "ok" {
		conn.Close()
		return fmt.Errorf("SQLite文件损坏: %s", result)
	}
	d.db = conn
	d.path = path
	d.readonly = readonly
	return nil
}

func (d *SQLiteDriver) Ping() error {
	if d.db == nil {
		return fmt.Errorf("not connected")
	}
	var result string
	if err := d.db.QueryRow("PRAGMA integrity_check(1)").Scan(&result); err != nil {
		return fmt.Errorf("integrity check failed: %w", err)
	}
	if result != "ok" {
		return fmt.Errorf("database corrupted: %s", result)
	}
	var count int
	return d.db.QueryRow("SELECT COUNT(*) FROM sqlite_master").Scan(&count)
}

func (d *SQLiteDriver) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

func (d *SQLiteDriver) DBType() string  { return "sqlite" }
func (d *SQLiteDriver) Dialect() string { return "sqlite" }

// ─── SQL Dialect ────────────────────────────────────────────────────────

func (d *SQLiteDriver) SQLFormatTime(col string) string {
	return fmt.Sprintf("strftime('%%Y-%%m-%%d %%H:%%M:%%S', %s)", col)
}
func (d *SQLiteDriver) SQLConcat(parts ...string) string {
	return strings.Join(parts, " || ")
}
func (d *SQLiteDriver) SQLIsNull(col, defaultVal string) string {
	return fmt.Sprintf("COALESCE(%s, %s)", col, defaultVal)
}
func (d *SQLiteDriver) SQLCurrentTimestamp() string { return "datetime('now')" }
func (d *SQLiteDriver) SQLQuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ─── Schema / Table Listing ─────────────────────────────────────────────

func (d *SQLiteDriver) ListSchemas() ([]SchemaInfo, error) {
	return []SchemaInfo{
		{Name: "main", Tables: nil, Views: nil},
	}, nil
}

func (d *SQLiteDriver) ListSchemaNames() ([]string, error) {
	return []string{"main"}, nil
}

func (d *SQLiteDriver) ListObjects(schema string) (*SchemaInfo, error) {
	tables, err := d.ListTables(schema)
	if err != nil {
		return nil, err
	}
	info := &SchemaInfo{Name: schema}
	for _, t := range tables {
		obj := ObjectInfo{Name: t.Name}
		if t.Type == "view" {
			info.Views = append(info.Views, obj)
		} else {
			info.Tables = append(info.Tables, obj)
		}
	}
	return info, nil
}

func (d *SQLiteDriver) ListSchemaDetail() ([]SchemaDetailItem, error) {
	return []SchemaDetailItem{{Name: "main"}}, nil
}

func (d *SQLiteDriver) ListTables(schema string) ([]TableListItem, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	rows, err := d.db.Query(`SELECT name, type FROM sqlite_master 
		WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%' 
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tables []TableListItem
	for rows.Next() {
		var name, typ string
		if err := rows.Scan(&name, &typ); err != nil {
			continue
		}
		tables = append(tables, TableListItem{Name: name, Type: typ})
	}
	return tables, nil
}

func (d *SQLiteDriver) ListDatabases() ([]DatabaseInfo, error) {
	return []DatabaseInfo{}, nil
}

// ─── Columns ────────────────────────────────────────────────────────────

func (d *SQLiteDriver) GetColumns(schema, table string) ([]ColumnDetail, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	qtable := d.SQLQuoteIdent(table)
	rows, err := d.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", qtable))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []ColumnDetail
	for rows.Next() {
		var cid int
		var name, typ, notNull, defVal string
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defVal, &pk); err != nil {
			continue
		}
		nullable := "YES"
		if notNull == "1" || notNull == "true" {
			nullable = "NO"
		}
		key := ""
		if pk > 0 {
			key = "PRI"
		}
		cols = append(cols, ColumnDetail{
			Name:     name,
			Type:     typ,
			Nullable: nullable,
			Default:  defVal,
			Key:      key,
		})
	}
	return cols, nil
}

func (d *SQLiteDriver) GetColumnDetails(schema, table string) ([]map[string]interface{}, error) {
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return nil, err
	}
	result := make([]map[string]interface{}, len(cols))
	for i, c := range cols {
		result[i] = map[string]interface{}{
			"name": c.Name, "type": c.Type, "nullable": c.Nullable,
			"default": c.Default, "key": c.Key,
		}
	}
	return result, nil
}

// ─── DDL ────────────────────────────────────────────────────────────────

func (d *SQLiteDriver) GetDDL(schema, table string) (string, error) {
	if d.db == nil {
		return "", fmt.Errorf("not connected")
	}
	var sql string
	err := d.db.QueryRow("SELECT sql FROM sqlite_master WHERE name = ?", table).Scan(&sql)
	return sql, err
}

func (d *SQLiteDriver) GetViewDefinition(schema, view string) (string, error) {
	return d.GetDDL(schema, view)
}

// ─── Query Execution ────────────────────────────────────────────────────

func (d *SQLiteDriver) ExecuteQuery(sqlStr string, schema string) (*QueryResult, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	// Readonly mode enforcement
	if d.readonly {
		upper := strings.ToUpper(strings.TrimSpace(sqlStr))
		for _, prefix := range []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "REPLACE"} {
			if strings.HasPrefix(upper, prefix) {
				return nil, fmt.Errorf("只读模式禁止写操作: %s", prefix)
			}
		}
	}

	start := time.Now()
	rows, err := d.db.Query(sqlStr)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, data, err := ScanQueryResult(rows)
	if err != nil {
		return nil, err
	}
	return &QueryResult{Columns: cols, Rows: data, Duration: time.Since(start).Milliseconds()}, nil
}

func (d *SQLiteDriver) GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	if page < 1 {
		page = 1
	}
	offset := (page - 1) * pageSize
	qtable := d.SQLQuoteIdent(table)

	var total int64
	d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", qtable)).Scan(&total)

	rows, err := d.db.Query(fmt.Sprintf("SELECT * FROM %s LIMIT ? OFFSET ?", qtable), pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, _ := rows.Columns()

	var data [][]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("扫描行数据失败: %w", err)
		}
		row := make([]interface{}, len(columns))
		for i := range columns {
			switch v := vals[i].(type) {
			case []byte:
				if utf8.Valid(v) {
					row[i] = string(v)
				} else {
					row[i] = v
				}
			default:
				row[i] = v
			}
		}
		data = append(data, row)
	}
	if data == nil {
		data = [][]interface{}{}
	}
	return &TableDataResult{Columns: columns, Rows: data, Total: total}, nil
}

func (d *SQLiteDriver) ExecuteDDL(ddl string) (int64, error) {
	if d.db == nil {
		return 0, fmt.Errorf("not connected")
	}
	if d.readonly {
		return 0, fmt.Errorf("只读模式禁止 DDL")
	}
	result, err := d.db.Exec(ddl)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (d *SQLiteDriver) ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error) {
	_, err = d.ExecuteDDL(ddl)
	if err != nil {
		return 1, 0, 1, 0, []string{err.Error()}, nil
	}
	return 1, 1, 0, 0, nil, nil
}

func (d *SQLiteDriver) ExplainSQL(ctx context.Context, sqlText string) (string, error) {
	if d.db == nil {
		return "", fmt.Errorf("not connected")
	}
	rows, err := d.db.QueryContext(ctx, "EXPLAIN QUERY PLAN "+sqlText)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var lines []string
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		line := fmt.Sprintf("%v", vals)
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"), nil
}

// ─── Server Info ────────────────────────────────────────────────────────

func (d *SQLiteDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	info := map[string]interface{}{"type": "SQLite", "path": d.path}
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT sqlite_version()").Scan(&version); err == nil {
		info["version"] = version
	}
	var pageSize, pageCount int
	d.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
	d.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	info["page_size"] = pageSize
	info["page_count"] = pageCount
	info["total_size_bytes"] = pageSize * pageCount
	var encoding string
	if err := d.db.QueryRowContext(ctx, "PRAGMA encoding").Scan(&encoding); err == nil {
		info["encoding"] = encoding
	}
	var journalMode string
	if err := d.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err == nil {
		info["journal_mode"] = journalMode
	}
	var schemaVersion int
	if err := d.db.QueryRowContext(ctx, "PRAGMA schema_version").Scan(&schemaVersion); err == nil {
		info["schema_version"] = schemaVersion
	}
	var freelistCount int
	if err := d.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freelistCount); err == nil {
		info["freelist_count"] = freelistCount
	}
	if d.path != "" {
		if fi, err := os.Stat(d.path); err == nil {
			info["file_size_bytes"] = fi.Size()
			info["modified_time"] = fi.ModTime().Format("2006-01-02 15:04:05")
		}
	}
	return info, nil
}

func (d *SQLiteDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	var tableCount int
	d.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	return map[string]interface{}{
		"table_count": tableCount,
		"path":        d.path,
		"readonly":    d.readonly,
	}, nil
}

func (d *SQLiteDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	start := time.Now()
	m := &ServerMetricsV2{
		DBType:           "sqlite",
		CollectedAt:      time.Now(),
		DatabaseSpecific: make(map[string]interface{}),
	}
	var pageSize, pageCount, freelistCount int
	d.db.QueryRowContext(ctx, "PRAGMA page_size").Scan(&pageSize)
	d.db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pageCount)
	d.db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freelistCount)
	totalSizeMB := float64(pageSize*pageCount) / 1024 / 1024
	usedSizeMB := float64(pageSize*(pageCount-freelistCount)) / 1024 / 1024
	freeSizeMB := float64(pageSize*freelistCount) / 1024 / 1024
	usagePct := float64(pageCount-freelistCount) * 100.0 / float64(pageCount)
	m.Storage.Tablespaces = []TablespaceMetric{
		{Name: "main", SizeMB: totalSizeMB, UsedMB: usedSizeMB, FreeMB: freeSizeMB, UsagePct: usagePct},
	}
	var cacheSize int
	d.db.QueryRowContext(ctx, "PRAGMA cache_size").Scan(&cacheSize)
	m.BufferCache.TotalMB = float64(cacheSize*pageSize) / 1024 / 1024
	m.BufferCache.HitRate = 0
	m.DatabaseSpecific["page_size"] = pageSize
	m.DatabaseSpecific["page_count"] = pageCount
	m.DatabaseSpecific["freelist_count"] = freelistCount
	m.DatabaseSpecific["cache_size_pages"] = cacheSize
	var journalMode string
	d.db.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode)
	m.DatabaseSpecific["journal_mode"] = journalMode
	if journalMode == "wal" {
		var walSize int64
		if d.path != "" {
			if fi, err := os.Stat(d.path + "-wal"); err == nil {
				walSize = fi.Size()
			}
		}
		m.DatabaseSpecific["wal_size_bytes"] = walSize
	}
	var busyTimeout int
	d.db.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout)
	m.DatabaseSpecific["busy_timeout_ms"] = busyTimeout
	if d.path != "" {
		if fi, err := os.Stat(d.path); err == nil {
			m.DatabaseSpecific["file_size_bytes"] = fi.Size()
			m.DatabaseSpecific["modified_time"] = fi.ModTime().Format("2006-01-02 15:04:05")
		}
	}
	var tableCount, viewCount, indexCount int
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='view'").Scan(&viewCount)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name NOT LIKE 'sqlite_%'").Scan(&indexCount)
	m.DatabaseSpecific["table_count"] = tableCount
	m.DatabaseSpecific["view_count"] = viewCount
	m.DatabaseSpecific["index_count"] = indexCount
	m.CostMs = time.Since(start).Milliseconds()
	return m, nil
}
func (d *SQLiteDriver) DetectCapability() (*CapabilitySet, error) {
	var version string
	if d.db != nil {
		d.db.QueryRow("SELECT sqlite_version()").Scan(&version)
	}
	if version == "" {
		version = "3.0.0"
	}
	majorVersion := 3
	if parts := strings.Split(version, "."); len(parts) > 0 {
		fmt.Sscanf(parts[0], "%d", &majorVersion)
	}
	return &CapabilitySet{
		DBType: "sqlite", Version: version, MajorVersion: majorVersion,
		HasRoles: false, HasDeny: false,
	}, nil
}

// ─── Tree / Context ─────────────────────────────────────────────────────

func (d *SQLiteDriver) GetTreeMetadata() TreeMetadata {
	return TreeMetadata{
		DBType: "sqlite",
		Levels: []TreeLevel{
			{Key: "schema", Label: "main", LabelKey: "tree.schema"},
		},
	}
}

func (d *SQLiteDriver) ResolveContext(arg string) DatabaseContext {
	return DatabaseContext{Schema: "main"}
}

// ─── Stub methods (SQLite has no server/user/role concepts) ─────────────

func (d *SQLiteDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) {
	return []map[string]interface{}{}, nil
}
func (d *SQLiteDriver) ListUsers(dbType string) ([]map[string]interface{}, error) {
	return nil, fmt.Errorf("SQLite 不支持用户管理，权限通过文件系统控制")
}
func (d *SQLiteDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	var pageSize, pageCount int
	d.db.QueryRow("PRAGMA page_size").Scan(&pageSize)
	d.db.QueryRow("PRAGMA page_count").Scan(&pageCount)
	totalMB := float64(pageSize*pageCount) / 1024 / 1024
	var fileBytes int64
	if d.path != "" {
		if fi, err := os.Stat(d.path); err == nil {
			fileBytes = fi.Size()
		}
	}
	return []map[string]interface{}{
		{"name": "main", "size_mb": totalMB, "file_bytes": fileBytes, "path": d.path},
	}, nil
}
func (d *SQLiteDriver) CreateDatabase(name string) error {
	if _, err := os.Create(name); err != nil {
		return fmt.Errorf("创建数据库文件失败: %w", err)
	}
	return nil
}
func (d *SQLiteDriver) DropDatabase(name string) error {
	if err := os.Remove(name); err != nil {
		return fmt.Errorf("删除数据库文件失败: %w", err)
	}
	return nil
}
func (d *SQLiteDriver) CreateUser(username, password string) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) DropUser(username string) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) AlterUserPassword(username, newPassword string) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) AlterUserLock(username string, lock bool) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) AlterUserRename(oldName, newName string) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) AlterUserDefaultSchema(username, schema string) error {
	return fmt.Errorf("SQLite 不支持用户管理")
}
func (d *SQLiteDriver) GrantPrivileges(username, database string, privileges []string) error {
	return fmt.Errorf("SQLite 不支持权限管理")
}
func (d *SQLiteDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error) {
	return nil, fmt.Errorf("SQLite 不支持权限管理")
}
func (d *SQLiteDriver) GetUserRoles(username string) ([]string, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) GetRoleInherit(roleName string) (bool, error) {
	return false, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) AlterRoleAttribute(roleName, attribute, value string) error {
	return fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) GetRoleMemberships(roleName string) ([]string, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) {
	return nil, fmt.Errorf("SQLite 不支持权限管理")
}
func (d *SQLiteDriver) ListRoles() ([]RoleInfo, error) {
	return nil, fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) CreateRole(name string) error {
	return fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) DropRole(name string) error {
	return fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) AddRoleMember(role, member string) error {
	return fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) RemoveRoleMember(role, member string) error {
	return fmt.Errorf("SQLite 不支持角色管理")
}
func (d *SQLiteDriver) ListDatabaseSchemas(database string) ([]string, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	rows, err := d.db.Query("PRAGMA database_list")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var schemas []string
	for rows.Next() {
		var seq int
		var name, file string
		if err := rows.Scan(&seq, &name, &file); err != nil {
			continue
		}
		schemas = append(schemas, name)
	}
	if schemas == nil {
		schemas = []string{"main"}
	}
	return schemas, nil
}

// ─── MSSQL stubs ────────────────────────────────────────────────────────
func (d *SQLiteDriver) ListLogins() ([]MSSQLLogin, error)        { return nil, nil }
func (d *SQLiteDriver) CreateLogin(req CreateLoginRequest) error { return nil }
func (d *SQLiteDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) {
	return nil, nil
}
func (d *SQLiteDriver) AlterLogin(loginName string, req AlterLoginRequest) error { return nil }
func (d *SQLiteDriver) GetLoginDetail(loginName string) (*LoginDetail, error)    { return nil, nil }
func (d *SQLiteDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) {
	return nil, nil
}
func (d *SQLiteDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error { return nil }
func (d *SQLiteDriver) DropDatabaseUser(database, userName string) error                  { return nil }
func (d *SQLiteDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error {
	return nil
}
func (d *SQLiteDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) { return nil, nil }
func (d *SQLiteDriver) FixOrphanedUser(database, userName, loginName string) error  { return nil }
func (d *SQLiteDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) {
	return nil, nil
}
func (d *SQLiteDriver) CheckGuestStatus(database string) (*GuestStatus, error) { return nil, nil }
func (d *SQLiteDriver) DisableGuest(database string) error                     { return nil }

// ─── Slow SQL stubs ─────────────────────────────────────────────────────
func (d *SQLiteDriver) KillSession(ctx context.Context, sessionID string) error { return nil }

// ─── Indexes / Constraints ──────────────────────────────────────────────
func (d *SQLiteDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	qtable := d.SQLQuoteIdent(table)
	type idxEntry struct {
		name   string
		unique bool
	}
	var entries []idxEntry
	idxRows, err := d.db.Query("PRAGMA index_list(" + qtable + ")")
	if err != nil {
		return nil, err
	}
	for idxRows.Next() {
		var seq int
		var idxName string
		var unique int
		var origin, partial string
		if err := idxRows.Scan(&seq, &idxName, &unique, &origin, &partial); err != nil {
			continue
		}
		if strings.HasPrefix(idxName, "sqlite_autoindex_") {
			continue
		}
		entries = append(entries, idxEntry{name: idxName, unique: unique == 0})
	}
	idxRows.Close()

	var result []map[string]interface{}
	for _, e := range entries {
		colRows, err := d.db.Query("PRAGMA index_info(" + d.SQLQuoteIdent(e.name) + ")")
		if err != nil {
			continue
		}
		for colRows.Next() {
			var colSeq, colIdx int
			var colName string
			if err := colRows.Scan(&colSeq, &colName, &colIdx); err != nil {
				continue
			}
			result = append(result, map[string]interface{}{
				"INDEX_NAME": e.name, "COLUMN_NAME": colName,
				"NON_UNIQUE": e.unique, "INDEX_TYPE": "BTREE",
			})
		}
		colRows.Close()
	}
	return result, nil
}
func (d *SQLiteDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	qtable := d.SQLQuoteIdent(table)
	fkRows, err := d.db.Query("PRAGMA foreign_key_list(" + qtable + ")")
	if err != nil {
		return nil, err
	}
	defer fkRows.Close()
	var result []map[string]interface{}
	for fkRows.Next() {
		var id, seq int
		var refTable, from, to string
		var onUpdate, onDelete, match string
		if err := fkRows.Scan(&id, &seq, &refTable, &from, &to, &onUpdate, &onDelete, &match); err != nil {
			continue
		}
		result = append(result, map[string]interface{}{
			"CONSTRAINT_NAME":        fmt.Sprintf("fk_%s_%s", table, from),
			"COLUMN_NAME":            from,
			"REFERENCED_TABLE_NAME":  refTable,
			"REFERENCED_COLUMN_NAME": to,
			"ON_UPDATE":              onUpdate,
			"ON_DELETE":              onDelete,
		})
	}
	return result, nil
}
func (d *SQLiteDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	var tableType string
	var rowCount int64
	err := d.db.QueryRow("SELECT type FROM sqlite_master WHERE name = ?", table).Scan(&tableType)
	if err != nil {
		return map[string]interface{}{}, fmt.Errorf("table not found: %s", table)
	}
	d.db.QueryRow("SELECT COUNT(*) FROM " + d.SQLQuoteIdent(table)).Scan(&rowCount)
	return map[string]interface{}{
		"engine":      "SQLite",
		"table_type":  tableType,
		"row_count":   rowCount,
		"row_format":  "",
		"comment":     "",
		"create_time": "",
		"update_time": "",
	}, nil
}
func (d *SQLiteDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string {
	var colDefs []string
	for _, col := range columns {
		name, _ := col["name"].(string)
		colType, _ := col["type"].(string)
		targetType := ConvertDDLType(sourceDBType, "sqlite", colType, "", colType)
		def := fmt.Sprintf("  \"%s\" %s", name, targetType)
		if nullable, ok := col["nullable"]; ok {
			if nullableStr, ok := nullable.(string); ok && nullableStr == "NO" {
				def += " NOT NULL"
			}
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf("CREATE TABLE \"%s\" (\n%s\n);", table, strings.Join(colDefs, ",\n"))
}
func (d *SQLiteDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string {
	return ""
}
func (d *SQLiteDriver) GetFullStructure(schema, table string) (*FullStructure, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}

	result := &FullStructure{}

	// Get table type and DDL from sqlite_master
	var tableType, ddl string
	err := d.db.QueryRow("SELECT type, sql FROM sqlite_master WHERE name = ?", table).Scan(&tableType, &ddl)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s", table)
	}
	if tableType == "view" {
		result.IsView = true
	}

	// Columns
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		tc := TableColumn{
			Name:     c.Name,
			Type:     c.Type,
			Nullable: c.Nullable == "YES",
			Default:  c.Default,
			HasDef:   c.Default != "",
			Key:      c.Key,
		}
		result.Columns = append(result.Columns, tc)
	}

	// Indexes (tables only)
	if !result.IsView {
		type idxEntry struct {
			name   string
			unique bool
		}
		var entries []idxEntry
		idxRows, err := d.db.Query("PRAGMA index_list(" + d.SQLQuoteIdent(table) + ")")
		if err == nil {
			for idxRows.Next() {
				var seq int
				var idxName string
				var unique int
				var origin, partial string
				if err := idxRows.Scan(&seq, &idxName, &unique, &origin, &partial); err != nil {
					continue
				}
				if strings.HasPrefix(idxName, "sqlite_autoindex_") {
					continue
				}
				entries = append(entries, idxEntry{name: idxName, unique: unique == 1})
			}
			idxRows.Close()
		}
		for _, e := range entries {
			idx := TableIndex{
				Name: e.name,
				Type: "BTREE",
			}
			if e.unique {
				idx.Type = "UNIQUE"
			}
			colRows, err := d.db.Query("PRAGMA index_info(" + d.SQLQuoteIdent(e.name) + ")")
			if err == nil {
				for colRows.Next() {
					var colSeq int
					var colName string
					var colIdx int
					if err := colRows.Scan(&colSeq, &colName, &colIdx); err != nil {
						continue
					}
					idx.Columns = append(idx.Columns, colName)
				}
				colRows.Close()
			}
			result.Indexes = append(result.Indexes, idx)
		}
	}

	if result.IsView {
		result.DDL = ddl
	} else {
		result.DDL = buildSQLiteDDL(table, result)
	}

	return result, nil
}

func buildSQLiteDDL(table string, st *FullStructure) string {
	q := func(s string) string { return `"` + s + `"` }
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", q(table)))
	for i, c := range st.Columns {
		b.WriteString("    " + q(c.Name) + " " + c.Type)
		if !c.Nullable {
			b.WriteString(" NOT NULL")
		}
		if c.HasDef && c.Default != "" {
			b.WriteString(" DEFAULT " + c.Default)
		}
		if c.Key == "PRI" {
			b.WriteString(" PRIMARY KEY")
		}
		if i < len(st.Columns)-1 || len(st.Indexes) > 0 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	for i, idx := range st.Indexes {
		unique := ""
		if idx.Type == "UNIQUE" {
			unique = "UNIQUE "
		}
		cols := make([]string, len(idx.Columns))
		for j, c := range idx.Columns {
			cols[j] = q(c)
		}
		b.WriteString(fmt.Sprintf("    CREATE %sINDEX %s ON %s (%s)", unique, q(idx.Name), q(table), strings.Join(cols, ", ")))
		if i < len(st.Indexes)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString(");")
	return b.String()
}

func (d *SQLiteDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	qtable := d.SQLQuoteIdent(table)
	rows, err := d.db.Query("PRAGMA table_info(" + qtable + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []AlterColumn
	for rows.Next() {
		var cid, pk int
		var name, typ, notNull string
		var defVal sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defVal, &pk); err != nil {
			continue
		}
		cols = append(cols, AlterColumn{
			Name:       name,
			Type:       typ,
			Nullable:   notNull != "1" && notNull != "true",
			DefaultVal: defVal.String,
		})
	}
	if cols == nil {
		cols = make([]AlterColumn, 0)
	}
	return cols, nil
}
func (d *SQLiteDriver) GetColumnTypes() []ColumnTypeInfo {
	return []ColumnTypeInfo{
		{Name: "INTEGER", NeedsLength: false, Description: "整数"},
		{Name: "REAL", NeedsLength: false, Description: "浮点数"},
		{Name: "TEXT", NeedsLength: false, Description: "文本"},
		{Name: "BLOB", NeedsLength: false, Description: "二进制"},
		{Name: "NUMERIC", NeedsLength: false, Description: "数值"},
		{Name: "BOOLEAN", NeedsLength: false, Description: "布尔"},
		{Name: "DATE", NeedsLength: false, Description: "日期"},
		{Name: "DATETIME", NeedsLength: false, Description: "日期时间"},
		{Name: "TIME", NeedsLength: false, Description: "时间"},
		{Name: "VARCHAR", NeedsLength: true, Description: "变长字符串"},
		{Name: "CHAR", NeedsLength: true, Description: "定长字符串"},
		{Name: "DECIMAL", NeedsLength: true, NeedsScale: true, Description: "定点数"},
		{Name: "FLOAT", NeedsLength: false, Description: "单精度浮点"},
		{Name: "DOUBLE", NeedsLength: false, Description: "双精度浮点"},
		{Name: "INT", NeedsLength: false, Description: "整数"},
		{Name: "BIGINT", NeedsLength: false, Description: "大整数"},
		{Name: "SMALLINT", NeedsLength: false, Description: "小整数"},
		{Name: "TINYINT", NeedsLength: false, Description: "微小整数"},
		{Name: "MEDIUMINT", NeedsLength: false, Description: "中等整数"},
		{Name: "CLOB", NeedsLength: false, Description: "字符大对象"},
	}
}
func (d *SQLiteDriver) GetIndexTypes() []IndexTypeInfo {
	return []IndexTypeInfo{
		{Name: "INDEX", Description: "普通索引"},
		{Name: "UNIQUE", Description: "唯一索引"},
	}
}

// ─── DDL Builder stubs ──────────────────────────────────────────────────
func (d *SQLiteDriver) FormatSQLValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case string:
		return "'" + strings.ReplaceAll(v, "'", "''") + "'"
	case []byte:
		return FormatSQLBytes(v)
	case bool:
		if v {
			return "1"
		}
		return "0"
	case int:
		return fmt.Sprintf("%d", v)
	case int8:
		return fmt.Sprintf("%d", v)
	case int16:
		return fmt.Sprintf("%d", v)
	case int32:
		return fmt.Sprintf("%d", v)
	case int64:
		return fmt.Sprintf("%d", v)
	case uint:
		return fmt.Sprintf("%d", v)
	case uint8:
		return fmt.Sprintf("%d", v)
	case uint16:
		return fmt.Sprintf("%d", v)
	case uint32:
		return fmt.Sprintf("%d", v)
	case uint64:
		return fmt.Sprintf("%d", v)
	case float32:
		return fmt.Sprintf("%g", v)
	case float64:
		return fmt.Sprintf("%g", v)
	case time.Time:
		return "'" + v.Format("2006-01-02 15:04:05") + "'"
	default:
		return "'" + strings.ReplaceAll(fmt.Sprintf("%v", v), "'", "''") + "'"
	}
}
func (d *SQLiteDriver) AlterColumnClause(columnName, colType, columnDef string) string { return "" }
func (d *SQLiteDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;", tableName, colList, strings.Join(rowValues, ",\n"))
}
func (d *SQLiteDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string {
	if sourceSchema != "" && targetSchema != "" {
		ddl = strings.ReplaceAll(ddl, `"`+strings.ToUpper(sourceSchema)+`".`, `"`+strings.ToUpper(targetSchema)+`".`)
		ddl = strings.ReplaceAll(ddl, `"`+strings.ToLower(sourceSchema)+`".`, `"`+strings.ToLower(targetSchema)+`".`)
	}
	return ddl
}
func (d *SQLiteDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string {
	return nil
}
func (d *SQLiteDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string {
	return fmt.Sprintf("DROP INDEX \"%s\";", indexName)
}
func (d *SQLiteDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string {
	var parts []string
	if length != "" && length != "0" {
		colType = fmt.Sprintf("%s(%s)", colType, length)
	}
	def := fmt.Sprintf("ALTER TABLE %s ADD COLUMN \"%s\" %s", qualifiedTable, columnName, colType)
	if !nullable {
		def += " NOT NULL"
	}
	if defaultVal != "" {
		def += " DEFAULT " + defaultVal
	}
	parts = append(parts, def+";")
	return parts
}
func (d *SQLiteDriver) DropColumnDDL(qualifiedTable, columnName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN \"%s\";", qualifiedTable, columnName)
}
func (d *SQLiteDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string {
	unique := ""
	if strings.ToUpper(indexType) == "UNIQUE" {
		unique = "UNIQUE "
	}
	quotedCols := make([]string, len(columns))
	for i, c := range columns {
		quotedCols[i] = fmt.Sprintf("\"%s\"", c)
	}
	return fmt.Sprintf("CREATE %sINDEX \"%s\" ON %s (%s);", unique, indexName, qualifiedTable, strings.Join(quotedCols, ", "))
}
func (d *SQLiteDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) {
	nullable := col.Nullable == nil || *col.Nullable
	defaultVal := ""
	if col.HasDef != nil && *col.HasDef {
		defaultVal = col.Default
	}
	parts := d.AddColumnDDL(table, col.Name, col.Type, col.Length, nullable, defaultVal, col.Comment, col.After)
	return strings.Join(parts, ";\n"), "", nil, false, nil
}
func (d *SQLiteDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	return nil, nil, []string{"SQLite 不支持直接修改列，需要重建表"}, true, fmt.Errorf("SQLite 不支持 MODIFY COLUMN，请使用表重建方式")
}
func (d *SQLiteDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) {
	stmt := d.DropColumnDDL(table, colName)
	return stmt, "", nil, true, nil
}
func (d *SQLiteDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) {
	stmt := d.AddIndexDDL(table, idx.Name, idx.Type, idx.Columns)
	return stmt, "", nil
}
func (d *SQLiteDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) {
	stmt := d.DropIndexDDL(table, schema, idxName)
	return stmt, "", nil, true, nil
}
func (d *SQLiteDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) {
	return "", "", fmt.Errorf("SQLite 不支持索引注释")
}
func (d *SQLiteDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) {
	return "", "", fmt.Errorf("SQLite 不支持动态添加约束")
}
func (d *SQLiteDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) {
	return "", "", fmt.Errorf("SQLite 不支持动态删除约束")
}
func (d *SQLiteDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) {
	return "", "", fmt.Errorf("SQLite 不支持表注释")
}
