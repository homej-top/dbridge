package service

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service/drivers"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"gorm.io/gorm"
)

type CompareService struct {
	db       *gorm.DB
	registry *SyncStrategyRegistry
}

func NewCompareService(db *gorm.DB) *CompareService {
	return &CompareService{
		db:       db,
		registry: NewSyncStrategyRegistry(),
	}
}

type CompareObject struct {
	Name   string `json:"name"`
	Type   string `json:"type"`   // "table" or "view"
	Status string `json:"status"` // "both", "source_only", "target_only"
}

type TableDataResult struct {
	Columns  []string        `json:"columns"`
	Rows     [][]interface{} `json:"rows"`
	Total    int64           `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type ColumnDetail struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Length   string `json:"length"`
	Nullable string `json:"nullable"`
	Default  string `json:"default"`
	Comment  string `json:"comment"`
	Key      string `json:"key"`
}

type TableStructureResult struct {
	Columns []ColumnDetail `json:"columns"`
	DDL     string         `json:"ddl"`
}

func (s *CompareService) connectToDS(dsID string, database ...string) (*sql.DB, repository.DataSource, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return nil, ds, fmt.Errorf("data source not found: %s", dsID)
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, ds, fmt.Errorf("failed to decrypt password")
	}

	if !isSupportedDBType(ds.Type) {
		return nil, ds, fmt.Errorf("unsupported database type: %s", ds.Type)
	}

	// Override database if provided
	if len(database) > 0 && database[0] != "" {
		ds.Database = database[0]
	}

	conn, err := openDBConn(ds, pwd)
	if err != nil {
		return nil, ds, err
	}

	return conn, ds, nil
}

// connectDriver creates a DatabaseDriver through the global pool manager.
func (s *CompareService) connectDriver(dsID string) (drivers.DatabaseDriver, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found: %s", dsID)
	}
	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password")
	}
	return ConnectDriver(context.Background(), ds, pwd, "")
}

// listObjects returns a map of object name → type ("table" or "view") using the driver's ListObjects.
func (s *CompareService) listObjects(dsID, schema string) (map[string]string, error) {
	return s.listObjectsWithDB(dsID, schema, "")
}

// listObjectsWithDB is like listObjects but allows overriding the database (for PG/SQL Server).
func (s *CompareService) listObjectsWithDB(dsID, schema, database string) (map[string]string, error) {
	driver, err := s.connectDriverWithDB(dsID, database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	info, err := driver.ListObjects(schema)
	if err != nil {
		return nil, err
	}
	objects := make(map[string]string)
	for _, t := range info.Tables {
		objects[t.Name] = "table"
	}
	for _, v := range info.Views {
		objects[v.Name] = "view"
	}
	return objects, nil
}

func (s *CompareService) connectDriverWithDB(dsID, database string) (drivers.DatabaseDriver, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found: %s", dsID)
	}
	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password")
	}
	return ConnectDriver(context.Background(), ds, pwd, database)
}

func (s *CompareService) CompareStructures(sourceDSID, sourceSchema, sourceDatabase, targetDSID, targetSchema, targetDatabase string) ([]CompareObject, error) {
	sourceConn, sourceDS, err := s.connectToDS(sourceDSID)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	defer sourceConn.Close()

	targetConn, targetDS, err := s.connectToDS(targetDSID)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	defer targetConn.Close()

	// Use explicit database override if provided (for PG/SQL Server with 3-level structure)
	srcDB := sourceDatabase
	if srcDB == "" {
		srcDB = sourceDS.Database
	}
	tgtDB := targetDatabase
	if tgtDB == "" {
		tgtDB = targetDS.Database
	}

	sourceSchemaName := sourceSchema
	if sourceSchemaName == "" {
		sourceSchemaName = sourceDS.Database
	}
	targetSchemaName := targetSchema
	if targetSchemaName == "" {
		targetSchemaName = targetDS.Database
	}

	// Apply database override for schema resolution
	if srcDB != sourceDS.Database {
		sourceSchemaName = sourceSchema
		if sourceSchemaName == "" {
			sourceSchemaName = srcDB
		}
	}
	if tgtDB != targetDS.Database {
		targetSchemaName = targetSchema
		if targetSchemaName == "" {
			targetSchemaName = tgtDB
		}
	}

	// Pass database context via driver connection
	sourceObjects, err := s.listObjectsWithDB(sourceDSID, sourceSchemaName, srcDB)
	if err != nil {
		return nil, fmt.Errorf("source objects: %w", err)
	}

	targetObjects, err := s.listObjectsWithDB(targetDSID, targetSchemaName, tgtDB)
	if err != nil {
		return nil, fmt.Errorf("target objects: %w", err)
	}

	resultMap := make(map[string]CompareObject)

	for name, objType := range sourceObjects {
		resultMap[name] = CompareObject{Name: name, Type: objType, Status: "source_only"}
	}
	for name, objType := range targetObjects {
		if existing, ok := resultMap[name]; ok {
			existing.Status = "both"
			resultMap[name] = existing
		} else {
			resultMap[name] = CompareObject{Name: name, Type: objType, Status: "target_only"}
		}
	}

	result := make([]CompareObject, 0, len(resultMap))
	for _, obj := range resultMap {
		result = append(result, obj)
	}

	return result, nil
}

func (s *CompareService) GetTableData(dsID, schemaName, tableName string, page, pageSize int) (*TableDataResult, error) {
	conn, ds, err := s.connectToDS(dsID)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 50
	}

	schema := schemaName
	if schema == "" {
		schema = ds.Database
	}

	var fullTable string
	isOracle := false
	switch ds.Type {
	case "mysql", "postgres", "sqlserver":
		fullTable = drivers.QuoteTableName(schema, tableName, drivers.DialectOf(ds.Type))
	case "oracle":
		isOracle = true
		schema = strings.ToUpper(schema)
		fullTable = drivers.QuoteTableName(schema, tableName, "oracle")
	}

	var total int64
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM %s", fullTable)
	if err := conn.QueryRow(countSQL).Scan(&total); err != nil {
		return nil, fmt.Errorf("count failed: %w", err)
	}

	offset := (page - 1) * pageSize
	var dataSQL string
	switch {
	case isOracle:
		dataSQL = fmt.Sprintf("SELECT * FROM %s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", fullTable, offset, pageSize)
	case ds.Type == "sqlserver":
		dataSQL = fmt.Sprintf("SELECT * FROM %s ORDER BY (SELECT NULL) OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", fullTable, offset, pageSize)
	default:
		dataSQL = fmt.Sprintf("SELECT * FROM %s LIMIT %d OFFSET %d", fullTable, pageSize, offset)
	}
	rows, err := conn.Query(dataSQL)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	colNames, err := rows.Columns()
	if err != nil {
		return nil, err
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
			if b, ok := v.([]byte); ok {
				row[i] = string(b)
			} else {
				row[i] = v
			}
		}
		resultRows = append(resultRows, row)
	}
	if resultRows == nil {
		resultRows = make([][]interface{}, 0)
	}

	return &TableDataResult{
		Columns:  colNames,
		Rows:     resultRows,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

func (s *CompareService) GetTableStructure(dsID, schemaName, tableName string) (*TableStructureResult, error) {
	driver, err := s.connectDriver(dsID)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	schema := schemaName
	if schema == "" {
		schema = "" // handled by driver
	}

	result := &TableStructureResult{}

	cols, err := driver.GetColumns(schema, tableName)
	if err != nil {
		return nil, err
	}
	for _, c := range cols {
		result.Columns = append(result.Columns, ColumnDetail{
			Name: c.Name, Type: c.Type, Length: c.Length,
			Nullable: c.Nullable, Default: c.Default, Comment: c.Comment, Key: c.Key,
		})
	}

	ddl, err := driver.GetDDL(schema, tableName)
	if err != nil {
		result.DDL = fmt.Sprintf("-- Failed to get DDL: %s", err.Error())
	} else {
		result.DDL = ddl
	}

	return result, nil
}

type SyncDataRequest struct {
	SourceDS       string          `json:"source_ds" binding:"required"`
	SourceSchema   string          `json:"source_schema"`
	SourceDatabase string          `json:"source_database"`
	TargetDS       string          `json:"target_ds" binding:"required"`
	TargetSchema   string          `json:"target_schema"`
	TargetDatabase string          `json:"target_database"`
	Table          string          `json:"table" binding:"required"`
	Options        DataSyncOptions `json:"options"`
}

type SyncStructureRequest struct {
	SourceDS       string `json:"source_ds" binding:"required"`
	SourceSchema   string `json:"source_schema"`
	SourceDatabase string `json:"source_database"`
	TargetDS       string `json:"target_ds" binding:"required"`
	TargetSchema   string `json:"target_schema"`
	TargetDatabase string `json:"target_database"`
	Table          string `json:"table" binding:"required"`
	Action         string `json:"action" binding:"required"`
	DryRun         bool   `json:"dry_run"`
	OverrideDDL    string `json:"override_ddl"`
}

type SyncStructureResult struct {
	DDL     string `json:"ddl"`
	Success bool   `json:"success"`
	Message string `json:"message"`
}

type DataSyncOptions struct {
	TruncateTarget bool             `json:"truncate_target"`
	SyncID         bool             `json:"sync_id"`
	Mode           string           `json:"mode"`
	CheckFields    []string         `json:"check_fields"`
	SyncColumns    []string         `json:"sync_columns"`
	SelectedRows   []map[string]any `json:"selected_rows"`
	Transactional  bool             `json:"transactional"` // when true, all operations run in a single transaction
}

type DataSyncRequest struct {
	SourceDS     string          `json:"source_ds" binding:"required"`
	SourceSchema string          `json:"source_schema"`
	TargetDS     string          `json:"target_ds" binding:"required"`
	TargetSchema string          `json:"target_schema"`
	Table        string          `json:"table" binding:"required"`
	Options      DataSyncOptions `json:"options"`
}

type DataSyncResult struct {
	Success     bool     `json:"success"`
	TotalRows   int      `json:"total_rows"`
	SyncedRows  int      `json:"synced_rows"`
	SkippedRows int      `json:"skipped_rows"`
	Errors      []string `json:"errors"`
}

func (s *CompareService) SyncStructure(req SyncStructureRequest) (*SyncStructureResult, error) {
	sourceConn, sourceDS, err := s.connectToDS(req.SourceDS, req.SourceDatabase)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	defer sourceConn.Close()

	targetConn, targetDS, err := s.connectToDS(req.TargetDS, req.TargetDatabase)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	defer targetConn.Close()

	sourceSchema := req.SourceSchema
	if sourceSchema == "" {
		sourceSchema = sourceDS.Database
	}
	targetSchema := req.TargetSchema
	if targetSchema == "" {
		targetSchema = targetDS.Database
	}

	if !req.DryRun && req.OverrideDDL != "" {
		ddl := req.OverrideDDL
		// Strip COMMENT ON for Oracle — go-ora only executes first statement
		if targetDS.Type == "oracle" {
			ddl = regexp.MustCompile(`\nCOMMENT ON (TABLE|COLUMN) [^;]+;`).ReplaceAllString(ddl, "")
		}
		if targetDS.Type == "mysql" && targetDS.Database == "" && targetSchema != "" {
			if err := validateSchemaName(targetSchema); err != nil {
				return &SyncStructureResult{DDL: req.OverrideDDL, Success: false, Message: fmt.Sprintf("Schema 名称验证失败: %s", err.Error())}, nil
			}
			safeSchema := strings.ReplaceAll(targetSchema, "`", "``")
			if _, err := targetConn.Exec("USE `" + safeSchema + "`"); err != nil {
				return &SyncStructureResult{DDL: req.OverrideDDL, Success: false, Message: fmt.Sprintf("切换 Schema 失败: %s", err.Error())}, nil
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		_, execErr := targetConn.ExecContext(ctx, ddl)
		if execErr != nil {
			return &SyncStructureResult{DDL: req.OverrideDDL, Success: false, Message: fmt.Sprintf("DDL 执行失败: %s", execErr.Error())}, nil
		}
		return &SyncStructureResult{DDL: req.OverrideDDL, Success: true, Message: "结构同步成功 (自定义 DDL)"}, nil
	}

	switch req.Action {
	case "create":
		return s.syncCreateTable(sourceConn, sourceDS, sourceSchema, req.SourceDatabase, targetConn, targetDS, targetSchema, req.TargetDatabase, req.Table, req.DryRun)
	case "alter":
		return s.syncAlterTable(sourceConn, sourceDS, sourceSchema, req.SourceDatabase, targetConn, targetDS, targetSchema, req.TargetDatabase, req.Table, req.DryRun)
	default:
		return nil, fmt.Errorf("unsupported action: %s", req.Action)
	}
}

func (s *CompareService) syncCreateTable(sourceConn *sql.DB, sourceDS repository.DataSource, sourceSchema, sourceDatabase string, targetConn *sql.DB, targetDS repository.DataSource, targetSchema, targetDatabase string, table string, dryRun bool) (*SyncStructureResult, error) {
	if sourceDS.Type == targetDS.Type {
		var ddl string
		switch sourceDS.Type {
		case "mysql":
			var name string
			var err error
			if sourceSchema != "" {
				err = sourceConn.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`.`%s`", sourceSchema, table)).Scan(&name, &ddl)
			} else {
				err = sourceConn.QueryRow(fmt.Sprintf("SHOW CREATE TABLE `%s`", table)).Scan(&name, &ddl)
			}
			if err != nil {
				return nil, fmt.Errorf("get source DDL failed: %w", err)
			}
		case "postgres":
			driver, err := s.connectDriverWithDB(sourceDS.ID, sourceDatabase)
			if err != nil {
				return nil, fmt.Errorf("connect source: %w", err)
			}
			defer driver.Close()
			ddl, err = driver.GetDDL(sourceSchema, table)
			if err != nil {
				return nil, fmt.Errorf("get source DDL failed: %w", err)
			}
			ddl = strings.Replace(ddl, fmt.Sprintf(`"%s"."%s"`, sourceSchema, table), fmt.Sprintf(`"%s"."%s"`, targetSchema, table), 1)
		case "oracle":
			var ddlObj sql.NullString
			err := sourceConn.QueryRow(fmt.Sprintf("SELECT DBMS_METADATA.GET_DDL('TABLE','%s','%s') FROM DUAL",
				strings.ReplaceAll(table, "'", "''"),
				strings.ReplaceAll(strings.ToUpper(sourceSchema), "'", "''"))).Scan(&ddlObj)
			if err != nil || !ddlObj.Valid {
				return nil, fmt.Errorf("get source DDL failed: %w", err)
			}
			ddl = ddlObj.String
			// Strip TABLESPACE clause (target user may not have quota)
			ddl = regexp.MustCompile(`(?i)\s+TABLESPACE\s+"[^"]*"`).ReplaceAllString(ddl, "")
			// Strip STORAGE clauses
			ddl = regexp.MustCompile(`(?i)\s+STORAGE\s*\([^)]*\)`).ReplaceAllString(ddl, "")
			// Strip SEGMENT CREATION clause
			ddl = regexp.MustCompile(`(?i)\s+SEGMENT CREATION\s+(IMMEDIATE|DEFERRED)`).ReplaceAllString(ddl, "")
			// Strip COMPUTE STATISTICS
			ddl = regexp.MustCompile(`(?i)\s+COMPUTE STATISTICS`).ReplaceAllString(ddl, "")
			// Strip PCTFREE/PCTUSED/PCTINCREASE clauses
			ddl = regexp.MustCompile(`(?i)\s+PCT(FREE|USED|INCREASE)\s+\d+`).ReplaceAllString(ddl, "")
			// Strip INITRANS/MAXTRANS clauses
			ddl = regexp.MustCompile(`(?i)\s+(INITRANS|MAXTRANS)\s+\d+`).ReplaceAllString(ddl, "")
			// Replace source schema with target schema
			ddl = strings.Replace(ddl, fmt.Sprintf(`"%s"`, strings.ToUpper(sourceSchema)), fmt.Sprintf(`"%s"`, strings.ToUpper(targetSchema)), -1)
		case "sqlserver":
			// SQL Server: reconstruct DDL from column definitions with PK and IDENTITY
			rows, err := sourceConn.Query(`SELECT
				c.COLUMN_NAME, c.DATA_TYPE, COALESCE(CAST(c.CHARACTER_MAXIMUM_LENGTH AS VARCHAR),''),
				c.IS_NULLABLE, COALESCE(c.COLUMN_DEFAULT,''),
				CASE WHEN ic.object_id IS NOT NULL THEN 1 ELSE 0 END AS is_identity,
				COALESCE(CAST(ic.seed_value AS VARCHAR),''), COALESCE(CAST(ic.increment_value AS VARCHAR),''),
				CASE WHEN pk.COLUMN_NAME IS NOT NULL THEN 1 ELSE 0 END AS is_pk
			FROM INFORMATION_SCHEMA.COLUMNS c
			LEFT JOIN sys.identity_columns ic ON OBJECT_ID(QUOTENAME(c.TABLE_SCHEMA)+'.'+QUOTENAME(c.TABLE_NAME)) = ic.object_id AND c.COLUMN_NAME = ic.name COLLATE SQL_Latin1_General_CP1_CI_AS
			LEFT JOIN (
				SELECT ku.TABLE_SCHEMA, ku.TABLE_NAME, ku.COLUMN_NAME
				FROM INFORMATION_SCHEMA.TABLE_CONSTRAINTS tc
				JOIN INFORMATION_SCHEMA.KEY_COLUMN_USAGE ku ON tc.CONSTRAINT_NAME = ku.CONSTRAINT_NAME
				WHERE tc.CONSTRAINT_TYPE = 'PRIMARY KEY'
			) pk ON c.TABLE_SCHEMA = pk.TABLE_SCHEMA AND c.TABLE_NAME = pk.TABLE_NAME AND c.COLUMN_NAME = pk.COLUMN_NAME
			WHERE c.TABLE_SCHEMA = @p1 AND c.TABLE_NAME = @p2
			ORDER BY c.ORDINAL_POSITION`, sourceSchema, table)
			if err != nil {
				return nil, fmt.Errorf("get columns failed: %w", err)
			}
			defer rows.Close()
			var colDefs []string
			var pkCols []string
			for rows.Next() {
				var name, dataType, charLen, nullable, defaultVal, seedVal, incVal string
				var isIdentity, isPK int
				if err := rows.Scan(&name, &dataType, &charLen, &nullable, &defaultVal, &isIdentity, &seedVal, &incVal, &isPK); err != nil {
					continue
				}
				colType := dataType
				if charLen != "" && charLen != "0" {
					colType = fmt.Sprintf("%s(%s)", dataType, charLen)
				}
				def := fmt.Sprintf("  [%s] %s", name, colType)
				if isIdentity == 1 {
					if seedVal != "" && incVal != "" {
						def += fmt.Sprintf(" IDENTITY(%s,%s)", seedVal, incVal)
					} else {
						def += " IDENTITY(1,1)"
					}
				}
				if nullable == "NO" {
					def += " NOT NULL"
				} else {
					def += " NULL"
				}
				if defaultVal != "" {
					def += " DEFAULT " + defaultVal
				}
				colDefs = append(colDefs, def)
				if isPK == 1 {
					pkCols = append(pkCols, name)
				}
			}
			if len(colDefs) == 0 {
				return nil, fmt.Errorf("table not found: %s.%s", sourceSchema, table)
			}
			if len(pkCols) > 0 {
				colDefs = append(colDefs, fmt.Sprintf("  CONSTRAINT [PK_%s] PRIMARY KEY ([%s])", table, strings.Join(pkCols, "], [")))
			}
			ddl = fmt.Sprintf("CREATE TABLE [%s].[%s] (\n%s\n);", targetSchema, table, strings.Join(colDefs, ",\n"))
		}

		if dryRun {
			return &SyncStructureResult{DDL: ddl, Success: true, Message: "DDL 预览 (CREATE TABLE)"}, nil
		}

		// Strip COMMENT ON for Oracle — go-ora only executes first statement
		if targetDS.Type == "oracle" {
			ddl = regexp.MustCompile(`\nCOMMENT ON (TABLE|COLUMN) [^;]+;`).ReplaceAllString(ddl, "")
		}

		if targetDS.Type == "mysql" && targetDS.Database == "" && targetSchema != "" {
			if err := validateSchemaName(targetSchema); err != nil {
				return &SyncStructureResult{DDL: ddl, Success: false, Message: fmt.Sprintf("Schema 名称验证失败: %s", err.Error())}, nil
			}
			safeSchema := strings.ReplaceAll(targetSchema, "`", "``")
			if _, err := targetConn.Exec("USE `" + safeSchema + "`"); err != nil {
				return &SyncStructureResult{DDL: ddl, Success: false, Message: fmt.Sprintf("切换 Schema 失败: %s", err.Error())}, nil
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		_, err := targetConn.ExecContext(ctx, ddl)
		if err != nil {
			return &SyncStructureResult{DDL: ddl, Success: false, Message: fmt.Sprintf("DDL 执行失败: %s", err.Error())}, nil
		}
		return &SyncStructureResult{DDL: ddl, Success: true, Message: "结构同步成功 (CREATE TABLE)"}, nil
	}

	sourceStruct, err := s.GetTableStructure(sourceDS.ID, sourceSchema, table)
	if err != nil {
		return nil, fmt.Errorf("get source structure failed: %w", err)
	}

	ddl := s.generateCrossDBCreateDDL(sourceStruct.Columns, table, targetDS.Type, targetSchema)

	if dryRun {
		return &SyncStructureResult{DDL: ddl, Success: true, Message: "DDL 预览 (CREATE TABLE, 跨库类型转换)"}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	_, execErr := targetConn.ExecContext(ctx, ddl)
	if execErr != nil {
		return &SyncStructureResult{DDL: ddl, Success: false, Message: fmt.Sprintf("DDL 执行失败: %s", execErr.Error())}, nil
	}
	return &SyncStructureResult{DDL: ddl, Success: true, Message: "结构同步成功 (CREATE TABLE, 跨库类型转换)"}, nil
}

func (s *CompareService) syncAlterTable(sourceConn *sql.DB, sourceDS repository.DataSource, sourceSchema, sourceDatabase string, targetConn *sql.DB, targetDS repository.DataSource, targetSchema, targetDatabase string, table string, dryRun bool) (*SyncStructureResult, error) {
	var sourceCols, targetCols []ColumnDetail
	var err error

	// Source columns via driver (with database override for PG/MSSQL)
	srcDriver, err := s.connectDriverWithDB(sourceDS.ID, sourceDatabase)
	if err != nil {
		return nil, fmt.Errorf("connect source: %w", err)
	}
	defer srcDriver.Close()
	srcCols, err := srcDriver.GetColumns(sourceSchema, table)
	if err != nil {
		return nil, fmt.Errorf("get source columns: %w", err)
	}
	for _, c := range srcCols {
		sourceCols = append(sourceCols, ColumnDetail{
			Name: c.Name, Type: c.Type, Length: c.Length,
			Nullable: c.Nullable, Default: c.Default, Comment: c.Comment, Key: c.Key,
		})
	}

	// Target columns via driver (with database override for PG/MSSQL)
	tgtDriver, err := s.connectDriverWithDB(targetDS.ID, targetDatabase)
	if err != nil {
		return nil, fmt.Errorf("connect target: %w", err)
	}
	defer tgtDriver.Close()
	tgtCols, err := tgtDriver.GetColumns(targetSchema, table)
	if err != nil {
		return nil, fmt.Errorf("get target columns: %w", err)
	}
	for _, c := range tgtCols {
		targetCols = append(targetCols, ColumnDetail{
			Name: c.Name, Type: c.Type, Length: c.Length,
			Nullable: c.Nullable, Default: c.Default, Comment: c.Comment, Key: c.Key,
		})
	}

	targetColMap := make(map[string]ColumnDetail)
	for _, c := range targetCols {
		targetColMap[c.Name] = c
	}

	var ddlStatements []string

	for _, srcCol := range sourceCols {
		tgtCol, exists := targetColMap[srcCol.Name]
		if !exists {
			// Column exists in source but not target → ADD_COLUMN
			colDef := s.buildColumnDef(srcCol, targetDS.Type)
			var stmt string
			switch targetDS.Type {
			case "mysql":
				stmt = fmt.Sprintf("ALTER TABLE `%s`.`%s` ADD COLUMN %s", targetSchema, table, colDef)
			case "postgres":
				stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ADD COLUMN %s`, targetSchema, table, colDef)
			case "oracle":
				stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ADD (%s)`, strings.ToUpper(targetSchema), strings.ToUpper(table), colDef)
			case "sqlserver":
				stmt = fmt.Sprintf("ALTER TABLE [%s].[%s] ADD %s", targetSchema, table, colDef)
			}
			if stmt != "" {
				ddlStatements = append(ddlStatements, stmt)
			}
		} else {
			// Column exists in both — check for type/nullable/default differences
			typeChanged := srcCol.Type != tgtCol.Type || srcCol.Length != tgtCol.Length
			nullChanged := srcCol.Nullable != tgtCol.Nullable
			defaultChanged := srcCol.Default != tgtCol.Default
			if typeChanged || nullChanged || defaultChanged {
				colDef := s.buildColumnDef(srcCol, targetDS.Type)
				var stmt string
				switch targetDS.Type {
				case "mysql":
					stmt = fmt.Sprintf("ALTER TABLE `%s`.`%s` MODIFY COLUMN %s", targetSchema, table, colDef)
				case "postgres":
					if srcCol.Type != tgtCol.Type || srcCol.Length != tgtCol.Length {
						stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ALTER COLUMN "%s" TYPE %s`, targetSchema, table, srcCol.Name, s.mapPGType(srcCol.Type, srcCol.Length))
						ddlStatements = append(ddlStatements, stmt)
					}
					if srcCol.Nullable != tgtCol.Nullable {
						if srcCol.Nullable == "NO" {
							stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ALTER COLUMN "%s" SET NOT NULL`, targetSchema, table, srcCol.Name)
						} else {
							stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ALTER COLUMN "%s" DROP NOT NULL`, targetSchema, table, srcCol.Name)
						}
						ddlStatements = append(ddlStatements, stmt)
					}
					if srcCol.Default != tgtCol.Default && srcCol.Default != "" {
						stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" ALTER COLUMN "%s" SET DEFAULT %s`, targetSchema, table, srcCol.Name, srcCol.Default)
						ddlStatements = append(ddlStatements, stmt)
					}
					continue // Already appended individual statements
				case "oracle":
					stmt = fmt.Sprintf(`ALTER TABLE "%s"."%s" MODIFY (%s)`, strings.ToUpper(targetSchema), strings.ToUpper(table), colDef)
				case "sqlserver":
					stmt = fmt.Sprintf("ALTER TABLE [%s].[%s] ALTER COLUMN %s", targetSchema, table, colDef)
				}
				if stmt != "" {
					ddlStatements = append(ddlStatements, stmt)
				}
			}
		}
	}

	if len(ddlStatements) == 0 {
		return &SyncStructureResult{DDL: fmt.Sprintf("-- 无结构差异需要同步 (源列数:%d, 目标列数:%d)", len(sourceCols), len(targetCols)), Success: true, Message: "结构已一致，无需同步"}, nil
	}

	fullDDL := strings.Join(ddlStatements, ";\n") + ";"

	if dryRun {
		return &SyncStructureResult{DDL: fullDDL, Success: true, Message: fmt.Sprintf("DDL 预览 (ALTER TABLE, %d 条语句)", len(ddlStatements))}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	for _, stmt := range ddlStatements {
		_, err := targetConn.ExecContext(ctx, stmt)
		if err != nil {
			return &SyncStructureResult{DDL: fullDDL, Success: false, Message: fmt.Sprintf("DDL 执行失败: %s\n语句: %s", err.Error(), stmt)}, nil
		}
	}

	return &SyncStructureResult{DDL: fullDDL, Success: true, Message: fmt.Sprintf("结构同步成功 (ALTER TABLE, %d 条语句)", len(ddlStatements))}, nil
}

func (s *CompareService) buildColumnDef(col ColumnDetail, targetType string) string {
	switch targetType {
	case "mysql":
		def := fmt.Sprintf("`%s` %s", col.Name, col.Type)
		if col.Nullable == "NO" {
			def += " NOT NULL"
		}
		if col.Default != "" {
			def += fmt.Sprintf(" DEFAULT %s", col.Default)
		}
		if col.Comment != "" {
			def += fmt.Sprintf(" COMMENT '%s'", strings.ReplaceAll(col.Comment, "'", "\\'"))
		}
		return def
	case "postgres":
		typeStr := s.mapPGType(col.Type, col.Length)
		def := fmt.Sprintf(`"%s" %s`, col.Name, typeStr)
		if col.Nullable == "NO" {
			def += " NOT NULL"
		}
		if col.Default != "" {
			def += fmt.Sprintf(" DEFAULT %s", col.Default)
		}
		return def
	case "oracle":
		def := fmt.Sprintf(`"%s" %s`, col.Name, col.Type)
		if col.Length != "" && col.Length != "0" {
			def = fmt.Sprintf(`"%s" %s(%s)`, col.Name, col.Type, col.Length)
		}
		if col.Default != "" {
			def += fmt.Sprintf(" DEFAULT %s", col.Default)
		}
		if col.Nullable == "NO" {
			def += " NOT NULL"
		}
		return def
	case "sqlserver":
		def := fmt.Sprintf("[%s] %s", col.Name, col.Type)
		if col.Length != "" && col.Length != "0" {
			def = fmt.Sprintf("[%s] %s(%s)", col.Name, col.Type, col.Length)
		}
		if col.Nullable == "NO" {
			def += " NOT NULL"
		}
		if col.Default != "" {
			def += fmt.Sprintf(" DEFAULT %s", col.Default)
		}
		return def
	}
	return fmt.Sprintf(`"%s" %s`, col.Name, col.Type)
}

func (s *CompareService) mapPGType(srcType, length string) string {
	t := strings.ToLower(srcType)
	typeMap := map[string]string{
		"int": "integer", "integer": "integer", "bigint": "bigint", "smallint": "smallint",
		"tinyint": "smallint", "mediumint": "integer",
		"float": "real", "double": "double precision", "decimal": "numeric",
		"varchar": "varchar", "char": "character", "text": "text",
		"mediumtext": "text", "longtext": "text", "tinytext": "text",
		"datetime": "timestamp", "timestamp": "timestamp", "date": "date", "time": "time",
		"boolean": "boolean", "bool": "boolean", "bit": "bit",
		"blob": "bytea", "mediumblob": "bytea", "longblob": "bytea", "tinyblob": "bytea",
		"binary": "bytea", "varbinary": "bytea",
		"json": "jsonb", "enum": "varchar(255)", "set": "varchar(255)",
		"character varying": "varchar", "character": "character",
		"numeric": "numeric", "real": "real", "double precision": "double precision",
		"timestamp without time zone": "timestamp", "timestamp with time zone": "timestamptz",
		"time without time zone": "time", "time with time zone": "timetz",
		"bytea": "bytea", "jsonb": "jsonb", "uuid": "uuid",
	}
	if mapped, ok := typeMap[t]; ok {
		if (mapped == "varchar" || mapped == "character") && length != "" && length != "-" {
			return fmt.Sprintf("%s(%s)", mapped, length)
		}
		if mapped == "numeric" && strings.Contains(length, ",") {
			return fmt.Sprintf("numeric(%s)", length)
		}
		return mapped
	}
	return srcType
}

func (s *CompareService) generateCrossDBCreateDDL(cols []ColumnDetail, table string, targetType string, targetSchema string) string {
	switch targetType {
	case "postgres":
		var parts []string
		for _, col := range cols {
			parts = append(parts, "  "+s.buildColumnDef(col, targetType))
		}
		return fmt.Sprintf("CREATE TABLE \"%s\".\"%s\" (\n%s\n);", targetSchema, table, strings.Join(parts, ",\n"))
	case "mysql":
		var parts []string
		for _, col := range cols {
			parts = append(parts, "  "+s.buildColumnDef(col, targetType))
		}
		return fmt.Sprintf("CREATE TABLE `%s`.`%s` (\n%s\n);", targetSchema, table, strings.Join(parts, ",\n"))
	}
	return ""
}

func (s *CompareService) SyncData(req SyncDataRequest) (*DataSyncResult, error) {
	sourceConn, sourceDS, err := s.connectToDS(req.SourceDS, req.SourceDatabase)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	defer sourceConn.Close()

	targetConn, targetDS, err := s.connectToDS(req.TargetDS, req.TargetDatabase)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	defer targetConn.Close()

	sourceSchema := req.SourceSchema
	if sourceSchema == "" {
		sourceSchema = sourceDS.Database
	}
	targetSchema := req.TargetSchema
	if targetSchema == "" {
		targetSchema = targetDS.Database
	}

	opts := req.Options
	if opts.Mode == "" {
		opts.Mode = "full"
	}

	sourceTable := s.quoteTable(sourceDS.Type, sourceSchema, req.Table)
	targetTable := s.quoteTable(targetDS.Type, targetSchema, req.Table)

	sourceColNames, err := s.getTableColumnNames(req.SourceDS, sourceSchema, req.Table, req.SourceDatabase)
	if err != nil {
		return nil, fmt.Errorf("get source columns failed: %w", err)
	}

	targetColNames, err := s.getTableColumnNames(req.TargetDS, targetSchema, req.Table, req.TargetDatabase)
	if err != nil {
		return nil, fmt.Errorf("get target columns failed: %w", err)
	}

	syncCols := s.resolveSyncColumns(sourceColNames, targetColNames, opts)
	if len(syncCols) == 0 {
		return nil, fmt.Errorf("no common columns to sync")
	}

	if opts.TruncateTarget {
		truncSQL := fmt.Sprintf("TRUNCATE TABLE %s", targetTable)
		if _, err := targetConn.Exec(truncSQL); err != nil {
			return nil, fmt.Errorf("truncate target table failed: %w", err)
		}
	}

	// Optional single-transaction mode: all operations succeed or all rollback
	if opts.Transactional {
		tx, err := targetConn.Begin()
		if err != nil {
			return nil, fmt.Errorf("begin transaction: %w", err)
		}
		defer tx.Rollback() // no-op if committed

		result, err := s.executeSyncMode(sourceConn, sourceDS, sourceTable, targetConn, targetDS, targetTable, syncCols, opts)
		if err != nil || (result != nil && !result.Success) {
			return result, err // Rollback via defer
		}

		if commitErr := tx.Commit(); commitErr != nil {
			return nil, fmt.Errorf("commit transaction: %w", commitErr)
		}
		return result, nil
	}

	return s.executeSyncMode(sourceConn, sourceDS, sourceTable, targetConn, targetDS, targetTable, syncCols, opts)
}

func (s *CompareService) executeSyncMode(sourceConn *sql.DB, sourceDS repository.DataSource, sourceTable string, targetConn *sql.DB, targetDS repository.DataSource, targetTable string, syncCols []string, opts DataSyncOptions) (*DataSyncResult, error) {
	ctx := context.Background()

	conn := &SyncConnection{
		SourceDB:    sourceConn,
		SourceDS:    sourceDS,
		SourceTable: sourceTable,
		TargetDB:    targetConn,
		TargetDS:    targetDS,
		TargetTable: targetTable,
		SyncColumns: syncCols,
	}

	strategy, err := s.registry.Get(opts.Mode)
	if err != nil {
		return nil, err
	}
	// Build minimal request for validation
	req := SyncDataRequest{Options: opts}
	if err := strategy.Validate(req); err != nil {
		return nil, err
	}

	return strategy.Execute(ctx, conn, req)
}

func (s *CompareService) quoteTable(dbType, schema, table string) string {
	return drivers.QuoteTableName(schema, table, drivers.DialectOf(dbType))
}

func (s *CompareService) quoteCol(dbType, col string) string {
	return drivers.QuoteIdent(col, drivers.DialectOf(dbType))
}

func (s *CompareService) getTableColumnNames(dsID, schema, table, database string) ([]string, error) {
	driver, err := s.connectDriverWithDB(dsID, database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()
	cols, err := driver.GetColumns(schema, table)
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = c.Name
	}
	return names, nil
}

func (s *CompareService) resolveSyncColumns(sourceCols, targetCols []string, opts DataSyncOptions) []string {
	// Build case-insensitive target set
	targetSet := make(map[string]string) // lowerCase → original case
	for _, c := range targetCols {
		targetSet[strings.ToLower(c)] = c
	}

	var candidates []string
	if len(opts.SyncColumns) > 0 {
		candidates = opts.SyncColumns
	} else {
		candidates = sourceCols
	}

	var result []string
	checkSet := make(map[string]bool)
	for _, cf := range opts.CheckFields {
		checkSet[strings.ToLower(cf)] = true
	}
	for _, c := range candidates {
		// Skip auto-increment ID unless explicitly requested or used as check field
		if !opts.SyncID && strings.EqualFold(c, "id") && !checkSet[strings.ToLower(c)] {
			continue
		}
		if tgt, ok := targetSet[strings.ToLower(c)]; ok {
			// Use target's original case for INSERT compatibility
			result = append(result, tgt)
		}
	}
	if result == nil {
		result = make([]string, 0)
	}
	return result
}

func (s *CompareService) syncDataFull(sourceConn *sql.DB, sourceType, sourceTable string, targetConn *sql.DB, targetType, targetTable string, syncCols []string, opts DataSyncOptions) (*DataSyncResult, error) {
	colList := s.buildColumnList(sourceType, syncCols)
	selectSQL := fmt.Sprintf("SELECT %s FROM %s", colList, sourceTable)

	rows, err := sourceConn.Query(selectSQL)
	if err != nil {
		return nil, fmt.Errorf("read source data failed: %w", err)
	}
	defer rows.Close()

	_, srcData, err := drivers.ScanQueryResult(rows)
	if err != nil {
		return nil, fmt.Errorf("scan source rows failed: %w", err)
	}
	allRows := srcData

	if len(allRows) == 0 {
		return &DataSyncResult{Success: true, TotalRows: 0, SyncedRows: 0, SkippedRows: 0, Errors: []string{}}, nil
	}

	return s.batchInsert(targetConn, targetType, targetTable, syncCols, allRows)
}

func (s *CompareService) syncDataSelected(targetConn *sql.DB, targetType, targetTable string, syncCols []string, opts DataSyncOptions) (*DataSyncResult, error) {
	if len(opts.SelectedRows) == 0 {
		return &DataSyncResult{Success: true, TotalRows: 0, SyncedRows: 0, SkippedRows: 0, Errors: []string{}}, nil
	}

	var allRows [][]interface{}
	for _, rowMap := range opts.SelectedRows {
		row := make([]interface{}, len(syncCols))
		for i, col := range syncCols {
			row[i] = rowMap[col]
		}
		allRows = append(allRows, row)
	}

	return s.batchInsert(targetConn, targetType, targetTable, syncCols, allRows)
}

func (s *CompareService) syncDataDiff(sourceConn *sql.DB, sourceType, sourceTable string, targetConn *sql.DB, targetType, targetTable string, syncCols []string, opts DataSyncOptions) (*DataSyncResult, error) {
	if len(opts.CheckFields) == 0 {
		return &DataSyncResult{
			Success: false,
			Errors:  []string{"行差异同步需要选择至少一个检查字段"},
		}, nil
	}

	for _, cf := range opts.CheckFields {
		found := false
		for _, sc := range syncCols {
			if sc == cf {
				found = true
				break
			}
		}
		if !found {
			return &DataSyncResult{
				Success: false,
				Errors:  []string{fmt.Sprintf("检查字段 %q 不在同步列中", cf)},
			}, nil
		}
	}

	colList := s.buildColumnList(sourceType, syncCols)
	sourceSQL := fmt.Sprintf("SELECT %s FROM %s", colList, sourceTable)
	sourceRows, err := sourceConn.Query(sourceSQL)
	if err != nil {
		return nil, fmt.Errorf("read source data failed: %w", err)
	}
	defer sourceRows.Close()

	_, srcData, err := drivers.ScanQueryResult(sourceRows)
	if err != nil {
		return nil, fmt.Errorf("scan source rows failed: %w", err)
	}

	targetSQL := fmt.Sprintf("SELECT %s FROM %s", s.buildColumnList(targetType, syncCols), targetTable)
	targetRows, err := targetConn.Query(targetSQL)
	if err != nil {
		return nil, fmt.Errorf("read target data failed: %w", err)
	}
	defer targetRows.Close()

	colIndex := make(map[string]int)
	for i, c := range syncCols {
		colIndex[c] = i
	}

	targetMap := make(map[string][]interface{})
	_, targetRowsData, err := drivers.ScanQueryResult(targetRows)
	if err != nil {
		return nil, fmt.Errorf("scan target rows failed: %w", err)
	}
	for _, row := range targetRowsData {
		key := s.buildRowKey(row, syncCols, colIndex, opts.CheckFields)
		targetMap[key] = row
	}

	var toInsert [][]interface{}
	var toUpdate []diffUpdate
	skipped := 0

	for _, srcRow := range srcData {
		key := s.buildRowKey(srcRow, syncCols, colIndex, opts.CheckFields)
		tgtRow, exists := targetMap[key]
		if !exists {
			toInsert = append(toInsert, srcRow)
		} else {
			needsUpdate := false
			for i, col := range syncCols {
				isCheckField := false
				for _, cf := range opts.CheckFields {
					if cf == col {
						isCheckField = true
						break
					}
				}
				if isCheckField {
					continue
				}
				if !s.valuesEqual(srcRow[i], tgtRow[i]) {
					needsUpdate = true
					break
				}
			}
			if needsUpdate {
				toUpdate = append(toUpdate, diffUpdate{srcRow: srcRow, tgtRow: tgtRow})
			} else {
				skipped++
			}
		}
	}

	totalRows := len(srcData)
	syncedRows := 0
	var errors []string

	if len(toInsert) > 0 {
		result, err := s.batchInsert(targetConn, targetType, targetTable, syncCols, toInsert)
		if err != nil {
			errors = append(errors, fmt.Sprintf("batch insert failed: %s", err.Error()))
		} else {
			syncedRows += result.SyncedRows
			errors = append(errors, result.Errors...)
		}
	}

	if len(toUpdate) > 0 {
		updateCols := s.getUpdateCols(syncCols, opts)
		for _, upd := range toUpdate {
			err := s.executeUpdate(targetConn, targetType, targetTable, syncCols, colIndex, opts.CheckFields, upd.srcRow, updateCols)
			if err != nil {
				errors = append(errors, fmt.Sprintf("update failed: %s", err.Error()))
			} else {
				syncedRows++
			}
		}
	}

	return &DataSyncResult{
		Success:     len(errors) == 0,
		TotalRows:   totalRows,
		SyncedRows:  syncedRows,
		SkippedRows: skipped,
		Errors:      errors,
	}, nil
}

type diffUpdate struct {
	srcRow []interface{}
	tgtRow []interface{}
}

func (s *CompareService) buildColumnList(dbType string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = s.quoteCol(dbType, c)
	}
	return strings.Join(quoted, ", ")
}

func (s *CompareService) buildRowKey(row []interface{}, cols []string, colIndex map[string]int, checkFields []string) string {
	parts := make([]string, len(checkFields))
	for i, cf := range checkFields {
		idx := colIndex[cf]
		if b, ok := row[idx].([]byte); ok {
			parts[i] = string(b)
		} else {
			parts[i] = toString(row[idx])
		}
	}
	return strings.Join(parts, "|||")
}

func (s *CompareService) valuesEqual(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func (s *CompareService) getUpdateCols(syncCols []string, opts DataSyncOptions) []string {
	if len(opts.SyncColumns) > 0 {
		return opts.SyncColumns
	}
	return syncCols
}

func (s *CompareService) batchInsert(conn *sql.DB, dbType, table string, cols []string, rows [][]interface{}) (*DataSyncResult, error) {
	if len(rows) == 0 {
		return &DataSyncResult{Success: true, Errors: []string{}}, nil
	}

	batchSize := 5000
	if dbType == "postgres" && len(cols) > 0 {
		maxCols := 65535 / len(cols)
		if maxCols < 1 {
			maxCols = 1
		}
		if batchSize > maxCols {
			batchSize = maxCols
		}
	}
	synced := 0
	var errors []string
	colList := s.buildColumnList(dbType, cols)
	placeholders := s.buildPlaceholders(dbType, len(cols))

	tx, err := conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}

	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		var valueParts []string
		var args []interface{}
		for j, row := range batch {
			ph := placeholders
			if dbType == "postgres" {
				ph = s.buildPGPlaceholders(len(cols), i+j)
			} else if dbType == "sqlserver" {
				ph = s.buildMSSQLPlaceholders(len(cols), i+j)
			}
			valueParts = append(valueParts, ph)
			args = append(args, row...)
		}

		var insertSQL string
		switch dbType {
		case "mysql":
			insertSQL = fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		case "postgres":
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s ON CONFLICT DO NOTHING", table, colList, strings.Join(valueParts, ", "))
		case "sqlite":
			insertSQL = fmt.Sprintf("INSERT OR IGNORE INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		case "oracle":
			// Oracle: use literal values (go-ora ? placeholder unreliable via database/sql)
			for _, row := range batch {
				var vals []string
				for _, v := range row {
					vals = append(vals, s.formatSQLValue(v))
				}
				singleSQL := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", table, colList, strings.Join(vals, ", "))
				_, err := tx.Exec(singleSQL)
				if err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("batch insert failed: %w", err)
				}
				synced++
			}
			continue
		default:
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		}
		result, err := tx.Exec(insertSQL, args...)
		if err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("batch insert failed at row %d: %w", i, err)
		}
		affected, _ := result.RowsAffected()
		synced += int(affected)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	return &DataSyncResult{
		Success:     true,
		TotalRows:   len(rows),
		SyncedRows:  synced,
		SkippedRows: len(rows) - synced,
		Errors:      errors,
	}, nil
}

func (s *CompareService) buildPlaceholders(dbType string, count int) string {
	if dbType == "postgres" {
		parts := make([]string, count)
		for i := 0; i < count; i++ {
			parts[i] = fmt.Sprintf("$%d", i+1)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	if dbType == "sqlserver" {
		parts := make([]string, count)
		for i := 0; i < count; i++ {
			parts[i] = fmt.Sprintf("@p%d", i+1)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	// MySQL, Oracle use ? via database/sql
	parts := make([]string, count)
	for i := 0; i < count; i++ {
		parts[i] = "?"
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// formatSQLValue converts a value to a SQL literal string
func (s *CompareService) formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}
	switch val := v.(type) {
	case []byte:
		if len(val) == 0 {
			return "NULL"
		}
		return fmt.Sprintf("'%s'", strings.ReplaceAll(string(val), "'", "''"))
	case string:
		if val == "" {
			return "NULL"
		}
		return fmt.Sprintf("'%s'", strings.ReplaceAll(val, "'", "''"))
	case time.Time:
		if val.IsZero() {
			return "NULL"
		}
		// Oracle DATE/TIMESTAMP format
		return fmt.Sprintf("TO_DATE('%s','YYYY-MM-DD HH24:MI:SS')", val.Format("2006-01-02 15:04:05"))
	case int64, float64, int, float32:
		return fmt.Sprintf("%v", val)
	default:
		s := fmt.Sprintf("%v", val)
		if s == "" || s == "<nil>" {
			return "NULL"
		}
		return fmt.Sprintf("'%s'", strings.ReplaceAll(s, "'", "''"))
	}
}

func (s *CompareService) buildPGPlaceholders(colCount, rowOffset int) string {
	parts := make([]string, colCount)
	for i := 0; i < colCount; i++ {
		parts[i] = fmt.Sprintf("$%d", rowOffset*colCount+i+1)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (s *CompareService) buildMSSQLPlaceholders(colCount, rowOffset int) string {
	parts := make([]string, colCount)
	for i := 0; i < colCount; i++ {
		parts[i] = fmt.Sprintf("@p%d", rowOffset*colCount+i+1)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func (s *CompareService) executeUpdate(conn *sql.DB, dbType, table string, allCols []string, colIndex map[string]int, checkFields []string, srcRow []interface{}, updateCols []string) error {
	var setParts []string
	var args []interface{}
	argIdx := 0

	for _, col := range updateCols {
		idx := colIndex[col]
		isCheckField := false
		for _, cf := range checkFields {
			if cf == col {
				isCheckField = true
				break
			}
		}
		if isCheckField {
			continue
		}
		argIdx++
		switch dbType {
		case "mysql":
			setParts = append(setParts, fmt.Sprintf("`%s` = ?", col))
		case "postgres":
			setParts = append(setParts, fmt.Sprintf(`"%s" = $%d`, col, argIdx))
		default:
			setParts = append(setParts, fmt.Sprintf(`"%s" = ?`, col))
		}
		args = append(args, srcRow[idx])
	}

	if len(setParts) == 0 {
		return nil
	}

	var whereParts []string
	for _, cf := range checkFields {
		idx := colIndex[cf]
		argIdx++
		switch dbType {
		case "mysql":
			whereParts = append(whereParts, fmt.Sprintf("`%s` = ?", cf))
		case "postgres":
			whereParts = append(whereParts, fmt.Sprintf(`"%s" = $%d`, cf, argIdx))
		default:
			whereParts = append(whereParts, fmt.Sprintf(`"%s" = ?`, cf))
		}
		args = append(args, srcRow[idx])
	}

	updateSQL := fmt.Sprintf("UPDATE %s SET %s WHERE %s", table, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))
	_, err := conn.Exec(updateSQL, args...)
	return err
}
