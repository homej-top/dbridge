package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OracleDriver implements DatabaseDriver for Oracle databases.
// Uses github.com/sijms/go-ora/v2 (pure Go Oracle driver, no CGO required).
// Add to go.mod:
//
//	require github.com/sijms/go-ora/v2 v2.8.19
//
// The driver registration name is "oracle".
// go-ora will be auto-registered via blank import in the caller:
//
//	_ "github.com/sijms/go-ora/v2"
//
// To use:
//
//	import (
//	    _ "github.com/sijms/go-ora/v2"
//	    "github.com/sijms/go-ora/v2"
//	)
//
// And connect using:
//
//	go-ora://user:pass@host:port/service_name
type OracleDriver struct {
	db     *sql.DB
	cfg    DriverConfig
	pooled bool
}

func NewOracleDriver(cfg DriverConfig) (DatabaseDriver, *sql.DB, error) {
	var db *sql.DB
	if cfg.DB != nil {
		db = cfg.DB
	} else {
		port := cfg.Port
		if port == 0 {
			port = 1521
		}
		connectMode := cfg.OracleConnectMode
		if connectMode == "" {
			connectMode = "service_name"
		}
		service := cfg.OracleService
		if service == "" {
			service = cfg.Database
		}
		dsn := fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
			cfg.Username, cfg.Password, cfg.Host, port, service)
		if connectMode == "sid" {
			dsn = fmt.Sprintf("oracle://%s:%s@%s:%d/%s?SID=%s",
				cfg.Username, cfg.Password, cfg.Host, port, service, service)
		}
		var err error
		db, err = sql.Open("oracle", dsn)
		if err != nil {
			return nil, nil, err
		}
		db.SetMaxOpenConns(cfg.MaxConnections)
		db.SetMaxIdleConns(cfg.MaxConnections / 2)
		db.SetConnMaxLifetime(10 * time.Minute)
	}

	if cfg.DB == nil {
		if err := db.Ping(); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("oracle connection failed: %w", err)
		}
	}

	// Set Oracle-specific session parameters
	if cfg.OracleRole != "" && cfg.OracleRole != "default" {
		roleSQL := fmt.Sprintf("ALTER SESSION SET CURRENT_SCHEMA = %s", cfg.Username)
		db.Exec(roleSQL)
	}

	return &OracleDriver{db: db, cfg: cfg, pooled: cfg.DB != nil}, db, nil
}

func (d *OracleDriver) Ping() error { return d.db.Ping() }
func (d *OracleDriver) Close() error {
	if d.pooled {
		return nil
	}
	return d.db.Close()
}
func (d *OracleDriver) DBType() string  { return "oracle" }
func (d *OracleDriver) Dialect() string { return "oracle" }

// Oracle uses the concept of "owner" = schema (uppercase by convention)
func (d *OracleDriver) currentUser() (string, error) {
	var u string
	err := d.db.QueryRow("SELECT USER FROM DUAL").Scan(&u)
	return strings.ToUpper(u), err
}

// ─── Schema Discovery ──────────────────────────────────────────────────────

func (d *OracleDriver) ListSchemas() ([]SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT username FROM all_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var schemas []SchemaInfo
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		schemas = append(schemas, SchemaInfo{Name: name})
	}
	if schemas == nil {
		schemas = make([]SchemaInfo, 0)
	}
	return schemas, nil
}

func (d *OracleDriver) ListSchemaNames() ([]string, error) {
	rows, err := d.db.Query(`SELECT username FROM all_users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		names = append(names, name)
	}
	if names == nil {
		names = make([]string, 0)
	}
	return names, nil
}

func (d *OracleDriver) ListObjects(schema string) (*SchemaInfo, error) {
	schema = strings.ToUpper(schema)
	rows, err := d.db.Query(`SELECT object_name, object_type FROM all_objects
		WHERE owner = :1 AND object_type IN ('TABLE', 'VIEW')
		ORDER BY object_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]ObjectInfo, 0)
	views := make([]ObjectInfo, 0)
	for rows.Next() {
		var name, objType string
		if err := rows.Scan(&name, &objType); err != nil {
			continue
		}
		if objType == "VIEW" {
			views = append(views, ObjectInfo{Name: name})
		} else {
			tables = append(tables, ObjectInfo{Name: name})
		}
	}
	return &SchemaInfo{Name: schema, Tables: tables, Views: views}, nil
}

func (d *OracleDriver) ListSchemaDetail() ([]SchemaDetailItem, error) {
	rows, err := d.db.Query(`SELECT
		u.username,
		COUNT(CASE WHEN o.object_type = 'TABLE' THEN 1 END),
		COUNT(CASE WHEN o.object_type = 'VIEW' THEN 1 END),
		'' AS charset, '' AS collation
	FROM all_users u
	LEFT JOIN all_objects o ON u.username = o.owner AND o.object_type IN ('TABLE', 'VIEW')
	GROUP BY u.username ORDER BY u.username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []SchemaDetailItem
	for rows.Next() {
		var item SchemaDetailItem
		if err := rows.Scan(&item.Name, &item.TableCount, &item.ViewCount, &item.Charset, &item.Collation); err != nil {
			continue
		}
		items = append(items, item)
	}
	if items == nil {
		items = make([]SchemaDetailItem, 0)
	}
	return items, nil
}

func (d *OracleDriver) ListTables(schema string) ([]TableListItem, error) {
	schema = strings.ToUpper(schema)
	rows, err := d.db.Query(`SELECT
		o.object_name,
		o.object_type,
		NULL AS engine,
		t.num_rows,
		COALESCE(c.comments, ''),
		o.created,
		o.last_ddl_time
	FROM all_objects o
	LEFT JOIN all_tables t ON o.owner = t.owner AND o.object_name = t.table_name
	LEFT JOIN all_tab_comments c ON o.owner = c.owner AND o.object_name = c.table_name
	WHERE o.owner = :1 AND o.object_type IN ('TABLE', 'VIEW')
	ORDER BY o.object_name`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []TableListItem
	for rows.Next() {
		var item TableListItem
		var tableType, engine, comment, createTime, updateTime sql.NullString
		var rowCount sql.NullInt64
		if err := rows.Scan(&item.Name, &tableType, &engine, &rowCount, &comment, &createTime, &updateTime); err != nil {
			continue
		}
		item.Type = "table"
		if tableType.Valid && tableType.String == "VIEW" {
			item.Type = "view"
		}
		if engine.Valid {
			item.Engine = &engine.String
		}
		if rowCount.Valid {
			item.RowCount = &rowCount.Int64
		}
		if comment.Valid {
			item.Comment = comment.String
		}
		if createTime.Valid {
			item.CreateTime = &createTime.String
		}
		if updateTime.Valid {
			item.UpdateTime = &updateTime.String
		}
		items = append(items, item)
	}
	if items == nil {
		items = make([]TableListItem, 0)
	}
	return items, nil
}

// ─── Column Metadata ──────────────────────────────────────────────────────

func (d *OracleDriver) GetColumns(schema, table string) ([]ColumnDetail, error) {
	// Preserve original case - Oracle all_tab_columns matches case-insensitively for unquoted identifiers,
	// and exactly for quoted identifiers. Do NOT uppercase as it breaks quoted mixed-case table names.
	schemaEsc := strings.ReplaceAll(schema, "'", "''")
	tableEsc := strings.ReplaceAll(table, "'", "''")
	query := fmt.Sprintf(`SELECT tc.COLUMN_NAME, tc.DATA_TYPE, NVL(TO_CHAR(tc.DATA_LENGTH),' ') AS len, tc.NULLABLE,
		tc.DATA_DEFAULT, cc.comments AS col_comment,
		CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 'PRI' ELSE '' END AS key_info
		FROM all_tab_columns tc
		LEFT JOIN (
			SELECT acc.OWNER, acc.COLUMN_NAME FROM all_cons_columns acc
			JOIN all_constraints ac ON acc.CONSTRAINT_NAME = ac.CONSTRAINT_NAME
			WHERE ac.CONSTRAINT_TYPE = 'P' AND acc.OWNER = '%s' AND ac.TABLE_NAME = '%s'
		) pk ON tc.OWNER = pk.OWNER AND tc.COLUMN_NAME = pk.COLUMN_NAME
		LEFT JOIN all_col_comments cc ON cc.OWNER = tc.OWNER AND cc.TABLE_NAME = tc.TABLE_NAME AND cc.COLUMN_NAME = tc.COLUMN_NAME
		WHERE tc.OWNER='%s' AND tc.TABLE_NAME='%s' ORDER BY tc.COLUMN_ID`,
		schemaEsc, tableEsc,
		schemaEsc, tableEsc)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnDetail
	for rows.Next() {
		var c ColumnDetail
		var defVal, comment, keyStr sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &c.Length, &c.Nullable, &defVal, &comment, &keyStr); err != nil {
			continue
		}
		c.Default = defVal.String
		c.Comment = comment.String
		c.Key = keyStr.String
		cols = append(cols, c)
	}
	if cols == nil {
		cols = make([]ColumnDetail, 0)
	}
	return cols, nil
}

func (d *OracleDriver) GetColumnDetails(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, DATA_TYPE, DATA_DEFAULT, NULLABLE
		FROM all_tab_columns WHERE OWNER = :1 AND TABLE_NAME = :2
		ORDER BY COLUMN_ID`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *OracleDriver) GetDDL(schema, table string) (string, error) {
	schema = strings.ToUpper(schema)
	table = strings.ToUpper(table)

	// Oracle doesn't have a simple "SHOW CREATE TABLE" like MySQL.
	// Use DBMS_METADATA to get the DDL.
	var ddl sql.NullString
	err := d.db.QueryRow(`SELECT DBMS_METADATA.GET_DDL('TABLE', :1, :2) FROM DUAL`, table, schema).Scan(&ddl)
	if err != nil {
		// Fallback: reconstruct from column definitions
		return d.reconstructDDL(schema, table)
	}
	if ddl.Valid {
		return ddl.String, nil
	}
	return d.reconstructDDL(schema, table)
}

func (d *OracleDriver) reconstructDDL(schema, table string) (string, error) {
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table not found: %s.%s", schema, table)
	}

	var colDefs []string
	for _, c := range cols {
		colType := c.Type
		if c.Length != "" && c.Length != "0" {
			colType = fmt.Sprintf("%s(%s)", c.Type, c.Length)
		}
		def := fmt.Sprintf(`  "%s" %s`, c.Name, colType)
		if c.Nullable == "N" {
			def += " NOT NULL"
		}
		if c.Default != "" {
			def += " DEFAULT " + c.Default
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf(`CREATE TABLE "%s"."%s" (\n%s\n);`, schema, table, strings.Join(colDefs, ",\n")), nil
}

func (d *OracleDriver) GetViewDefinition(schema, view string) (string, error) {
	schema = strings.ToUpper(schema)
	view = strings.ToUpper(view)

	// Try ALL_VIEWS.TEXT first (more reliable than DBMS_METADATA for non-DBA users)
	var text sql.NullString
	err := d.db.QueryRow(
		`SELECT TEXT FROM all_views WHERE OWNER = :1 AND VIEW_NAME = :2`,
		schema, view,
	).Scan(&text)
	if err == nil && text.Valid && text.String != "" {
		return "CREATE OR REPLACE VIEW " + schema + "." + view + " AS\n" + text.String, nil
	}

	// Fallback: try without OWNER filter (views accessible to current user)
	err = d.db.QueryRow(
		`SELECT TEXT FROM all_views WHERE VIEW_NAME = :1`,
		view,
	).Scan(&text)
	if err == nil && text.Valid && text.String != "" {
		return "CREATE OR REPLACE VIEW " + view + " AS\n" + text.String, nil
	}

	// Last resort: DBMS_METADATA
	err = d.db.QueryRow(
		`SELECT DBMS_METADATA.GET_DDL('VIEW', :1, :2) FROM DUAL`,
		view, schema,
	).Scan(&text)
	if err == nil && text.Valid && text.String != "" {
		return text.String, nil
	}

	return "", fmt.Errorf("view %s.%s not found or not accessible", schema, view)
}

// ─── Query Execution ──────────────────────────────────────────────────────

func (d *OracleDriver) ExecuteQuery(sql string, schema string) (*QueryResult, error) {
	if schema != "" {
		// Best-effort schema switch — the SQL may already use qualified names.
		// Ignore errors like ORA-01031 (insufficient privileges) or
		// ORA-01435 (user does not exist) when the DDL already specifies the owner.
		_, _ = d.db.Exec(`ALTER SESSION SET CURRENT_SCHEMA = "` + strings.ToUpper(schema) + `"`)
	}

	start := time.Now()
	isSelect := IsSelectStatement(sql)

	if isSelect {
		rows, err := d.db.Query(sql)
		if err != nil {
			return nil, fmt.Errorf("query error: %w", err)
		}
		defer rows.Close()

		colNames, resultRows, err := ScanQueryResult(rows)
		if err != nil {
			return nil, err
		}
		return &QueryResult{
			Columns:   colNames,
			Rows:      resultRows,
			TotalRows: int64(len(resultRows)),
			Duration:  time.Since(start).Milliseconds(),
			IsSelect:  true,
		}, nil
	}

	// go-ora does not support multiple ;-separated statements in a single Exec call.
	// Split multi-statement DDL and execute each part individually.
	if !isSelect && strings.Contains(sql, ";") {
		return d.executeMultiStatement(sql, start)
	}

	res, err := d.db.Exec(sql)
	if err != nil {
		return nil, fmt.Errorf("exec error: %w", err)
	}
	affected, _ := res.RowsAffected()
	msg := fmt.Sprintf("Query OK, %d rows affected", affected)
	return &QueryResult{
		Columns:      []string{"result"},
		Rows:         [][]interface{}{{msg}},
		TotalRows:    1,
		Duration:     time.Since(start).Milliseconds(),
		IsSelect:     false,
		AffectedRows: affected,
	}, nil
}

// executeMultiStatement splits Oracle DDL by ; and executes each part.
func (d *OracleDriver) executeMultiStatement(sql string, start time.Time) (*QueryResult, error) {
	statements := strings.Split(sql, ";")
	var totalAffected int64
	for _, stmt := range statements {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		res, err := d.db.Exec(stmt)
		if err != nil {
			return nil, fmt.Errorf("exec error: %w", err)
		}
		if res != nil {
			aff, _ := res.RowsAffected()
			totalAffected += aff
		}
	}
	msg := fmt.Sprintf("Query OK, %d rows affected", totalAffected)
	return &QueryResult{
		Columns:      []string{"result"},
		Rows:         [][]interface{}{{msg}},
		TotalRows:    1,
		Duration:     time.Since(start).Milliseconds(),
		IsSelect:     false,
		AffectedRows: totalAffected,
	}, nil
}

func (d *OracleDriver) GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	schema = strings.ToUpper(schema)
	table = strings.ToUpper(table)
	offset := (page - 1) * pageSize

	// Oracle uses ROWNUM with subquery for pagination
	var total int64
	d.db.QueryRow(`SELECT COUNT(*) FROM "` + schema + `"."` + table + `"`).Scan(&total)

	query := fmt.Sprintf(`SELECT * FROM (
		SELECT a.*, ROWNUM rnum FROM (
			SELECT * FROM "%s"."%s" ORDER BY 1
		) a WHERE ROWNUM <= %d
	) WHERE rnum > %d`, schema, table, page*pageSize, offset)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colNames, resultRows, err := ScanQueryResult(rows)
	if err != nil {
		return nil, err
	}

	// Remove the rnum column from results
	if len(colNames) > 0 && colNames[len(colNames)-1] == "RNUM" {
		colNames = colNames[:len(colNames)-1]
		for i := range resultRows {
			if len(resultRows[i]) > 0 {
				resultRows[i] = resultRows[i][:len(resultRows[i])-1]
			}
		}
	}

	return &TableDataResult{
		Columns:  colNames,
		Rows:     resultRows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// ─── DDL Execution ────────────────────────────────────────────────────────

func (d *OracleDriver) ExecuteDDL(ddl string) (int64, error) {
	res, err := d.db.Exec(ddl)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// Oracle DDL batch execution (each statement separately)
func (d *OracleDriver) ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error) {
	statements := SplitDDLWithDialect(ddl, "oracle")
	total = int64(len(statements))

	for _, stmt := range statements {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}
		// Auto-commit each statement
		res, e := d.db.Exec(s)
		if e != nil {
			fail++
			errors = append(errors, fmt.Sprintf("statement %d: %s", success+fail, e.Error()))
			continue
		}
		success++
		if res != nil {
			ra, _ := res.RowsAffected()
			rowsAffected += ra
		}
	}
	return
}

// ─── DDL Helpers ──────────────────────────────────────────────────────────

func (d *OracleDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string {
	var parts []string
	if length != "" && length != "0" {
		parts = append(parts, fmt.Sprintf("%s(%s)", colType, length))
	} else {
		parts = append(parts, colType)
	}
	ddl := fmt.Sprintf(`ALTER TABLE %s MODIFY ("%s" %s)`, qualifiedTable, columnName, strings.Join(parts, " "))
	result := []string{ddl}
	if comment != "" {
		result = append(result, fmt.Sprintf(`COMMENT ON COLUMN %s."%s" IS '%s'`, qualifiedTable, columnName,
			strings.ReplaceAll(comment, "'", "''")))
	}
	return result
}

func (d *OracleDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string {
	return fmt.Sprintf(`DROP INDEX "%s"."%s"`, strings.ToUpper(schema), strings.ToUpper(indexName))
}

func (d *OracleDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string {
	var parts []string
	if length != "" && length != "0" {
		parts = append(parts, fmt.Sprintf("%s(%s)", colType, length))
	} else {
		parts = append(parts, colType)
	}
	if !nullable {
		parts = append(parts, "NOT NULL")
	}
	if defaultVal != "" {
		parts = append(parts, "DEFAULT "+defaultVal)
	}
	ddl := fmt.Sprintf(`ALTER TABLE %s ADD ("%s" %s)`, qualifiedTable, columnName, strings.Join(parts, " "))
	result := []string{ddl}
	if comment != "" {
		result = append(result, fmt.Sprintf(`COMMENT ON COLUMN %s."%s" IS '%s'`, qualifiedTable, columnName,
			strings.ReplaceAll(comment, "'", "''")))
	}
	return result
}

func (d *OracleDriver) DropColumnDDL(qualifiedTable, columnName string) string {
	return fmt.Sprintf(`ALTER TABLE %s DROP COLUMN "%s"`, qualifiedTable, columnName)
}

func (d *OracleDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string {
	var colList []string
	for _, c := range columns {
		colList = append(colList, `"`+c+`"`)
	}
	if indexType == "UNIQUE" {
		return fmt.Sprintf(`CREATE UNIQUE INDEX "%s" ON %s (%s)`, indexName, qualifiedTable, strings.Join(colList, ", "))
	}
	return fmt.Sprintf(`CREATE INDEX "%s" ON %s (%s)`, indexName, qualifiedTable, strings.Join(colList, ", "))
}

func (d *OracleDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT index_name, column_name, uniqueness FROM all_ind_columns
		JOIN all_indexes USING (owner, index_name)
		WHERE table_owner = :1 AND table_name = :2
		ORDER BY index_name, column_position`, strings.ToUpper(schema), strings.ToUpper(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *OracleDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT ac.constraint_name, acc.column_name, ac.r_owner, ac.r_constraint_name
		FROM all_constraints ac
		JOIN all_cons_columns acc ON ac.constraint_name = acc.constraint_name AND ac.owner = acc.owner
		WHERE ac.owner = :1 AND ac.table_name = :2 AND ac.constraint_type = 'R'`,
		strings.ToUpper(schema), strings.ToUpper(table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *OracleDriver) GetFullStructure(schema, table string) (*FullStructure, error) {
	result := &FullStructure{}
	// Note: do NOT ToUpper — Oracle objects created with double-quoted
	// identifiers (via quoteIdent) preserve their original case.

	query := fmt.Sprintf(`SELECT tc.COLUMN_NAME, tc.DATA_TYPE, NVL(TO_CHAR(tc.DATA_LENGTH),' ') AS len, tc.NULLABLE,
		CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 'PRI' ELSE '' END AS key_info,
		tc.DATA_DEFAULT, cc.comments AS col_comment
		FROM all_tab_columns tc
		LEFT JOIN (
			SELECT acc.OWNER, acc.COLUMN_NAME FROM all_cons_columns acc
			JOIN all_constraints ac ON acc.CONSTRAINT_NAME = ac.CONSTRAINT_NAME
			WHERE ac.CONSTRAINT_TYPE = 'P' AND acc.OWNER = '%s' AND ac.TABLE_NAME = '%s'
		) pk ON tc.OWNER = pk.OWNER AND tc.COLUMN_NAME = pk.COLUMN_NAME
		LEFT JOIN all_col_comments cc ON cc.OWNER = tc.OWNER AND cc.TABLE_NAME = tc.TABLE_NAME AND cc.COLUMN_NAME = tc.COLUMN_NAME
		WHERE tc.OWNER='%s' AND tc.TABLE_NAME='%s' ORDER BY tc.COLUMN_ID`,
		strings.ReplaceAll(schema, "'", "''"), strings.ReplaceAll(table, "'", "''"),
		strings.ReplaceAll(schema, "'", "''"), strings.ReplaceAll(table, "'", "''"))

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("oracle columns query failed: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var col TableColumn
		var nullableStr string
		var keyStr sql.NullString
		var defStr sql.NullString
		var commentStr sql.NullString
		if err := rows.Scan(&col.Name, &col.Type, &col.Length, &nullableStr, &keyStr, &defStr, &commentStr); err != nil {
			return nil, fmt.Errorf("oracle column scan failed: %w", err)
		}
		col.Nullable = nullableStr == "Y"
		if keyStr.Valid {
			col.Key = keyStr.String
		}
		// Default value (Oracle DATA_DEFAULT can be NULL)
		if defStr.Valid {
			col.Default = strings.TrimSpace(defStr.String)
			if col.Default != "" {
				col.HasDef = true
			}
		}
		// Column comment
		if commentStr.Valid {
			col.Comment = commentStr.String
		}
		result.Columns = append(result.Columns, col)
	}
	if rows.Err() != nil {
		return nil, fmt.Errorf("oracle rows error: %w", rows.Err())
	}

	var ddl sql.NullString
	if err := d.db.QueryRow(`SELECT DBMS_METADATA.GET_DDL('TABLE',:1,:2) FROM DUAL`, table, schema).Scan(&ddl); err == nil && ddl.Valid {
		result.DDL = ddl.String
		// Strip platform-specific clauses for portability
		result.DDL = regexp.MustCompile(`(?i)\s+TABLESPACE\s+"[^"]*"`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+STORAGE\s*\([^)]*\)`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+SEGMENT CREATION\s+(IMMEDIATE|DEFERRED)`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+(PCTFREE|PCTUSED|PCTINCREASE)\s+\d+`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+(INITRANS|MAXTRANS)\s+\d+`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+(BUFFER_POOL|FLASH_CACHE|CELL_FLASH_CACHE)\s+\w+`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+COMPUTE STATISTICS`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+ENABLE\b`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+NOCACHE\s+LOGGING`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+ENABLE\s+STORAGE IN ROW`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+KEEP_DUPLICATES`).ReplaceAllString(result.DDL, "")
		result.DDL = regexp.MustCompile(`(?i)\s+ALLOW NONSCHEMA DISALLOW ANYSCHEMA`).ReplaceAllString(result.DDL, "")
	}

	// Append column comments and table comment to DDL
	if result.DDL != "" {
		for _, col := range result.Columns {
			if col.Comment != "" {
				safeComment := strings.ReplaceAll(col.Comment, "'", "''")
				result.DDL += fmt.Sprintf("\nCOMMENT ON COLUMN %s.%s.%s IS '%s';", quoteOracle(schema), quoteOracle(table), quotePG(col.Name), safeComment)
			}
		}
		if result.TableMeta.Comment != "" {
			safeComment := strings.ReplaceAll(result.TableMeta.Comment, "'", "''")
			result.DDL += fmt.Sprintf("\nCOMMENT ON TABLE %s.%s IS '%s';", quoteOracle(schema), quoteOracle(table), safeComment)
		}
	}

	// Table meta (comment, create/update time from dictionary views)
	var m TableMeta
	d.db.QueryRow(`SELECT COALESCE(c.comments, ''),
		COALESCE(TO_CHAR(o.created, 'YYYY-MM-DD HH24:MI:SS'), ''),
		COALESCE(TO_CHAR(o.last_ddl_time, 'YYYY-MM-DD HH24:MI:SS'), '')
		FROM all_tables t
		LEFT JOIN all_objects o ON o.owner = t.owner AND o.object_name = t.table_name AND o.object_type = 'TABLE'
		LEFT JOIN all_tab_comments c ON c.owner = t.owner AND c.table_name = t.table_name
		WHERE t.owner = :1 AND t.table_name = :2`, schema, table).Scan(&m.Comment, &m.CreateTime, &m.UpdateTime)
	result.TableMeta = m

	// Exact row count — all_tables.num_rows is a stale estimate
	qualified := fmt.Sprintf(`"%s"."%s"`, schema, table)
	d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s", qualified)).Scan(&m.RowCount)
	result.TableMeta = m

	// Indexes
	idxRows, idxErr := d.db.Query(`SELECT i.index_name, i.uniqueness,
		c.column_name
		FROM all_indexes i
		JOIN all_ind_columns c ON i.owner = c.index_owner AND i.index_name = c.index_name
		WHERE i.table_owner = :1 AND i.table_name = :2
		ORDER BY i.index_name, c.column_position`, schema, table)
	if idxErr == nil {
		defer idxRows.Close()
		idxMap := make(map[string]*TableIndex)
		var idxOrder []string
		for idxRows.Next() {
			var name, uniqueness, col string
			if idxRows.Scan(&name, &uniqueness, &col) != nil {
				continue
			}
			if _, ok := idxMap[name]; !ok {
				idxMap[name] = &TableIndex{Name: name, Type: uniqueness}
				idxOrder = append(idxOrder, name)
			}
			idxMap[name].Columns = append(idxMap[name].Columns, col)
		}
		for _, name := range idxOrder {
			result.Indexes = append(result.Indexes, *idxMap[name])
		}
	}
	if result.Indexes == nil {
		result.Indexes = make([]TableIndex, 0)
	}

	// Constraints (PK, FK, UNIQUE — exclude CHECK constraints which are just NOT NULL enforcement)
	conRows, conErr := d.db.Query(`SELECT ac.constraint_name,
		CASE ac.constraint_type WHEN 'P' THEN 'PRIMARY KEY' WHEN 'R' THEN 'FOREIGN KEY' WHEN 'U' THEN 'UNIQUE' ELSE ac.constraint_type END,
		acc.column_name
		FROM all_constraints ac
		JOIN all_cons_columns acc ON ac.owner = acc.owner AND ac.constraint_name = acc.constraint_name
		WHERE ac.owner = :1 AND ac.table_name = :2 AND ac.constraint_type IN ('P', 'R', 'U')
		ORDER BY ac.constraint_name, acc.position`, schema, table)
	if conErr == nil {
		defer conRows.Close()
		conMap := make(map[string]*TableConstraint)
		var conOrder []string
		for conRows.Next() {
			var name, ctype, col string
			if conRows.Scan(&name, &ctype, &col) != nil {
				continue
			}
			if _, ok := conMap[name]; !ok {
				conMap[name] = &TableConstraint{Name: name, Type: ctype}
				conOrder = append(conOrder, name)
			}
			conMap[name].Columns = append(conMap[name].Columns, col)
		}
		for _, name := range conOrder {
			result.Constraints = append(result.Constraints, *conMap[name])
		}
	}
	if result.Constraints == nil {
		result.Constraints = make([]TableConstraint, 0)
	}

	return result, nil
}

func (d *OracleDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) {
	row := d.db.QueryRow(`SELECT num_rows, COALESCE(c.comments, '')
		FROM all_tables t LEFT JOIN all_tab_comments c ON t.owner = c.owner AND t.table_name = c.table_name
		WHERE t.owner = :1 AND t.table_name = :2`, strings.ToUpper(schema), strings.ToUpper(table))
	var rowCount sql.NullInt64
	var comment string
	if err := row.Scan(&rowCount, &comment); err != nil {
		return map[string]interface{}{}, err
	}
	return map[string]interface{}{
		"row_count": rowCount.Int64,
		"comment":   comment,
	}, nil
}

func (d *OracleDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string {
	var colDefs []string
	for _, col := range columns {
		name, _ := col["name"].(string)
		colType, _ := col["type"].(string)
		charLen := ""
		if v, ok := col["length"]; ok {
			charLen = fmt.Sprintf("%v", v)
		}
		targetType := ConvertDDLType(sourceDBType, "oracle", colType, charLen, colType)
		def := fmt.Sprintf(`  "%s" %s`, strings.ToUpper(name), targetType)

		if nullable, ok := col["nullable"]; ok {
			if nullableStr, ok := nullable.(string); ok && nullableStr == "NO" {
				def += " NOT NULL"
			}
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf(`CREATE TABLE "%s"."%s" (\n%s\n);`,
		strings.ToUpper(schema), strings.ToUpper(table), strings.Join(colDefs, ",\n"))
}

func (d *OracleDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string {
	return fmt.Sprintf(`COMMENT ON TABLE %s IS '%s'`, qualifiedTable, strings.ReplaceAll(comment, "'", "''"))
}

func (d *OracleDriver) FormatSQLValue(val interface{}) string {
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
	case time.Time:
		return FormatSQLTime(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (d *OracleDriver) AlterColumnClause(columnName, colType, columnDef string) string {
	return fmt.Sprintf(`MODIFY ("%s" %s)`, columnName, columnDef)
}

func (d *OracleDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string {
	return fmt.Sprintf("INSERT ALL\n%s\nSELECT * FROM DUAL;",
		strings.Join(rowValues, "\n"))
}

func (d *OracleDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string {
	// Replace source schema with target schema in quoted identifiers
	ddl = strings.ReplaceAll(ddl, `"`+strings.ToUpper(sourceSchema)+`"`, `"`+strings.ToUpper(targetSchema)+`"`)
	return ddl
}

func (d *OracleDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) {
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return nil, err
	}
	var result []AlterColumn
	for _, c := range cols {
		result = append(result, AlterColumn{
			Name:       c.Name,
			Type:       c.Type,
			Nullable:   c.Nullable == "YES" || c.Nullable == "Y",
			DefaultVal: c.Default,
		})
	}
	if result == nil {
		result = make([]AlterColumn, 0)
	}
	return result, nil
}

// GetColumnTypes returns Oracle-specific column type definitions
func (d *OracleDriver) GetColumnTypes() []ColumnTypeInfo {
	return []ColumnTypeInfo{
		{Name: "NUMBER", NeedsLength: true, NeedsScale: true, Description: "数值(可指定精度)"},
		{Name: "INTEGER", NeedsLength: false, Description: "整数"},
		{Name: "FLOAT", NeedsLength: false, Description: "浮点数"},
		{Name: "BINARY_FLOAT", NeedsLength: false, Description: "二进制单精度"},
		{Name: "BINARY_DOUBLE", NeedsLength: false, Description: "二进制双精度"},
		{Name: "VARCHAR2", NeedsLength: true, Description: "变长字符串"},
		{Name: "NVARCHAR2", NeedsLength: true, Description: "Unicode变长字符串"},
		{Name: "CHAR", NeedsLength: true, Description: "定长字符"},
		{Name: "NCHAR", NeedsLength: true, Description: "Unicode定长字符"},
		{Name: "CLOB", NeedsLength: false, Description: "大文本(字符)"},
		{Name: "NCLOB", NeedsLength: false, Description: "Unicode大文本"},
		{Name: "LONG", NeedsLength: false, Description: "长文本(已弃用)"},
		{Name: "BLOB", NeedsLength: false, Description: "大二进制"},
		{Name: "RAW", NeedsLength: true, Description: "变长二进制"},
		{Name: "LONG RAW", NeedsLength: false, Description: "长二进制(已弃用)"},
		{Name: "BFILE", NeedsLength: false, Description: "外部文件指针"},
		{Name: "DATE", NeedsLength: false, Description: "日期时间"},
		{Name: "TIMESTAMP", NeedsLength: false, Description: "时间戳"},
		{Name: "TIMESTAMP WITH TIME ZONE", NeedsLength: false, Description: "时间戳(带时区)"},
		{Name: "TIMESTAMP WITH LOCAL TIME ZONE", NeedsLength: false, Description: "时间戳(本地时区)"},
		{Name: "INTERVAL YEAR TO MONTH", NeedsLength: false, Description: "年月间隔"},
		{Name: "INTERVAL DAY TO SECOND", NeedsLength: false, Description: "日时间隔"},
		{Name: "ROWID", NeedsLength: false, Description: "行ID"},
		{Name: "UROWID", NeedsLength: false, Description: "通用行ID"},
		{Name: "XMLTYPE", NeedsLength: false, Description: "XML"},
	}
}

func (d *OracleDriver) GetIndexTypes() []IndexTypeInfo {
	return []IndexTypeInfo{
		{Name: "INDEX", Description: "普通 B-Tree 索引"},
		{Name: "UNIQUE", Description: "唯一索引"},
		{Name: "BITMAP", Description: "位图索引"},
		{Name: "FUNCTION-BASED", Description: "函数索引"},
	}
}

// ─── Tree Metadata ─────────────────────────────────────────────────────────

func (d *OracleDriver) GetTreeMetadata() TreeMetadata {
	return TreeMetadata{
		DBType: "oracle",
		Levels: []TreeLevel{
			{Key: "server", Label: "Server", LabelKey: "tree.server", Icon: "CloudServerOutlined"},
			{Key: "user", Label: "User", LabelKey: "tree.user", Icon: "DatabaseOutlined"},
			{Key: "tables_folder", Label: "Tables", LabelKey: "tree.tables", Icon: "TableOutlined"},
			{Key: "views_folder", Label: "Views", LabelKey: "tree.views", Icon: "EyeOutlined"},
		},
		AllowCreate: map[string]bool{"user": true},
		SystemFilter: &SystemFilter{
			ExcludeNames: []string{
				"SYS", "SYSTEM", "AUDSYS", "CTXSYS", "DBSFWUSER", "DBSNMP", "DIP",
				"DVF", "DVSYS", "GGSYS", "GSMADMIN_INTERNAL", "GSMCATUSER", "GSMUSER",
				"LBACSYS", "MDSYS", "OJVMSYS", "OLAPSYS", "ORDDATA", "ORDPLUGINS",
				"ORDSYS", "OUTLN", "SI_INFORMTN_SCHEMA", "WMSYS", "XDB", "XS$NULL",
				"APPQOSSYS", "ORACLE_OCM", "REMOTE_SCHEDULER_AGENT",
				"ANONYMOUS", "MDDATA", "PDBADMIN",
				"SYSBACKUP", "SYSDG", "SYSKM", "SYSRAC",
			},
			ExcludePrefixes: []string{"SYS$", "APEX_", "FLOWS_"},
		},
	}
}

func (d *OracleDriver) ListDatabases() ([]DatabaseInfo, error) {
	rows, err := d.db.Query(`SELECT u.USERNAME,
		(SELECT NLS_CHARACTERSET FROM V$NLS_PARAMETERS WHERE PARAMETER='NLS_CHARACTERSET'),
		COALESCE(ROUND(SUM(f.BYTES)/1024/1024, 2), 0),
		COALESCE((SELECT COUNT(*) FROM ALL_TABLES WHERE OWNER=u.USERNAME), 0)
	FROM ALL_USERS u LEFT JOIN DBA_DATA_FILES f ON u.USERNAME=f.OWNER
	WHERE u.USERNAME NOT IN ('SYS','SYSTEM','XDB','OUTLN','GSMCATUSER','MDDATA','ORACLE_OCM','DBSNMP','APPQOSSYS','WMSYS','OJVMSYS','CTXSYS','ORDDATA','ORDSYS','MDSYS','OLAPSYS','LBACSYS','DVSYS','AUDSYS')
	GROUP BY u.USERNAME ORDER BY u.USERNAME`)
	if err != nil {
		// Fallback: query without DBA_DATA_FILES (may need DBA privilege)
		names, err2 := d.ListSchemaNames()
		if err2 != nil { return nil, err2 }
		var result []DatabaseInfo
		for _, n := range names {
			result = append(result, DatabaseInfo{Name: n})
		}
		if result == nil { result = []DatabaseInfo{} }
		return result, nil
	}
	defer rows.Close()
	var result []DatabaseInfo
	for rows.Next() {
		var info DatabaseInfo
		if err := rows.Scan(&info.Name, &info.Charset, &info.SizeMB, &info.Tables); err == nil {
			result = append(result, info)
		}
	}
	if result == nil { result = []DatabaseInfo{} }
	return result, nil
}

func (d *OracleDriver) ResolveContext(arg string) DatabaseContext {
	return DatabaseContext{User: arg}
}

func (d *OracleDriver) ListDatabaseSchemas(database string) ([]string, error) {
	return nil, nil
}

// ─── DDL Builder stubs ────────────────────────────────────────────

func (d *OracleDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) {
	nullable := col.Nullable == nil || *col.Nullable
	defaultVal := ""
	if col.HasDef != nil && *col.HasDef {
		defaultVal = col.Default
	}
	parts := d.AddColumnDDL(table, col.Name, col.Type, col.Length, nullable, defaultVal, col.Comment, col.After)
	return strings.Join(parts, ";\n"), "", nil, false, nil
}
func (d *OracleDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	return BuildOracleModifyColumn(table, col, orig)
}

// BuildOracleModifyColumn generates MODIFY column DDL for Oracle.
func BuildOracleModifyColumn(tbl string, ch AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	var stmts, rollbacks, warnings []string
	highRisk := false

	if ch.Length != "" && orig.Length != "" && ch.Length != orig.Length {
		origL, newL := toInt(orig.Length), toInt(ch.Length)
		if origL > 0 && newL > 0 && newL < origL {
			warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: length shrink may truncate data", ch.Name))
			highRisk = true
		}
	}
	if ch.Nullable != nil && !*ch.Nullable && orig.Nullable {
		warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: tightening NOT NULL may fail on existing NULL values", ch.Name))
		highRisk = true
	}

	useType := orig.Type
	if ch.Type != "" && ch.Type != orig.Type {
		useType = ch.Type
	}
	colDef := quoteOracle(ch.Name) + " " + strings.ToUpper(useType)
	needsLen := strings.EqualFold(useType, "VARCHAR2") || strings.EqualFold(useType, "NVARCHAR2") || strings.EqualFold(useType, "CHAR") || strings.EqualFold(useType, "NCHAR")
	if ch.Length != "" && ch.Length != "0" && ch.Length != orig.Length {
		colDef += "(" + ch.Length + ")"
	} else if needsLen && orig.Length != "" && orig.Length != "0" {
		useLen := orig.Length
		if strings.EqualFold(useType, "NVARCHAR2") || strings.EqualFold(useType, "NCHAR") {
			if n, err := strconv.Atoi(orig.Length); err == nil && n > 2000 {
				useLen = strconv.Itoa(n / 2)
			}
		}
		colDef += "(" + useLen + ")"
	}

	// DEFAULT before NOT NULL for Oracle MODIFY
	if ch.HasDef != nil && *ch.HasDef {
		colDef += " DEFAULT " + formatDefault(ch.Default)
	} else if ch.HasDef != nil && !*ch.HasDef && orig.HasDef {
		colDef += " DEFAULT NULL"
	}
	if ch.Nullable != nil && *ch.Nullable != orig.Nullable {
		if *ch.Nullable {
			colDef += " NULL"
		} else {
			colDef += " NOT NULL"
		}
	}

	typeChanged := ch.Type != "" && ch.Type != orig.Type
	lenChanged := ch.Length != "" && ch.Length != "0" && ch.Length != orig.Length
	nullChanged := ch.Nullable != nil && *ch.Nullable != orig.Nullable
	defChanged := (ch.HasDef != nil && *ch.HasDef) || (ch.HasDef != nil && !*ch.HasDef && orig.HasDef)
	if typeChanged || lenChanged || nullChanged || defChanged {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY (%s)", tbl, colDef))
		origDef := quoteOracle(orig.Name) + " " + strings.ToUpper(orig.Type)
		if !orig.Nullable {
			origDef += " NOT NULL"
		}
		if orig.HasDef {
			origDef += " DEFAULT " + formatDefault(orig.Default)
		}
		rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s MODIFY (%s)", tbl, origDef))
	}
	if ch.Comment != "" && ch.Comment != orig.Comment {
		stmts = append(stmts, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quoteOracle(ch.Name), pgString(ch.Comment)))
		rollbacks = append(rollbacks, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quoteOracle(ch.Name), pgString(orig.Comment)))
	}

	return stmts, rollbacks, warnings, highRisk, nil
}

// GetServerInfo returns Oracle server info
func (d *OracleDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT VERSION FROM V$INSTANCE").Scan(&version); err == nil {
		info["version"] = version
	}
	return info, nil
}

// GetMetrics returns Oracle current metrics
func (d *OracleDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	m := make(map[string]interface{})

	// Safe queries that work with standard privileges
	safeQueries := map[string]string{
		"数据库版本": "SELECT VERSION FROM V$INSTANCE",
		"实例状态":   "SELECT STATUS FROM V$INSTANCE",
		"启动时间":    "SELECT TO_CHAR(STARTUP_TIME, 'YYYY-MM-DD HH24:MI') FROM V$INSTANCE",
		"日志模式":   "SELECT LOG_MODE FROM V$DATABASE",
	}
	for name, sql := range safeQueries {
		var v string
		if err := d.db.QueryRowContext(ctx, sql).Scan(&v); err == nil {
			m[name] = v
		}
	}

	// Session stats (may fail if no DBA privilege)
	tryQuery := func(name, sql string) {
		if _, exists := m[name]; exists { return }
		var v string
		if err := d.db.QueryRowContext(ctx, sql).Scan(&v); err == nil {
			m[name] = v
		}
	}
	tryQuery("会话总数", "SELECT COUNT(*) FROM V$SESSION")
	tryQuery("活跃会话", "SELECT COUNT(*) FROM V$SESSION WHERE STATUS='ACTIVE' AND TYPE!='BACKGROUND'")
	tryQuery("最大会话数", "SELECT TO_CHAR(value) FROM V$PARAMETER WHERE name='sessions'")
	tryQuery("最大进程数", "SELECT TO_CHAR(value) FROM V$PARAMETER WHERE name='processes'")

	return m, nil
}

func (d *OracleDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) {
	return d.DropColumnDDL(table, colName), "", nil, true, nil
}
func (d *OracleDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) {
	return d.AddIndexDDL(table, idx.Name, idx.Type, idx.Columns), "", nil
}
func (d *OracleDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) {
	return d.DropIndexDDL(table, schema, idxName), "", nil, true, nil
}
func (d *OracleDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) {
	return "", "", fmt.Errorf("not supported")
}
func (d *OracleDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *OracleDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *OracleDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) {
	return d.SetTableCommentDDL(table, "", "", newComment), "", nil
}

// ListProcesses returns current process list
func (d *OracleDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, processQueries[dbType])
}

// ListUsers returns user list
func (d *OracleDriver) ListUsers(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, userQueries[dbType])
}

// ListTablespaces returns tablespace info
func (d *OracleDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, tablespaceQueries[dbType])
}

func (d *OracleDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) {
	start := time.Now()
	m := &ServerMetricsV2{DBType: "oracle", CollectedAt: time.Now(), DatabaseSpecific: make(map[string]interface{})}

	// Connections (sessions)
	var total, active int
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM V$SESSION").Scan(&total)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM V$SESSION WHERE STATUS='ACTIVE'").Scan(&active)
	m.Connections.Total = total
	m.Connections.Active = active
	m.Connections.Idle = total - active
	var maxSessions, maxProcs string
	d.db.QueryRowContext(ctx, "SELECT TO_CHAR(value) FROM V$PARAMETER WHERE name='sessions'").Scan(&maxSessions)
	d.db.QueryRowContext(ctx, "SELECT TO_CHAR(value) FROM V$PARAMETER WHERE name='processes'").Scan(&maxProcs)
	m.Connections.MaxConnections = int(parseIntStr(maxSessions))
	if m.Connections.MaxConnections > 0 {
		m.Connections.UsagePercent = float64(total) * 100.0 / float64(m.Connections.MaxConnections)
	}
	m.DatabaseSpecific["max_processes"] = maxProcs

	// Buffer cache
	var consistent, dbGets, physical int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(value),0) FROM V$SYSSTAT WHERE name IN ('consistent gets','db block gets')").Scan(&consistent)
	d.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(value),0) FROM V$SYSSTAT WHERE name='physical reads'").Scan(&physical)
	logical := consistent
	if logical > 0 {
		m.BufferCache.HitRate = (1.0 - float64(physical)/float64(logical)) * 100.0
	}

	// Locks
	var deadlocks int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(SUM(value),0) FROM V$SYSSTAT WHERE name='enqueue deadlocks'").Scan(&deadlocks)
	m.Locks.Deadlocks = int(deadlocks)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM V$LOCKED_OBJECT").Scan(&m.Locks.LockWaits)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM V$SESSION WHERE BLOCKING_SESSION IS NOT NULL").Scan(&m.Locks.BlockedSessions)

	// Storage — tablespaces
	rows, err := d.db.QueryContext(ctx, `SELECT t.TABLESPACE_NAME, ROUND(SUM(f.BYTES)/1024/1024,2), ROUND(SUM(NVL(f.BYTES-NVL(e.BYTES,0),0))/1024/1024,2), ROUND(SUM(NVL(e.BYTES,0))/1024/1024,2) FROM DBA_DATA_FILES f LEFT JOIN DBA_FREE_SPACE e ON f.TABLESPACE_NAME=e.TABLESPACE_NAME AND f.FILE_ID=e.FILE_ID JOIN DBA_TABLESPACES t ON f.TABLESPACE_NAME=t.TABLESPACE_NAME GROUP BY t.TABLESPACE_NAME`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ts TablespaceMetric
			var used, free float64
			if rows.Scan(&ts.Name, &ts.SizeMB, &used, &free) == nil {
				ts.UsedMB = used; ts.FreeMB = free
				if ts.SizeMB > 0 { ts.UsagePct = used * 100.0 / ts.SizeMB }
				m.Storage.Tablespaces = append(m.Storage.Tablespaces, ts)
			}
		}
	}

	// DB-specific
	var version, status, logMode string
	d.db.QueryRowContext(ctx, "SELECT VERSION FROM V$INSTANCE").Scan(&version)
	d.db.QueryRowContext(ctx, "SELECT STATUS FROM V$INSTANCE").Scan(&status)
	d.db.QueryRowContext(ctx, "SELECT LOG_MODE FROM V$DATABASE").Scan(&logMode)
	m.DatabaseSpecific["version"] = version
	m.DatabaseSpecific["status"] = status
	m.DatabaseSpecific["log_mode"] = logMode
	if logMode == "NOARCHIVELOG" { m.DatabaseSpecific["archive_warning"] = "NOARCHIVELOG 模式存在数据丢失风险" }

	var archiveUsed float64
	d.db.QueryRowContext(ctx, "SELECT NVL(PERCENT_SPACE_USED,0) FROM V$RECOVERY_AREA_USAGE").Scan(&archiveUsed)
	m.DatabaseSpecific["archive_used_pct"] = archiveUsed

	_ = dbGets
	m.CostMs = time.Since(start).Milliseconds()
	return m, nil
}

func (d *OracleDriver) CreateDatabase(name string) error {
	return fmt.Errorf("Oracle does not support CREATE DATABASE via SQL; use CREATE PLUGGABLE DATABASE or manage externally")
}
func (d *OracleDriver) DropDatabase(name string) error {
	return fmt.Errorf("Oracle does not support DROP DATABASE via SQL")
}
func (d *OracleDriver) CreateUser(username, password string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE USER %s IDENTIFIED BY %s", username, password))
	return err
}
func (d *OracleDriver) DropUser(username string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP USER %s CASCADE", username))
	return err
}
func (d *OracleDriver) GrantPrivileges(username, database string, privileges []string) error {
	privs := "CONNECT, RESOURCE"
	if len(privileges) > 0 { privs = strings.Join(privileges, ", ") }
	_, err := d.db.Exec(fmt.Sprintf("GRANT %s TO %s", privs, username))
	return err
}

func (d *OracleDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error) {
	var result []PrivilegeEntry

	// 1. Owned tables (implicit ALL privileges)
	ownedRows, err := d.db.Query("SELECT TABLE_NAME FROM ALL_TABLES WHERE OWNER=:1", username)
	if err == nil && ownedRows != nil {
		defer ownedRows.Close()
		for ownedRows.Next() {
			var table string
			if ownedRows.Scan(&table) == nil {
				result = append(result, PrivilegeEntry{Database: username, ObjectType: "TABLE", ObjectName: table,
					Privileges: []string{"ALL"}, Grantable: true, IsSystem: true})
			}
		}
	}

	// 2. Granted object privileges
	privRows, err2 := d.db.Query("SELECT OWNER, TABLE_NAME, PRIVILEGE, GRANTABLE FROM DBA_TAB_PRIVS WHERE GRANTEE=:1", username)
	if err2 == nil && privRows != nil {
		defer privRows.Close()
		privMap := make(map[string]*PrivilegeEntry)
		for privRows.Next() {
			var owner, table, priv, grantable string
			if privRows.Scan(&owner, &table, &priv, &grantable) == nil {
				key := owner + "." + table
				if _, ok := privMap[key]; !ok {
					privMap[key] = &PrivilegeEntry{Database: owner, ObjectType: "TABLE", ObjectName: table}
				}
				privMap[key].Privileges = append(privMap[key].Privileges, priv)
				if grantable == "YES" { privMap[key].Grantable = true }
			}
		}
		for _, v := range privMap {
			found := false
			for i := range result {
				if result[i].Database == v.Database && result[i].ObjectName == v.ObjectName { found = true; break }
			}
			if !found { result = append(result, *v) }
		}
	}

	// 3. System privileges
	sysRows, err3 := d.db.Query("SELECT PRIVILEGE, ADMIN_OPTION FROM DBA_SYS_PRIVS WHERE GRANTEE=:1", username)
	if err3 == nil && sysRows != nil {
		defer sysRows.Close()
		for sysRows.Next() {
			var priv, admin string
			if sysRows.Scan(&priv, &admin) == nil {
				result = append(result, PrivilegeEntry{Database: "*", ObjectType: "INSTANCE", ObjectName: "*",
					Privileges: []string{priv}, Grantable: admin == "YES", IsSystem: true})
			}
		}
	}

	if result == nil { result = []PrivilegeEntry{} }
	return result, nil
}
func (d *OracleDriver) GetUserRoles(username string) ([]string, error) {
	rows, err := d.db.Query("SELECT GRANTED_ROLE FROM DBA_ROLE_PRIVS WHERE GRANTEE=:1", username)
	if err != nil { return []string{}, nil }
	defer rows.Close()
	var roles []string
	for rows.Next() { var r string; if rows.Scan(&r) == nil { roles = append(roles, r) } }
	if roles == nil { roles = []string{} }
	return roles, nil
}
func (d *OracleDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) {
	result := &ChangeResult{}
	for _, ch := range changes {
		if ch.ObjectType == "DATABASE" || (ch.Database == "*" && ch.ObjectName == "*") {
			// System-level privilege
			for _, p := range ch.Grant {
				priv := oracleSystemPrivilege(p)
				result.Statements = append(result.Statements, fmt.Sprintf("GRANT %s TO %s", priv, username))
			}
			for _, p := range ch.Revoke {
				priv := oracleSystemPrivilege(p)
				result.Statements = append(result.Statements, fmt.Sprintf("REVOKE %s FROM %s", priv, username))
			}
		} else {
			// Object-level privilege
			obj := ch.Database + "." + ch.ObjectName
			for _, p := range ch.Grant {
				result.Statements = append(result.Statements, fmt.Sprintf("GRANT %s ON %s TO %s", p, obj, username))
			}
			for _, p := range ch.Revoke {
				result.Statements = append(result.Statements, fmt.Sprintf("REVOKE %s ON %s FROM %s", p, obj, username))
			}
		}
	}
	if len(changes) > 0 && changes[0].DryRun { return result, nil }
	for _, stmt := range result.Statements {
		if _, err := d.db.Exec(stmt); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", stmt, err))
		} else { result.Executed++ }
	}
	return result, nil
}

func oracleSystemPrivilege(p string) string {
	switch p {
	case "SELECT": return "SELECT ANY TABLE"
	case "INSERT": return "INSERT ANY TABLE"
	case "UPDATE": return "UPDATE ANY TABLE"
	case "DELETE": return "DELETE ANY TABLE"
	case "EXECUTE": return "EXECUTE ANY PROCEDURE"
	case "ALL": return "DBA"
	default: return p
	}
}

func (d *OracleDriver) DetectCapability() (*CapabilitySet, error) {
	var v string
	d.db.QueryRow("SELECT BANNER FROM V$VERSION WHERE ROWNUM=1").Scan(&v)
	return DetectCapability("oracle", v), nil
}
func (d *OracleDriver) ListRoles() ([]RoleInfo, error) {
	rows, err := d.db.Query("SELECT ROLE FROM DBA_ROLES")
	if err != nil { return []RoleInfo{}, nil }
	defer rows.Close()
	var result []RoleInfo
	sysRoles := map[string]bool{"DBA": true, "CONNECT": true, "RESOURCE": true, "SYSDBA": true, "SYSOPER": true}
	roleMap := make(map[string]*RoleInfo)
	for rows.Next() { var r string; if rows.Scan(&r) == nil { roleMap[r] = &RoleInfo{Name: r, IsSystem: sysRoles[r]} } }
	// Populate members from DBA_ROLE_PRIVS
	memRows, err := d.db.Query("SELECT GRANTEE, GRANTED_ROLE FROM DBA_ROLE_PRIVS WHERE GRANTEE IN (SELECT USERNAME FROM DBA_USERS)")
	if err == nil && memRows != nil {
		defer memRows.Close()
		for memRows.Next() {
			var grantee, role string
			if memRows.Scan(&grantee, &role) == nil {
				if r, ok := roleMap[role]; ok { r.Members = append(r.Members, grantee) }
			}
		}
	}
	for _, v := range roleMap { result = append(result, *v) }
	if result == nil { result = []RoleInfo{} }
	return result, nil
}
func (d *OracleDriver) CreateRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE ROLE %s", name))
	return err
}
func (d *OracleDriver) DropRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP ROLE %s", name))
	return err
}
func (d *OracleDriver) AddRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf("GRANT %s TO %s", role, member))
	return err
}
func (d *OracleDriver) RemoveRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf("REVOKE %s FROM %s", role, member))
	return err
}
func (d *OracleDriver) AlterUserPassword(username, newPassword string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER %s IDENTIFIED BY %s", username, newPassword))
	return err
}
func (d *OracleDriver) AlterUserLock(username string, lock bool) error {
	action := "UNLOCK"
	if lock { action = "LOCK" }
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER %s ACCOUNT %s", username, action))
	return err
}
func (d *OracleDriver) AlterUserRename(oldName, newName string) error {
	return fmt.Errorf("rename not supported for OracleDriver")
}
func (d *OracleDriver) AlterUserDefaultSchema(username, schema string) error {
	return fmt.Errorf("default schema not supported for OracleDriver")
}

func (d *OracleDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error) {
	return d.GetUserPrivileges(roleName)
}

func (d *OracleDriver) AlterRoleAttribute(roleName, attribute, value string) error {
	return fmt.Errorf("AlterRoleAttribute not supported for this database")
}

func (d *OracleDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error) {
	return []ParentRoleInfo{}, nil
}

func (d *OracleDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error) {
	return []MemberRoleInfo{}, nil
}

func (d *OracleDriver) GetRoleInherit(roleName string) (bool, error) {
	return true, nil
}
func (d *OracleDriver) GetRoleMemberships(roleName string) ([]string, error) {
	return []string{}, nil
}

// ─── System SQL Dialect Methods ──────────────────────────────────────────
func (d *OracleDriver) SQLFormatTime(col string) string {
	return fmt.Sprintf("TO_CHAR(%s, \x27YYYY-MM-DD HH24:MI:SS\x27)", col)
}
func (d *OracleDriver) SQLConcat(parts ...string) string {
	return strings.Join(parts, " || ")
}
func (d *OracleDriver) SQLIsNull(col, defaultVal string) string {
	return fmt.Sprintf("NVL(%s, %s)", col, defaultVal)
}
func (d *OracleDriver) SQLCurrentTimestamp() string { return "SYSDATE" }
func (d *OracleDriver) SQLQuoteIdent(name string) string { return `"` + name + `"` }

