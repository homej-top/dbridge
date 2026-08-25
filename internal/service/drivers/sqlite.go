package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

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
	if err != nil { return nil, err }
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

	rows, err := d.db.Query(fmt.Sprintf("SELECT rowid, * FROM %s LIMIT ? OFFSET ?", qtable), pageSize, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columns, _ := rows.Columns()
	// Skip the rowid column in output
	if len(columns) > 0 && columns[0] == "rowid" {
		columns = columns[1:]
	}

	var data [][]interface{}
	for rows.Next() {
		vals := make([]interface{}, len(columns)+1)
		ptrs := make([]interface{}, len(columns)+1)
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		rows.Scan(ptrs...)
		row := make([]interface{}, len(columns))
		for i := range columns {
			switch v := vals[i+1].(type) {
			case []byte:
				row[i] = string(v)
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

// ─── Server Info ────────────────────────────────────────────────────────

func (d *SQLiteDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	return map[string]interface{}{
		"type": "SQLite", "path": d.path, "version": "3.x",
	}, nil
}

func (d *SQLiteDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	if d.db == nil {
		return nil, fmt.Errorf("not connected")
	}
	var tableCount int
	d.db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").Scan(&tableCount)
	return map[string]interface{}{
		"table_count":  tableCount,
		"path":         d.path,
		"readonly":     d.readonly,
	}, nil
}

func (d *SQLiteDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) { return nil, nil }
func (d *SQLiteDriver) DetectCapability() (*CapabilitySet, error) {
	return &CapabilitySet{
		DBType: "sqlite", Version: "3.x",
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

func (d *SQLiteDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) { return nil, nil }
func (d *SQLiteDriver) ListUsers(dbType string) ([]map[string]interface{}, error)     { return nil, nil }
func (d *SQLiteDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) { return nil, nil }
func (d *SQLiteDriver) CreateDatabase(name string) error                              { return nil }
func (d *SQLiteDriver) DropDatabase(name string) error                                { return nil }
func (d *SQLiteDriver) CreateUser(username, password string) error                    { return nil }
func (d *SQLiteDriver) DropUser(username string) error                                { return nil }
func (d *SQLiteDriver) AlterUserPassword(username, newPassword string) error          { return nil }
func (d *SQLiteDriver) AlterUserLock(username string, lock bool) error                { return nil }
func (d *SQLiteDriver) AlterUserRename(oldName, newName string) error                 { return nil }
func (d *SQLiteDriver) AlterUserDefaultSchema(username, schema string) error          { return nil }
func (d *SQLiteDriver) GrantPrivileges(username, database string, privileges []string) error { return nil }
func (d *SQLiteDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error)   { return nil, nil }
func (d *SQLiteDriver) GetUserRoles(username string) ([]string, error)                { return nil, nil }
func (d *SQLiteDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error)   { return nil, nil }
func (d *SQLiteDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error)     { return nil, nil }
func (d *SQLiteDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error)      { return nil, nil }
func (d *SQLiteDriver) GetRoleInherit(roleName string) (bool, error)                  { return false, nil }
func (d *SQLiteDriver) AlterRoleAttribute(roleName, attribute, value string) error    { return nil }
func (d *SQLiteDriver) GetRoleMemberships(roleName string) ([]string, error)          { return nil, nil }
func (d *SQLiteDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) { return nil, nil }
func (d *SQLiteDriver) ListRoles() ([]RoleInfo, error)                               { return nil, nil }
func (d *SQLiteDriver) CreateRole(name string) error                                 { return nil }
func (d *SQLiteDriver) DropRole(name string) error                                   { return nil }
func (d *SQLiteDriver) AddRoleMember(role, member string) error                      { return nil }
func (d *SQLiteDriver) RemoveRoleMember(role, member string) error                   { return nil }
func (d *SQLiteDriver) ListDatabaseSchemas(database string) ([]string, error)         { return nil, nil }

// ─── MSSQL stubs ────────────────────────────────────────────────────────
func (d *SQLiteDriver) ListLogins() ([]MSSQLLogin, error)                            { return nil, nil }
func (d *SQLiteDriver) CreateLogin(req CreateLoginRequest) error                      { return nil }
func (d *SQLiteDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) { return nil, nil }
func (d *SQLiteDriver) AlterLogin(loginName string, req AlterLoginRequest) error      { return nil }
func (d *SQLiteDriver) GetLoginDetail(loginName string) (*LoginDetail, error)         { return nil, nil }
func (d *SQLiteDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) { return nil, nil }
func (d *SQLiteDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error { return nil }
func (d *SQLiteDriver) DropDatabaseUser(database, userName string) error             { return nil }
func (d *SQLiteDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error { return nil }
func (d *SQLiteDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) { return nil, nil }
func (d *SQLiteDriver) FixOrphanedUser(database, userName, loginName string) error   { return nil }
func (d *SQLiteDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) { return nil, nil }
func (d *SQLiteDriver) CheckGuestStatus(database string) (*GuestStatus, error)       { return nil, nil }
func (d *SQLiteDriver) DisableGuest(database string) error                           { return nil }

// ─── Indexes / Constraints ──────────────────────────────────────────────
func (d *SQLiteDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) { return nil, nil }
func (d *SQLiteDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) { return nil, nil }
func (d *SQLiteDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) { return nil, nil }
func (d *SQLiteDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string { return "" }
func (d *SQLiteDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string { return "" }
func (d *SQLiteDriver) GetFullStructure(schema, table string) (*FullStructure, error) { return nil, nil }
func (d *SQLiteDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) { return nil, nil }
func (d *SQLiteDriver) GetColumnTypes() []ColumnTypeInfo { return nil }
func (d *SQLiteDriver) GetIndexTypes() []IndexTypeInfo { return nil }

// ─── DDL Builder stubs ──────────────────────────────────────────────────
func (d *SQLiteDriver) FormatSQLValue(val interface{}) string                     { return fmt.Sprintf("%v", val) }
func (d *SQLiteDriver) AlterColumnClause(columnName, colType, columnDef string) string { return "" }
func (d *SQLiteDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string { return "" }
func (d *SQLiteDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string { return "" }
func (d *SQLiteDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string { return nil }
func (d *SQLiteDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string { return "" }
func (d *SQLiteDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string { return nil }
func (d *SQLiteDriver) DropColumnDDL(qualifiedTable, columnName string) string { return "" }
func (d *SQLiteDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string { return "" }
func (d *SQLiteDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) { return "", "", nil, false, nil }
func (d *SQLiteDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) { return nil, nil, nil, false, nil }
func (d *SQLiteDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) { return "", "", nil, false, nil }
func (d *SQLiteDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) { return "", "", nil }
func (d *SQLiteDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) { return "", "", nil, false, nil }
func (d *SQLiteDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) { return "", "", nil }
func (d *SQLiteDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) { return "", "", nil }
func (d *SQLiteDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) { return "", "", nil }
func (d *SQLiteDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) { return "", "", nil }
