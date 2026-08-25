package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

// MySQLDriver implements DatabaseDriver for MySQL/MariaDB/OceanBase
// MySQLDriver implements DatabaseDriver for MySQL
type MySQLDriver struct {
	db     *sql.DB
	cfg    DriverConfig
	pooled bool
}

func NewMySQLDriver(cfg DriverConfig) (DatabaseDriver, *sql.DB, error) {
	var db *sql.DB
	if cfg.DB != nil {
		db = cfg.DB
	} else {
		port := cfg.Port
		if port == 0 {
			port = 3306
		}
		dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=10s&readTimeout=30s&parseTime=true",
			cfg.Username, cfg.Password, cfg.Host, port, cfg.Database)
		var err error
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return nil, nil, err
		}
		db.SetMaxOpenConns(cfg.MaxConnections)
		db.SetMaxIdleConns(cfg.MaxConnections / 2)
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	// Only ping if NOT pooled (pool manager already validated)
	if cfg.DB == nil {
		if err := db.Ping(); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("mysql connection failed: %w", err)
		}
	}

	return &MySQLDriver{db: db, cfg: cfg, pooled: cfg.DB != nil}, db, nil
}

func (d *MySQLDriver) Ping() error     { return d.db.Ping() }
func (d *MySQLDriver) Close() error {
	if d.pooled {
		return nil
	}
	return d.db.Close()
}
func (d *MySQLDriver) DBType() string  { return "mysql" }
func (d *MySQLDriver) Dialect() string { return "mysql" }

func (d *MySQLDriver) useSchema(schema string) error {
	if schema == "" {
		return nil
	}
	_, err := d.db.Exec("USE `" + strings.ReplaceAll(schema, "`", "``") + "`")
	return err
}

// ─── Schema Discovery ──────────────────────────────────────────────────────

func (d *MySQLDriver) ListSchemas() ([]SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
		ORDER BY SCHEMA_NAME`)
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
		si, err := d.ListObjects(name)
		if err != nil {
			schemas = append(schemas, SchemaInfo{Name: name})
			continue
		}
		schemas = append(schemas, *si)
	}
	if schemas == nil {
		schemas = make([]SchemaInfo, 0)
	}
	return schemas, nil
}

func (d *MySQLDriver) ListSchemaNames() ([]string, error) {
	rows, err := d.db.Query(`SELECT SCHEMA_NAME FROM information_schema.SCHEMATA
		WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
		ORDER BY SCHEMA_NAME`)
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

func (d *MySQLDriver) ListObjects(schema string) (*SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT TABLE_NAME, TABLE_TYPE
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?
		ORDER BY TABLE_NAME`, schema)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tables := make([]ObjectInfo, 0)
	views := make([]ObjectInfo, 0)
	for rows.Next() {
		var name, tableType string
		if err := rows.Scan(&name, &tableType); err != nil {
			continue
		}
		if tableType == "VIEW" {
			views = append(views, ObjectInfo{Name: name})
		} else {
			tables = append(tables, ObjectInfo{Name: name})
		}
	}
	return &SchemaInfo{Name: schema, Tables: tables, Views: views}, nil
}

func (d *MySQLDriver) ListSchemaDetail() ([]SchemaDetailItem, error) {
	rows, err := d.db.Query(`SELECT
		SCHEMA_NAME,
		COUNT(CASE WHEN TABLE_TYPE = 'BASE TABLE' THEN 1 END) AS table_count,
		COUNT(CASE WHEN TABLE_TYPE = 'VIEW' THEN 1 END) AS view_count,
		DEFAULT_CHARACTER_SET_NAME,
		DEFAULT_COLLATION_NAME
	FROM information_schema.SCHEMATA
	LEFT JOIN information_schema.TABLES ON SCHEMA_NAME = TABLE_SCHEMA
	WHERE SCHEMA_NAME NOT IN ('information_schema', 'performance_schema', 'mysql', 'sys')
	GROUP BY SCHEMA_NAME, DEFAULT_CHARACTER_SET_NAME, DEFAULT_COLLATION_NAME
	ORDER BY SCHEMA_NAME`)
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

func (d *MySQLDriver) ListTables(schema string) ([]TableListItem, error) {
	rows, err := d.db.Query(`SELECT TABLE_NAME, TABLE_TYPE, ENGINE, TABLE_ROWS, TABLE_COMMENT, CREATE_TIME, UPDATE_TIME
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? ORDER BY TABLE_NAME`, schema)
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

func (d *MySQLDriver) GetColumns(schema, table string) ([]ColumnDetail, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, COLUMN_TYPE, IFNULL(CHARACTER_MAXIMUM_LENGTH, '') as length,
		IS_NULLABLE, IFNULL(COLUMN_DEFAULT, ''), IFNULL(COLUMN_COMMENT, ''), COLUMN_KEY
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []ColumnDetail
	for rows.Next() {
		var c ColumnDetail
		if err := rows.Scan(&c.Name, &c.Type, &c.Length, &c.Nullable, &c.Default, &c.Comment, &c.Key); err != nil {
			continue
		}
		cols = append(cols, c)
	}
	if cols == nil {
		cols = make([]ColumnDetail, 0)
	}
	return cols, nil
}

func (d *MySQLDriver) GetColumnDetails(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, COLUMN_TYPE, COLUMN_DEFAULT, IS_NULLABLE, COLUMN_COMMENT
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *MySQLDriver) GetDDL(schema, table string) (string, error) {
	if schema != "" {
		d.db.Exec("USE `" + strings.ReplaceAll(schema, "`", "``") + "`")
	}
	var name, ddl string
	err := d.db.QueryRow("SHOW CREATE TABLE `"+strings.ReplaceAll(table, "`", "``")+"`").Scan(&name, &ddl)
	if err != nil {
		// Try as view — SHOW CREATE VIEW returns 4 columns in MySQL
		var charset, collation string
		err = d.db.QueryRow("SHOW CREATE VIEW `"+strings.ReplaceAll(table, "`", "``")+"`").Scan(&name, &ddl, &charset, &collation)
		if err != nil {
			return "", fmt.Errorf("failed to get DDL for %s: %w", table, err)
		}
	}
	return ddl, nil
}

func (d *MySQLDriver) GetViewDefinition(schema, view string) (string, error) {
	var def string
	err := d.db.QueryRow(`SELECT VIEW_DEFINITION FROM information_schema.VIEWS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, view).Scan(&def)
	return def, err
}

// ─── Query Execution ──────────────────────────────────────────────────────

func (d *MySQLDriver) ExecuteQuery(sql string, schema string) (*QueryResult, error) {
	if schema != "" {
		if err := d.useSchema(schema); err != nil {
			return nil, err
		}
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

func (d *MySQLDriver) GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	quoted := QuoteTableName(schema, table, "mysql")

	// Count total
	var total int64
	if err := d.db.QueryRow("SELECT COUNT(*) FROM " + quoted).Scan(&total); err != nil {
		return nil, err
	}

	offset := (page - 1) * pageSize
	query := fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", quoted, pageSize, offset)

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	colNames, resultRows, err := ScanQueryResult(rows)
	if err != nil {
		return nil, err
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

func (d *MySQLDriver) ExecuteDDL(ddl string) (int64, error) {
	res, err := d.db.Exec(ddl)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *MySQLDriver) ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error) {
	statements := SplitDDLWithDialect(ddl, "mysql")
	total = int64(len(statements))

	for _, stmt := range statements {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}

		// Apply import strategy for INSERT statements
		if importStrategy != "" && strings.HasPrefix(strings.ToUpper(s), "INSERT") {
			switch importStrategy {
			case "skip":
				s = strings.Replace(s, "INSERT INTO", "INSERT IGNORE INTO", 1)
			case "replace":
				s = strings.Replace(s, "INSERT INTO", "REPLACE INTO", 1)
			}
		}

		res, e := d.db.Exec(s)
		if e != nil {
			fail++
			errors = append(errors, fmt.Sprintf("statement %d: %s", success+fail, e.Error()))
			continue
		}
		success++
		ra, _ := res.RowsAffected()
		rowsAffected += ra
	}

	return
}

// ─── DDL Helpers ──────────────────────────────────────────────────────────

func (d *MySQLDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string {
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
	if comment != "" {
		parts = append(parts, "COMMENT '"+strings.ReplaceAll(comment, "'", "''")+"'")
	}
	ddl := fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN `%s` %s",
		qualifiedTable, columnName, strings.Join(parts, " "))
	return []string{ddl}
}

func (d *MySQLDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP INDEX `%s`", qualifiedTable, indexName)
}

func (d *MySQLDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string {
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
	if comment != "" {
		parts = append(parts, "COMMENT '"+strings.ReplaceAll(comment, "'", "''")+"'")
	}
	ddl := fmt.Sprintf("ALTER TABLE %s ADD COLUMN `%s` %s", qualifiedTable, columnName, strings.Join(parts, " "))
	if after != "" {
		ddl += " AFTER `" + after + "`"
	}
	return []string{ddl}
}

func (d *MySQLDriver) DropColumnDDL(qualifiedTable, columnName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN `%s`", qualifiedTable, columnName)
}

func (d *MySQLDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string {
	var colList []string
	for _, c := range columns {
		colList = append(colList, "`"+c+"`")
	}
	cols := strings.Join(colList, ", ")
	if indexType == "UNIQUE" {
		return fmt.Sprintf("CREATE UNIQUE INDEX `%s` ON %s (%s)", indexName, qualifiedTable, cols)
	}
	return fmt.Sprintf("CREATE INDEX `%s` ON %s (%s)", indexName, qualifiedTable, cols)
}

// ─── Index & Constraint Metadata ──────────────────────────────────────────

func (d *MySQLDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT INDEX_NAME, COLUMN_NAME, NON_UNIQUE, INDEX_TYPE
		FROM information_schema.STATISTICS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND INDEX_NAME != 'PRIMARY'
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *MySQLDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT CONSTRAINT_NAME, COLUMN_NAME, REFERENCED_TABLE_SCHEMA, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND REFERENCED_TABLE_NAME IS NOT NULL`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *MySQLDriver) GetFullStructure(schema, table string) (*FullStructure, error) {
	result := &FullStructure{}

	var tableType string
	err := d.db.QueryRow(`SELECT TABLE_TYPE FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, table).Scan(&tableType)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s.%s", schema, table)
	}
	if tableType == "VIEW" {
		result.IsView = true
		var name, ddl, client, connStr string
		if err := d.db.QueryRow(fmt.Sprintf("SHOW CREATE VIEW %s.%s", QuoteIdent(schema, "mysql"), QuoteIdent(table, "mysql"))).Scan(&name, &ddl, &client, &connStr); err == nil {
			result.DDL = ddl
		}
		// Views have columns from the SELECT statement — continue to query columns below
	}

	// Columns
	rows, err := d.db.Query(`
		SELECT COLUMN_NAME, COLUMN_TYPE, CHARACTER_MAXIMUM_LENGTH, IS_NULLABLE,
			COLUMN_DEFAULT, COLUMN_COMMENT, COLUMN_KEY, EXTRA,
			CHARACTER_SET_NAME, COLLATION_NAME
		FROM information_schema.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c TableColumn
		var length, defVal, charset, collation sql.NullString
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &length, &nullable, &defVal, &c.Comment, &c.Key, &c.Extra, &charset, &collation); err != nil {
			continue
		}
		c.Nullable = nullable == "YES"
		if defVal.Valid {
			c.Default = defVal.String
			c.HasDef = true
		}
		if length.Valid && length.String != "" {
			c.Length = length.String
		}
		if charset.Valid {
			c.Charset = charset.String
		}
		if collation.Valid {
			c.Collation = collation.String
		}
		result.Columns = append(result.Columns, c)
	}

	// Indexes (tables only)
	if !result.IsView {
	idxRows, err := d.db.Query(`
		SELECT INDEX_NAME, NON_UNIQUE, INDEX_TYPE, COMMENT, COLUMN_NAME, SEQ_IN_INDEX
		FROM information_schema.STATISTICS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY INDEX_NAME, SEQ_IN_INDEX`, schema, table)
	if err == nil {
		defer idxRows.Close()
		idxMap := make(map[string]*TableIndex)
		var idxOrder []string
		for idxRows.Next() {
			var name, idxType, comment, col string
			var nonUnique int
			var seq int
			if err := idxRows.Scan(&name, &nonUnique, &idxType, &comment, &col, &seq); err != nil {
				continue
			}
			if _, ok := idxMap[name]; !ok {
				t := idxType
				if name == "PRIMARY" {
					t = "PRIMARY"
				} else if nonUnique == 0 {
					t = "UNIQUE"
				} else {
					t = "INDEX"
				}
				idxMap[name] = &TableIndex{Name: name, Type: t, Comment: comment}
				idxOrder = append(idxOrder, name)
			}
			idxMap[name].Columns = append(idxMap[name].Columns, col)
		}
		for _, name := range idxOrder {
			result.Indexes = append(result.Indexes, *idxMap[name])
		}
	}
	}

	// Constraints (tables only)
	if !result.IsView {
	conRows, err := d.db.Query(`
		SELECT tc.CONSTRAINT_NAME, tc.CONSTRAINT_TYPE, kcu.COLUMN_NAME,
			COALESCE(kcu.REFERENCED_TABLE_NAME, ''), COALESCE(kcu.REFERENCED_COLUMN_NAME, ''),
			COALESCE(rc.DELETE_RULE, ''), COALESCE(rc.UPDATE_RULE, '')
		FROM information_schema.TABLE_CONSTRAINTS tc
		JOIN information_schema.KEY_COLUMN_USAGE kcu
			ON tc.CONSTRAINT_NAME = kcu.CONSTRAINT_NAME AND tc.TABLE_SCHEMA = kcu.TABLE_SCHEMA AND tc.TABLE_NAME = kcu.TABLE_NAME
		LEFT JOIN information_schema.REFERENTIAL_CONSTRAINTS rc
			ON tc.CONSTRAINT_NAME = rc.CONSTRAINT_NAME AND tc.CONSTRAINT_SCHEMA = rc.CONSTRAINT_SCHEMA
		WHERE tc.TABLE_SCHEMA = ? AND tc.TABLE_NAME = ?
		ORDER BY tc.CONSTRAINT_NAME, kcu.ORDINAL_POSITION`, schema, table)
	if err == nil {
		defer conRows.Close()
		cMap := make(map[string]*TableConstraint)
		var cOrder []string
		for conRows.Next() {
			var name, ctype, col, refT, refC, delR, updR string
			if err := conRows.Scan(&name, &ctype, &col, &refT, &refC, &delR, &updR); err != nil {
				continue
			}
			if _, ok := cMap[name]; !ok {
				cMap[name] = &TableConstraint{
					Name: name, Type: ctype, RefTable: refT, OnDelete: delR, OnUpdate: updR,
				}
				cOrder = append(cOrder, name)
			}
			cMap[name].Columns = append(cMap[name].Columns, col)
			if refC != "" {
				cMap[name].RefColumns = append(cMap[name].RefColumns, refC)
			}
		}
		for _, name := range cOrder {
			result.Constraints = append(result.Constraints, *cMap[name])
		}
	}
	}

	// Table meta
	var m TableMeta
	var engine, collation, rowFmt, comment, createTime, updateTime sql.NullString
	var rowCount sql.NullInt64
	err = d.db.QueryRow(`
		SELECT ENGINE, TABLE_COLLATION, ROW_FORMAT, TABLE_COMMENT, TABLE_ROWS, CREATE_TIME, UPDATE_TIME
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, table).Scan(
		&engine, &collation, &rowFmt, &comment, &rowCount, &createTime, &updateTime)
	if err == nil {
		m.Engine = engine.String
		m.Collation = collation.String
		m.RowFormat = rowFmt.String
		m.Comment = comment.String
		m.RowCount = rowCount.Int64
		m.CreateTime = createTime.String
		m.UpdateTime = updateTime.String
		if idx := strings.Index(m.Collation, "_"); idx > 0 {
			m.Charset = m.Collation[:idx]
		}
		result.TableMeta = m
	}

	// Exact row count — information_schema.TABLE_ROWS is an estimate for InnoDB
	var exactCount int64
	if err := d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM %s.%s", QuoteIdent(schema, "mysql"), QuoteIdent(table, "mysql"))).Scan(&exactCount); err == nil {
		if m.RowCount == 0 || m.RowCount != exactCount {
			m.RowCount = exactCount
			result.TableMeta = m
		}
	}

	// DDL
	var name, ddl string
	if schema != "" {
		err = d.db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE %s.%s", QuoteIdent(schema, "mysql"), QuoteIdent(table, "mysql"))).Scan(&name, &ddl)
	} else {
		err = d.db.QueryRow(fmt.Sprintf("SHOW CREATE TABLE %s", QuoteIdent(table, "mysql"))).Scan(&name, &ddl)
	}
	if err == nil {
		result.DDL = ddl
	}

	return result, nil
}

func (d *MySQLDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) {
	row := d.db.QueryRow(`SELECT ENGINE, TABLE_ROWS, TABLE_COMMENT, CREATE_TIME, UPDATE_TIME, TABLE_COLLATION, ROW_FORMAT
		FROM information_schema.TABLES WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`, schema, table)
	var engine, comment, createTime, updateTime, tableCollation, rowFormat sql.NullString
	var rowCount sql.NullInt64
	if err := row.Scan(&engine, &rowCount, &comment, &createTime, &updateTime, &tableCollation, &rowFormat); err != nil {
		return map[string]interface{}{}, err
	}
	charset := ""
	if tableCollation.Valid && tableCollation.String != "" {
		if idx := strings.Index(tableCollation.String, "_"); idx > 0 {
			charset = tableCollation.String[:idx]
		}
	}
	return map[string]interface{}{
		"engine":      engine.String,
		"charset":     charset,
		"collation":   tableCollation.String,
		"row_format":  rowFormat.String,
		"row_count":   rowCount.Int64,
		"comment":     comment.String,
		"create_time": createTime.String,
		"update_time": updateTime.String,
	}, nil
}

// ─── Cross-Database DDL Helpers ────────────────────────────────────────────

func (d *MySQLDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string {
	var colDefs []string
	for _, col := range columns {
		name, _ := col["name"].(string)
		colType, _ := col["type"].(string)
		charLen := ""
		if v, ok := col["length"]; ok {
			charLen = fmt.Sprintf("%v", v)
		}
		targetType := ConvertDDLType(sourceDBType, "mysql", colType, charLen, colType)
		def := fmt.Sprintf("  `%s` %s", name, targetType)

		if nullable, ok := col["nullable"]; ok {
			if nullableStr, ok := nullable.(string); ok && nullableStr == "NO" {
				def += " NOT NULL"
			}
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf("CREATE TABLE `%s`.`%s` (\n%s\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;",
		schema, table, strings.Join(colDefs, ",\n"))
}

func (d *MySQLDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string {
	return fmt.Sprintf("ALTER TABLE %s COMMENT='%s'", qualifiedTable, strings.ReplaceAll(comment, "'", "''"))
}

func (d *MySQLDriver) FormatSQLValue(val interface{}) string {
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

func (d *MySQLDriver) AlterColumnClause(columnName, colType, columnDef string) string {
	return fmt.Sprintf("MODIFY COLUMN `%s` %s", columnName, columnDef)
}

func (d *MySQLDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;",
		tableName, colList, strings.Join(rowValues, ",\n"))
}

func (d *MySQLDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string {
	// For MySQL target, backtick-quoted schema prefixes need to be kept
	return ddl
}

func (d *MySQLDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, IFNULL(COLUMN_DEFAULT, '')
		FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var cols []AlterColumn
	for rows.Next() {
		var c AlterColumn
		var nullable string
		if err := rows.Scan(&c.Name, &c.Type, &nullable, &c.DefaultVal); err != nil {
			continue
		}
		c.Nullable = nullable == "YES"
		cols = append(cols, c)
	}
	if cols == nil {
		cols = make([]AlterColumn, 0)
	}
	return cols, nil
}

// GetColumnTypes returns MySQL-specific column type definitions
func (d *MySQLDriver) GetColumnTypes() []ColumnTypeInfo {
	return []ColumnTypeInfo{
		{Name: "int", NeedsLength: false, Description: "整数"},
		{Name: "bigint", NeedsLength: false, Description: "大整数"},
		{Name: "smallint", NeedsLength: false, Description: "小整数"},
		{Name: "tinyint", NeedsLength: false, Description: "微小整数"},
		{Name: "mediumint", NeedsLength: false, Description: "中等整数"},
		{Name: "decimal", NeedsLength: true, NeedsScale: true, Description: "定点数"},
		{Name: "numeric", NeedsLength: true, NeedsScale: true, Description: "数值"},
		{Name: "float", NeedsLength: false, Description: "浮点数"},
		{Name: "double", NeedsLength: false, Description: "双精度"},
		{Name: "char", NeedsLength: true, Description: "定长字符"},
		{Name: "varchar", NeedsLength: true, Description: "变长字符"},
		{Name: "text", NeedsLength: false, Description: "文本"},
		{Name: "tinytext", NeedsLength: false, Description: "微小文本"},
		{Name: "mediumtext", NeedsLength: false, Description: "中等文本"},
		{Name: "longtext", NeedsLength: false, Description: "长文本"},
		{Name: "binary", NeedsLength: true, Description: "定长二进制"},
		{Name: "varbinary", NeedsLength: true, Description: "变长二进制"},
		{Name: "blob", NeedsLength: false, Description: "大二进制"},
		{Name: "tinyblob", NeedsLength: false, Description: "微小二进制"},
		{Name: "mediumblob", NeedsLength: false, Description: "中等二进制"},
		{Name: "longblob", NeedsLength: false, Description: "长二进制"},
		{Name: "date", NeedsLength: false, Description: "日期"},
		{Name: "datetime", NeedsLength: false, Description: "日期时间"},
		{Name: "timestamp", NeedsLength: false, Description: "时间戳"},
		{Name: "time", NeedsLength: false, Description: "时间"},
		{Name: "year", NeedsLength: false, Description: "年份"},
		{Name: "enum", NeedsLength: false, Description: "枚举"},
		{Name: "set", NeedsLength: false, Description: "集合"},
		{Name: "json", NeedsLength: false, Description: "JSON"},
		{Name: "bit", NeedsLength: false, Description: "位"},
		{Name: "boolean", NeedsLength: false, Description: "布尔"},
		{Name: "geometry", NeedsLength: false, Description: "几何-通用"},
		{Name: "point", NeedsLength: false, Description: "几何-点"},
		{Name: "linestring", NeedsLength: false, Description: "几何-线"},
		{Name: "polygon", NeedsLength: false, Description: "几何-多边形"},
		{Name: "multipoint", NeedsLength: false, Description: "几何-多点"},
		{Name: "multilinestring", NeedsLength: false, Description: "几何-多线"},
		{Name: "multipolygon", NeedsLength: false, Description: "几何-多多边形"},
		{Name: "geometrycollection", NeedsLength: false, Description: "几何-集合"},
	}
}

func (d *MySQLDriver) GetIndexTypes() []IndexTypeInfo {
	return []IndexTypeInfo{
		{Name: "INDEX", Description: "普通索引 (B-Tree)"},
		{Name: "UNIQUE", Description: "唯一索引"},
		{Name: "FULLTEXT", Description: "全文索引"},
		{Name: "SPATIAL", Description: "空间索引"},
	}
}

// ─── Tree Metadata ─────────────────────────────────────────────────────────

func (d *MySQLDriver) GetTreeMetadata() TreeMetadata {
	return TreeMetadata{
		DBType: "mysql",
		Levels: []TreeLevel{
			{Key: "server", Label: "Server", LabelKey: "tree.server", Icon: "CloudServerOutlined"},
			{Key: "database", Label: "Database", LabelKey: "tree.database", PlaceholderKey: "tree.db_name_hint", Icon: "DatabaseOutlined"},
			{Key: "tables_folder", Label: "Tables", LabelKey: "tree.tables", Icon: "TableOutlined"},
			{Key: "views_folder", Label: "Views", LabelKey: "tree.views", Icon: "EyeOutlined"},
		},
		AllowCreate: map[string]bool{"database": true},
		SystemFilter: &SystemFilter{
			ExcludeNames: []string{"information_schema", "performance_schema", "mysql", "sys"},
		},
	}
}

// MySQL: ListDatabases = ListSchemaNames (Database = Schema)
func (d *MySQLDriver) ListDatabases() ([]DatabaseInfo, error) {
    rows, err := d.db.Query("SHOW DATABASES")
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var result []DatabaseInfo
    for rows.Next() {
        var name string
        if err := rows.Scan(&name); err == nil {
            if name != "information_schema" && name != "mysql" && name != "performance_schema" && name != "sys" {
                result = append(result, DatabaseInfo{Name: name})
            }
        }
    }
    // Enrich with charset/size for each database
    for i := range result {
        var charset string
        if err := d.db.QueryRow("SELECT DEFAULT_CHARACTER_SET_NAME FROM information_schema.SCHEMATA WHERE SCHEMA_NAME=?", result[i].Name).Scan(&charset); err == nil {
            result[i].Charset = charset
        }
        var size float64
        if err := d.db.QueryRow("SELECT COALESCE(ROUND(SUM(DATA_LENGTH+INDEX_LENGTH)/1024/1024,2),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", result[i].Name).Scan(&size); err == nil {
            result[i].SizeMB = int64(size)
        }
        var tables int
        if err := d.db.QueryRow("SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?", result[i].Name).Scan(&tables); err == nil {
            result[i].Tables = tables
        }
    }
    if result == nil { result = []DatabaseInfo{} }
    return result, nil
}

func (d *MySQLDriver) ResolveContext(arg string) DatabaseContext {
	return DatabaseContext{Database: arg}
}

func (d *MySQLDriver) ListDatabaseSchemas(database string) ([]string, error) {
	// MySQL: database = schema, no separate schema list within a database
	return nil, nil
}

// GetServerInfo returns MySQL server info
func (d *MySQLDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})
	// Version
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err == nil {
		info["version"] = version
	}
	// Uptime
	var uptime int64
	if err := d.db.QueryRowContext(ctx, "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Uptime'").Scan(&uptime); err == nil {
		info["uptime_seconds"] = uptime
	}
	// Max connections
	rows, err := d.db.QueryContext(ctx, "SHOW VARIABLES LIKE 'max_connections'")
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var name, val string
			rows.Scan(&name, &val)
			info[name] = val
		}
	}
	return info, nil
}

// GetMetrics returns MySQL current metrics
func (d *MySQLDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	queries := map[string]string{
		"当前连接数":     "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Threads_connected'",
		"活跃线程":      "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Threads_running'",
		"累计查询":      "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Questions'",
		"慢查询数":      "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Slow_queries'",
		"缓冲池命中率":  "SELECT ROUND(100 - (SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_reads') * 100.0 / GREATEST((SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_read_requests'), 1), 2)",
		"最大连接数":     "SELECT VARIABLE_VALUE FROM performance_schema.global_variables WHERE VARIABLE_NAME='max_connections'",
		"运行时间(秒)":   "SELECT VARIABLE_VALUE FROM performance_schema.global_status WHERE VARIABLE_NAME='Uptime'",
	}
	for name, sql := range queries {
		var v string
		if err := d.db.QueryRowContext(ctx, sql).Scan(&v); err == nil {
			m[name] = v
		}
	}
	return m, nil
}

// ─── DDL Builder (high-level) ────────────────────────────────────────────

func (d *MySQLDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) {
	// Reuse existing AddColumnDDL
	nullable := col.Nullable == nil || *col.Nullable
	defaultVal := ""
	if col.HasDef != nil && *col.HasDef { defaultVal = col.Default }
	parts := d.AddColumnDDL(table, col.Name, col.Type, col.Length, nullable, defaultVal, col.Comment, col.After)
	return strings.Join(parts, ";\n"), "", nil, false, nil
}

func (d *MySQLDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	return BuildMySQLModifyColumn(table, col, orig)
}

// BuildMySQLModifyColumn generates MODIFY COLUMN DDL for MySQL.
func BuildMySQLModifyColumn(tbl string, ch AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	var stmts, rollbacks, warnings []string
	highRisk := false

	// Detect length shrink
	if ch.Length != "" && orig.Length != "" && ch.Length != orig.Length {
		origL, newL := toInt(orig.Length), toInt(ch.Length)
		if origL > 0 && newL > 0 && newL < origL {
			warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: length shrink (%s → %s) may truncate data", ch.Name, orig.Length, ch.Length))
			highRisk = true
		}
	}
	if ch.Nullable != nil && !*ch.Nullable && orig.Nullable {
		warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: tightening NOT NULL may fail on existing NULL values", ch.Name))
		highRisk = true
	}

	colDef := quoteMySQL(ch.Name) + " " + mysqlColumnType(ch.Type, ch.Length)
	if ch.Nullable != nil {
		if *ch.Nullable { colDef += " NULL" } else { colDef += " NOT NULL" }
	}
	if ch.HasDef != nil && *ch.HasDef {
		colDef += " DEFAULT " + formatDefault(ch.Default)
	} else if ch.HasDef != nil && !*ch.HasDef {
		colDef += " DEFAULT NULL"
	}
	if ch.Comment != "" {
		colDef += " COMMENT " + mysqlString(ch.Comment)
	}
	stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", tbl, colDef))

	origDef := quoteMySQL(orig.Name) + " " + orig.Type
	if !orig.Nullable { origDef += " NOT NULL" } else { origDef += " NULL" }
	if orig.HasDef { origDef += " DEFAULT " + formatDefault(orig.Default) }
	if orig.Comment != "" { origDef += " COMMENT " + mysqlString(orig.Comment) }
	rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", tbl, origDef))

	return stmts, rollbacks, warnings, highRisk, nil
}

func (d *MySQLDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) {
	return d.DropColumnDDL(table, colName), "", nil, true, nil
}

func (d *MySQLDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) {
	stmt := d.AddIndexDDL(table, idx.Name, idx.Type, idx.Columns)
	return stmt, "", nil
}

func (d *MySQLDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) {
	stmt := d.DropIndexDDL(table, schema, idxName)
	return stmt, "", nil, true, nil
}

func (d *MySQLDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) {
	return "", "", fmt.Errorf("MySQL does not support index comments")
}

func (d *MySQLDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) {
	// Handled by table_manager buildAddConstraint - forward
	return "", "", fmt.Errorf("use table_manager for constraints")
}

func (d *MySQLDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) {
	return "", "", fmt.Errorf("use table_manager for constraints")
}

func (d *MySQLDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) {
	return d.SetTableCommentDDL(table, "", "", newComment), "", nil
}

// ListProcesses returns current process list
func (d *MySQLDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, processQueries[dbType])
}

// ListUsers returns user list
func (d *MySQLDriver) ListUsers(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, userQueries[dbType])
}

// ListTablespaces returns tablespace info
func (d *MySQLDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, tablespaceQueries[dbType])
}

// GetMetricsV2 returns structured MySQL monitoring metrics
func (d *MySQLDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) {
	start := time.Now()
	m := &ServerMetricsV2{DBType: "mysql", CollectedAt: time.Now(), DatabaseSpecific: make(map[string]interface{})}
	var v int64

	// Connections
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST").Scan(&m.Connections.Total)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.PROCESSLIST WHERE COMMAND!='Sleep'").Scan(&m.Connections.Active)
	if m.Connections.Total > m.Connections.Active {
		m.Connections.Idle = m.Connections.Total - m.Connections.Active
	}
	d.db.QueryRowContext(ctx, "SELECT @@max_connections").Scan(&m.Connections.MaxConnections)
	if m.Connections.MaxConnections > 0 {
		m.Connections.UsagePercent = float64(m.Connections.Total) * 100.0 / float64(m.Connections.MaxConnections)
	}

	// Throughput
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Questions'").Scan(&m.Throughput.QuestionsTotal)
	var uptime int64
	if d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Uptime'").Scan(&uptime); uptime > 1 {
		m.Throughput.QPS = float64(m.Throughput.QuestionsTotal) / float64(uptime)
	}
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Com_commit'").Scan(&m.Throughput.CommitTotal)
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Com_rollback'").Scan(&m.Throughput.RollbackTotal)
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Slow_queries'").Scan(&m.Throughput.SlowQueries)

	// Buffer cache
	var reads, readReqs int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_reads'").Scan(&reads)
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_read_requests'").Scan(&readReqs)
	if readReqs > 0 {
		m.BufferCache.HitRate = (1.0 - float64(reads)/float64(readReqs)) * 100.0
	}
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_buffer_pool_pages_dirty'").Scan(&m.BufferCache.DirtyPages)
	var poolSize int64
	d.db.QueryRowContext(ctx, "SELECT @@innodb_buffer_pool_size/1024/1024").Scan(&poolSize)
	m.BufferCache.TotalMB = float64(poolSize)

	// Locks
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_deadlocks'").Scan(&m.Locks.Deadlocks)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE trx_state='LOCK WAIT'").Scan(&m.Locks.LockWaits)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.INNODB_TRX WHERE TIMESTAMPDIFF(SECOND, trx_started, NOW()) > 60").Scan(&m.Locks.LongTransactions)

	// Storage — database sizes
	rows, err := d.db.QueryContext(ctx, `SELECT TABLE_SCHEMA, ROUND(SUM(DATA_LENGTH+INDEX_LENGTH)/1024/1024,2) FROM information_schema.TABLES GROUP BY TABLE_SCHEMA ORDER BY 2 DESC LIMIT 10`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var ts TablespaceMetric
			if rows.Scan(&ts.Name, &ts.SizeMB) == nil {
				m.Storage.Tablespaces = append(m.Storage.Tablespaces, ts)
			}
		}
	}

	// Replication
	// Replication
	var ioRunning, sqlRunning string
	var lagSeconds float64
	if d.db.QueryRowContext(ctx, `SHOW SLAVE STATUS`).Scan(new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), new(interface{}), &ioRunning, &sqlRunning, &lagSeconds); err == nil {
		io := ioRunning == "Yes"
		sql := sqlRunning == "Yes"
		m.Replication = &ReplicationMetrics{SQLThreadRunning: &sql, IOThreadRunning: &io, LagSeconds: lagSeconds}
	}

	// DB-specific: Aborted connects, Binlog count, max used connections
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Aborted_connects'").Scan(&v)
	m.DatabaseSpecific["aborted_connects"] = v
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Max_used_connections'").Scan(&v)
	m.DatabaseSpecific["max_used_connections"] = v
	d.db.QueryRowContext(ctx, "SELECT COALESCE(VARIABLE_VALUE,0) FROM performance_schema.global_status WHERE VARIABLE_NAME='Innodb_row_lock_current_waits'").Scan(&v)
	m.DatabaseSpecific["row_lock_waits"] = v

	var binlogCount int64
	if d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM information_schema.FILES WHERE FILE_TYPE='BINARY LOG'").Scan(&binlogCount); binlogCount > 0 {
		m.DatabaseSpecific["binlog_file_count"] = binlogCount
	}

	m.CostMs = time.Since(start).Milliseconds()
	return m, nil
}

func (d *MySQLDriver) CreateDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE DATABASE %s", name))
	return err
}

func (d *MySQLDriver) DropDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP DATABASE %s", name))
	return err
}

func (d *MySQLDriver) CreateUser(username, password string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s'", username, password))
	return err
}

func (d *MySQLDriver) DropUser(username string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP USER '%s'", username))
	return err
}

func (d *MySQLDriver) GrantPrivileges(username, database string, privileges []string) error {
	privs := "ALL PRIVILEGES"
	if len(privileges) > 0 {
		privs = strings.Join(privileges, ", ")
	}
	_, err := d.db.Exec(fmt.Sprintf("GRANT %s ON %s.* TO '%s'@'%%'", privs, database, username))
	return err
}

func (d *MySQLDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error) {
	var result []PrivilegeEntry
	likePattern := fmt.Sprintf("'%s'@'%%'", username)

	// 1. SHOW GRANTS for global/database-level privileges
	grantRows, err := d.db.Query(fmt.Sprintf("SHOW GRANTS FOR %s", likePattern))
	if err == nil && grantRows != nil {
		defer grantRows.Close()
		for grantRows.Next() {
			var grant string
			if grantRows.Scan(&grant) == nil {
				// Parse GRANT ALL PRIVILEGES ON *.* TO ...
				if strings.Contains(grant, "GRANT") {
					parsed := parseMySQLGrant(grant)
					if parsed != nil {
						result = append(result, *parsed)
					}
				}
			}
		}
	}

	// 2. Table-level privileges from information_schema
	rows, err2 := d.db.Query("SELECT TABLE_SCHEMA, TABLE_NAME, PRIVILEGE_TYPE, IS_GRANTABLE FROM information_schema.TABLE_PRIVILEGES WHERE GRANTEE LIKE ?", likePattern)
	if err2 == nil && rows != nil {
		defer rows.Close()
		privMap := make(map[string]*PrivilegeEntry)
		for rows.Next() {
			var db, table, priv, grantable string
			if rows.Scan(&db, &table, &priv, &grantable) == nil {
				key := db + "." + table
				if _, ok := privMap[key]; !ok {
					privMap[key] = &PrivilegeEntry{Database: db, ObjectType: "TABLE", ObjectName: table}
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

	if result == nil { result = []PrivilegeEntry{} }
	return result, nil
}

// parseMySQLGrant parses a MySQL SHOW GRANTS statement into a PrivilegeEntry
func parseMySQLGrant(grant string) *PrivilegeEntry {
	// "GRANT ALL PRIVILEGES ON *.* TO 'root'@'localhost' WITH GRANT OPTION"
	// "GRANT SELECT, INSERT ON `db`.`table` TO 'user'@'%'"
	grant = strings.TrimPrefix(grant, "GRANT ")
	
	// Extract privileges
	onIdx := strings.Index(grant, " ON ")
	if onIdx < 0 { return nil }
	privsStr := strings.TrimSpace(grant[:onIdx])
	rest := grant[onIdx+4:]
	
	// Extract object
	toIdx := strings.Index(rest, " TO ")
	if toIdx < 0 { return nil }
	objStr := strings.TrimSpace(rest[:toIdx])
	objStr = strings.ReplaceAll(objStr, "`", "")
	
	// Parse object
	var db, objType, objName string
	if objStr == "*.*" {
		db = "*"; objType = "DATABASE"; objName = "*"
	} else if strings.HasSuffix(objStr, ".*") {
		db = strings.TrimSuffix(objStr, ".*"); objType = "DATABASE"; objName = "*"
	} else if strings.Contains(objStr, ".") {
		parts := strings.SplitN(objStr, ".", 2)
		db = parts[0]; objType = "TABLE"; objName = parts[1]
	} else {
		db = objStr; objType = "DATABASE"; objName = "*"
	}
	
	// Parse privileges
	var privs []string
	if privsStr == "ALL PRIVILEGES" || privsStr == "ALL" {
		privs = []string{"ALL"}
	} else {
		privs = strings.Split(privsStr, ", ")
	}

	// USAGE is implicit for all users and cannot be revoked; skip display-only entries
	if len(privs) == 1 && strings.EqualFold(privs[0], "USAGE") { return nil }
	
	grantable := strings.Contains(grant, "WITH GRANT OPTION")
	return &PrivilegeEntry{Database: db, ObjectType: objType, ObjectName: objName,
		Privileges: privs, Grantable: grantable, IsSystem: db == "*"}
}

func (d *MySQLDriver) GetUserRoles(username string) ([]string, error) {
	likePattern := "'" + username + "'@'%'"
	rows, err := d.db.Query("SELECT FROM_USER FROM mysql.role_edges WHERE TO_USER LIKE ?", likePattern)
	if err != nil { return []string{}, nil }
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil { roles = append(roles, r) }
	}
	if roles == nil { roles = []string{} }
	return roles, nil
}

func (d *MySQLDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) {
	result := &ChangeResult{}
	for _, ch := range changes {
		obj := ch.Database
		if ch.ObjectName != "" { obj += "." + ch.ObjectName }
		for _, p := range ch.Grant {
			stmt := fmt.Sprintf("GRANT %s ON %s TO '%s'@'%%'", p, obj, username)
			result.Statements = append(result.Statements, stmt)
		}
		for _, p := range ch.Revoke {
			stmt := fmt.Sprintf("REVOKE %s ON %s FROM '%s'@'%%'", p, obj, username)
			result.Statements = append(result.Statements, stmt)
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

func (d *MySQLDriver) DetectCapability() (*CapabilitySet, error) {
	var v string
	d.db.QueryRow("SELECT VERSION()").Scan(&v)
	return DetectCapability("mysql", v), nil
}
func (d *MySQLDriver) ListRoles() ([]RoleInfo, error) {
	rows, err := d.db.Query("SELECT User, Host FROM mysql.user WHERE account_locked='Y'")
	if err != nil { return []RoleInfo{}, nil }
	defer rows.Close()
	var result []RoleInfo
	for rows.Next() { var u, h string; if rows.Scan(&u, &h) == nil { result = append(result, RoleInfo{Name: u, IsSystem: false}) } }
	if result == nil { result = []RoleInfo{} }
	return result, nil
}
func (d *MySQLDriver) CreateRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE ROLE '%s'", name))
	return err
}
func (d *MySQLDriver) DropRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP ROLE '%s'", name))
	return err
}
func (d *MySQLDriver) AddRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf("GRANT '%s' TO '%s'@'%%'", role, member))
	return err
}
func (d *MySQLDriver) RemoveRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf("REVOKE '%s' FROM '%s'@'%%'", role, member))
	return err
}
func (d *MySQLDriver) AlterUserPassword(username, newPassword string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER '%s'@'%%' IDENTIFIED BY '%s'", username, newPassword))
	return err
}
func (d *MySQLDriver) AlterUserLock(username string, lock bool) error {
	action := "UNLOCK"
	if lock { action = "LOCK" }
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER '%s'@'%%' ACCOUNT %s", username, action))
	return err
}
func (d *MySQLDriver) AlterUserRename(oldName, newName string) error {
	return fmt.Errorf("rename not supported for MySQLDriver")
}
func (d *MySQLDriver) AlterUserDefaultSchema(username, schema string) error {
	return fmt.Errorf("default schema not supported for MySQLDriver")
}

func (d *MySQLDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error) {
	return d.GetUserPrivileges(roleName)
}

func (d *MySQLDriver) AlterRoleAttribute(roleName, attribute, value string) error {
	return fmt.Errorf("AlterRoleAttribute not supported for this database")
}

func (d *MySQLDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error) {
	return []ParentRoleInfo{}, nil
}

func (d *MySQLDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error) {
	return []MemberRoleInfo{}, nil
}

func (d *MySQLDriver) GetRoleInherit(roleName string) (bool, error) {
	return true, nil
}

func (d *MySQLDriver) GetRoleMemberships(roleName string) ([]string, error) {
	return []string{}, nil
}

// ─── System SQL Dialect Methods ──────────────────────────────────────────
func (d *MySQLDriver) SQLFormatTime(col string) string {
	return fmt.Sprintf("DATE_FORMAT(%s, \x27%%Y-%%m-%%d %%H:%%i:%%S\x27)", col)
}
func (d *MySQLDriver) SQLConcat(parts ...string) string {
	return "CONCAT(" + strings.Join(parts, ", ") + ")"
}
func (d *MySQLDriver) SQLIsNull(col, defaultVal string) string {
	return fmt.Sprintf("IFNULL(%s, %s)", col, defaultVal)
}
func (d *MySQLDriver) SQLCurrentTimestamp() string { return "NOW()" }
func (d *MySQLDriver) SQLQuoteIdent(name string) string { return "`" + name + "`" }

