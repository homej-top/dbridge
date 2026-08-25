package drivers

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

// PostgresDriver implements DatabaseDriver for PostgreSQL
type PostgresDriver struct {
	db     *sql.DB
	cfg    DriverConfig
	pooled bool
}

func NewPostgresDriver(cfg DriverConfig) (DatabaseDriver, *sql.DB, error) {
	var db *sql.DB
	if cfg.DB != nil {
		db = cfg.DB
	} else {
		port := cfg.Port
		if port == 0 {
			port = 5432
		}
		dbName := cfg.Database
		if dbName == "" {
			dbName = "postgres"
		}
		dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=10",
			cfg.Host, port, cfg.Username, cfg.Password, dbName)
		var err error
		db, err = sql.Open("postgres", dsn)
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
			return nil, nil, fmt.Errorf("postgres connection failed: %w", err)
		}
	}

	return &PostgresDriver{db: db, cfg: cfg, pooled: cfg.DB != nil}, db, nil
}

func (d *PostgresDriver) Ping() error     { return d.db.Ping() }
func (d *PostgresDriver) Close() error {
	if d.pooled {
		return nil
	}
	return d.db.Close()
}
func (d *PostgresDriver) DBType() string  { return "postgres" }
func (d *PostgresDriver) Dialect() string { return "postgres" }

func (d *PostgresDriver) setSearchPath(schema string) error {
	if schema == "" {
		return nil
	}
	_, err := d.db.Exec(`SET search_path TO "` + strings.ReplaceAll(schema, `"`, `""`) + `"`)
	return err
}

// ─── Schema Discovery ──────────────────────────────────────────────────────

func (d *PostgresDriver) ListSchemas() ([]SchemaInfo, error) {
	sRows, err := d.db.Query(`SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name`)
	if err != nil {
		return nil, err
	}
	defer sRows.Close()

	var schemas []SchemaInfo
	for sRows.Next() {
		var sn string
		if err := sRows.Scan(&sn); err != nil {
			continue
		}
		si, err := d.ListObjects(sn)
		if err != nil {
			schemas = append(schemas, SchemaInfo{Name: sn})
			continue
		}
		schemas = append(schemas, *si)
	}
	if schemas == nil {
		schemas = make([]SchemaInfo, 0)
	}
	return schemas, nil
}

func (d *PostgresDriver) ListSchemaNames() ([]string, error) {
	rows, err := d.db.Query(`SELECT schema_name FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name`)
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

func (d *PostgresDriver) ListObjects(schema string) (*SchemaInfo, error) {
	rows, err := d.db.Query(`SELECT table_name, table_type
		FROM information_schema.tables WHERE table_schema = $1
		ORDER BY table_name`, schema)
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

func (d *PostgresDriver) ListSchemaDetail() ([]SchemaDetailItem, error) {
	rows, err := d.db.Query(`SELECT
		s.schema_name,
		COALESCE(t.table_count, 0) AS table_count,
		COALESCE(v.view_count, 0) AS view_count,
		'' AS charset,
		'' AS collation
	FROM information_schema.schemata s
	LEFT JOIN (SELECT table_schema, COUNT(*) AS table_count FROM information_schema.tables WHERE table_type = 'BASE TABLE' GROUP BY table_schema) t ON s.schema_name = t.table_schema
	LEFT JOIN (SELECT table_schema, COUNT(*) AS view_count FROM information_schema.tables WHERE table_type = 'VIEW' GROUP BY table_schema) v ON s.schema_name = v.table_schema
	WHERE s.schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
	ORDER BY s.schema_name`)
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

func (d *PostgresDriver) ListTables(schema string) ([]TableListItem, error) {
	rows, err := d.db.Query(`SELECT
		t.table_name, t.table_type, NULL AS engine,
		COALESCE(NULLIF(c.reltuples, -1), 0)::bigint AS row_count,
		COALESCE(obj_description(c.oid), '') AS comment,
		'' AS create_time, '' AS update_time
	FROM information_schema.tables t
	LEFT JOIN pg_class c ON c.relname = t.table_name
		AND c.relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = t.table_schema)
	WHERE t.table_schema = $1 ORDER BY t.table_name`, schema)
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
		items = append(items, item)
	}
	if items == nil {
		items = make([]TableListItem, 0)
	}
	return items, nil
}

// ─── Column Metadata ──────────────────────────────────────────────────────

func (d *PostgresDriver) GetColumns(schema, table string) ([]ColumnDetail, error) {
	rows, err := d.db.Query(`SELECT
		column_name, data_type,
		COALESCE(character_maximum_length::text, '') as length,
		is_nullable,
		COALESCE(column_default, ''),
		COALESCE(col_description((SELECT oid FROM pg_class WHERE relname = $2 AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1)), ordinal_position)::text, ''),
		CASE WHEN EXISTS (SELECT 1 FROM information_schema.key_column_usage k WHERE k.table_schema = c.table_schema AND k.table_name = c.table_name AND k.column_name = c.column_name AND k.constraint_name LIKE '%_pkey') THEN 'PRI' ELSE '' END
	FROM information_schema.columns c
	WHERE table_schema = $1 AND table_name = $2
	ORDER BY ordinal_position`, schema, table)
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

func (d *PostgresDriver) GetColumnDetails(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT column_name, data_type, is_nullable, column_default
		FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *PostgresDriver) GetDDL(schema, table string) (string, error) {
	cols, err := d.GetColumns(schema, table)
	if err != nil {
		return "", err
	}
	if len(cols) == 0 {
		return "", fmt.Errorf("table not found: %s.%s", schema, table)
	}

	// Extract sequences referenced by nextval() defaults
	var seqNames []string
	// Match: nextval('anything'::regclass) — extract the sequence name (last dot-separated part, strip quotes)
	nextvalRe := regexp.MustCompile(`nextval\('([^']+)'::regclass\)`)

	var colDefs []string
	for _, c := range cols {
		colType := c.Type
		if c.Length != "" && c.Length != "0" {
			colType = fmt.Sprintf("%s(%s)", c.Type, c.Length)
		}
		def := fmt.Sprintf(`    "%s" %s`, c.Name, colType)
		if c.Nullable == "NO" {
			def += " NOT NULL"
		}
		if c.Default != "" {
			def += " DEFAULT " + c.Default
			if matches := nextvalRe.FindStringSubmatch(c.Default); len(matches) > 1 {
				raw := matches[1]
				// Extract just the sequence name: strip schema prefix and quotes
				parts := strings.Split(raw, ".")
				sn := parts[len(parts)-1]
				sn = strings.Trim(sn, "\"")
				seqNames = append(seqNames, sn)
			}
		}
		colDefs = append(colDefs, def)
	}

	ddl := fmt.Sprintf(`CREATE TABLE "%s"."%s" (\n%s\n);`, schema, table, strings.Join(colDefs, ",\n"))

	// Prepend CREATE SEQUENCE statements for any referenced sequences
	if len(seqNames) > 0 {
		var seqDDL []string
		for _, sn := range seqNames {
			seqDDL = append(seqDDL, fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS "%s"."%s";`, schema, sn))
		}
		ddl = strings.Join(seqDDL, "\n") + "\n\n" + ddl
	}

	return ddl, nil
}

func (d *PostgresDriver) GetViewDefinition(schema, view string) (string, error) {
	var def string
	err := d.db.QueryRow(`SELECT definition FROM pg_views WHERE schemaname = $1 AND viewname = $2`, schema, view).Scan(&def)
	return def, err
}

// ─── Query Execution ──────────────────────────────────────────────────────

func (d *PostgresDriver) ExecuteQuery(sql string, schema string) (*QueryResult, error) {
	if schema != "" {
		if err := d.setSearchPath(schema); err != nil {
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

func (d *PostgresDriver) GetTableData(schema, table string, page, pageSize int) (*TableDataResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}

	quoted := QuoteTableName(schema, table, "postgres")
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

func (d *PostgresDriver) ExecuteDDL(ddl string) (int64, error) {
	res, err := d.db.Exec(ddl)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (d *PostgresDriver) ExecuteDDLBatch(ddl string, importStrategy string) (total, success, fail, rowsAffected int64, errors []string, err error) {
	statements := SplitDDLWithDialect(ddl, "postgres")
	total = int64(len(statements))

	for _, stmt := range statements {
		s := strings.TrimSpace(stmt)
		if s == "" {
			continue
		}

		if importStrategy != "" && strings.HasPrefix(strings.ToUpper(s), "INSERT") {
			switch importStrategy {
			case "skip":
				s = strings.Replace(s, "INSERT INTO", "INSERT INTO", 1)
				s += " ON CONFLICT DO NOTHING"
			case "update":
				s += " ON CONFLICT ON CONSTRAINT _pk DO UPDATE SET " // simplified
			}
		}

		_, e := d.db.Exec(s)
		if e != nil {
			fail++
			errors = append(errors, fmt.Sprintf("statement %d: %s", success+fail, e.Error()))
			continue
		}
		success++
	}

	return
}

// ─── DDL Helpers ──────────────────────────────────────────────────────────

func (d *PostgresDriver) AlterColumnModifyDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment string) []string {
	var parts []string
	if length != "" && length != "0" {
		parts = append(parts, fmt.Sprintf("%s(%s)", colType, length))
	} else {
		parts = append(parts, colType)
	}
	def := strings.Join(parts, " ")
	ddl := fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN "%s" TYPE %s`, qualifiedTable, columnName, def)
	result := []string{ddl}

	if !nullable {
		result = append(result, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN "%s" SET NOT NULL`, qualifiedTable, columnName))
	}
	if defaultVal != "" {
		result = append(result, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN "%s" SET DEFAULT %s`, qualifiedTable, columnName, defaultVal))
	}
	if comment != "" {
		result = append(result, fmt.Sprintf(`COMMENT ON COLUMN %s."%s" IS '%s'`, qualifiedTable, columnName, strings.ReplaceAll(comment, "'", "''")))
	}
	return result
}

func (d *PostgresDriver) DropIndexDDL(qualifiedTable, schema, indexName string) string {
	return fmt.Sprintf(`DROP INDEX IF EXISTS "%s"."%s"`, schema, indexName)
}

func (d *PostgresDriver) AddColumnDDL(qualifiedTable, columnName, colType, length string, nullable bool, defaultVal, comment, after string) []string {
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
	ddl := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN "%s" %s`, qualifiedTable, columnName, strings.Join(parts, " "))
	result := []string{ddl}
	if comment != "" {
		result = append(result, fmt.Sprintf(`COMMENT ON COLUMN %s."%s" IS '%s'`, qualifiedTable, columnName, strings.ReplaceAll(comment, "'", "''")))
	}
	return result
}

func (d *PostgresDriver) DropColumnDDL(qualifiedTable, columnName string) string {
	return fmt.Sprintf(`ALTER TABLE %s DROP COLUMN "%s"`, qualifiedTable, columnName)
}

func (d *PostgresDriver) AddIndexDDL(qualifiedTable, indexName, indexType string, columns []string) string {
	var colList []string
	for _, c := range columns {
		colList = append(colList, `"`+c+`"`)
	}
	cols := strings.Join(colList, ", ")
	if indexType == "UNIQUE" {
		return fmt.Sprintf(`CREATE UNIQUE INDEX "%s" ON %s (%s)`, indexName, qualifiedTable, cols)
	}
	return fmt.Sprintf(`CREATE INDEX "%s" ON %s (%s)`, indexName, qualifiedTable, cols)
}

// ─── Index & Constraint Metadata ──────────────────────────────────────────

func (d *PostgresDriver) GetIndexes(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT i.indexname, i.indexdef, COALESCE(obj_description((SELECT oid FROM pg_class WHERE relname = i.indexname AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1))), '') AS comment FROM pg_indexes i WHERE i.schemaname = $1 AND i.tablename = $2`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *PostgresDriver) GetConstraints(schema, table string) ([]map[string]interface{}, error) {
	rows, err := d.db.Query(`SELECT conname, conrelid::regclass::text, confrelid::regclass::text FROM pg_constraint
		WHERE conrelid = (SELECT oid FROM pg_class WHERE relname = $2 AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = $1))
		AND contype = 'f'`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanMapRows(rows)
}

func (d *PostgresDriver) GetFullStructure(schema, table string) (*FullStructure, error) {
	result := &FullStructure{}

	// Detect view
	var relkind string
	err := d.db.QueryRow(`
		SELECT c.relkind FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, schema, table).Scan(&relkind)
	if err != nil {
		return nil, fmt.Errorf("table not found: %s.%s (note: PostgreSQL object names are case-sensitive when quoted)", schema, table)
	}
	if relkind == "v" {
		result.IsView = true
		var ddl sql.NullString
		d.db.QueryRow(`SELECT pg_get_viewdef(c.oid, true) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = $1 AND c.relname = $2`, schema, table).Scan(&ddl)
		if ddl.Valid {
			result.DDL = fmt.Sprintf("CREATE VIEW %s AS\n%s", quotePGTable(schema, table), ddl.String)
		}
		// Views have columns — continue to query them below
	}

	// Columns
	rows, err := d.db.Query(`
		SELECT c.column_name, c.data_type, COALESCE(c.character_maximum_length::text, c.numeric_precision::text, ''),
			c.is_nullable, c.column_default, c.collation_name,
			COALESCE(pgd.description, '')
		FROM information_schema.columns c
		LEFT JOIN pg_catalog.pg_statio_all_tables st ON st.schemaname = c.table_schema AND st.relname = c.table_name
		LEFT JOIN pg_catalog.pg_description pgd ON pgd.objoid = st.relid AND pgd.objsubid = c.ordinal_position
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c TableColumn
		var length, defVal, collation, nullable sql.NullString
		if err := rows.Scan(&c.Name, &c.Type, &length, &nullable, &defVal, &collation, &c.Comment); err != nil {
			continue
		}
		c.Nullable = nullable.String == "YES"
		if defVal.Valid {
			c.Default = defVal.String
			c.HasDef = true
			if strings.HasPrefix(defVal.String, "nextval(") {
				c.Extra = "auto_increment"
			}
		}
		if length.Valid {
			c.Length = length.String
		}
		if collation.Valid {
			c.Collation = collation.String
		}
		result.Columns = append(result.Columns, c)
	}

	// Mark PK columns
	pkCols := map[string]bool{}
	pkRows, _ := d.db.Query(`
		SELECT kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.table_schema = $1 AND tc.table_name = $2 AND tc.constraint_type = 'PRIMARY KEY'`, schema, table)
	if pkRows != nil {
		defer pkRows.Close()
		for pkRows.Next() {
			var col string
			if pkRows.Scan(&col) == nil {
				pkCols[col] = true
			}
		}
	}
	for i := range result.Columns {
		if pkCols[result.Columns[i].Name] {
			result.Columns[i].Key = "PRI"
		}
	}

	// Indexes (tables only)
	if !result.IsView {
	idxRows, err := d.db.Query(`
		SELECT i.relname, ix.indisunique, ix.indisprimary,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)),
			COALESCE(obj_description(i.oid), ''),
			am.amname
		FROM pg_index ix
		JOIN pg_class t ON t.oid = ix.indrelid
		JOIN pg_class i ON i.oid = ix.indexrelid
		JOIN pg_namespace n ON n.oid = t.relnamespace
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ANY(ix.indkey)
		JOIN pg_am am ON am.oid = i.relam
		WHERE n.nspname = $1 AND t.relname = $2
		GROUP BY i.relname, ix.indisunique, ix.indisprimary, i.oid, am.amname
		ORDER BY i.relname`, schema, table)
	if err == nil {
		defer idxRows.Close()
		for idxRows.Next() {
			var idx TableIndex
			var isUnique, isPrimary bool
			var cols []byte
			var comment, amName string
			if err := idxRows.Scan(&idx.Name, &isUnique, &isPrimary, &cols, &comment, &amName); err != nil {
				continue
			}
			idx.Comment = comment
			s := strings.Trim(string(cols), "{}")
			if s != "" {
				idx.Columns = strings.Split(s, ",")
			}
			// Map access method to index type
			switch {
			case isPrimary:
				idx.Type = "PRIMARY"
			case isUnique:
				idx.Type = "UNIQUE"
			default:
				switch amName {
				case "btree":
					idx.Type = "INDEX"
				default:
					idx.Type = strings.ToUpper(amName)
				}
			}
			result.Indexes = append(result.Indexes, idx)
		}
	}

	}

	// Constraints (tables only)
	if !result.IsView {
	conRows, err := d.db.Query(`
		SELECT tc.constraint_name, tc.constraint_type,
			array_agg(kcu.column_name ORDER BY kcu.ordinal_position),
			COALESCE(ccu.table_name, ''),
			COALESCE(array_agg(ccu.column_name ORDER BY kcu.ordinal_position), ARRAY[]::text[]),
			COALESCE(rc.delete_rule, ''), COALESCE(rc.update_rule, '')
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		LEFT JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_name = ccu.constraint_name AND tc.table_schema = ccu.constraint_schema
			AND tc.constraint_type = 'FOREIGN KEY'
		LEFT JOIN information_schema.referential_constraints rc
			ON tc.constraint_name = rc.constraint_name AND tc.constraint_schema = rc.constraint_schema
		WHERE tc.table_schema = $1 AND tc.table_name = $2
		GROUP BY tc.constraint_name, tc.constraint_type, ccu.table_name, rc.delete_rule, rc.update_rule
		ORDER BY tc.constraint_name`, schema, table)
	if err == nil {
		defer conRows.Close()
		for conRows.Next() {
			var c TableConstraint
			var cols, refCols []byte
			if err := conRows.Scan(&c.Name, &c.Type, &cols, &c.RefTable, &refCols, &c.OnDelete, &c.OnUpdate); err != nil {
				continue
			}
			if s := strings.Trim(string(cols), "{}"); s != "" {
				c.Columns = strings.Split(s, ",")
			}
			if s := strings.Trim(string(refCols), "{}"); s != "" {
				c.RefColumns = strings.Split(s, ",")
			}
			result.Constraints = append(result.Constraints, c)
		}
	}
	}

	// Table meta
	var m TableMeta
	err = d.db.QueryRow(`
		SELECT COALESCE(obj_description(c.oid), ''),
			(SELECT datcollate FROM pg_database WHERE datname = current_database()),
			(SELECT pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database())
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2`, schema, table).Scan(&m.Comment, &m.Collation, &m.Charset)
	if err == nil {
		result.TableMeta = m
	}

	// Exact row count — pg_class.reltuples is an estimate, often 0 for new tables
	d.db.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, quotePGTable(schema, table))).Scan(&m.RowCount)
	result.TableMeta = m

	result.DDL = buildPGDDL(schema, table, result)
	return result, nil
}

// buildPGDDL generates a full DDL script for a PostgreSQL table
func buildPGDDL(schema, table string, st *FullStructure) string {
	var b strings.Builder
	q := func(s string) string { return QuoteIdent(s, "postgres") }
	b.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", quotePGTable(schema, table)))
	for i, c := range st.Columns {
		b.WriteString("    " + q(c.Name) + " " + pgColumnType(c))
		if !c.Nullable {
			b.WriteString(" NOT NULL")
		}
		if c.HasDef && c.Default != "" {
			b.WriteString(" DEFAULT " + c.Default)
		}
		if i < len(st.Columns)-1 || len(st.Constraints) > 0 {
			b.WriteString(",")
		}
		if c.Comment != "" {
			b.WriteString(" -- " + c.Comment)
		}
		b.WriteString("\n")
	}
	for _, con := range st.Constraints {
		switch con.Type {
		case "PRIMARY KEY":
			b.WriteString(fmt.Sprintf("    CONSTRAINT %s PRIMARY KEY (%s)",
				q(con.Name), strings.Join(qList(con.Columns), ", ")))
		case "UNIQUE":
			b.WriteString(fmt.Sprintf("    CONSTRAINT %s UNIQUE (%s)",
				q(con.Name), strings.Join(qList(con.Columns), ", ")))
		case "FOREIGN KEY":
			b.WriteString(fmt.Sprintf("    CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)",
				q(con.Name), strings.Join(qList(con.Columns), ", "),
				q(con.RefTable), strings.Join(qList(con.RefColumns), ", ")))
			if con.OnDelete != "" && con.OnDelete != "NO ACTION" {
				b.WriteString(" ON DELETE " + con.OnDelete)
			}
			if con.OnUpdate != "" && con.OnUpdate != "NO ACTION" {
				b.WriteString(" ON UPDATE " + con.OnUpdate)
			}
		}
		b.WriteString(",\n")
	}
	body := b.String()
	body = strings.TrimRight(body, ",\n")
	b.Reset()
	b.WriteString(body)
	b.WriteString("\n);\n")

	for _, idx := range st.Indexes {
		if idx.Type == "PRIMARY" {
			continue
		}
		unique := ""
		if idx.Type == "UNIQUE" {
			unique = "UNIQUE "
		}
		b.WriteString(fmt.Sprintf("CREATE %sINDEX %s ON %s (%s);\n",
			unique, q(idx.Name), quotePGTable(schema, table),
			strings.Join(qList(idx.Columns), ", ")))
	}
	for _, c := range st.Columns {
		if c.Comment != "" {
			b.WriteString(fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s;\n",
				quotePGTable(schema, table), q(c.Name), pgQuoteString(c.Comment)))
		}
	}
	if st.TableMeta.Comment != "" {
		b.WriteString(fmt.Sprintf("COMMENT ON TABLE %s IS %s;\n",
			quotePGTable(schema, table), pgQuoteString(st.TableMeta.Comment)))
	}
	return b.String()
}

func pgColumnType(c TableColumn) string {
	t := strings.ToLower(c.Type)
	switch t {
	case "character varying":
		t = "varchar"
	case "character":
		t = "char"
	case "timestamp without time zone":
		t = "timestamp"
	case "timestamp with time zone":
		t = "timestamptz"
	}
	if c.Length != "" && (t == "varchar" || t == "char") {
		return fmt.Sprintf("%s(%s)", t, c.Length)
	}
	return t
}

func pgQuoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func quotePGTable(schema, table string) string {
	return QuoteIdent(schema, "postgres") + "." + QuoteIdent(table, "postgres")
}

func qList(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = QuoteIdent(n, "postgres")
	}
	return out
}

func (d *PostgresDriver) GetTableMeta(schema, table string) (map[string]interface{}, error) {
	row := d.db.QueryRow(`SELECT
		COALESCE(NULLIF(c.reltuples, -1)::bigint, 0),
		COALESCE(obj_description(c.oid), ''),
		COALESCE((SELECT datcollate FROM pg_database WHERE datname = current_database()), ''),
		COALESCE((SELECT pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database()), '')
	FROM pg_class c JOIN pg_namespace n ON c.relnamespace = n.oid
	WHERE n.nspname = $1 AND c.relname = $2`, schema, table)
	var rowCount int64
	var comment, collation, charset string
	if err := row.Scan(&rowCount, &comment, &collation, &charset); err != nil {
		return map[string]interface{}{}, err
	}
	return map[string]interface{}{
		"engine":      "",
		"charset":     charset,
		"collation":   collation,
		"row_count":   rowCount,
		"comment":     comment,
		"create_time": "",
		"update_time": "",
	}, nil
}

// ─── Cross-Database DDL Helpers ────────────────────────────────────────────

func (d *PostgresDriver) BuildCreateTableDDL(schema, table string, columns []map[string]interface{}, sourceDBType string) string {
	var colDefs []string
	for _, col := range columns {
		name, _ := col["name"].(string)
		colType, _ := col["type"].(string)
		charLen := ""
		if v, ok := col["length"]; ok {
			charLen = fmt.Sprintf("%v", v)
		}
		targetType := ConvertDDLType(sourceDBType, "postgres", colType, charLen, colType)
		def := fmt.Sprintf(`  "%s" %s`, name, targetType)

		if nullable, ok := col["nullable"]; ok {
			if nullableStr, ok := nullable.(string); ok && nullableStr == "NO" {
				def += " NOT NULL"
			}
		}
		colDefs = append(colDefs, def)
	}
	return fmt.Sprintf(`CREATE TABLE "%s"."%s" (\n%s\n);`, schema, table, strings.Join(colDefs, ",\n"))
}

func (d *PostgresDriver) SetTableCommentDDL(qualifiedTable, schema, table, comment string) string {
	return fmt.Sprintf(`COMMENT ON TABLE %s IS '%s'`, qualifiedTable, strings.ReplaceAll(comment, "'", "''"))
}

func (d *PostgresDriver) FormatSQLValue(val interface{}) string {
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
			return "TRUE"
		}
		return "FALSE"
	case time.Time:
		return FormatSQLTime(v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func (d *PostgresDriver) AlterColumnClause(columnName, colType, columnDef string) string {
	return fmt.Sprintf(`ALTER COLUMN "%s" TYPE %s`, columnName, columnDef)
}

func (d *PostgresDriver) BuildInsertSQL(tableName, colList string, rowValues []string) string {
	return fmt.Sprintf("INSERT INTO %s (%s) VALUES\n%s;",
		tableName, colList, strings.Join(rowValues, ",\n"))
}

func (d *PostgresDriver) RewriteCreateDDL(ddl, sourceSchema, targetSchema string) string {
	return ddl
}

func (d *PostgresDriver) ListColumnsForAlter(schema, table string) ([]AlterColumn, error) {
	rows, err := d.db.Query(`SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position`, schema, table)
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

// GetColumnTypes returns PostgreSQL-specific column type definitions
func (d *PostgresDriver) GetColumnTypes() []ColumnTypeInfo {
	return []ColumnTypeInfo{
		{Name: "integer", NeedsLength: false, Description: "整数"},
		{Name: "bigint", NeedsLength: false, Description: "大整数"},
		{Name: "smallint", NeedsLength: false, Description: "小整数"},
		{Name: "serial", NeedsLength: false, Description: "自增整数"},
		{Name: "bigserial", NeedsLength: false, Description: "自增大整数"},
		{Name: "smallserial", NeedsLength: false, Description: "自增小整数"},
		{Name: "numeric", NeedsLength: true, NeedsScale: true, Description: "精确数值"},
		{Name: "decimal", NeedsLength: true, NeedsScale: true, Description: "定点数"},
		{Name: "real", NeedsLength: false, Description: "单精度浮点"},
		{Name: "double precision", NeedsLength: false, Description: "双精度浮点"},
		{Name: "money", NeedsLength: false, Description: "货币"},
		{Name: "varchar", NeedsLength: true, Description: "变长字符"},
		{Name: "char", NeedsLength: true, Description: "定长字符"},
		{Name: "text", NeedsLength: false, Description: "文本"},
		{Name: "bytea", NeedsLength: false, Description: "二进制"},
		{Name: "timestamp", NeedsLength: false, Description: "时间戳(无时区)"},
		{Name: "timestamptz", NeedsLength: false, Description: "时间戳(有时区)"},
		{Name: "date", NeedsLength: false, Description: "日期"},
		{Name: "time", NeedsLength: false, Description: "时间"},
		{Name: "timetz", NeedsLength: false, Description: "时间(有时区)"},
		{Name: "interval", NeedsLength: false, Description: "时间间隔"},
		{Name: "boolean", NeedsLength: false, Description: "布尔"},
		{Name: "json", NeedsLength: false, Description: "JSON"},
		{Name: "jsonb", NeedsLength: false, Description: "JSONB(索引)"},
		{Name: "uuid", NeedsLength: false, Description: "UUID"},
		{Name: "inet", NeedsLength: false, Description: "IP地址"},
		{Name: "cidr", NeedsLength: false, Description: "CIDR网络"},
		{Name: "macaddr", NeedsLength: false, Description: "MAC地址"},
	}
}

func (d *PostgresDriver) GetIndexTypes() []IndexTypeInfo {
	return []IndexTypeInfo{
		{Name: "INDEX", Description: "B-Tree 索引"},
		{Name: "UNIQUE", Description: "唯一 B-Tree 索引"},
		{Name: "HASH", Description: "Hash 索引"},
		{Name: "GIST", Description: "GiST 通用搜索树"},
		{Name: "GIN", Description: "GIN 倒排索引"},
		{Name: "SPGIST", Description: "SP-GiST 空间分区"},
		{Name: "BRIN", Description: "BRIN 块范围索引"},
	}
}

// ─── Tree Metadata ─────────────────────────────────────────────────────────

func (d *PostgresDriver) GetTreeMetadata() TreeMetadata {
	return TreeMetadata{
		DBType: "postgres",
		Levels: []TreeLevel{
			{Key: "server", Label: "Server", LabelKey: "tree.server", Icon: "CloudServerOutlined"},
			{Key: "database", Label: "Database", LabelKey: "tree.database", Icon: "DatabaseOutlined"},
			{Key: "schema", Label: "Schema", LabelKey: "tree.schema", PlaceholderKey: "tree.schema_name_hint", Icon: "ClusterOutlined"},
			{Key: "tables_folder", Label: "Tables", LabelKey: "tree.tables", Icon: "TableOutlined"},
			{Key: "views_folder", Label: "Views", LabelKey: "tree.views", Icon: "EyeOutlined"},
		},
		AllowCreate: map[string]bool{"database": true, "schema": true},
		SystemFilter: &SystemFilter{
			ExcludeNames: []string{"pg_catalog", "information_schema", "pg_toast"},
		},
	}
}

func (d *PostgresDriver) ListDatabases() ([]DatabaseInfo, error) {
    rows, err := d.db.Query(`SELECT d.datname, pg_encoding_to_char(d.encoding),
        pg_database_size(d.datname)/1024/1024 AS size_mb,
        COALESCE((SELECT count(*) FROM pg_stat_user_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')), 0) AS table_count
    FROM pg_database d WHERE d.datistemplate=false AND d.datallowconn=true ORDER BY d.datname`)
    if err != nil {
        // Fallback: pg_stat_user_tables requires connection to the specific database, use 0
        rows, err = d.db.Query("SELECT datname, pg_encoding_to_char(encoding), pg_database_size(datname)/1024/1024 AS size_mb, 0 FROM pg_database WHERE datistemplate=false AND datallowconn=true ORDER BY datname")
        if err != nil {
            return nil, err
        }
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

func (d *PostgresDriver) ResolveContext(arg string) DatabaseContext {
	return DatabaseContext{Schema: arg}
}


// GetServerInfo returns PostgreSQL server info
func (d *PostgresDriver) GetServerInfo(ctx context.Context) (map[string]interface{}, error) {
	info := make(map[string]interface{})
	var version string
	if err := d.db.QueryRowContext(ctx, "SELECT version()").Scan(&version); err == nil {
		info["version"] = version
	}
	var uptime string
	if err := d.db.QueryRowContext(ctx, "SELECT pg_postmaster_start_time()::text").Scan(&uptime); err == nil {
		info["start_time"] = uptime
	}
	return info, nil
}

// GetMetrics returns PostgreSQL current metrics
func (d *PostgresDriver) GetMetrics(ctx context.Context) (map[string]interface{}, error) {
	m := make(map[string]interface{})
	queries := map[string]string{
		"总连接数":   "SELECT count(*) FROM pg_stat_activity",
		"活跃查询":    "SELECT count(*) FROM pg_stat_activity WHERE state='active'",
		"空闲连接":    "SELECT count(*) FROM pg_stat_activity WHERE state='idle'",
		"等待锁连接":  "SELECT count(*) FROM pg_stat_activity WHERE wait_event IS NOT NULL",
		"缓冲命中率":  "SELECT ROUND(COALESCE(100.0 * sum(blks_hit) / GREATEST(sum(blks_hit) + sum(blks_read), 1), 100), 2) FROM pg_stat_database",
		"死锁数":     "SELECT COALESCE(sum(deadlocks), 0) FROM pg_stat_database",
		"最大连接数":  "SELECT setting FROM pg_settings WHERE name='max_connections'",
	}
	for name, sql := range queries {
		var v string
		if err := d.db.QueryRowContext(ctx, sql).Scan(&v); err == nil {
			m[name] = v
		}
	}
	return m, nil
}

func (d *PostgresDriver) ListDatabaseSchemas(database string) ([]string, error) {
	// Build a new DSN connecting to the target database
	newDsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=5",
		d.cfg.Host, d.cfg.Port, d.cfg.Username, d.cfg.Password, database)
	db2, err := sql.Open("postgres", newDsn)
	if err != nil {
		return nil, fmt.Errorf("connect to database %s: %w", database, err)
	}
	defer db2.Close()

	if err := db2.Ping(); err != nil {
		return nil, fmt.Errorf("ping database %s: %w", database, err)
	}

	rows, err := db2.Query(`SELECT schema_name FROM information_schema.schemata 
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY schema_name`)
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

func (d *PostgresDriver) BuildAddColumn(table, schema string, col AlterColumnChange, curCols map[string]TableColumn) (string, string, []string, bool, error) {
	nullable := col.Nullable == nil || *col.Nullable
	defaultVal := ""
	if col.HasDef != nil && *col.HasDef { defaultVal = col.Default }
	parts := d.AddColumnDDL(table, col.Name, col.Type, col.Length, nullable, defaultVal, col.Comment, col.After)
	return strings.Join(parts, ";\n"), "", nil, false, nil
}
func (d *PostgresDriver) BuildModifyColumn(table string, col AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
	return BuildPGModifyColumn(table, col, orig)
}

// BuildPGModifyColumn generates ALTER COLUMN DDL for PostgreSQL.
func BuildPGModifyColumn(tbl string, ch AlterColumnChange, orig TableColumn) ([]string, []string, []string, bool, error) {
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

	newType := pgColumnTypeFromParts(ch.Type, ch.Length)
	origTypeVal := pgColumnTypeFromParts(orig.Type, orig.Length)
	if newType != origTypeVal {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tbl, quotePG(ch.Name), newType))
		rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tbl, quotePG(ch.Name), origTypeVal))
	}
	if ch.Nullable != nil && *ch.Nullable != orig.Nullable {
		if *ch.Nullable {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tbl, quotePG(ch.Name)))
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", tbl, quotePG(ch.Name)))
		} else {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL", tbl, quotePG(ch.Name)))
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL", tbl, quotePG(ch.Name)))
		}
	}
	if ch.HasDef != nil && *ch.HasDef {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tbl, quotePG(ch.Name), formatDefault(ch.Default)))
		if orig.HasDef {
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tbl, quotePG(ch.Name), formatDefault(orig.Default)))
		} else {
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tbl, quotePG(ch.Name)))
		}
	} else if ch.HasDef != nil && !*ch.HasDef && orig.HasDef {
		stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT", tbl, quotePG(ch.Name)))
		rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tbl, quotePG(ch.Name), formatDefault(orig.Default)))
	}
	if ch.Comment != "" {
		stmts = append(stmts, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quotePG(ch.Name), pgString(ch.Comment)))
		rollbacks = append(rollbacks, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quotePG(ch.Name), pgString(orig.Comment)))
	}

	return stmts, rollbacks, warnings, highRisk, nil
}
func (d *PostgresDriver) BuildDropColumn(table string, colName string, orig TableColumn) (string, string, []string, bool, error) {
	return d.DropColumnDDL(table, colName), "", nil, true, nil
}
func (d *PostgresDriver) BuildAddIndex(table, schema string, idx AlterIndexChange) (string, string, error) {
	return d.AddIndexDDL(table, idx.Name, idx.Type, idx.Columns), "", nil
}
func (d *PostgresDriver) BuildDropIndex(table, schema string, idxName string, orig TableIndex) (string, string, []string, bool, error) {
	return d.DropIndexDDL(table, schema, idxName), "", nil, true, nil
}
func (d *PostgresDriver) BuildIndexComment(table, schema string, idx AlterIndexChange, orig TableIndex) (string, string, error) {
	return "", "", fmt.Errorf("not supported")
}
func (d *PostgresDriver) BuildAddConstraint(table string, idx AlterIndexChange) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *PostgresDriver) BuildDropConstraint(table string, constraintName string) (string, string, error) {
	return "", "", fmt.Errorf("not implemented")
}
func (d *PostgresDriver) BuildTableComment(table, newComment, oldComment string) (string, string, error) {
	return d.SetTableCommentDDL(table, "", "", newComment), "", nil
}

// ListProcesses returns current process list
func (d *PostgresDriver) ListProcesses(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, processQueries[dbType])
}

// ListUsers returns user list
func (d *PostgresDriver) ListUsers(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, userQueries[dbType])
}

// ListTablespaces returns tablespace info
func (d *PostgresDriver) ListTablespaces(dbType string) ([]map[string]interface{}, error) {
    return queryToList(d.db, tablespaceQueries[dbType])
}

func (d *PostgresDriver) GetMetricsV2(ctx context.Context) (*ServerMetricsV2, error) {
	start := time.Now()
	m := &ServerMetricsV2{DBType: "postgres", CollectedAt: time.Now(), DatabaseSpecific: make(map[string]interface{})}

	// Connections
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state='active'").Scan(&m.Connections.Active)
	var idle, idleInTx int
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state='idle'").Scan(&idle)
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE state='idle in transaction'").Scan(&idleInTx)
	m.Connections.Idle = idle + idleInTx
	m.Connections.Waiting = idleInTx
	m.Connections.Total = m.Connections.Active + m.Connections.Idle
	d.db.QueryRowContext(ctx, "SELECT setting::int FROM pg_settings WHERE name='max_connections'").Scan(&m.Connections.MaxConnections)
	if m.Connections.MaxConnections > 0 {
		m.Connections.UsagePercent = float64(m.Connections.Total) * 100.0 / float64(m.Connections.MaxConnections)
	}

	// Throughput
	var commits, rollbacks int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(xact_commit),0) FROM pg_stat_database").Scan(&commits)
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(xact_rollback),0) FROM pg_stat_database").Scan(&rollbacks)
	m.Throughput.CommitTotal = commits
	m.Throughput.RollbackTotal = rollbacks

	// Buffer cache
	var hit, read int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(blks_hit),0), COALESCE(sum(blks_read),0) FROM pg_stat_database").Scan(&hit, &read)
	if hit+read > 0 {
		m.BufferCache.HitRate = float64(hit) * 100.0 / float64(hit+read)
	}

	// Locks
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(deadlocks),0) FROM pg_stat_database").Scan(&m.Locks.Deadlocks)
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE wait_event IS NOT NULL").Scan(&m.Locks.LockWaits)
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE xact_start < NOW() - INTERVAL '5 minutes' AND state!='idle'").Scan(&m.Locks.LongTransactions)
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_stat_activity WHERE wait_event_type='Lock'").Scan(&m.Locks.BlockedSessions)

	// Storage — tablespaces
	rows, _ := d.db.QueryContext(ctx, "SELECT spcname, pg_tablespace_size(spcname)/1024/1024 FROM pg_tablespace")
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var ts TablespaceMetric
			if rows.Scan(&ts.Name, &ts.SizeMB) == nil { m.Storage.Tablespaces = append(m.Storage.Tablespaces, ts) }
		}
	}
	// Database sizes
	dbRows, _ := d.db.QueryContext(ctx, "SELECT datname, pg_database_size(datname)/1024/1024 FROM pg_database WHERE datistemplate=false AND datallowconn=true")
	if dbRows != nil {
		defer dbRows.Close()
		for dbRows.Next() {
			var ts TablespaceMetric
			if dbRows.Scan(&ts.Name, &ts.SizeMB) == nil { m.Storage.Tablespaces = append(m.Storage.Tablespaces, ts) }
		}
	}

	// Replication
	var lagSec float64
	if err := d.db.QueryRowContext(ctx, "SELECT COALESCE(EXTRACT(EPOCH FROM replay_lag),0) FROM pg_stat_replication WHERE state='streaming' LIMIT 1").Scan(&lagSec); err == nil && lagSec >= 0 {
		m.Replication = &ReplicationMetrics{LagSeconds: lagSec}
	}

	// PG-specific: XID age, dangling slots, autovacuum
	var xidAge int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(max(age(datfrozenxid)),0) FROM pg_database").Scan(&xidAge)
	m.DatabaseSpecific["xid_age"] = xidAge
	if xidAge > 1500000000 { m.DatabaseSpecific["xid_warning"] = "critical"
	} else if xidAge > 1000000000 { m.DatabaseSpecific["xid_warning"] = "warning" }

	var danglingSlots int
	d.db.QueryRowContext(ctx, "SELECT count(*) FROM pg_replication_slots WHERE active='f'").Scan(&danglingSlots)
	if danglingSlots > 0 { m.DatabaseSpecific["dangling_replication_slots"] = danglingSlots }

	var deadTuples int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(n_dead_tup),0) FROM pg_stat_user_tables").Scan(&deadTuples)
	m.DatabaseSpecific["dead_tuples"] = deadTuples

	var tableBloat int64
	d.db.QueryRowContext(ctx, "SELECT COALESCE(sum(n_dead_tup),0)*100/GREATEST(COALESCE(sum(n_live_tup),1),1) FROM pg_stat_user_tables").Scan(&tableBloat)
	m.DatabaseSpecific["table_bloat_pct"] = tableBloat

	m.CostMs = time.Since(start).Milliseconds()
	return m, nil
}

func (d *PostgresDriver) CreateDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf(`CREATE DATABASE "%s"`, name))
	return err
}
func (d *PostgresDriver) DropDatabase(name string) error {
	_, err := d.db.Exec(fmt.Sprintf(`DROP DATABASE "%s"`, name))
	return err
}
func (d *PostgresDriver) CreateUser(username, password string) error {
	_, err := d.db.Exec(fmt.Sprintf(`CREATE USER "%s" WITH PASSWORD '%s'`, username, password))
	return err
}
func (d *PostgresDriver) DropUser(username string) error {
	_, err := d.db.Exec(fmt.Sprintf(`DROP USER "%s"`, username))
	return err
}
func (d *PostgresDriver) GrantPrivileges(username, database string, privileges []string) error {
	privs := "ALL PRIVILEGES"
	if len(privileges) > 0 { privs = strings.Join(privileges, ", ") }
	_, err := d.db.Exec(fmt.Sprintf(`GRANT %s ON ALL TABLES IN SCHEMA public TO "%s"`, privs, username))
	return err
}

func (d *PostgresDriver) GetUserPrivileges(username string) ([]PrivilegeEntry, error) {
	var dbName string
	d.db.QueryRow("SELECT current_database()").Scan(&dbName)

	privMap := make(map[string]*PrivilegeEntry)

	// 1. Table privileges
	rows, err := d.db.Query("SELECT table_schema, table_name, privilege_type, is_grantable FROM information_schema.table_privileges WHERE grantee=$1", username)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var schema, table, priv, grantable string
			if rows.Scan(&schema, &table, &priv, &grantable) == nil {
				key := "T:" + dbName + "." + schema + "." + table
				if _, ok := privMap[key]; !ok {
					privMap[key] = &PrivilegeEntry{Database: dbName, Schema: schema, ObjectType: "TABLE", ObjectName: table}
				}
				privMap[key].Privileges = append(privMap[key].Privileges, priv)
				if grantable == "YES" { privMap[key].Grantable = true }
			}
		}
	}

	// 2. Database-level privileges (CONNECT, CREATE, TEMP)
	dbRows, err := d.db.Query(`SELECT datname, (aclexplode(datacl)).grantee::regrole::text, (aclexplode(datacl)).privilege_type
		FROM pg_catalog.pg_database`)
	if err == nil {
		defer dbRows.Close()
		for dbRows.Next() {
			var dn, grantee, priv string
			if dbRows.Scan(&dn, &grantee, &priv) == nil && grantee == username {
				key := "D:" + dn
				if _, ok := privMap[key]; !ok {
					privMap[key] = &PrivilegeEntry{Database: dn, ObjectType: "DATABASE", ObjectName: dn}
				}
				privMap[key].Privileges = append(privMap[key].Privileges, priv)
			}
		}
	}

	// 3. Schema-level privileges (USAGE, CREATE)
	schRows, err := d.db.Query(`SELECT schema_name, privilege_type FROM information_schema.schema_privileges WHERE grantee=$1`, username)
	if err == nil {
		defer schRows.Close()
		for schRows.Next() {
			var sn, priv string
			if schRows.Scan(&sn, &priv) == nil {
				key := "S:" + dbName + "." + sn
				if _, ok := privMap[key]; !ok {
					privMap[key] = &PrivilegeEntry{Database: dbName, Schema: sn, ObjectType: "SCHEMA", ObjectName: sn}
				}
				privMap[key].Privileges = append(privMap[key].Privileges, priv)
			}
		}
	}

	var result []PrivilegeEntry
	for _, v := range privMap { result = append(result, *v) }
	if result == nil { result = []PrivilegeEntry{} }
	return result, nil
}

func (d *PostgresDriver) GetUserRoles(username string) ([]string, error) {
	rows, err := d.db.Query("SELECT r.rolname FROM pg_user u JOIN pg_auth_members m ON u.usesysid=m.member JOIN pg_roles r ON m.roleid=r.oid WHERE u.usename=$1", username)
	if err != nil { return []string{}, nil }
	defer rows.Close()
	var roles []string
	for rows.Next() { var r string; if rows.Scan(&r) == nil { roles = append(roles, r) } }
	if roles == nil { roles = []string{} }
	return roles, nil
}

func (d *PostgresDriver) ApplyPrivilegeChanges(username string, changes []PrivilegeDelta) (*ChangeResult, error) {
	result := &ChangeResult{}

	// valid privileges per object type
	dbPrivs := map[string]bool{"CONNECT": true, "CREATE": true, "TEMP": true}
	schPrivs := map[string]bool{"USAGE": true, "CREATE": true}

	for _, ch := range changes {
		// Filter/expand privileges to only valid ones for this object type
		grantList := ch.Grant
		revokeList := ch.Revoke
		filterValid := func(list []string, valid map[string]bool) []string {
			var out []string
			for _, p := range list {
				if p == "ALL" { for k := range valid { out = append(out, k) } } else if valid[p] { out = append(out, p) }
			}
			return out
		}
		switch {
		case ch.ObjectType == "DATABASE":
			grantList = filterValid(grantList, dbPrivs)
			revokeList = filterValid(revokeList, dbPrivs)
		case ch.ObjectType == "SCHEMA":
			grantList = filterValid(grantList, schPrivs)
			revokeList = filterValid(revokeList, schPrivs)
		}

		if ch.Database == "*" && ch.ObjectName == "*" {
			// All databases — grant on all tables in all schemas (requires superuser)
			for _, p := range grantList {
				result.Statements = append(result.Statements, fmt.Sprintf(`GRANT %s ON ALL TABLES IN SCHEMA public TO "%s"`, p, username))
			}
			for _, p := range revokeList {
				result.Statements = append(result.Statements, fmt.Sprintf(`REVOKE %s ON ALL TABLES IN SCHEMA public FROM "%s"`, p, username))
			}
		} else if ch.ObjectType == "DATABASE" {
			// Database-level — CONNECT, CREATE
			for _, p := range grantList {
				result.Statements = append(result.Statements, fmt.Sprintf(`GRANT %s ON DATABASE "%s" TO "%s"`, p, ch.Database, username))
			}
			for _, p := range revokeList {
				result.Statements = append(result.Statements, fmt.Sprintf(`REVOKE %s ON DATABASE "%s" FROM "%s"`, p, ch.Database, username))
			}
		} else if ch.ObjectName == "*" {
			// Schema-level — USAGE, CREATE on schema
			for _, p := range grantList {
				result.Statements = append(result.Statements, fmt.Sprintf(`GRANT %s ON ALL TABLES IN SCHEMA "%s" TO "%s"`, p, ch.Database, username))
			}
			for _, p := range revokeList {
				result.Statements = append(result.Statements, fmt.Sprintf(`REVOKE %s ON ALL TABLES IN SCHEMA "%s" FROM "%s"`, p, ch.Database, username))
			}
		} else {
			// Table-level
			obj := fmt.Sprintf(`"%s"."%s"`, ch.Database, ch.ObjectName)
			for _, p := range grantList {
				result.Statements = append(result.Statements, fmt.Sprintf(`GRANT %s ON %s TO "%s"`, p, obj, username))
			}
			for _, p := range revokeList {
				result.Statements = append(result.Statements, fmt.Sprintf(`REVOKE %s ON %s FROM "%s"`, p, obj, username))
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

func (d *PostgresDriver) DetectCapability() (*CapabilitySet, error) {
	var v string
	d.db.QueryRow("SELECT version()").Scan(&v)
	return DetectCapability("postgres", v), nil
}
func (d *PostgresDriver) ListRoles() ([]RoleInfo, error) {
	rows, err := d.db.Query("SELECT rolname, rolsuper, rolcanlogin, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls, rolinherit, rolconnlimit::text, rolvaliduntil::text FROM pg_authid ORDER BY rolname")
	if err != nil { return []RoleInfo{}, nil }
	defer rows.Close()
	var result []RoleInfo
	roleMap := make(map[string]*RoleInfo)
	sysNames := map[string]bool{"pg_signal_backend": true, "pg_read_all_data": true, "pg_write_all_data": true, "pg_monitor": true, "postgres": true}
	for rows.Next() {
		var n string; var super, login, createdb, createrole, replication, bypassrls, inherit bool; var connLimit, validUntil *string
		if rows.Scan(&n, &super, &login, &createdb, &createrole, &replication, &bypassrls, &inherit, &connLimit, &validUntil) == nil {
			ri := &RoleInfo{Name: n, IsSystem: sysNames[n] || strings.HasPrefix(n, "pg_")}
			if login { ri.CanLogin = "true" }
			if super { ri.IsSuperuser = "true" }
			if createdb { ri.CanCreatedb = "true" }
			if createrole { ri.CanCreaterole = "true" }
			if replication { ri.CanReplication = "true" }
			if bypassrls { ri.CanBypassrls = "true" }
			if inherit { ri.CanInherit = "true" } else { ri.CanInherit = "false" }
			if connLimit != nil { ri.ConnLimit = *connLimit }
			if validUntil != nil { ri.ValidUntil = *validUntil }
			roleMap[n] = ri
		}
	}
	// Populate members
	memRows, err := d.db.Query("SELECT r.rolname, m.rolname FROM pg_auth_members am JOIN pg_roles r ON am.roleid=r.oid JOIN pg_roles m ON am.member=m.oid")
	if err == nil && memRows != nil {
		defer memRows.Close()
		for memRows.Next() { var role, member string; if memRows.Scan(&role, &member) == nil { if r, ok := roleMap[role]; ok { r.Members = append(r.Members, member) } } }
	}
	for _, v := range roleMap { result = append(result, *v) }
	if result == nil { result = []RoleInfo{} }
	return result, nil
}
func (d *PostgresDriver) CreateRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf(`CREATE ROLE "%s"`, name))
	return err
}
func (d *PostgresDriver) DropRole(name string) error {
	_, err := d.db.Exec(fmt.Sprintf(`DROP ROLE "%s"`, name))
	return err
}
func (d *PostgresDriver) AddRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf(`GRANT "%s" TO "%s"`, role, member))
	return err
}
func (d *PostgresDriver) RemoveRoleMember(role, member string) error {
	_, err := d.db.Exec(fmt.Sprintf(`REVOKE "%s" FROM "%s"`, role, member))
	return err
}
func (d *PostgresDriver) AlterUserPassword(username, newPassword string) error {
	_, err := d.db.Exec(fmt.Sprintf(`ALTER USER "%s" WITH PASSWORD '%s'`, username, newPassword))
	return err
}
func (d *PostgresDriver) AlterUserLock(username string, lock bool) error {
	if lock { _, err := d.db.Exec(fmt.Sprintf(`ALTER USER "%s" WITH NOLOGIN`, username)); return err }
	_, err := d.db.Exec(fmt.Sprintf(`ALTER USER "%s" WITH LOGIN`, username))
	return err
}
func (d *PostgresDriver) AlterUserRename(oldName, newName string) error {
	return fmt.Errorf("rename not supported for PostgresDriver")
}
func (d *PostgresDriver) AlterUserDefaultSchema(username, schema string) error {
	return fmt.Errorf("default schema not supported for PostgresDriver")
}

func (d *PostgresDriver) GetRolePrivileges(roleName string) ([]PrivilegeEntry, error) {
	return d.GetUserPrivileges(roleName)
}

func (d *PostgresDriver) AlterRoleAttribute(roleName, attribute, value string) error {
	var sql string
	switch attribute {
	case "password":
		if value == "" { sql = fmt.Sprintf(`ALTER ROLE "%s" PASSWORD NULL`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" PASSWORD '%s'`, roleName, value) }
	case "login":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" LOGIN`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOLOGIN`, roleName) }
	case "superuser":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" SUPERUSER`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOSUPERUSER`, roleName) }
	case "createdb":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" CREATEDB`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOCREATEDB`, roleName) }
	case "createrole":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" CREATEROLE`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOCREATEROLE`, roleName) }
	case "replication":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" REPLICATION`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOREPLICATION`, roleName) }
	case "bypassrls":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" BYPASSRLS`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOBYPASSRLS`, roleName) }
	case "inherit":
		if value == "true" { sql = fmt.Sprintf(`ALTER ROLE "%s" INHERIT`, roleName) } else { sql = fmt.Sprintf(`ALTER ROLE "%s" NOINHERIT`, roleName) }
	case "valid_until":
		sql = fmt.Sprintf(`ALTER ROLE "%s" VALID UNTIL '%s'`, roleName, value)
	case "conn_limit":
		sql = fmt.Sprintf(`ALTER ROLE "%s" CONNECTION LIMIT %s`, roleName, value)
	default:
		return fmt.Errorf("unknown attribute: %s", attribute)
	}
	_, err := d.db.Exec(sql)
	return err
}

func (d *PostgresDriver) GetRoleMemberships(roleName string) ([]string, error) {
	rows, err := d.db.Query(`SELECT g.rolname FROM pg_auth_members m JOIN pg_roles u ON m.member=u.oid JOIN pg_roles g ON m.roleid=g.oid WHERE u.rolname=$1`, roleName)
	if err != nil { return []string{}, nil }
	defer rows.Close()
	var result []string
	for rows.Next() { var r string; if rows.Scan(&r) == nil { result = append(result, r) } }
	if result == nil { result = []string{} }
	return result, nil
}

// GetParentRoles returns direct parent roles of roleName with attributes and admin_option
func (d *PostgresDriver) GetParentRoles(roleName string) ([]ParentRoleInfo, error) {
	rows, err := d.db.Query(`SELECT
		g.rolname, g.rolsuper, g.rolcanlogin, g.rolcreatedb,
		g.rolcreaterole, g.rolreplication, g.rolbypassrls, m.admin_option
	FROM pg_auth_members m
	JOIN pg_roles g ON m.roleid = g.oid
	WHERE m.member = $1::regrole
	ORDER BY g.rolname`, roleName)
	if err != nil { return []ParentRoleInfo{}, nil }
	defer rows.Close()
	var result []ParentRoleInfo
	for rows.Next() {
		var p ParentRoleInfo
		if rows.Scan(&p.Name, &p.IsSuperuser, &p.CanLogin, &p.CanCreatedb,
			&p.CanCreaterole, &p.CanReplication, &p.CanBypassrls, &p.AdminOption) == nil {
			result = append(result, p)
		}
	}
	if result == nil { result = []ParentRoleInfo{} }
	return result, nil
}

// GetRoleMembers returns direct child roles (members) of roleName with attributes and admin_option
func (d *PostgresDriver) GetRoleMembers(roleName string) ([]MemberRoleInfo, error) {
	rows, err := d.db.Query(`SELECT
		u.rolname, u.rolsuper, u.rolcanlogin, u.rolcreatedb,
		u.rolcreaterole, u.rolreplication, u.rolbypassrls, m.admin_option
	FROM pg_auth_members m
	JOIN pg_roles u ON m.member = u.oid
	WHERE m.roleid = $1::regrole
	ORDER BY u.rolname`, roleName)
	if err != nil { return []MemberRoleInfo{}, nil }
	defer rows.Close()
	var result []MemberRoleInfo
	for rows.Next() {
		var m MemberRoleInfo
		if rows.Scan(&m.Name, &m.IsSuperuser, &m.CanLogin, &m.CanCreatedb,
			&m.CanCreaterole, &m.CanReplication, &m.CanBypassrls, &m.AdminOption) == nil {
			result = append(result, m)
		}
	}
	if result == nil { result = []MemberRoleInfo{} }
	return result, nil
}

// GetRoleInherit returns the rolinherit flag for the given role
func (d *PostgresDriver) GetRoleInherit(roleName string) (bool, error) {
	var inherit bool
	if err := d.db.QueryRow("SELECT rolinherit FROM pg_roles WHERE rolname=$1", roleName).Scan(&inherit); err != nil {
		return true, nil
	}
	return inherit, nil
}

// ─── System SQL Dialect Methods ──────────────────────────────────────────
func (d *PostgresDriver) SQLFormatTime(col string) string {
	return fmt.Sprintf("TO_CHAR(%s, \x27YYYY-MM-DD HH24:MI:SS\x27)", col)
}
func (d *PostgresDriver) SQLConcat(parts ...string) string {
	return strings.Join(parts, " || ")
}
func (d *PostgresDriver) SQLIsNull(col, defaultVal string) string {
	return fmt.Sprintf("COALESCE(%s, %s)", col, defaultVal)
}
func (d *PostgresDriver) SQLCurrentTimestamp() string { return "NOW()" }
func (d *PostgresDriver) SQLQuoteIdent(name string) string { return `"` + name + `"` }

