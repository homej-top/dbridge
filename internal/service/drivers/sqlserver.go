package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	// go-mssqldb - official Microsoft SQL Server driver
	// Add to go.mod:
	//   require github.com/microsoft/go-mssqldb v1.7.2
	// Blank import registers the "sqlserver" driver:
	//   _ "github.com/microsoft/go-mssqldb"
	//
	// Connection string format:
	//   sqlserver://user:pass@host:port?database=dbname
)

// SQLServerDriver implements DatabaseDriver for SQL Server
type SQLServerDriver struct {
	db     *sql.DB
	cfg    DriverConfig
	pooled bool
}

func NewSQLServerDriver(cfg DriverConfig) (DatabaseDriver, *sql.DB, error) {
	var db *sql.DB
	if cfg.DB != nil {
		db = cfg.DB
	} else {
		port := cfg.Port
		if port == 0 {
			port = 1433
		}
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
			cfg.Username, cfg.Password, cfg.Host, port, cfg.Database)
		var err error
		db, err = sql.Open("sqlserver", dsn)
		if err != nil {
			return nil, nil, err
		}
		db.SetMaxOpenConns(cfg.MaxConnections)
		db.SetMaxIdleConns(cfg.MaxConnections / 2)
		db.SetConnMaxLifetime(5 * time.Minute)
	}

	if cfg.DB == nil {
		if err := db.Ping(); err != nil {
			db.Close()
			return nil, nil, fmt.Errorf("sqlserver connection failed: %w", err)
		}
	}

	return &SQLServerDriver{db: db, cfg: cfg, pooled: cfg.DB != nil}, db, nil
}

func (d *SQLServerDriver) Ping() error     { return d.db.Ping() }
func (d *SQLServerDriver) Close() error {
	if d.pooled {
		return nil
	}
	return d.db.Close()
}
func (d *SQLServerDriver) DBType() string  { return "sqlserver" }
func (d *SQLServerDriver) Dialect() string { return "sqlserver" }

func (d *SQLServerDriver) useSchema(schema string) error {
	// SQL Server has no session-level schema switch.
	// Database context switching is done at connection time.
	// Schema targeting is done via qualified names in SQL (e.g. [schema].[table]).
	return nil
}

// ─── Schema Discovery ──────────────────────────────────────────────────────

func (d *SQLServerDriver) ListSchemas() ([]SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT name FROM sys.schemas
		WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin', 'db_backupoperator', 'db_datareader', 'db_datawriter', 'db_denydatareader', 'db_denydatawriter')
		ORDER BY name`)
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

func (d *SQLServerDriver) ListSchemaNames() ([]string, error) {
	rows, err := d.db.Query(`SELECT name FROM sys.schemas
		WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin', 'db_backupoperator', 'db_datareader', 'db_datawriter', 'db_denydatareader', 'db_denydatawriter')
		ORDER BY name`)
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

func (d *SQLServerDriver) ListObjects(schema string) (*SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT TABLE_NAME, TABLE_TYPE
		FROM INFORMATION_SCHEMA.TABLES
		WHERE TABLE_SCHEMA = @p1 ORDER BY TABLE_NAME`, schema)
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

func (d *SQLServerDriver) ListSchemaDetail() ([]SchemaDetailItem, error) {
	rows, err := d.db.Query(`SELECT
		s.name,
		COUNT(CASE WHEN o.type = 'U' THEN 1 END),
		COUNT(CASE WHEN o.type = 'V' THEN 1 END),
		'' AS charset, '' AS collation
	FROM sys.schemas s
	LEFT JOIN sys.objects o ON s.schema_id = o.schema_id AND o.type IN ('U', 'V')
	WHERE s.name NOT IN ('sys', 'INFORMATION_SCHEMA')
	GROUP BY s.name ORDER BY s.name`)
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

func (d *SQLServerDriver) ListTables(schema string) ([]TableListItem, error) {
	rows, err := d.db.Query(`SELECT
		o.name,
		CASE o.type WHEN 'U' THEN 'TABLE' WHEN 'V' THEN 'VIEW' END,
		NULL AS engine,
		ISNULL(SUM(p.rows), 0),
		ISNULL(CAST(ep.value AS NVARCHAR(MAX)), ''),
		o.create_date,
		o.modify_date
	FROM sys.objects o
	JOIN sys.schemas s ON o.schema_id = s.schema_id
	LEFT JOIN sys.partitions p ON o.object_id = p.object_id AND p.index_id IN (0, 1)
	LEFT JOIN sys.extended_properties ep ON o.object_id = ep.major_id AND ep.minor_id = 0 AND ep.name = 'MS_Description'
	WHERE s.name = @p1 AND o.type IN ('U', 'V')
	GROUP BY o.name, o.type, CAST(ep.value AS NVARCHAR(MAX)), o.create_date, o.modify_date
	ORDER BY o.name`, schema)
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

func (d *SQLServerDriver) GetColumns(schema, table string) ([]ColumnDetail, error) {
	rows, err := d.db.Query(`SELECT
		c.COLUMN_NAME,
		c.DATA_TYPE,
		COALESCE(CAST(c.CHARACTER_MAXIMUM_LENGTH AS VARCHAR), ''),
		c.IS_NULLABLE,
		COALESCE(c.COLUMN_DEFAULT, ''),
		COALESCE(ep.value, ''),
		CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 'PRI' ELSE '' END
	FROM INFORMATION_SCHEMA.COLUMNS c
	LEFT JOIN sys.extended_properties ep ON OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME)) = ep.major_id
		AND c.ORDINAL_POSITION = ep.minor_id AND ep.name = 'MS_Description'
	LEFT JOIN (
		SELECT ku.TABLE_SCHEMA, ku.TABLE_NAME, ku.COLUMN_NAME
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
		WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
	) pk ON c.TABLE_SCHEMA = pk.TABLE_SCHEMA AND c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
	WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
	ORDER BY c.ORDINAL_POSITION`, schema, table)
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

func (d *SQLServerDriver) GetColumnDetails(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, DATA_TYPE, IS_NULLABLE, COLUMN_DEFAULT
		FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2
		ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *SQLServerDriver) GetDDL(schema, table string) (string, error) {
	rows, err := d.db.Query(`SELECT COLUMN_NAME, DATA_TYPE, COALESCE(CAST(CHARACTER_MAXIMUM_LENGTH AS VARCHAR),''), IS_NULLABLE, COLUMN_DEFAULT, '' AS cmt, '' AS pk FROM INFORMATION_SCHEMA.COLUMNS WHERE TABLE_SCHEMA=@p1 AND TABLE_NAME=@p2 ORDER BY ORDINAL_POSITION`, schema, table)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var colDefs []string
	for rows.Next() {
		var name, dataType, length, nullable string
		var colDefault sql.NullString
		var dummy1, dummy2 string
		if err := rows.Scan(&name, &dataType, &length, &nullable, &colDefault, &dummy1, &dummy2); err != nil {
			continue
		}
		colType := dataType
		if length != "" && length != "0" {
			colType = fmt.Sprintf("%s(%s)", dataType, length)
		}
		def := fmt.Sprintf("  [%s] %s", name, colType)
		if nullable == "NO" {
			def += " NOT NULL"
		}
		if colDefault.Valid && colDefault.String != "" {
			def += " DEFAULT " + colDefault.String
		}
		colDefs = append(colDefs, def)
	}
	if len(colDefs) == 0 {
		return "", fmt.Errorf("table not found: %s.%s", schema, table)
	}
	return fmt.Sprintf("CREATE TABLE [%s].[%s] (\n%s\n);", schema, table, strings.Join(colDefs, ",\n")), nil
}

func (d *SQLServerDriver) GetViewDefinition(schema, view string) (string, error) {
	var def sql.NullString
	err := d.db.QueryRow(`SELECT VIEW_DEFINITION FROM INFORMATION_SCHEMA.VIEWS
		WHERE TABLE_SCHEMA = @p1 AND TABLE_NAME = @p2`, schema, view).Scan(&def)
	if err != nil || !def.Valid {
		return "", fmt.Errorf("view not found: %s.%s", schema, view)
	}
	// Strip any CREATE VIEW prefix that may be embedded
	result := strings.TrimSpace(def.String)
	result = regexp.MustCompile(`(?i)^CREATE\s+(OR\s+REPLACE\s+)?(OR\s+ALTER\s+)?VIEW\s+\S+\s+AS\s*`).ReplaceAllString(result, "")
	return result, nil
}

// ─── Query Execution ──────────────────────────────────────────────────────

func (d *SQLServerDriver) ExecuteQuery(sql string, schema string) (*QueryResult, error) {
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

	// Ensure QUOTED_IDENTIFIER is ON for non-SELECT statements.
	// Skip for CREATE SCHEMA/CREATE VIEW which must be first in batch.
	batchSQL := sql
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(upper, "CREATE SCHEMA") && !strings.HasPrefix(upper, "CREATE VIEW") && !strings.HasPrefix(upper, "CREATE OR ALTER VIEW") {
		batchSQL = "SET QUOTED_IDENTIFIER ON; " + sql
	}
	res, err := d.db.Exec(batchSQL)
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

func (d *SQLServerDriver) GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	safeSchema := strings.ReplaceAll(schema, "]", "]]")
	safeTable := strings.ReplaceAll(table, "]", "]]")
	offset := (page - 1) * pageSize

	var total int64
	d.db.QueryRow(fmt.Sprintf("SELECT COUNT(*) FROM [%s].[%s]", safeSchema, safeTable)).Scan(&total)

	query := fmt.Sprintf(
		"SELECT * FROM [%s].[%s] ORDER BY (SELECT NULL) OFFSET %d ROWS FETCH NEXT %d ROWS ONLY",
		safeSchema, safeTable, offset, pageSize)

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

func (d *SQLServerDriver) ExecuteDDL(ddl string) (int64, error) {
	res, err := d.db.Exec(ddl)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *SQLServerDriver) ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error) {
	statements := SplitDDLWithDialect(ddl, "sqlserver")
	total = int64(len(statements))

	for _, stmt := range statements {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}

		if importStrategy != "" && strings.HasPrefix(strings.ToUpper(s), "INSERT") {
			// SQL Server doesn't have INSERT IGNORE; use MERGE pattern is complex
			// skip/replace strategies not directly supported
		}

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

func (d *SQLServerDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string {
	var parts []string
	if length != "" && length != "0" {
		parts = append(parts, fmt.Sprintf("%s(%s)", colType, length))
	} else {
		parts = append(parts, colType)
	}
	if !nullable {
		parts = append(parts, "NOT NULL")
	}
	ddl := fmt.Sprintf("ALTER TABLE %s ALTER COLUMN [%s] %s", qualifiedTable, columnName, strings.Join(parts, " "))
	result := []string{ddl}
	if comment != "" {
		result = append(result, BuildSQLServerColumnComment(qualifiedTable, columnName, comment))
	}
	return result
}

func (d *SQLServerDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string {
	return fmt.Sprintf("DROP INDEX [%s] ON %s", indexName, qualifiedTable)
}

func (d *SQLServerDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string {
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
	ddl := fmt.Sprintf("ALTER TABLE %s ADD [%s] %s", qualifiedTable, columnName, strings.Join(parts, " "))
	result := []string{ddl}
	if comment != "" {
		result = append(result, BuildSQLServerColumnComment(qualifiedTable, columnName, comment))
	}
	return result
}

func (d *SQLServerDriver) DropColumnDDL(qualifiedTable, columnName string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN [%s]", qualifiedTable, columnName)
}

func (d *SQLServerDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string {
	var colList []string
	for _, c := range columns {
		colList = append(colList, "["+c+"]")
	}
	cols := strings.Join(colList, ", ")
	if indexType == "UNIQUE" {
		return fmt.Sprintf("CREATE UNIQUE INDEX [%s] ON %s (%s)", indexName, qualifiedTable, cols)
	}
	return fmt.Sprintf("CREATE INDEX [%s] ON %s (%s)", indexName, qualifiedTable, cols)
}

func (d *SQLServerDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT i.name, c.name AS column_name, i.is_unique, i.type
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.objects o ON i.object_id = o.object_id
		JOIN sys.schemas s ON o.schema_id = s.schema_id
		WHERE s.name = @p1 AND o.name = @p2 AND i.name IS NOT NULL
		ORDER BY i.name`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *SQLServerDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT
		fk.name AS constraint_name,
		pc.name AS column_name,
		ref_s.name AS referenced_schema,
		ref_o.name AS referenced_table,
		ref_c.name AS referenced_column
	FROM sys.foreign_keys fk
	JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
	JOIN sys.columns pc ON fkc.parent_object_id = pc.object_id AND fkc.parent_column_id = pc.column_id
	JOIN sys.objects o ON fk.parent_object_id = o.object_id
	JOIN sys.objects ref_o ON fk.referenced_object_id = ref_o.object_id
	JOIN sys.columns ref_c ON fkc.referenced_object_id = ref_c.object_id AND fkc.referenced_column_id = ref_c.column_id
	JOIN sys.schemas s ON o.schema_id = s.schema_id
	JOIN sys.schemas ref_s ON ref_o.schema_id = ref_s.schema_id
	WHERE s.name = @p1 AND o.name = @p2`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *SQLServerDriver) GetFullStructure(schema, table string) (*FullStructure, error) {
	result := &FullStructure{}

	// Columns
	rows, err := d.db.Query(`
		SELECT
			c.COLUMN_NAME,
			c.DATA_TYPE,
			COALESCE(CAST(c.CHARACTER_MAXIMUM_LENGTH AS VARCHAR), ''),
			c.IS_NULLABLE,
			COALESCE(c.COLUMN_DEFAULT, ''),
			COALESCE(CAST(ep.value AS NVARCHAR(MAX)), ''),
			CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 'PRI' ELSE '' END
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN sys.extended_properties ep
			ON ep.major_id = OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME))
			AND ep.minor_id = c.ORDINAL_POSITION
			AND ep.class = 1
			AND ep.name = 'MS_Description'
		LEFT JOIN (
			SELECT ku.TABLE_SCHEMA, ku.TABLE_NAME, ku.COLUMN_NAME
			FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
			JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
				ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
			WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		) pk ON c.TABLE_SCHEMA = pk.TABLE_SCHEMA
			AND c.TABLE_NAME = pk.TABLE_NAME
			AND c.COLUMN_NAME = pk.COLUMN_NAME
		WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION
	`, schema, table)
	if err != nil {
		return nil, fmt.Errorf("failed to get columns: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var col TableColumn
		var nullableStr string
		if err := rows.Scan(&col.Name, &col.Type, &col.Length, &nullableStr, &col.Default, &col.Comment, &col.Key); err != nil {
			continue
		}
		col.Nullable = nullableStr == "YES"
		if col.Default != "" {
			col.HasDef = true
		}
		result.Columns = append(result.Columns, col)
	}

	result.DDL = d.buildFullDDL(schema, table)

	// Indexes
	idxRows, err := d.db.Query(`
		SELECT
			i.name,
			CASE WHEN i.is_primary_key = 1 THEN 'PRIMARY'
			     WHEN i.is_unique = 1 AND i.type = 1 THEN 'UNIQUE CLUSTERED'
			     WHEN i.is_unique = 1 THEN 'UNIQUE'
			     WHEN i.type = 1 THEN 'CLUSTERED'
			     ELSE 'INDEX' END,
			STRING_AGG(c.name, ',') WITHIN GROUP (ORDER BY ic.key_ordinal)
		FROM sys.indexes i
		JOIN sys.index_columns ic ON i.object_id = ic.object_id AND i.index_id = ic.index_id
		JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
		JOIN sys.tables t ON i.object_id = t.object_id
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		WHERE s.name = @p1 AND t.name = @p2 AND i.name IS NOT NULL
		GROUP BY i.name, i.is_primary_key, i.is_unique, i.type
		ORDER BY i.name`, schema, table)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var idx TableIndex
			var cols sql.NullString
			if err := idxRows.Scan(&idx.Name, &idx.Type, &cols); err != nil {
				continue
			}
			if cols.Valid && cols.String != "" {
				idx.Columns = strings.Split(cols.String, ",")
			}
			result.Indexes = append(result.Indexes, idx)
		}
	}
	if result.Indexes == nil {
		result.Indexes = make([]TableIndex, 0)
	}

	// Constraints
	conRows, err := d.db.Query(`
		SELECT con.name, con.type, cols,
			COALESCE(ref_table, ''), COALESCE(ref_cols, ''),
			COALESCE(delete_action, ''), COALESCE(update_action, '')
		FROM (
			SELECT kc.name,
				CASE WHEN kc.type = 'PK' THEN 'PRIMARY KEY' ELSE 'UNIQUE' END AS type,
				STRING_AGG(c.name, ',') WITHIN GROUP (ORDER BY ic.key_ordinal) AS cols,
				NULL AS ref_table, NULL AS ref_cols,
				NULL AS delete_action, NULL AS update_action
			FROM sys.key_constraints kc
			JOIN sys.index_columns ic ON kc.parent_object_id = ic.object_id AND kc.unique_index_id = ic.index_id
			JOIN sys.columns c ON ic.object_id = c.object_id AND ic.column_id = c.column_id
			JOIN sys.tables t ON kc.parent_object_id = t.object_id
			JOIN sys.schemas s ON t.schema_id = s.schema_id
			WHERE s.name = @p1 AND t.name = @p2
			GROUP BY kc.name, kc.type
			UNION ALL
			SELECT fk.name, 'FOREIGN KEY' AS type,
				STRING_AGG(pc.name, ',') WITHIN GROUP (ORDER BY fkc.constraint_column_id) AS cols,
				OBJECT_SCHEMA_NAME(fk.referenced_object_id) + '.' + OBJECT_NAME(fk.referenced_object_id) AS ref_table,
				STRING_AGG(rc.name, ',') WITHIN GROUP (ORDER BY fkc.constraint_column_id) AS ref_cols,
				fk.delete_referential_action_desc AS delete_action,
				fk.update_referential_action_desc AS update_action
			FROM sys.foreign_keys fk
			JOIN sys.foreign_key_columns fkc ON fk.object_id = fkc.constraint_object_id
			JOIN sys.columns pc ON fkc.parent_object_id = pc.object_id AND fkc.parent_column_id = pc.column_id
			JOIN sys.columns rc ON fkc.referenced_object_id = rc.object_id AND fkc.referenced_column_id = rc.column_id
			JOIN sys.tables t ON fk.parent_object_id = t.object_id
			JOIN sys.schemas s ON t.schema_id = s.schema_id
			WHERE s.name = @p1 AND t.name = @p2
			GROUP BY fk.name, fk.referenced_object_id, fk.delete_referential_action_desc, fk.update_referential_action_desc
			UNION ALL
			SELECT cc.name, 'CHECK' AS type, '' AS cols,
				NULL AS ref_table, NULL AS ref_cols, NULL AS delete_action, NULL AS update_action
			FROM sys.check_constraints cc
			JOIN sys.tables t ON cc.parent_object_id = t.object_id
			JOIN sys.schemas s ON t.schema_id = s.schema_id
			WHERE s.name = @p1 AND t.name = @p2
		) con ORDER BY con.name`, schema, table)
	if err == nil {
		defer conRows.Close()
		for conRows.Next() {
			var c TableConstraint
			var colsStr, refTableStr, refColsStr, delAction, updAction sql.NullString
			if err := conRows.Scan(&c.Name, &c.Type, &colsStr, &refTableStr, &refColsStr, &delAction, &updAction); err != nil {
				continue
			}
			if colsStr.Valid && colsStr.String != "" {
				c.Columns = strings.Split(colsStr.String, ",")
			}
			if refTableStr.Valid && refTableStr.String != "" {
				c.RefTable = refTableStr.String
			}
			if refColsStr.Valid && refColsStr.String != "" {
				c.RefColumns = strings.Split(refColsStr.String, ",")
			}
			if delAction.Valid && delAction.String != "" {
				c.OnDelete = delAction.String
			}
			if updAction.Valid && updAction.String != "" {
				c.OnUpdate = updAction.String
			}
			result.Constraints = append(result.Constraints, c)
		}
	}
	if result.Constraints == nil {
		result.Constraints = make([]TableConstraint, 0)
	}

	// Table meta
	var m TableMeta
	d.db.QueryRow(`
		SELECT COALESCE(CAST(ep.value AS NVARCHAR(MAX)), ''),
			COALESCE((SELECT SUM(rows) FROM sys.partitions WHERE object_id = t.object_id AND index_id IN (0,1)), 0),
			COALESCE(CONVERT(VARCHAR, t.create_date, 120), ''),
			COALESCE(CONVERT(VARCHAR, t.modify_date, 120), '')
		FROM sys.tables t
		JOIN sys.schemas s ON t.schema_id = s.schema_id
		LEFT JOIN sys.extended_properties ep
			ON ep.major_id = t.object_id AND ep.minor_id = 0
			AND ep.class = 1 AND ep.name = 'MS_Description'
		WHERE s.name = @p1 AND t.name = @p2`, schema, table).Scan(&m.Comment, &m.RowCount, &m.CreateTime, &m.UpdateTime)
	result.TableMeta = m

	return result, nil
}

// buildFullDDL generates a CREATE TABLE DDL statement for SQL Server
func (d *SQLServerDriver) buildFullDDL(schema, table string) string {
	rows, err := d.db.Query(`
		SELECT c.COLUMN_NAME, c.DATA_TYPE, c.CHARACTER_MAXIMUM_LENGTH,
			c.NUMERIC_PRECISION, c.NUMERIC_SCALE, c.IS_NULLABLE, c.COLUMN_DEFAULT,
			COALESCE(CAST(ep.value AS NVARCHAR(MAX)), '')
		FROM INFORMATION_SCHEMA.COLUMNS c
		LEFT JOIN sys.extended_properties ep
			ON ep.major_id = OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA) + '.' + QUOTENAME(c.TABLE_NAME))
			AND ep.minor_id = c.ORDINAL_POSITION AND ep.class = 1 AND ep.name = 'MS_Description'
		WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
		ORDER BY c.ORDINAL_POSITION`, schema, table)
	if err != nil {
		return ""
	}
	defer rows.Close()

	type ddlCol struct {
		name         string
		dataType     string
		charMaxLen   sql.NullInt64
		numPrecision sql.NullInt64
		numScale     sql.NullInt64
		isNullable   string
		colDefault   sql.NullString
		colComment   string
	}
	var columns []ddlCol
	for rows.Next() {
		var c ddlCol
		if err := rows.Scan(&c.name, &c.dataType, &c.charMaxLen, &c.numPrecision, &c.numScale, &c.isNullable, &c.colDefault, &c.colComment); err != nil {
			continue
		}
		columns = append(columns, c)
	}
	if len(columns) == 0 {
		return ""
	}

	pkRows, err := d.db.Query(`
		SELECT ku.COLUMN_NAME
		FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
		JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku
			ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME AND tc.TABLE_SCHEMA = ku.TABLE_SCHEMA
		WHERE tc.TABLE_SCHEMA = @p1 AND tc.TABLE_NAME = @p2
			AND tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
		ORDER BY ku.ORDINAL_POSITION`, schema, table)
	var pkCols []string
	if err == nil {
		defer pkRows.Close()
		for pkRows.Next() {
			var colName string
			if pkRows.Scan(&colName) == nil {
				pkCols = append(pkCols, colName)
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("CREATE TABLE [%s].[%s] (\n", schema, table))
	for i, col := range columns {
		sb.WriteString("    ")
		dt := SqlServerFullType(col.dataType, col.charMaxLen, col.numPrecision, col.numScale)
		sb.WriteString(fmt.Sprintf("[%s] %s", col.name, dt))
		if col.isNullable == "NO" {
			sb.WriteString(" NOT NULL")
		}
		if col.colDefault.Valid && col.colDefault.String != "" {
			def := strings.TrimSpace(col.colDefault.String)
			if len(def) >= 4 && strings.HasPrefix(def, "((") && strings.HasSuffix(def, "))") {
				def = def[2 : len(def)-2]
			}
			sb.WriteString(" DEFAULT " + def)
		}
		if i < len(columns)-1 || len(pkCols) > 0 {
			sb.WriteString(",")
		}
		sb.WriteString("\n")
	}
	if len(pkCols) > 0 {
		sb.WriteString("    PRIMARY KEY (")
		for i, pk := range pkCols {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(fmt.Sprintf("[%s]", pk))
		}
		sb.WriteString(")\n")
	}
	sb.WriteString(");")

	// Add column comments via sp_addextendedproperty
	for _, col := range columns {
		if col.colComment != "" {
			safeComment := strings.ReplaceAll(col.colComment, "'", "''")
			sb.WriteString(fmt.Sprintf("\nEXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s', @level2type=N'COLUMN', @level2name=N'%s';", safeComment, schema, table, col.name))
		}
	}

	// Add table comment
	var tableComment string
	d.db.QueryRow(`SELECT COALESCE(CAST(ep.value AS NVARCHAR(MAX)), '') FROM sys.extended_properties ep
		WHERE ep.major_id = OBJECT_ID(QUOTENAME(@p1) + '.' + QUOTENAME(@p2))
		AND ep.minor_id = 0 AND ep.class = 1 AND ep.name = 'MS_Description'`,
		schema, table).Scan(&tableComment)
	if tableComment != "" {
		safeComment := strings.ReplaceAll(tableComment, "'", "''")
		sb.WriteString(fmt.Sprintf("\nEXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s';", safeComment, schema, table))
	}

	sb.WriteString("\n")
	return sb.String()
}

// SqlServerFullType formats a SQL Server data type with length/precision/scale.
func SqlServerFullType(dataType string, charMaxLen, numPrecision, numScale sql.NullInt64) string {
	dt := strings.ToUpper(dataType)
	switch dt {
	case "VARCHAR", "NVARCHAR", "CHAR", "NCHAR", "VARBINARY", "BINARY":
		if charMaxLen.Valid && charMaxLen.Int64 > 0 {
			if charMaxLen.Int64 == -1 {
				return fmt.Sprintf("%s(MAX)", dt)
			}
			return fmt.Sprintf("%s(%d)", dt, charMaxLen.Int64)
		}
		return dt
	case "DECIMAL", "NUMERIC":
		p := int64(18)
		s := int64(0)
		if numPrecision.Valid {
			p = numPrecision.Int64
		}
		if numScale.Valid {
			s = numScale.Int64
		}
		return fmt.Sprintf("%s(%d,%d)", dt, p, s)
	case "FLOAT":
		if numPrecision.Valid && numPrecision.Int64 > 0 {
			return fmt.Sprintf("%s(%d)", dt, numPrecision.Int64)
		}
		return dt
	case "DATETIME2", "DATETIMEOFFSET", "TIME":
		if numScale.Valid && numScale.Int64 >= 0 {
			return fmt.Sprintf("%s(%d)", dt, numScale.Int64)
		}
		return dt
	default:
		return dt
	}
}

func (d *SQLServerDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) {
	row := d.db.QueryRow(`SELECT
		p.rows,
		COALESCE(ep.value, ''),
		o.create_date,
		o.modify_date
	FROM sys.objects o
	JOIN sys.schemas s ON o.schema_id = s.schema_id
	LEFT JOIN sys.partitions p ON o.object_id = p.object_id AND p.index_id IN (0, 1)
	LEFT JOIN sys.extended_properties ep ON o.object_id = ep.major_id AND ep.name = 'MS_Description'
	WHERE s.name = @p1 AND o.name = @p2 AND o.type = 'U'`, schema, table)
	var rowCount sql.NullInt64
	var comment, createTime, updateTime sql.NullString
	if err := row.Scan(&rowCount, &comment, &createTime, &updateTime); err != nil {
		return map[string]interface{}{}, err
	}
	return map[string]interface{}{
		"row_count":   rowCount.Int64,
		"comment":     comment.String,
		"create_time": createTime.String,
		"update_time": updateTime.String,
	}, nil
}

// ─── Cross-Database DDL Helpers ────────────────────────────────────────────

func (d *SQLServerDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string {
	var colDefs []string
	for _, col := range columns {
		name, _ := col["name"].(string)
		colType, _ := col["type"].(string)
		charLen := ""
		if v, ok := col["length"]; ok {
			charLen = fmt.Sprintf("%v", v)
		}
		targetType := ConvertDDLType(sourceDBType, "sqlserver", colType, charLen, colType)
		def := fmt.Sprintf("  [%s] %s", name, targetType)

		if nullable, ok := col["nullable"]; ok {
			if nullableStr, ok := nullable.(string); ok && nullableStr == "NO" {
				def += " NOT NULL"
			}
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf("CREATE TABLE [%s].[%s] (\n%s\n);", schema, table, strings.Join(colDefs, ",\n"))
}

func (d *SQLServerDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string {
	safeComment := strings.ReplaceAll(comment, "'", "''")
	return fmt.Sprintf(
		"EXEC('IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID(''%s.%s'') AND minor_id = 0 AND name = ''MS_Description'') "+
			"EXEC sys.sp_dropextendedproperty @name=N''MS_Description'', @level0type=N''SCHEMA'', @level0name=N''%s'', @level1type=N''TABLE'', @level1name=N''%s''; "+
			"EXEC sys.sp_addextendedproperty @name=N''MS_Description'', @value=N''%s'', @level0type=N''SCHEMA'', @level0name=N''%s'', @level1type=N''TABLE'', @level1name=N''%s''')",
		schema, table, schema, table, safeComment, schema, table)
}

// BuildSQLServerColumnComment generates extended property statements for a column comment.
// Uses EXEC to avoid SQL Server 'CREATE/ALTER PROCEDURE must be the first statement' error.
// Drops existing MS_Description if present, then adds the new one.
// tbl is a qualified table name like [schema].[table].
func BuildSQLServerColumnComment(tbl, colName, comment string) string {
	// Parse database.schema.table from [database].[schema].[table] or [schema].[table]
	schema := "dbo"
	table := tbl
	clean := strings.ReplaceAll(strings.ReplaceAll(tbl, "[", ""), "]", "")
	parts := strings.SplitN(clean, ".", 3)
	if len(parts) == 3 {
		// [database].[schema].[table]
		schema = parts[1]
		table = parts[2]
	} else if len(parts) == 2 {
		schema = parts[0]
		table = parts[1]
	}
	safeComment := strings.ReplaceAll(comment, "'", "''")
	// DROP existing + ADD new to handle both new and update cases
	return fmt.Sprintf(
		"EXEC('IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID(''%s.%s'') AND minor_id = COLUMNPROPERTY(OBJECT_ID(''%s.%s''), ''%s'', ''ColumnId'') AND name = ''MS_Description'') "+
			"EXEC sys.sp_dropextendedproperty @name=N''MS_Description'', @level0type=N''SCHEMA'', @level0name=N''%s'', @level1type=N''TABLE'', @level1name=N''%s'', @level2type=N''COLUMN'', @level2name=N''%s''; "+
			"EXEC sys.sp_addextendedproperty @name=N''MS_Description'', @value=N''%s'', @level0type=N''SCHEMA'', @level0name=N''%s'', @level1type=N''TABLE'', @level1name=N''%s'', @level2type=N''COLUMN'', @level2name=N''%s''')",
		schema, table, schema, table, colName, schema, table, colName, safeComment, schema, table, colName)
}

func (d *SQLServerDriver) FormatSQLValue(val interface{}) string {
	if val == nil {
		return "NULL"
	}
	switch v := val.(type) {
	case string:
		// 使用 N 前缀，确保中文等非 ASCII 字符按 Unicode(NVARCHAR) 处理，
		// 否则会被数据库默认排序规则(如 Latin1_General_CP1)按单字节码页转换而变成 "?"。
		return "N'" + strings.ReplaceAll(v, "'", "''") + "'"
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

func (d *SQLServerDriver) AlterColumnClause(columnName, colType, columnDef string) string {
	return fmt.Sprintf("ALTER COLUMN [%s] %s", columnName, columnDef)
}

func (d *SQLServerDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;",
		tableName, colList, strings.Join(rowValues, ",\n"))
}

func (d *SQLServerDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string {
	safeSource := strings.ReplaceAll(sourceSchema, "]", "]]")
	safeTarget := strings.ReplaceAll(targetSchema, "]", "]]")
	ddl = strings.ReplaceAll(ddl, "["+safeSource+"]", "["+safeTarget+"]")
	return ddl
}

func (d *SQLServerDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) {
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return nil, err
	}
	var result []AlterColumn
	for _, c := range cols {
		result = append(result, AlterColumn{
			Name:       c.Name,
			Type:       c.Type,
			Nullable:   c.Nullable == "YES",
			DefaultVal: c.Default,
		})
	}
	if result == nil {
		result = make([]AlterColumn, 0)
	}
	return result, nil
}

// GetColumnTypes returns SQL Server-specific column type definitions
func (d *SQLServerDriver) GetColumnTypes() []ColumnTypeInfo {
	return []ColumnTypeInfo{
		{Name: "int", NeedsLength: false, Description: "整数"},
		{Name: "bigint", NeedsLength: false, Description: "大整数"},
		{Name: "smallint", NeedsLength: false, Description: "小整数"},
		{Name: "tinyint", NeedsLength: false, Description: "微小整数"},
		{Name: "bit", NeedsLength: false, Description: "位/布尔"},
		{Name: "decimal", NeedsLength: true, NeedsScale: true, Description: "定点数"},
		{Name: "numeric", NeedsLength: true, NeedsScale: true, Description: "数值"},
		{Name: "float", NeedsLength: false, Description: "浮点"},
		{Name: "real", NeedsLength: false, Description: "单精度"},
		{Name: "money", NeedsLength: false, Description: "货币"},
		{Name: "smallmoney", NeedsLength: false, Description: "小额货币"},
		{Name: "varchar", NeedsLength: true, Description: "变长字符"},
		{Name: "nvarchar", NeedsLength: true, Description: "Unicode变长字符"},
		{Name: "char", NeedsLength: true, Description: "定长字符"},
		{Name: "nchar", NeedsLength: true, Description: "Unicode定长字符"},
		{Name: "text", NeedsLength: false, Description: "文本(已弃用)"},
		{Name: "ntext", NeedsLength: false, Description: "Unicode文本(已弃用)"},
		{Name: "varbinary", NeedsLength: true, Description: "变长二进制"},
		{Name: "binary", NeedsLength: true, Description: "定长二进制"},
		{Name: "image", NeedsLength: false, Description: "大二进制(已弃用)"},
		{Name: "datetime", NeedsLength: false, Description: "日期时间"},
		{Name: "datetime2", NeedsLength: false, Description: "高精度日期时间"},
		{Name: "smalldatetime", NeedsLength: false, Description: "低精度日期时间"},
		{Name: "date", NeedsLength: false, Description: "日期"},
		{Name: "time", NeedsLength: false, Description: "时间"},
		{Name: "datetimeoffset", NeedsLength: false, Description: "日期时间(带时区偏移)"},
		{Name: "uniqueidentifier", NeedsLength: false, Description: "GUID"},
		{Name: "xml", NeedsLength: false, Description: "XML"},
	}
}

func (d *SQLServerDriver) GetIndexTypes() []IndexTypeInfo {
	return []IndexTypeInfo{
		{Name: "INDEX", Description: "非聚集索引 (Non-Clustered)"},
		{Name: "UNIQUE", Description: "唯一非聚集索引"},
		{Name: "CLUSTERED", Description: "聚集索引"},
		{Name: "UNIQUE CLUSTERED", Description: "唯一聚集索引"},
		{Name: "XML", Description: "XML 索引"},
		{Name: "SPATIAL", Description: "空间索引"},
	}
}

// ─── Tree Metadata ─────────────────────────────────────────────────────────

func (d *SQLServerDriver) GetTreeMetadata() TreeMetadata {
	return TreeMetadata{
		DBType: "sqlserver",
		Levels: []TreeLevel{
			{Key: "server", Label: "Server", LabelKey: "tree.server", Icon: "CloudServerOutlined"},
			{Key: "database", Label: "Database", LabelKey: "tree.database", Icon: "DatabaseOutlined"},
			{Key: "schema", Label: "Schema", LabelKey: "tree.schema", PlaceholderKey: "tree.schema_name_hint", Icon: "ClusterOutlined"},
			{Key: "tables_folder", Label: "Tables", LabelKey: "tree.tables", Icon: "TableOutlined"},
			{Key: "views_folder", Label: "Views", LabelKey: "tree.views", Icon: "EyeOutlined"},
		},
		AllowCreate: map[string]bool{"database": true, "schema": true},
		SystemFilter: &SystemFilter{
			ExcludeNames: []string{"sys", "INFORMATION_SCHEMA"},
			ExcludePrefixes: []string{"db_"},
		},
	}
}

func (d *SQLServerDriver) ListDatabases() ([]DatabaseInfo, error) {
    query := `SELECT d.name, d.collation_name,
        CAST(SUM(CAST(f.size AS BIGINT)*8/1024) AS BIGINT) AS size_mb,
        (SELECT COUNT(*) FROM sys.tables t JOIN sys.schemas s ON t.schema_id=s.schema_id WHERE s.name='dbo')
    FROM sys.databases d
    JOIN sys.master_files f ON d.database_id=f.database_id AND f.file_id=1
    WHERE d.state=0 AND d.name NOT IN ('master','tempdb','model','msdb')
    GROUP BY d.name, d.collation_name ORDER BY d.name`
    rows, err := d.db.Query(query)
    if err != nil {
        return nil, err
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

func (d *SQLServerDriver) ResolveContext(arg string) DatabaseContext {
	return DatabaseContext{Schema: arg}
}

func (d *SQLServerDriver) ListDatabaseSchemas(database string) ([]string, error) {
	// Create a new connection to the target database
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database %s: %w", database, err)
	}
	defer db2.Close()

	if err := db2.Ping(); err != nil {
		return nil, fmt.Errorf("ping database %s: %w", database, err)
	}

	rows, err := db2.Query(`SELECT name FROM sys.schemas 
		WHERE name NOT IN ('sys', 'INFORMATION_SCHEMA', 'db_owner', 'db_accessadmin', 'db_securityadmin', 'db_ddladmin', 'db_backupoperator', 'db_datareader', 'db_datawriter', 'db_denydatareader', 'db_denydatawriter')
		ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			names = append(names, n)
		}
	}
	if names == nil {
		names = []string{}
	}
	return names, nil
}

// ─── DDL Builder stubs ────────────────────────────────────────────

func (d *SQLServerDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) {
	nullable := col.Nullable == nil || *col.Nullable
	defaultVal := ""
	if col.HasDef != nil && *col.HasDef { defaultVal = col.Default }
	parts := d.AddColumnDDL(table, col.Name, col.Type, col.Length, nullable, defaultVal, col.Comment, col.After)
	return strings.Join(parts, ";\n"), "", nil, false, nil
}
func (d *SQLServerDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	return BuildMSSQLModifyColumn(table, col, orig)
}

// BuildMSSQLModifyColumn generates ALTER COLUMN DDL for SQL Server.
func BuildMSSQLModifyColumn(tbl string, ch AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
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

	colModified := ch.Type != "" || ch.Length != "" || ch.Nullable != nil
	if colModified {
		colDef := quoteMSSQL(ch.Name) + " " + ch.Type
		if ch.Length != "" && ch.Length != "0" {
			colDef = quoteMSSQL(ch.Name) + " " + ch.Type + "(" + ch.Length + ")"
		}
		if ch.Nullable != nil {
			if *ch.Nullable { colDef += " NULL" } else { colDef += " NOT NULL" }
		}
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", tbl, colDef))
		origDef := quoteMSSQL(orig.Name) + " " + orig.Type
		if !orig.Nullable { origDef += " NOT NULL" }
		rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", tbl, origDef))
	}

	if ch.HasDef != nil && *ch.HasDef {
		stmts = append(stmts, fmt.Sprintf(
			"DECLARE @cn NVARCHAR(200); "+
				"SELECT @cn = name FROM sys.default_constraints "+
				"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
				"IF @cn IS NOT NULL EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn)",
			tbl, tbl, ch.Name, tbl))
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD DEFAULT %s FOR %s", tbl, formatDefault(ch.Default), quoteMSSQL(ch.Name)))
	} else if ch.HasDef != nil && !*ch.HasDef && orig.HasDef {
		stmts = append(stmts, fmt.Sprintf(
			"DECLARE @cn NVARCHAR(200); "+
				"SELECT @cn = name FROM sys.default_constraints "+
				"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
				"EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn)",
			tbl, tbl, ch.Name, tbl))
	}
	if ch.Comment != "" {
		stmts = append(stmts, BuildSQLServerColumnComment(tbl, ch.Name, ch.Comment))
	}
	if orig.Comment != "" {
		rollbacks = append(rollbacks, BuildSQLServerColumnComment(tbl, ch.Name, orig.Comment))
	}

	return stmts, rollbacks, warnings, highRisk, nil
}

// GetServerInfo returns SQL Server info
func (d *SQLServerDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT @@VERSION").Scan(&version); err == nil {
		info["version"] = version
	}
	return info, nil
}

// GetMetrics returns SQL Server current metrics
func (d *SQLServerDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	queries := map[string]string{
		"当前连接数":      "SELECT COUNT(*) FROM sys.dm_exec_connections",
		"活跃请求":       "SELECT COUNT(*) FROM sys.dm_exec_requests WHERE status NOT IN ('background','sleeping')",
		"阻塞进程":       "SELECT COUNT(*) FROM sys.dm_exec_requests WHERE blocking_session_id > 0",
		"缓冲命中率":     "SELECT ROUND(CAST(CAST(cntr_value AS BIGINT)*100.0/(SELECT CAST(cntr_value AS BIGINT) FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Buffer Manager%' AND counter_name='Buffer cache hit ratio base') AS DECIMAL(18,2)), 2) FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Buffer Manager%' AND counter_name='Buffer cache hit ratio'",
		"最大内存(MB)":   "SELECT CAST(value_in_use AS VARCHAR) FROM sys.configurations WHERE name='max server memory (MB)'",
	}
	for name, sql := range queries {
		var v string
		if err := d.db.QueryRowContext(ctx, sql).Scan(&v); err == nil {
			m[name] = v
		}
	}
	return m, nil
}

func (d *SQLServerDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) {
	return d.DropColumnDDL(table, colName), "", nil, true, nil
}
func (d *SQLServerDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) {
	return d.AddIndexDDL(table, idx.Name, idx.Type, idx.Columns), "", nil
}
func (d *SQLServerDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) {
	return d.DropIndexDDL(table, schema, idxName), "", nil, true, nil
}
func (d *SQLServerDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) {
	return "", "", fmt.Errorf("not supported")
}
func (d *SQLServerDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *SQLServerDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *SQLServerDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) {
	return d.SetTableCommentDDL(table, "", "", newComment), "", nil
}

// ListProcesses returns current process list
func (d *SQLServerDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, processQueries[dbType])
}

// ListUsers returns user list
func (d *SQLServerDriver) ListUsers(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, userQueries[dbType])
}

// ListTablespaces returns tablespace info
func (d *SQLServerDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, tablespaceQueries[dbType])
}

func (d *SQLServerDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) {
	start := time.Now()
	m := &ServerMetricsV2{DBType: "sqlserver", CollectedAt: time.Now(), DatabaseSpecific: make(map[string]interface{})}

	// Connections
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_exec_connections").Scan(&m.Connections.Total)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_exec_requests WHERE status='running'").Scan(&m.Connections.Active)
	m.Connections.Idle = m.Connections.Total - m.Connections.Active
	d.db.QueryRowContext(ctx, "SELECT maximum FROM sys.configurations WHERE name='user connections'").Scan(&m.Connections.MaxConnections)
	if m.Connections.MaxConnections > 0 {
		m.Connections.UsagePercent = float64(m.Connections.Total) * 100.0 / float64(m.Connections.MaxConnections)
	}

	// Throughput
	var batchReqs float64
	d.db.QueryRowContext(ctx, "SELECT cntr_value FROM sys.dm_os_performance_counters WHERE object_name LIKE '%SQL Statistics%' AND counter_name='Batch Requests/sec'").Scan(&batchReqs)
	m.Throughput.QPS = batchReqs

	// Buffer cache (PLE)
	var ple int64
	d.db.QueryRowContext(ctx, "SELECT cntr_value FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Buffer Manager%' AND counter_name='Page life expectancy'").Scan(&ple)
	m.DatabaseSpecific["page_life_expectancy_sec"] = ple
	var hitRate float64
	d.db.QueryRowContext(ctx, "SELECT cntr_value*100.0/(SELECT cntr_value FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Buffer Manager%' AND counter_name='Buffer cache hit ratio base') FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Buffer Manager%' AND counter_name='Buffer cache hit ratio'").Scan(&hitRate)
	m.BufferCache.HitRate = hitRate

	// Locks
	d.db.QueryRowContext(ctx, "SELECT cntr_value FROM sys.dm_os_performance_counters WHERE object_name LIKE '%Locks%' AND counter_name='Number of Deadlocks/sec' AND instance_name='_Total'").Scan(&m.Locks.Deadlocks)
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_exec_requests WHERE blocking_session_id > 0").Scan(&m.Locks.BlockedSessions)
	var lockWaits int
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_tran_locks WHERE request_status='WAIT'").Scan(&lockWaits)
	m.Locks.LockWaits = lockWaits

	// Long transactions
	var longTx int
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_tran_active_transactions t JOIN sys.dm_tran_session_transactions s ON t.transaction_id=s.transaction_id WHERE DATEDIFF(SECOND, s.enlist_date, GETDATE()) > 60").Scan(&longTx)
	m.Locks.LongTransactions = longTx

	// Storage — database files
	dbRows, _ := d.db.QueryContext(ctx, "SELECT d.name, CAST(SUM(CAST(f.size AS BIGINT)*8)/1024 AS BIGINT) FROM sys.databases d JOIN sys.master_files f ON d.database_id=f.database_id WHERE d.state=0 AND d.name NOT IN ('master','tempdb','model','msdb') GROUP BY d.name")
	if dbRows != nil {
		defer dbRows.Close()
		for dbRows.Next() {
			var ts TablespaceMetric
			if dbRows.Scan(&ts.Name, &ts.SizeMB) == nil { m.Storage.Tablespaces = append(m.Storage.Tablespaces, ts) }
		}
	}

	// DB-specific: waiting tasks, memory
	var waitingTasks int
	d.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sys.dm_os_waiting_tasks WHERE wait_type NOT IN ('BROKER_EVENTHANDLER','BROKER_RECEIVE_WAITFOR','BROKER_TASK_STOP','BROKER_TO_FLUSH','BROKER_TRANSMITTER','CHECKPOINT_QUEUE','KSOURCE_WAKEUP','LAZYWRITER_SLEEP','LOGMGR_QUEUE','MEMORY_ALLOCATION_EXT','ONDEMAND_TASK_QUEUE','PREEMPTIVE_OS_LIBRARYOPS','REQUEST_FOR_DEADLOCK_SEARCH','SLEEP_TASK','SQLTRACE_BUFFER_FLUSH','XE_DISPATCHER_WAIT','XE_TIMER_EVENT')").Scan(&waitingTasks)
	m.DatabaseSpecific["waiting_tasks"] = waitingTasks

	var maxMem int64
	d.db.QueryRowContext(ctx, "SELECT value_in_use FROM sys.configurations WHERE name='max server memory (MB)'").Scan(&maxMem)
	m.DatabaseSpecific["max_server_memory_mb"] = maxMem

	m.CostMs = time.Since(start).Milliseconds()
	return m, nil
}

func (d *SQLServerDriver) CreateDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE DATABASE [%s]", name))
	return err
}
func (d *SQLServerDriver) DropDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER DATABASE [%s] SET SINGLE_USER WITH ROLLBACK IMMEDIATE; DROP DATABASE [%s]", name, name))
	return err
}
func (d *SQLServerDriver) CreateUser(username, password string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE LOGIN [%s] WITH PASSWORD='%s', CHECK_POLICY=OFF, CHECK_EXPIRATION=OFF; CREATE USER [%s] FOR LOGIN [%s]", username, password, username, username))
	return err
}
func (d *SQLServerDriver) DropUser(username string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP USER [%s]; DROP LOGIN [%s]", username, username))
	return err
}
func (d *SQLServerDriver) GrantPrivileges(username, database string, privileges []string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER ROLE db_owner ADD MEMBER [%s]", username))
	return err
}

func (d *SQLServerDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error) {
	// Check if sysadmin
	var isSysadmin int
	d.db.QueryRow("SELECT COUNT(*) FROM sys.server_principals p JOIN sys.server_role_members m ON p.principal_id=m.member_principal_id JOIN sys.server_principals r ON m.role_principal_id=r.principal_id WHERE p.name=? AND r.name='sysadmin'", username).Scan(&isSysadmin)
	var result []PrivilegeEntry
	if isSysadmin > 0 || strings.ToUpper(username) == "SA" {
		dbRows, _ := d.db.Query("SELECT name FROM sys.databases WHERE state=0 AND name NOT IN ('master','tempdb','model','msdb')")
		if dbRows != nil {
			defer dbRows.Close()
			for dbRows.Next() {
				var dbName string
				if dbRows.Scan(&dbName) == nil {
					result = append(result, PrivilegeEntry{Database: dbName, ObjectType: "DATABASE", ObjectName: "*", Privileges: []string{"ALL"}, Grantable: true, IsSystem: true})
				}
			}
		}
		if result == nil { result = []PrivilegeEntry{} }
		return result, nil
	}
	rows, err := d.db.Query("SELECT DB_NAME(), CASE WHEN class=0 THEN 'DATABASE' ELSE 'TABLE' END, COALESCE(OBJECT_NAME(major_id),'*'), permission_name, state_desc FROM sys.database_permissions WHERE USER_NAME(grantee_principal_id)=?", username)
	if err != nil { return []PrivilegeEntry{}, nil }
	defer rows.Close()
	privMap := make(map[string]*PrivilegeEntry)
	for rows.Next() {
		var db, objType, obj, priv, state string
		if rows.Scan(&db, &objType, &obj, &priv, &state) == nil {
			key := db + "." + objType + "." + obj
			if _, ok := privMap[key]; !ok { privMap[key] = &PrivilegeEntry{Database: db, ObjectType: objType, ObjectName: obj} }
			if state == "DENY" { privMap[key].IsSystem = true }
			privMap[key].Privileges = append(privMap[key].Privileges, priv)
		}
	}
	for _, v := range privMap { result = append(result, *v) }
	if result == nil { result = []PrivilegeEntry{} }
	return result, nil
}
func (d *SQLServerDriver) GetUserRoles(username string) ([]string, error) {
	var roles []string
	// Database-level roles
	rows, err := d.db.Query("SELECT r.name FROM sys.database_role_members m JOIN sys.database_principals u ON m.member_principal_id=u.principal_id JOIN sys.database_principals r ON m.role_principal_id=r.principal_id WHERE u.name=?", username)
	if err == nil && rows != nil {
		defer rows.Close()
		for rows.Next() { var r string; if rows.Scan(&r) == nil { roles = append(roles, r) } }
	}
	// Server-level roles
	srvRows, err := d.db.Query("SELECT r.name FROM sys.server_role_members m JOIN sys.server_principals u ON m.member_principal_id=u.principal_id JOIN sys.server_principals r ON m.role_principal_id=r.principal_id WHERE u.name=?", username)
	if err == nil && srvRows != nil {
		defer srvRows.Close()
		for srvRows.Next() { var r string; if srvRows.Scan(&r) == nil { roles = append(roles, r) } }
	}
	if roles == nil { roles = []string{} }
	return roles, nil
}
func (d *SQLServerDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) {
	result := &ChangeResult{}
	for _, ch := range changes {
		var obj string
		switch {
		case ch.ObjectType == "DATABASE" || ch.ObjectName == "*" || ch.ObjectName == "":
			// Database-level permission — executed within the current DB context
			obj = ""
		case ch.ObjectType == "SCHEMA":
			obj = "SCHEMA::[" + ch.Database + "]"
		default:
			// Table-level: [schema].[table] — Database field holds schema, ObjectName holds table
			obj = "[" + ch.Database + "].[" + ch.ObjectName + "]"
		}
		for _, p := range ch.Grant {
			if obj == "" {
				result.Statements = append(result.Statements, fmt.Sprintf("GRANT %s TO [%s]", p, username))
			} else {
				result.Statements = append(result.Statements, fmt.Sprintf("GRANT %s ON %s TO [%s]", p, obj, username))
			}
		}
		for _, p := range ch.Revoke {
			if obj == "" {
				result.Statements = append(result.Statements, fmt.Sprintf("REVOKE %s TO [%s]", p, username))
			} else {
				result.Statements = append(result.Statements, fmt.Sprintf("REVOKE %s ON %s FROM [%s]", p, obj, username))
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

func (d *SQLServerDriver) DetectCapability() (*CapabilitySet, error) {
	var v string
	d.db.QueryRow("SELECT @@VERSION").Scan(&v)
	return DetectCapability("sqlserver", v), nil
}
func (d *SQLServerDriver) ListRoles() ([]RoleInfo, error) {
	rows, err := d.db.Query("SELECT name, is_fixed_role FROM sys.database_principals WHERE type='R'")
	if err != nil { return []RoleInfo{}, nil }
	defer rows.Close()
	var result []RoleInfo
	roleMap := make(map[string]*RoleInfo)
	for rows.Next() { var n string; var isFixed bool; if rows.Scan(&n, &isFixed) == nil { roleMap[n] = &RoleInfo{Name: n, IsSystem: isFixed} } }
	// Populate members
	memRows, err := d.db.Query("SELECT r.name, m.name FROM sys.database_role_members rm JOIN sys.database_principals r ON rm.role_principal_id=r.principal_id JOIN sys.database_principals m ON rm.member_principal_id=m.principal_id")
	if err == nil && memRows != nil {
		defer memRows.Close()
		for memRows.Next() { var role, member string; if memRows.Scan(&role, &member) == nil { if r, ok := roleMap[role]; ok { r.Members = append(r.Members, member) } } }
	}
	for _, v := range roleMap { result = append(result, *v) }
	if result == nil { result = []RoleInfo{} }
	return result, nil
}
func (d *SQLServerDriver) CreateRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("CREATE ROLE [%s]", name))
	return err
}
func (d *SQLServerDriver) DropRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf("DROP ROLE [%s]", name))
	return err
}
// serverLevelRoles 固定的 8 个 SQL Server 服务器级角色
var serverLevelRoles = map[string]bool{
	"sysadmin": true, "securityadmin": true, "serveradmin": true,
	"setupadmin": true, "processadmin": true, "diskadmin": true,
	"dbcreator": true, "bulkadmin": true,
}

// isServerRole 判断是否为服务器级角色
func isServerRole(role string) bool {
	return serverLevelRoles[strings.ToLower(role)]
}

func (d *SQLServerDriver) AddRoleMember(role, member string) error {
	if isServerRole(role) {
		_, err := d.db.Exec(fmt.Sprintf("ALTER SERVER ROLE [%s] ADD MEMBER [%s]", role, member))
		return err
	}
	_, err := d.db.Exec(fmt.Sprintf("ALTER ROLE [%s] ADD MEMBER [%s]", role, member))
	return err
}

func (d *SQLServerDriver) RemoveRoleMember(role, member string) error {
	if isServerRole(role) {
		_, err := d.db.Exec(fmt.Sprintf("ALTER SERVER ROLE [%s] DROP MEMBER [%s]", role, member))
		return err
	}
	_, err := d.db.Exec(fmt.Sprintf("ALTER ROLE [%s] DROP MEMBER [%s]", role, member))
	return err
}
func (d *SQLServerDriver) AlterUserPassword(username, newPassword string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] WITH PASSWORD='%s'", username, newPassword))
	return err
}
func (d *SQLServerDriver) AlterUserLock(username string, lock bool) error {
	action := "ENABLE"
	if lock { action = "DISABLE" }
	_, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] %s", username, action))
	return err
}
func (d *SQLServerDriver) AlterUserRename(oldName, newName string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER [%s] WITH NAME = [%s]", oldName, newName))
	return err
}
func (d *SQLServerDriver) AlterUserDefaultSchema(username, schema string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER USER [%s] WITH DEFAULT_SCHEMA = [%s]", username, schema))
	return err
}

func (d *SQLServerDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error) {
	return d.GetUserPrivileges(roleName)
}

func (d *SQLServerDriver) AlterRoleAttribute(roleName, attribute, value string) error {
	return fmt.Errorf("AlterRoleAttribute not supported for this database")
}

func (d *SQLServerDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error) {
	return []ParentRoleInfo{}, nil
}

func (d *SQLServerDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error) {
	return []MemberRoleInfo{}, nil
}

func (d *SQLServerDriver) GetRoleInherit(roleName string) (bool, error) {
	return true, nil
}
func (d *SQLServerDriver) GetRoleMemberships(roleName string) ([]string, error) {
	return []string{}, nil
}

// ─── MSSQL Login Management ───────────────────────────────────────────────

// ListLogins 返回 SQL Server 实例级登录名列表
func (d *SQLServerDriver) ListLogins() ([]MSSQLLogin, error) {
	query := `SELECT
		sp.name,
		CASE sp.type WHEN 'S' THEN 'SQL Login' WHEN 'U' THEN 'Windows Login' WHEN 'G' THEN 'Windows Group' ELSE sp.type_desc END,
		sp.is_disabled,
		COALESCE(sp.default_database_name, 'master'),
		COALESCE(sp.default_language_name, 'us_english'),
		IS_SRVROLEMEMBER('sysadmin', sp.name),
		CONVERT(VARCHAR, sp.create_date, 120),
		CONVERT(VARCHAR, sp.modify_date, 120)
	FROM sys.server_principals sp
	WHERE sp.type IN ('S', 'U', 'G') AND sp.name NOT LIKE '##%' AND sp.name NOT IN ('sa')
	ORDER BY sp.name`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("ListLogins query failed: %w", err)
	}
	defer rows.Close()

	var logins []MSSQLLogin
	for rows.Next() {
		var l MSSQLLogin
		if err := rows.Scan(&l.Name, &l.Type, &l.IsDisabled, &l.DefaultDatabase,
			&l.DefaultLanguage, &l.IsSysadmin, &l.CreatedAt, &l.ModifiedAt); err != nil {
			continue
		}

		// 获取服务器角色
		l.ServerRoles, _ = d.getServerRolesForLogin(l.Name)

		// 获取有映射用户的数据库
		l.MappedDatabases, _ = d.getMappedDatabasesForLogin(l.Name)

		logins = append(logins, l)
	}

	// 单独查询 sa
	var saLogin MSSQLLogin
	err = d.db.QueryRow(`SELECT name, 'SQL Login', 0, COALESCE(default_database_name,'master'), COALESCE(default_language_name,'us_english'), 1, CONVERT(VARCHAR,create_date,120), CONVERT(VARCHAR,modify_date,120) FROM sys.server_principals WHERE name='sa'`).
		Scan(&saLogin.Name, &saLogin.Type, &saLogin.IsDisabled, &saLogin.DefaultDatabase, &saLogin.DefaultLanguage, &saLogin.IsSysadmin, &saLogin.CreatedAt, &saLogin.ModifiedAt)
	if err == nil {
		saLogin.ServerRoles = []string{"sysadmin"}
		saLogin.MappedDatabases, _ = d.getMappedDatabasesForLogin("sa")
		logins = append([]MSSQLLogin{saLogin}, logins...)
	}

	if logins == nil {
		logins = make([]MSSQLLogin, 0)
	}
	return logins, nil
}

func (d *SQLServerDriver) getServerRolesForLogin(loginName string) ([]string, error) {
	rows, err := d.db.Query(`SELECT r.name FROM sys.server_role_members m
		JOIN sys.server_principals u ON m.member_principal_id = u.principal_id
		JOIN sys.server_principals r ON m.role_principal_id = r.principal_id
		WHERE u.name = @p1`, loginName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []string
	for rows.Next() {
		var r string
		if rows.Scan(&r) == nil {
			roles = append(roles, r)
		}
	}
	if roles == nil {
		roles = make([]string, 0)
	}
	return roles, nil
}

func (d *SQLServerDriver) getMappedDatabasesForLogin(loginName string) ([]string, error) {
	// 遍历所有用户数据库查找该 Login 的映射
	dbRows, err := d.db.Query(`SELECT name FROM sys.databases WHERE state = 0 AND name NOT IN ('master','tempdb','model','msdb')`)
	if err != nil {
		return nil, err
	}
	defer dbRows.Close()

	var dbs []string
	var dbNames []string
	for dbRows.Next() {
		var name string
		if dbRows.Scan(&name) == nil {
			dbNames = append(dbNames, name)
		}
	}

	for _, dbName := range dbNames {
		newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
			d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, dbName)
		db2, err := sql.Open("sqlserver", newDsn)
		if err != nil {
			continue
		}
		var cnt int
		if db2.QueryRow(`SELECT COUNT(*) FROM sys.database_principals WHERE name = @p1 AND type IN ('S','U')`, loginName).Scan(&cnt) == nil && cnt > 0 {
			dbs = append(dbs, dbName)
		}
		db2.Close()
	}
	if dbs == nil {
		dbs = make([]string, 0)
	}
	return dbs, nil
}

// CreateLogin 创建 Login（含服务器角色分配 + 多库用户映射）
func (d *SQLServerDriver) CreateLogin(req CreateLoginRequest) error {
	if req.Name == "" {
		return fmt.Errorf("login name is required")
	}
	if strings.EqualFold(req.Name, "sa") {
		return fmt.Errorf("cannot create 'sa' login")
	}
	if req.LoginType == "SQL" && req.Password == "" {
		return fmt.Errorf("password is required for SQL login")
	}

	var ddl strings.Builder
	switch req.LoginType {
	case "Windows":
		ddl.WriteString(fmt.Sprintf("CREATE LOGIN [%s] FROM WINDOWS", req.Name))
	default:
		ddl.WriteString(fmt.Sprintf("CREATE LOGIN [%s] WITH PASSWORD = '%s'",
			req.Name, strings.ReplaceAll(req.Password, "'", "''")))
		if !req.EnforcePolicy {
			ddl.WriteString(", CHECK_POLICY = OFF, CHECK_EXPIRATION = OFF")
		}
	}
	// Windows 登录需在 FROM WINDOWS 后显式加 WITH
	hasOptions := req.DefaultDatabase != "" || req.DefaultLanguage != ""
	if hasOptions && req.LoginType == "Windows" {
		ddl.WriteString(" WITH")
	}
	firstOpt := true
	if req.DefaultDatabase != "" {
		prefix := ","
		if firstOpt && req.LoginType == "Windows" {
			prefix = ""
		}
		ddl.WriteString(fmt.Sprintf("%s DEFAULT_DATABASE = [%s]", prefix, req.DefaultDatabase))
		firstOpt = false
	}
	if req.DefaultLanguage != "" {
		prefix := ","
		if firstOpt && req.LoginType == "Windows" {
			prefix = ""
		}
		ddl.WriteString(fmt.Sprintf("%s DEFAULT_LANGUAGE = [%s]", prefix, req.DefaultLanguage))
	}

	if _, err := d.db.Exec(ddl.String()); err != nil {
		return fmt.Errorf("create login failed: %w", err)
	}

	for _, role := range req.ServerRoles {
		d.addServerRoleMember(req.Name, role)
	}

	for _, m := range req.DBUserMappings {
		if err := d.createDatabaseUserForLogin(req.Name, m); err != nil {
			return fmt.Errorf("create user mapping for %s in %s failed: %w", req.Name, m.Database, err)
		}
	}

	return nil
}

func (d *SQLServerDriver) addServerRoleMember(loginName, roleName string) error {
	_, err := d.db.Exec(fmt.Sprintf("ALTER SERVER ROLE [%s] ADD MEMBER [%s]", roleName, loginName))
	if err != nil {
		if isSyntaxError(err) {
			_, err = d.db.Exec(fmt.Sprintf("EXEC sp_addsrvrolemember @loginame = N'%s', @rolename = N'%s'", loginName, roleName))
		}
	}
	return err
}

func (d *SQLServerDriver) createDatabaseUserForLogin(loginName string, m DBUserMapping) error {
	userName := m.UserName
	if userName == "" {
		userName = loginName
	}
	schema := m.DefaultSchema
	if schema == "" {
		schema = "dbo"
	}

	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, m.Database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", m.Database, err)
	}
	defer db2.Close()

	if _, err := db2.Exec(fmt.Sprintf("CREATE USER [%s] FOR LOGIN [%s] WITH DEFAULT_SCHEMA = [%s]", userName, loginName, schema)); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	for _, role := range m.DatabaseRoles {
		db2.Exec(fmt.Sprintf("ALTER ROLE [%s] ADD MEMBER [%s]", role, userName))
	}

	return nil
}

// DropLogin 删除 Login（含前置检查与级联选项）
func (d *SQLServerDriver) DropLogin(loginName string, cascadeUsers bool) (*DropLoginResult, error) {
	result := &DropLoginResult{}

	if strings.EqualFold(loginName, "sa") {
		return nil, fmt.Errorf("cannot drop 'sa' login")
	}
	if strings.HasPrefix(loginName, "##") {
		return nil, fmt.Errorf("cannot drop system login: %s", loginName)
	}

	dbUsers, err := d.findLoginDatabaseUsers(loginName)
	if err != nil {
		return nil, fmt.Errorf("find database users failed: %w", err)
	}

	if len(dbUsers) > 0 && !cascadeUsers {
		result.CascadeUsers = false
		result.Warnings = append(result.Warnings, fmt.Sprintf("Login %s has %d mapped database users. Use cascadeUsers=true to drop them together.", loginName, len(dbUsers)))
		for _, u := range dbUsers {
			result.Warnings = append(result.Warnings, fmt.Sprintf("· [%s].[%s]", u.Database, u.UserName))
		}
		return result, nil
	}

	for _, u := range dbUsers {
		if u.HasSchemaOwnership {
			result.Warnings = append(result.Warnings, fmt.Sprintf("User [%s].[%s] owns schemas. Transfer ownership first with ALTER AUTHORIZATION.", u.Database, u.UserName))
		}
	}
	if len(result.Warnings) > 0 {
		return result, nil
	}

	for _, u := range dbUsers {
		newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
			d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, u.Database)
		db2, err := sql.Open("sqlserver", newDsn)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Cannot connect to %s: %v", u.Database, err))
			continue
		}
		if _, err := db2.Exec(fmt.Sprintf("DROP USER [%s]", u.UserName)); err != nil {
			db2.Close()
			result.Warnings = append(result.Warnings, fmt.Sprintf("Drop user [%s].[%s] failed: %v", u.Database, u.UserName, err))
			continue
		}
		result.DroppedUsers = append(result.DroppedUsers, fmt.Sprintf("%s.%s", u.Database, u.UserName))
		db2.Close()
	}

	if _, err := d.db.Exec(fmt.Sprintf("DROP LOGIN [%s]", loginName)); err != nil {
		return nil, fmt.Errorf("drop login failed: %w", err)
	}
	result.LoginDropped = true

	return result, nil
}

// DBUserInfoWithOwnership holds database user info with schema ownership flag
type DBUserInfoWithOwnership struct {
	Database          string
	UserName          string
	HasSchemaOwnership bool
}

func (d *SQLServerDriver) findLoginDatabaseUsers(loginName string) ([]DBUserInfoWithOwnership, error) {
	query := `SELECT DB_NAME(), name FROM sys.database_principals WHERE name = @p1 AND type IN ('S','U')`
	rows, err := d.db.Query(query, loginName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []DBUserInfoWithOwnership
	for rows.Next() {
		var db, name string
		if rows.Scan(&db, &name) == nil {
			users = append(users, DBUserInfoWithOwnership{Database: db, UserName: name})
		}
	}
	if users == nil {
		users = make([]DBUserInfoWithOwnership, 0)
	}
	return users, nil
}

// AlterLogin 修改 Login 属性
func (d *SQLServerDriver) AlterLogin(loginName string, req AlterLoginRequest) error {
	if req.NewPassword != nil && *req.NewPassword != "" {
		if _, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] WITH PASSWORD = '%s'", loginName, strings.ReplaceAll(*req.NewPassword, "'", "''"))); err != nil {
			return fmt.Errorf("alter login password failed: %w", err)
		}
	}
	if req.Disable != nil {
		action := "ENABLE"
		if *req.Disable {
			action = "DISABLE"
		}
		if _, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] %s", loginName, action)); err != nil {
			return fmt.Errorf("alter login enable/disable failed: %w", err)
		}
	}
	if req.Unlock != nil && *req.Unlock {
		if _, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] WITH CHECK_POLICY = OFF UNLOCK", loginName)); err != nil {
			return fmt.Errorf("unlock login failed: %w", err)
		}
	}
	if req.DefaultDatabase != nil && *req.DefaultDatabase != "" {
		if _, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] WITH DEFAULT_DATABASE = [%s]", loginName, *req.DefaultDatabase)); err != nil {
			return fmt.Errorf("alter login default database failed: %w", err)
		}
	}
	if req.RenameTo != nil && *req.RenameTo != "" {
		if _, err := d.db.Exec(fmt.Sprintf("ALTER LOGIN [%s] WITH NAME = [%s]", loginName, *req.RenameTo)); err != nil {
			return fmt.Errorf("rename login failed: %w", err)
		}
	}
	return nil
}

// GetLoginDetail 获取 Login 详情
func (d *SQLServerDriver) GetLoginDetail(loginName string) (*LoginDetail, error) {
	var l MSSQLLogin
	err := d.db.QueryRow(`SELECT name, CASE type WHEN 'S' THEN 'SQL Login' WHEN 'U' THEN 'Windows Login' ELSE type_desc END, is_disabled, COALESCE(default_database_name,'master'), COALESCE(default_language_name,'us_english'), IS_SRVROLEMEMBER('sysadmin', name), CONVERT(VARCHAR,create_date,120), CONVERT(VARCHAR,modify_date,120) FROM sys.server_principals WHERE name = @p1`, loginName).
		Scan(&l.Name, &l.Type, &l.IsDisabled, &l.DefaultDatabase, &l.DefaultLanguage, &l.IsSysadmin, &l.CreatedAt, &l.ModifiedAt)
	if err != nil {
		return nil, fmt.Errorf("login not found: %s", loginName)
	}

	l.ServerRoles, _ = d.getServerRolesForLogin(loginName)

	detail := &LoginDetail{Login: l}

	// 查询服务器角色详情
	roleRows, _ := d.db.Query(`SELECT name, is_fixed_role FROM sys.server_principals WHERE type = 'R' ORDER BY name`)
	if roleRows != nil {
		defer roleRows.Close()
		for roleRows.Next() {
			var sri ServerRoleInfo
			if roleRows.Scan(&sri.Name, &sri.IsFixedRole) == nil {
				detail.ServerRoles = append(detail.ServerRoles, sri)
			}
		}
	}
	if detail.ServerRoles == nil {
		detail.ServerRoles = make([]ServerRoleInfo, 0)
	}

	// 获取 Login 的 SID，用于后续孤用用户检测
	var loginSID string
	d.db.QueryRow(`SELECT CONVERT(VARCHAR(256), sid, 1) FROM sys.server_principals WHERE name = @p1`, loginName).Scan(&loginSID)

	// 遍历所有用户数据库，查询该 Login 的映射用户详情
	detail.DBUserMappings = make([]DBUserMappingDetail, 0)
	dbRows, dbErr := d.db.Query(`SELECT name FROM sys.databases WHERE state = 0 AND name NOT IN ('master','tempdb','model','msdb')`)
	if dbErr == nil && dbRows != nil {
		defer dbRows.Close()
		var dbNames []string
		for dbRows.Next() {
			var name string
			if dbRows.Scan(&name) == nil {
				dbNames = append(dbNames, name)
			}
		}

		for _, dbName := range dbNames {
			newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
				d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, dbName)
			db2, connErr := sql.Open("sqlserver", newDsn)
			if connErr != nil {
				continue
			}

			var userName, defaultSchema, userSID string
			userErr := db2.QueryRow(`SELECT name, COALESCE(default_schema_name, 'dbo'), CONVERT(VARCHAR(256), sid, 1) FROM sys.database_principals WHERE name = @p1 AND type IN ('S','U')`, loginName).
				Scan(&userName, &defaultSchema, &userSID)
			if userErr != nil {
				db2.Close()
				continue
			}

			mapping := DBUserMappingDetail{
				Database:      dbName,
				UserName:      userName,
				DefaultSchema: defaultSchema,
				IsOrphaned:    loginSID != "" && userSID != "" && !strings.EqualFold(loginSID, userSID),
			}

			// 查询数据库角色
			roleRows2, _ := db2.Query(`SELECT r.name FROM sys.database_role_members m JOIN sys.database_principals u ON m.member_principal_id = u.principal_id JOIN sys.database_principals r ON m.role_principal_id = r.principal_id WHERE u.name = @p1`, loginName)
			if roleRows2 != nil {
				for roleRows2.Next() {
					var roleName string
					if roleRows2.Scan(&roleName) == nil {
						mapping.DBRoles = append(mapping.DBRoles, roleName)
					}
				}
				roleRows2.Close()
			}
			if mapping.DBRoles == nil {
				mapping.DBRoles = make([]string, 0)
			}

			detail.DBUserMappings = append(detail.DBUserMappings, mapping)
			db2.Close()
		}

		// 更新 MappedDatabases
		for _, m := range detail.DBUserMappings {
			detail.Login.MappedDatabases = append(detail.Login.MappedDatabases, m.Database)
		}
	}

	return detail, nil
}

// ─── MSSQL Database User Management ───────────────────────────────────────

// ListDatabaseUsers 列出指定数据库的所有用户
func (d *SQLServerDriver) ListDatabaseUsers(database string) ([]MSSQLDatabaseUser, error) {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database %s: %w", database, err)
	}
	defer db2.Close()

	query := `SELECT dp.name, COALESCE(sp.name, ''), dp.type_desc, COALESCE(dp.default_schema_name, 'dbo'),
		CASE WHEN dp.name IN ('dbo','guest','INFORMATION_SCHEMA','sys') THEN 1 ELSE 0 END,
		CONVERT(VARCHAR, dp.create_date, 120)
	FROM sys.database_principals dp
	LEFT JOIN sys.server_principals sp ON dp.sid = sp.sid
	WHERE dp.type IN ('S','U','G') AND dp.name NOT LIKE '##%'
	ORDER BY dp.name`

	rows, err := db2.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []MSSQLDatabaseUser
	for rows.Next() {
		var u MSSQLDatabaseUser
		if err := rows.Scan(&u.Name, &u.LoginName, &u.Type, &u.DefaultSchema, &u.IsSystem, &u.CreatedAt); err != nil {
			continue
		}
		// 获取数据库角色
		roleRows, _ := db2.Query(`SELECT r.name FROM sys.database_role_members m JOIN sys.database_principals u ON m.member_principal_id=u.principal_id JOIN sys.database_principals r ON m.role_principal_id=r.principal_id WHERE u.name=@p1`, u.Name)
		if roleRows != nil {
			for roleRows.Next() {
				var role string
				if roleRows.Scan(&role) == nil {
					u.DatabaseRoles = append(u.DatabaseRoles, role)
				}
			}
			roleRows.Close()
		}
		if u.DatabaseRoles == nil {
			u.DatabaseRoles = make([]string, 0)
		}
		users = append(users, u)
	}
	if users == nil {
		users = make([]MSSQLDatabaseUser, 0)
	}
	return users, nil
}

// CreateDatabaseUser 在指定数据库中创建用户
func (d *SQLServerDriver) CreateDatabaseUser(database string, req CreateDBUserRequest) error {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	schema := req.DefaultSchema
	if schema == "" {
		schema = "dbo"
	}

	if req.LoginName != "" {
		if _, err := db2.Exec(fmt.Sprintf("CREATE USER [%s] FOR LOGIN [%s] WITH DEFAULT_SCHEMA = [%s]", req.UserName, req.LoginName, schema)); err != nil {
			return fmt.Errorf("create user: %w", err)
		}
	} else if req.IsContained {
		if _, err := db2.Exec(fmt.Sprintf("CREATE USER [%s] WITH PASSWORD = '%s', DEFAULT_SCHEMA = [%s]", req.UserName, strings.ReplaceAll(req.UserName, "'", "''"), schema)); err != nil {
			return fmt.Errorf("create contained user: %w", err)
		}
	} else {
		return fmt.Errorf("login_name or is_contained is required")
	}

	for _, role := range req.DatabaseRoles {
		db2.Exec(fmt.Sprintf("ALTER ROLE [%s] ADD MEMBER [%s]", role, req.UserName))
	}

	return nil
}

// DropDatabaseUser 从数据库删除用户
func (d *SQLServerDriver) DropDatabaseUser(database, userName string) error {
	if strings.EqualFold(userName, "dbo") {
		return fmt.Errorf("cannot drop 'dbo' user")
	}

	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	if _, err := db2.Exec(fmt.Sprintf("DROP USER [%s]", userName)); err != nil {
		return fmt.Errorf("drop user: %w", err)
	}

	return nil
}

// BatchCreateDatabaseUsers 批量在多个数据库中创建同一 Login 的映射用户
func (d *SQLServerDriver) BatchCreateDatabaseUsers(loginName string, mappings []DBUserMapping) error {
	var errs []string
	for _, m := range mappings {
		userName := m.UserName
		if userName == "" {
			userName = loginName
		}
		err := d.createDatabaseUserForLogin(loginName, m)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", m.Database, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("batch create users partially failed: %s", strings.Join(errs, "; "))
	}
	return nil
}

// ─── MSSQL Orphaned Users ─────────────────────────────────────────────────

// DetectOrphanedUsers 检测指定数据库中的孤用用户（含同名 Login 自动匹配）
func (d *SQLServerDriver) DetectOrphanedUsers(database string) ([]OrphanedUser, error) {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	query := `SELECT dp.name, DB_NAME(), CONVERT(VARCHAR(256), dp.sid, 1)
	FROM sys.database_principals dp
	WHERE dp.type IN ('S', 'U') AND dp.sid IS NOT NULL AND dp.sid <> 0x01
	  AND dp.name NOT IN ('dbo', 'guest', 'INFORMATION_SCHEMA', 'sys')
	  AND dp.authentication_type <> 2
	  AND NOT EXISTS (SELECT 1 FROM sys.server_principals sp WHERE sp.sid = dp.sid)`

	rows, err := db2.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var orphaned []OrphanedUser
	for rows.Next() {
		var o OrphanedUser
		if err := rows.Scan(&o.UserName, &o.Database, &o.UserSID); err != nil {
			continue
		}
		o.Database = database
		o.SuggestedFix = fmt.Sprintf("ALTER USER [%s] WITH LOGIN = [?]", o.UserName)
		orphaned = append(orphaned, o)
	}
	if orphaned == nil {
		orphaned = make([]OrphanedUser, 0)
		return orphaned, nil
	}

	// 同名 Login 自动匹配
	loginNames, err := d.listLoginNames()
	if err == nil {
		loginSet := make(map[string]bool, len(loginNames))
		for _, n := range loginNames {
			loginSet[strings.ToLower(n)] = true
		}
		for i := range orphaned {
			if loginSet[strings.ToLower(orphaned[i].UserName)] {
				orphaned[i].MatchedLogin = orphaned[i].UserName
				orphaned[i].SuggestedFix = fmt.Sprintf("ALTER USER [%s] WITH LOGIN = [%s]", orphaned[i].UserName, orphaned[i].UserName)
			}
		}
	}

	return orphaned, nil
}

func (d *SQLServerDriver) listLoginNames() ([]string, error) {
	rows, err := d.db.Query("SELECT name FROM sys.server_principals WHERE type IN ('S','U','G') ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			names = append(names, n)
		}
	}
	return names, nil
}

// FixOrphanedUser 修复孤用用户
func (d *SQLServerDriver) FixOrphanedUser(database, userName, loginName string) error {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	if _, err := db2.Exec(fmt.Sprintf("ALTER USER [%s] WITH LOGIN = [%s]", userName, loginName)); err != nil {
		return fmt.Errorf("fix orphaned user: %w", err)
	}
	return nil
}

// ─── MSSQL Effective Permissions ──────────────────────────────────────────

// GetEffectivePermissions 计算指定主体在指定对象上的有效权限
func (d *SQLServerDriver) GetEffectivePermissions(database, principalName, objectType, objectName string) (*EffectivePermission, error) {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	result := &EffectivePermission{
		PrincipalName: principalName,
		ObjectType:    objectType,
		ObjectName:    objectName,
	}

	query := `WITH RoleMembership AS (
		SELECT m.member_principal_id, m.role_principal_id, 0 AS depth
		FROM sys.database_role_members m
		UNION ALL
		SELECT rm.member_principal_id, rm.role_principal_id, r.depth + 1
		FROM sys.database_role_members rm
		JOIN RoleMembership r ON rm.member_principal_id = r.role_principal_id
		WHERE r.depth < 32
	)
	SELECT DISTINCT p.permission_name, p.state_desc
	FROM sys.database_permissions p
	WHERE (p.grantee_principal_id = USER_ID(@p1)
	   OR p.grantee_principal_id IN (SELECT role_principal_id FROM RoleMembership WHERE member_principal_id = USER_ID(@p1)))
	  AND p.class = 0`

	rows, err := db2.Query(query, principalName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pd PermissionDetail
		if err := rows.Scan(&pd.Permission, &pd.State); err != nil {
			continue
		}
		pd.Source = "inherited"
		result.Permissions = append(result.Permissions, pd)
	}
	if result.Permissions == nil {
		result.Permissions = make([]PermissionDetail, 0)
	}
	result.Source = "composite"
	return result, nil
}

// ─── MSSQL Guest Compliance ───────────────────────────────────────────────

// CheckGuestStatus 检查 guest 用户状态
func (d *SQLServerDriver) CheckGuestStatus(database string) (*GuestStatus, error) {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	gs := &GuestStatus{Database: database}

	var count int
	if err := db2.QueryRow("SELECT COUNT(*) FROM sys.database_principals WHERE name = 'guest'").Scan(&count); err == nil && count > 0 {
		gs.HasGuest = true
	}

	if gs.HasGuest {
		// 检查 guest 是否还有 CONNECT 权限（数据库级）
		var connectPerm int
		if err := db2.QueryRow("SELECT HAS_PERMS_BY_NAME('guest', 'DATABASE', 'CONNECT')").Scan(&connectPerm); err == nil && connectPerm == 0 {
			gs.IsDisabled = true
		} else {
			gs.Warning = "guest 用户未禁用，存在匿名访问风险"
		}
	}

	return gs, nil
}

// DisableGuest 禁用 guest 用户
func (d *SQLServerDriver) DisableGuest(database string) error {
	newDsn := fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable",
		d.cfg.Username, d.cfg.Password, d.cfg.Host, d.cfg.Port, database)
	db2, err := sql.Open("sqlserver", newDsn)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", database, err)
	}
	defer db2.Close()

	if _, err := db2.Exec("REVOKE CONNECT FROM GUEST"); err != nil {
		return fmt.Errorf("disable guest: %w", err)
	}
	return nil
}

// isSyntaxError 判断 MSSQL 错误是否为语法/对象不存在（可回退），而非权限错误（不可回退）
func isSyntaxError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	syntaxCodes := []string{"Msg 102,", "Msg 15151,", "Msg 2812,"}
	for _, code := range syntaxCodes {
		if strings.Contains(errStr, code) {
			return true
		}
	}
	return false
}

// ─── System SQL Dialect Methods ──────────────────────────────────────────
func (d *SQLServerDriver) SQLFormatTime(col string) string {
	return fmt.Sprintf("FORMAT(%s, \x27yyyy-MM-dd HH:mm:ss\x27)", col)
}
func (d *SQLServerDriver) SQLConcat(parts ...string) string {
	return strings.Join(parts, " + ")
}
func (d *SQLServerDriver) SQLIsNull(col, defaultVal string) string {
	return fmt.Sprintf("ISNULL(%s, %s)", col, defaultVal)
}
func (d *SQLServerDriver) SQLCurrentTimestamp() string { return "GETDATE()" }
func (d *SQLServerDriver) SQLQuoteIdent(name string) string { return "[" + name + "]" }

