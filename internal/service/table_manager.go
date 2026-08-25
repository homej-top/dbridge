package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service/drivers"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"gorm.io/gorm"
)

type TableManagerService struct {
	db       *gorm.DB
	auditSvc *AuditLogService
}

func NewTableManagerService(db *gorm.DB) *TableManagerService {
	return &TableManagerService{
		db:       db,
		auditSvc: NewAuditLogService(db),
	}
}

// Type aliases (definitions moved to drivers package)
type (
	TableColumn     = drivers.TableColumn
	TableIndex      = drivers.TableIndex
	TableConstraint = drivers.TableConstraint
	TableMeta       = drivers.TableMeta
	FullStructure   = drivers.FullStructure
)

type ColumnChange struct {
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

type IndexChange struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
	Comment string   `json:"comment"`
}

type Change struct {
	Action  string       `json:"action"`
	Column  ColumnChange `json:"column"`
	Index   IndexChange  `json:"index"`
	Comment string       `json:"comment"`
}

type AlterRequest struct {
	DataSourceID string   `json:"data_source_id"`
	Schema       string   `json:"schema"`
	Table        string   `json:"table"`
	Database     string   `json:"database"` // optional, for PG/MSSQL three-part table names
	Changes      []Change `json:"changes"`
	OverrideDDL  string   `json:"override_ddl"`
	DryRun       bool     `json:"dry_run"`
}

type AlterPreview struct {
	DDL         string   `json:"ddl"`
	RollbackDDL string   `json:"rollback_ddl"`
	Warnings    []string `json:"warnings"`
	HighRisk    bool     `json:"high_risk"`
}

type AlterResult struct {
	Success      bool     `json:"success"`
	ExecutedDDL  string   `json:"executed_ddl"`
	RollbackPath string   `json:"rollback_script_path"`
	AuditID      string   `json:"audit_id"`
	Duration     int64    `json:"duration"`
	Executed     []string `json:"executed"`
	NotExecuted  []string `json:"not_executed"`
	Error        string   `json:"error"`
}

var systemDBBlacklist = map[string]bool{
	"mysql": true, "information_schema": true, "performance_schema": true,
	"sys": true, "pg_catalog": true, "pg_toast": true,
	"pg_temp_1": true,
}

func (s *TableManagerService) connectToDS(dsID string) (*sql.DB, repository.DataSource, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return nil, ds, fmt.Errorf("data source not found: %s", dsID)
	}
	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, ds, fmt.Errorf("failed to decrypt password")
	}
	conn, err := openDBConn(ds, pwd)
	if err != nil {
		return nil, ds, err
	}
	return conn, ds, nil
}

// connectDriverForDB creates a DatabaseDriver through the global pool manager.
func (s *TableManagerService) connectDriverForDB(dsID, database string) (drivers.DatabaseDriver, error) {
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

func resolveSchema(ds repository.DataSource, schema string) string {
	if schema != "" {
		return schema
	}
	if ds.Database != "" {
		return ds.Database
	}
	if ds.Type == "postgres" {
		return "public"
	}
	return ""
}

func quoteMySQL(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteTable(dbType, database, schema, table string) string {
	if dbType == "mysql" {
		if schema != "" {
			return quoteMySQL(schema) + "." + quoteMySQL(table)
		}
		return quoteMySQL(table)
	}
	if dbType == "sqlserver" {
		q := func(s string) string { return "[" + strings.ReplaceAll(s, "]", "]]") + "]" }
		if database != "" {
			if schema != "" {
				return q(database) + "." + q(schema) + "." + q(table)
			}
			return q(database) + "." + q(table)
		}
		if schema != "" {
			return q(schema) + "." + q(table)
		}
		return q(table)
	}
	if schema != "" {
		return quotePG(schema) + "." + quotePG(table)
	}
	return quotePG(table)
}

func (s *TableManagerService) GetFullStructure(dsID, schema, table, database string) (*FullStructure, error) {
	// Connect via driver for database-specific handling
	driver, err := s.connectDriverForDB(dsID, database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()
	return driver.GetFullStructure(schema, table)
}

func quotePGList(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = quotePG(n)
	}
	return out
}

func pgString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func mysqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// PreviewAlter generates DDL + rollback + warnings without executing.
func (s *TableManagerService) PreviewAlter(req AlterRequest) (*AlterPreview, error) {
	if err := s.validateSchema(req.Schema); err != nil {
		return nil, err
	}
	if len(req.Changes) == 0 {
		return nil, fmt.Errorf("no changes provided")
	}
	if len(req.Changes) > 20 {
		return nil, fmt.Errorf("too many changes (max 20)")
	}

	driver, err := s.connectDriverForDB(req.DataSourceID, req.Database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	sch := req.Schema
	if sch == "" {
		sch = "public" // PG default
	}
	current, err := driver.GetFullStructure(sch, req.Table)
	if err != nil {
		return nil, err
	}

	ddl, rollback, warnings, highRisk, err := s.buildAlterDDL(driver.DBType(), req.Database, sch, req.Table, req.Changes, current)
	if err != nil {
		return nil, err
	}
	return &AlterPreview{
		DDL:         ddl,
		RollbackDDL: rollback,
		Warnings:    warnings,
		HighRisk:    highRisk,
	}, nil
}

func (s *TableManagerService) validateSchema(schema string) error {
	if systemDBBlacklist[strings.ToLower(schema)] {
		return fmt.Errorf("modification of system schema '%s' is not allowed", schema)
	}
	return nil
}

func (s *TableManagerService) getCurrentStructure(dsID, dbType, schema, table, database string) (*FullStructure, error) {
	driver, err := s.connectDriverForDB(dsID, database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()
	return driver.GetFullStructure(schema, table)
}

// ExecuteAlter runs the ALTER DDL, writes audit log, stores rollback script.
func (s *TableManagerService) ExecuteAlter(req AlterRequest, operator, userID, tenantID, ip, ua string) (*AlterResult, error) {
	if err := s.validateSchema(req.Schema); err != nil {
		return nil, err
	}
	if len(req.Changes) > 20 {
		return nil, fmt.Errorf("too many changes (max 20)")
	}

	start := time.Now()
	conn, ds, err := s.connectToDS(req.DataSourceID)
	if err != nil {
		return nil, err
	}

	// If a different database is requested, reconnect to it
	if req.Database != "" && req.Database != ds.Database {
		conn.Close()
		var pwd string
		pwd, err = cryptoPkg.Decrypt(ds.Password)
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt password: %w", err)
		}
		ds.Database = req.Database
		conn, err = openDBConn(ds, pwd)
		if err != nil {
			return nil, fmt.Errorf("connect to database %s: %w", req.Database, err)
		}
		defer conn.Close()
	} else {
		defer conn.Close()
	}

	sch := resolveSchema(ds, req.Schema)
	current, err := s.getCurrentStructure(req.DataSourceID, ds.Type, sch, req.Table, req.Database)
	if err != nil {
		return nil, err
	}

	var ddlToExec string
	var rollback string
	var warnings []string
	if req.OverrideDDL != "" {
		ddlToExec = req.OverrideDDL
		rollback = "-- user provided override, no auto rollback"
	} else {
		var err error
		var highRisk bool
		ddlToExec, rollback, warnings, highRisk, err = s.buildAlterDDL(ds.Type, req.Database, sch, req.Table, req.Changes, current)
		if err != nil {
			return nil, err
		}
		_ = highRisk
	}
	_ = warnings

	// Split by ';\n' into individual statements
	stmts := splitStatements(ddlToExec)
	if len(stmts) == 0 {
		return nil, fmt.Errorf("no valid DDL statements to execute")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	result := &AlterResult{ExecutedDDL: ddlToExec}

	// Oracle: wrap all DDL in an explicit transaction to ensure COMMIT ON TABLE
	// and similar statements are properly persisted.
	var tx *sql.Tx
	if ds.Type == "oracle" {
		tx, err = conn.BeginTx(ctx, nil)
		if err != nil {
			result.Error = fmt.Sprintf("begin tx: %v", err)
			result.Success = false
			result.Duration = time.Since(start).Milliseconds()
			s.writeAudit(req, ds.Name, operator, userID, tenantID, ip, ua, result, rollback)
			return result, nil
		}
	}

	for _, stmt := range stmts {
		// Strip trailing semicolons - Oracle/go-ora doesn't tolerate them
		cleanStmt := strings.TrimRight(strings.TrimSpace(stmt), ";")
		if cleanStmt == "" {
			continue
		}
		var execErr error
		if tx != nil {
			_, execErr = tx.ExecContext(ctx, cleanStmt)
		} else {
			_, execErr = conn.ExecContext(ctx, cleanStmt)
		}
		if execErr != nil {
			// Oracle: tolerate ORA-02441 (nonexistent PK) and ORA-02260/ORA-01442 (column already PK)
			if strings.EqualFold(ds.Type, "oracle") && (strings.Contains(execErr.Error(), "ORA-02441") || strings.Contains(execErr.Error(), "ORA-02260") || strings.Contains(execErr.Error(), "ORA-01442")) {
				result.Executed = append(result.Executed, cleanStmt)
				continue
			}
			if tx != nil {
				tx.Rollback()
			}
			result.Error = execErr.Error()
			result.Success = false
			result.Duration = time.Since(start).Milliseconds()
			s.writeAudit(req, ds.Name, operator, userID, tenantID, ip, ua, result, rollback)
			return result, nil
		}
		result.Executed = append(result.Executed, stmt)
	}

	// Oracle: commit the transaction
	if tx != nil {
		if err := tx.Commit(); err != nil {
			result.Error = fmt.Sprintf("commit: %v", err)
			result.Success = false
			result.Duration = time.Since(start).Milliseconds()
			s.writeAudit(req, ds.Name, operator, userID, tenantID, ip, ua, result, rollback)
			return result, nil
		}
	}

	// Save rollback script
	rollbackPath, err := s.saveRollbackScript(ds.ID, sch, req.Table, rollback)
	if err == nil {
		result.RollbackPath = rollbackPath
	}

	result.Success = true
	result.Duration = time.Since(start).Milliseconds()
	auditID := s.writeAudit(req, ds.Name, operator, userID, tenantID, ip, ua, result, rollback)
	result.AuditID = auditID
	return result, nil
}

func splitStatements(ddl string) []string {
	parts := strings.Split(ddl, ";\n")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = strings.TrimSuffix(p, ";")
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	// Handle trailing single statement without newline
	if len(out) == 0 {
		p := strings.TrimSpace(strings.TrimSuffix(ddl, ";"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *TableManagerService) saveRollbackScript(dsID, schema, table, rollback string) (string, error) {
	dateDir := time.Now().Format("2006-01-02")
	root := filepath.Join(".", "data", "rollback", dateDir)
	if err := os.MkdirAll(root, 0755); err != nil {
		return "", err
	}
	filename := fmt.Sprintf("alter_table_%s_%s_%s_%s.sql", dsID[:8], schema, table, time.Now().Format("20060102150405"))
	path := filepath.Join(root, filename)
	if err := os.WriteFile(path, []byte(rollback), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func (s *TableManagerService) writeAudit(req AlterRequest, dsName, operator, userID, tenantID, ip, ua string, result *AlterResult, rollback string) string {
	subActions := make([]string, 0, len(req.Changes))
	for _, c := range req.Changes {
		switch c.Action {
		case "ADD_COLUMN":
			subActions = append(subActions, fmt.Sprintf("ADD_COLUMN %s", c.Column.Name))
		case "MODIFY_COLUMN":
			subActions = append(subActions, fmt.Sprintf("MODIFY_COLUMN %s", c.Column.Name))
		case "DROP_COLUMN":
			subActions = append(subActions, fmt.Sprintf("DROP_COLUMN %s", c.Column.Name))
		case "RENAME_COLUMN":
			subActions = append(subActions, fmt.Sprintf("RENAME_COLUMN %s → %s", c.Column.Name, c.Column.NewName))
		case "ADD_INDEX":
			subActions = append(subActions, fmt.Sprintf("ADD_INDEX %s", c.Index.Name))
		case "DROP_INDEX":
			subActions = append(subActions, fmt.Sprintf("DROP_INDEX %s", c.Index.Name))
		case "ADD_CONSTRAINT":
			subActions = append(subActions, fmt.Sprintf("ADD_CONSTRAINT %s", c.Index.Name))
		case "DROP_CONSTRAINT":
			subActions = append(subActions, fmt.Sprintf("DROP_CONSTRAINT %s", c.Index.Name))
		case "TABLE_COMMENT":
			subActions = append(subActions, "TABLE_COMMENT")
		}
	}
	details, _ := json.Marshal(map[string]interface{}{
		"data_source_id":   req.DataSourceID,
		"data_source_name": dsName,
		"schema":           req.Schema,
		"table":            req.Table,
		"sub_actions":      subActions,
		"ddl":              result.ExecutedDDL,
		"rollback_path":    result.RollbackPath,
		"rollback_ddl":     rollback,
		"success":          result.Success,
		"error":            result.Error,
		"executed":         result.Executed,
		"not_executed":     result.NotExecuted,
		"duration_ms":      result.Duration,
		"operator":         operator,
	})
	log := repository.AuditLog{
		UserID:    userID,
		Operation: "alter_table",
		Details:   string(details),
		IP:        ip,
		UserAgent: ua,
		TenantID:  tenantID,
	}
	if err := s.auditSvc.Create(&log); err != nil {
		return ""
	}
	return fmt.Sprintf("%d", log.ID)
}

func (s *TableManagerService) GetViewDefinition(dsID, schema, view, database string) (string, error) {
	driver, err := s.connectDriverForDB(dsID, database)
	if err != nil {
		return "", err
	}
	defer driver.Close()
	return driver.GetViewDefinition(schema, view)
}

// buildAlterDDL generates the forward DDL, rollback DDL, and warnings for the given changes.
func (s *TableManagerService) buildAlterDDL(dbType, database, schema, table string, changes []Change, current *FullStructure) (string, string, []string, bool, error) {
	tbl := quoteTable(dbType, database, schema, table)
	curCols := map[string]TableColumn{}
	for _, c := range current.Columns {
		curCols[c.Name] = c
	}
	curIdx := map[string]TableIndex{}
	for _, i := range current.Indexes {
		curIdx[i.Name] = i
	}

	var stmts []string
	var rollbackStmts []string
	var warnings []string
	highRisk := false

	// Reorder: process DROP operations (constraint, index, column) before MODIFY
	reordered := make([]Change, 0, len(changes))
	var modifyChanges []Change
	for _, ch := range changes {
		if ch.Action == "DROP_CONSTRAINT" || ch.Action == "DROP_INDEX" || ch.Action == "DROP_COLUMN" {
			reordered = append(reordered, ch)
		} else {
			modifyChanges = append(modifyChanges, ch)
		}
	}
	reordered = append(reordered, modifyChanges...)

	for _, ch := range reordered {
		switch ch.Action {
		case "ADD_COLUMN":
			s, r, w, hr, err := s.buildAddColumnDispatch(dbType, tbl, ch.Column, curCols)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
			warnings = append(warnings, w...)
			if hr {
				highRisk = true
			}
		case "MODIFY_COLUMN":
			// SQL Server: drop indexes on the column before ALTER COLUMN
			if dbType == "sqlserver" {
				for _, idx := range current.Indexes {
					for _, col := range idx.Columns {
						if strings.EqualFold(col, ch.Column.Name) {
							dropDDL, _, _, _, dropErr := s.buildDropIndex(dbType, tbl, schema, IndexChange{Name: idx.Name}, curIdx)
							if dropErr == nil {
								stmts = append(stmts, dropDDL)
							}
							break
						}
					}
				}
			}
			modStmts, modRollbacks, w, hr, err := s.buildModifyColumnDispatch(dbType, tbl, ch.Column, curCols)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, modStmts...)
			rollbackStmts = append(rollbackStmts, modRollbacks...)
			warnings = append(warnings, w...)
			if hr {
				highRisk = true
			}
			// SQL Server: recreate dropped indexes
			if dbType == "sqlserver" {
				for _, idx := range current.Indexes {
					for _, col := range idx.Columns {
						if strings.EqualFold(col, ch.Column.Name) {
							addDDL, _, addErr := s.buildAddIndexDispatch(dbType, tbl, schema, IndexChange{Name: idx.Name, Type: idx.Type, Columns: idx.Columns, Comment: idx.Comment}, curCols)
							if addErr == nil {
								stmts = append(stmts, addDDL)
							}
							break
						}
					}
				}
			}
		case "DROP_COLUMN":
			s, r, w, hr, err := s.buildDropColumnDispatch(dbType, tbl, ch.Column, curCols)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
			warnings = append(warnings, w...)
			if hr {
				highRisk = true
			}
		case "RENAME_COLUMN":
			s, r, err := s.buildRenameColumn(dbType, tbl, ch.Column)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		case "ADD_INDEX":
			s, r, err := s.buildAddIndexDispatch(dbType, tbl, schema, ch.Index, curCols)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		case "DROP_INDEX":
			s, r, w, hr, err := s.buildDropIndexDispatch(dbType, tbl, schema, ch.Index, curIdx)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
			warnings = append(warnings, w...)
			if hr {
				highRisk = true
			}
		case "INDEX_COMMENT":
			s, r, err := s.buildIndexCommentDispatch(dbType, tbl, schema, ch.Index, curIdx)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		case "ADD_CONSTRAINT":
			s, r, err := s.buildAddConstraintDispatch(dbType, tbl, ch.Index)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		case "DROP_CONSTRAINT":
			s, r, err := s.buildDropConstraintDispatch(dbType, tbl, ch.Index)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		case "TABLE_COMMENT":
			s, r, err := s.buildTableCommentDispatch(dbType, tbl, ch.Comment, current.TableMeta.Comment)
			if err != nil {
				return "", "", nil, false, err
			}
			stmts = append(stmts, s)
			rollbackStmts = append(rollbackStmts, r)
		default:
			return "", "", nil, false, fmt.Errorf("unknown action: %s", ch.Action)
		}
	}

	ddl := s.joinAlterStatements(dbType, tbl, stmts)
	rollback := s.joinAlterStatements(dbType, tbl, reverseStrings(rollbackStmts))
	return ddl, rollback, warnings, highRisk, nil
}

func reverseStrings(s []string) []string {
	out := make([]string, len(s))
	for i, v := range s {
		out[len(s)-1-i] = v
	}
	return out
}

func (s *TableManagerService) joinAlterStatements(dbType, tbl string, stmts []string) string {
	if len(stmts) == 0 {
		return ""
	}
	// Remove consecutive duplicate statements
	unique := make([]string, 0, len(stmts))
	for _, stmt := range stmts {
		if len(unique) == 0 || stmt != unique[len(unique)-1] {
			unique = append(unique, stmt)
		}
	}
	return strings.Join(unique, ";\n") + ";"
}

func (s *TableManagerService) mysqlColumnType(ch ColumnChange) string {
	t := strings.ToLower(ch.Type)
	if ch.Length != "" && needsLength(t) {
		return fmt.Sprintf("%s(%s)", t, ch.Length)
	}
	return t
}

func (s *TableManagerService) pgColumnTypeFromChange(ch ColumnChange) string {
	t := strings.ToLower(ch.Type)
	switch t {
	case "character varying":
		t = "varchar"
	case "character":
		t = "char"
	}
	if ch.Length != "" && needsLength(t) {
		return fmt.Sprintf("%s(%s)", t, ch.Length)
	}
	// Default length for types that require it but user didn't specify
	if ch.Length == "" && needsLength(t) {
		return fmt.Sprintf("%s(%d)", t, 255)
	}
	return t
}

func needsLength(t string) bool {
	switch strings.ToLower(t) {
	case "varchar", "char", "character varying", "character", "varbinary", "binary",
		"varchar2", "nvarchar2", "nchar", "raw":
		return true
	}
	return false
}

func (s *TableManagerService) buildAddColumn(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) (string, string, []string, bool, error) {
	if ch.Name == "" {
		return "", "", nil, false, fmt.Errorf("column name required")
	}
	if _, exists := cur[ch.Name]; exists {
		return "", "", nil, false, fmt.Errorf("column '%s' already exists", ch.Name)
	}
	var warnings []string
	if isReservedWord(ch.Name) {
		warnings = append(warnings, fmt.Sprintf("column '%s' is a reserved word", ch.Name))
	}

	var colDef string
	var rollback string
	if dbType == "mysql" {
		colDef = quoteMySQL(ch.Name) + " " + s.mysqlColumnType(ch)
		if ch.Nullable != nil && !*ch.Nullable {
			colDef += " NOT NULL"
		} else {
			colDef += " NULL"
		}
		if ch.HasDef != nil && *ch.HasDef {
			colDef += " DEFAULT " + formatDefault(ch.Default)
		}
		if ch.Comment != "" {
			colDef += " COMMENT " + mysqlString(ch.Comment)
		}
		if ch.After != "" {
			colDef += " AFTER " + quoteMySQL(ch.After)
		}
		rollback = fmt.Sprintf("DROP COLUMN %s", quoteMySQL(ch.Name))
	} else {
		colDef = quotePG(ch.Name) + " " + s.pgColumnTypeFromChange(ch)
		if ch.Nullable != nil && !*ch.Nullable {
			colDef += " NOT NULL"
		}
		if ch.HasDef != nil && *ch.HasDef {
			colDef += " DEFAULT " + formatDefault(ch.Default)
		}
		rollback = fmt.Sprintf("DROP COLUMN %s", quotePG(ch.Name))
	}

	// Dialect-specific ADD COLUMN syntax
	var stmt string
	switch dbType {
	case "oracle":
		stmt = fmt.Sprintf("ALTER TABLE %s ADD (%s)", tbl, colDef)
	case "sqlserver":
		stmt = fmt.Sprintf("ALTER TABLE %s ADD %s", tbl, colDef)
	default:
		stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tbl, colDef)
	}
	if (dbType == "postgres" || dbType == "oracle") && ch.Comment != "" {
		stmt += ";\nCOMMENT ON COLUMN " + tbl + "." + quotePG(ch.Name) + " IS " + pgString(ch.Comment)
	}
	if dbType == "sqlserver" && ch.Comment != "" {
		stmt += ";\n" + drivers.BuildSQLServerColumnComment(tbl, ch.Name, ch.Comment)
	}
	rollbackStmt := "ALTER TABLE " + tbl + " " + rollback
	return stmt, rollbackStmt, warnings, false, nil
}

func (s *TableManagerService) buildModifyColumnDispatch(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) ([]string, []string, []string, bool, error) {
	orig, exists := cur[ch.Name]
	if !exists {
		return nil, nil, nil, false, fmt.Errorf("column '%s' not found", ch.Name)
	}

	// Convert service.ColumnChange to drivers.AlterColumnChange
	dc := drivers.AlterColumnChange{
		Name: ch.Name, Type: ch.Type, Length: ch.Length,
		Nullable: ch.Nullable, Default: ch.Default,
		HasDef: ch.HasDef, Comment: ch.Comment,
		After: ch.After, NewName: ch.NewName,
	}

	switch dbType {
	case "mysql", "mariadb", "oceanbase":
		return drivers.BuildMySQLModifyColumn(tbl, dc, orig)
	case "postgres", "postgresql":
		return drivers.BuildPGModifyColumn(tbl, dc, orig)
	case "oracle":
		return drivers.BuildOracleModifyColumn(tbl, dc, orig)
	case "sqlserver":
		return drivers.BuildMSSQLModifyColumn(tbl, dc, orig)
	default:
		return drivers.BuildPGModifyColumn(tbl, dc, orig)
	}
}

func (s *TableManagerService) buildTableCommentDispatch(dbType, tbl, newComment, oldComment string) (string, string, error) {
	return s.buildTableComment(dbType, tbl, newComment, oldComment)
}

func (s *TableManagerService) buildAddColumnDispatch(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) (string, string, []string, bool, error) {
	return s.buildAddColumn(dbType, tbl, ch, cur)
}
func (s *TableManagerService) buildDropColumnDispatch(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) (string, string, []string, bool, error) {
	return s.buildDropColumn(dbType, tbl, ch, cur)
}
func (s *TableManagerService) buildAddIndexDispatch(dbType, tbl, schema string, ch IndexChange, curCols map[string]TableColumn) (string, string, error) {
	return s.buildAddIndex(dbType, tbl, ch, curCols)
}
func (s *TableManagerService) buildDropIndexDispatch(dbType, tbl, schema string, ch IndexChange, cur map[string]TableIndex) (string, string, []string, bool, error) {
	return s.buildDropIndex(dbType, tbl, schema, ch, cur)
}
func (s *TableManagerService) buildIndexCommentDispatch(dbType, tbl, schema string, ch IndexChange, cur map[string]TableIndex) (string, string, error) {
	return s.buildIndexComment(dbType, tbl, schema, ch, cur)
}
func (s *TableManagerService) buildAddConstraintDispatch(dbType, tbl string, ch IndexChange) (string, string, error) {
	return s.buildAddConstraint(dbType, tbl, ch)
}
func (s *TableManagerService) buildDropConstraintDispatch(dbType, tbl string, ch IndexChange) (string, string, error) {
	return s.buildDropConstraint(dbType, tbl, ch)
}

func (s *TableManagerService) buildModifyColumn(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) ([]string, []string, []string, bool, error) {
	if ch.Name == "" {
		return nil, nil, nil, false, fmt.Errorf("column name required")
	}
	orig, exists := cur[ch.Name]
	if !exists {
		return nil, nil, nil, false, fmt.Errorf("column '%s' not found", ch.Name)
	}

	var stmts []string
	var rollbacks []string
	var warnings []string
	highRisk := false

	// Detect length shrink
	if ch.Length != "" && orig.Length != "" && ch.Length != orig.Length {
		origL, newL := toInt(orig.Length), toInt(ch.Length)
		if origL > 0 && newL > 0 && newL < origL {
			warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: length shrink (%s → %s) may truncate data", ch.Name, orig.Length, ch.Length))
			highRisk = true
		}
	}
	// Detect tightening NOT NULL
	if ch.Nullable != nil && !*ch.Nullable && orig.Nullable {
		warnings = append(warnings, fmt.Sprintf("MODIFY_COLUMN %s: tightening NOT NULL may fail on existing NULL values", ch.Name))
		highRisk = true
	}

	if dbType == "mysql" {
		colDef := quoteMySQL(ch.Name) + " " + s.mysqlColumnType(ch)
		if ch.Nullable != nil {
			if *ch.Nullable {
				colDef += " NULL"
			} else {
				colDef += " NOT NULL"
			}
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
		// Rollback: restore original definition
		origDef := quoteMySQL(orig.Name) + " " + orig.Type
		if !orig.Nullable {
			origDef += " NOT NULL"
		} else {
			origDef += " NULL"
		}
		if orig.HasDef {
			origDef += " DEFAULT " + formatDefault(orig.Default)
		}
		if orig.Comment != "" {
			origDef += " COMMENT " + mysqlString(orig.Comment)
		}
		rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", tbl, origDef))
	} else if dbType == "oracle" {
		// Use original type; only include length if explicitly changed to avoid ORA-01440
		useType := orig.Type
		if ch.Type != "" && ch.Type != orig.Type {
			useType = ch.Type
		}
		colDef := quotePG(ch.Name) + " " + useType
		// Include length if: explicitly changed, OR type requires it (VARCHAR2/NVARCHAR2/CHAR)
		needsLen := strings.EqualFold(useType, "VARCHAR2") || strings.EqualFold(useType, "NVARCHAR2") || strings.EqualFold(useType, "CHAR") || strings.EqualFold(useType, "NCHAR")
		if ch.Length != "" && ch.Length != "0" && ch.Length != orig.Length {
			colDef += "(" + ch.Length + ")"
		} else if needsLen && orig.Length != "" && orig.Length != "0" {
			// For NVARCHAR2/NCHAR, Oracle reports byte length (x2 of char length for AL16UTF16)
			useLen := orig.Length
			if strings.EqualFold(useType, "NVARCHAR2") || strings.EqualFold(useType, "NCHAR") {
				if n, err := strconv.Atoi(orig.Length); err == nil && n > 2000 {
					useLen = strconv.Itoa(n / 2)
				}
			}
			colDef += "(" + useLen + ")"
		}
		// Include DEFAULT (must come before NULL/NOT NULL in Oracle MODIFY parens)
		var defaultPart string
		if ch.HasDef != nil && *ch.HasDef {
			defaultPart = " DEFAULT " + formatDefault(ch.Default)
		} else if ch.HasDef != nil && !*ch.HasDef && orig.HasDef {
			defaultPart = " DEFAULT NULL"
		}
		colDef += defaultPart
		// Only include NULL/NOT NULL if nullability is actually changing
		if ch.Nullable != nil && *ch.Nullable != orig.Nullable {
			if *ch.Nullable {
				colDef += " NULL"
			} else {
				colDef += " NOT NULL"
			}
		}
		// Only emit MODIFY if something other than comment actually changed
		typeChanged := ch.Type != "" && ch.Type != orig.Type
		lenChanged := ch.Length != "" && ch.Length != "0" && ch.Length != orig.Length
		nullChanged := ch.Nullable != nil && *ch.Nullable != orig.Nullable
		defChanged := (ch.HasDef != nil && *ch.HasDef) || (ch.HasDef != nil && !*ch.HasDef && orig.HasDef)
		if typeChanged || lenChanged || nullChanged || defChanged {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s MODIFY (%s)", tbl, colDef))
			origDef := quotePG(orig.Name) + " " + orig.Type
			if !orig.Nullable {
				origDef += " NOT NULL"
			}
			if orig.HasDef {
				origDef += " DEFAULT " + formatDefault(orig.Default)
			}
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s MODIFY (%s)", tbl, origDef))
		}
		// Comment change (only emit if actually changed)
		if ch.Comment != "" && ch.Comment != orig.Comment {
			stmts = append(stmts, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quotePG(ch.Name), pgString(ch.Comment)))
			rollbacks = append(rollbacks, fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s", tbl, quotePG(ch.Name), pgString(orig.Comment)))
		}
	} else if dbType == "sqlserver" {
		// Only generate ALTER COLUMN if there are actual column definition changes
		colModified := ch.Type != "" || ch.Length != "" || ch.Nullable != nil
		if colModified {
			colDef := "[" + ch.Name + "] "
			if ch.Type != "" {
				colDef += ch.Type
			} else {
				colDef += orig.Type
			}
			if ch.Length != "" && ch.Length != "0" {
				colDef = "[" + ch.Name + "] " + ch.Type + "(" + ch.Length + ")"
			}
			if ch.Nullable != nil {
				if *ch.Nullable {
					colDef += " NULL"
				} else {
					colDef += " NOT NULL"
				}
			}
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", tbl, colDef))
			origDef := "[" + orig.Name + "] " + orig.Type
			if !orig.Nullable {
				origDef += " NOT NULL"
			}
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s", tbl, origDef))
		}
		// Default value (SQL Server uses ADD/DROP DEFAULT as separate statements)
		// Always drop any existing default constraint first to be safe
		if ch.HasDef != nil && *ch.HasDef {
			// Drop any existing default constraint
			stmts = append(stmts, fmt.Sprintf(
				"DECLARE @cn NVARCHAR(200); "+
					"SELECT @cn = name FROM sys.default_constraints "+
					"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
					"IF @cn IS NOT NULL EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn)",
				tbl, tbl, ch.Name, tbl))
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ADD DEFAULT %s FOR [%s]", tbl, formatDefault(ch.Default), ch.Name))
			// Rollback: drop the newly added default
			rollbacks = append(rollbacks, fmt.Sprintf(
				"DECLARE @cn NVARCHAR(200); "+
					"SELECT @cn = name FROM sys.default_constraints "+
					"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
					"EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn)",
				tbl, tbl, ch.Name, tbl))
		} else if ch.HasDef != nil && !*ch.HasDef && orig.HasDef {
			// Removing an existing default
			stmts = append(stmts, fmt.Sprintf(
				"DECLARE @cn NVARCHAR(200); "+
					"SELECT @cn = name FROM sys.default_constraints "+
					"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
					"EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn)",
				tbl, tbl, ch.Name, tbl))
			// Rollback: restore original default
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ADD DEFAULT %s FOR [%s]", tbl, formatDefault(orig.Default), ch.Name))
		}
		// Column comment via extended properties (always generate if comment changed)
		if ch.Comment != "" {
			stmts = append(stmts, drivers.BuildSQLServerColumnComment(tbl, ch.Name, ch.Comment))
		}
		if orig.Comment != "" {
			rollbacks = append(rollbacks, drivers.BuildSQLServerColumnComment(tbl, ch.Name, orig.Comment))
		}
	} else {
		// PG/Oracle/SQL Server: only generate statements for actual changes
		newType := s.pgColumnTypeFromChange(ch)
		origType := s.pgColumnTypeFromColumn(orig)
		if newType != origType {
			stmts = append(stmts, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tbl, quotePG(ch.Name), newType))
			rollbacks = append(rollbacks, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s", tbl, quotePG(ch.Name), origType))
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
	}
	return stmts, rollbacks, warnings, highRisk, nil
}

func (s *TableManagerService) pgColumnTypeFromColumn(c TableColumn) string {
	t := strings.ToLower(c.Type)
	switch t {
	case "character varying":
		t = "varchar"
	case "character":
		t = "char"
	}
	if c.Length != "" && needsLength(t) {
		return fmt.Sprintf("%s(%s)", t, c.Length)
	}
	return t
}

func (s *TableManagerService) buildDropColumn(dbType, tbl string, ch ColumnChange, cur map[string]TableColumn) (string, string, []string, bool, error) {
	if ch.Name == "" {
		return "", "", nil, false, fmt.Errorf("column name required")
	}
	orig, exists := cur[ch.Name]
	if !exists {
		return "", "", nil, false, fmt.Errorf("column '%s' not found", ch.Name)
	}
	warnings := []string{fmt.Sprintf("DROP_COLUMN %s: data in this column will be permanently lost", ch.Name)}
	highRisk := true

	var stmt, rollback string
	switch dbType {
	case "mysql", "mariadb", "oceanbase":
		stmt = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tbl, quoteMySQL(ch.Name))
		origDef := quoteMySQL(orig.Name) + " " + orig.Type
		if !orig.Nullable {
			origDef += " NOT NULL"
		} else {
			origDef += " NULL"
		}
		if orig.HasDef {
			origDef += " DEFAULT " + formatDefault(orig.Default)
		}
		if orig.Comment != "" {
			origDef += " COMMENT " + mysqlString(orig.Comment)
		}
		rollback = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tbl, origDef)
	case "postgres", "postgresql":
		stmt = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tbl, quotePG(ch.Name))
		origDef := quotePG(orig.Name) + " " + s.pgColumnTypeFromColumn(orig)
		if !orig.Nullable {
			origDef += " NOT NULL"
		}
		if orig.HasDef {
			origDef += " DEFAULT " + formatDefault(orig.Default)
		}
		rollback = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tbl, origDef)
		if orig.Comment != "" {
			rollback += fmt.Sprintf(";\nCOMMENT ON COLUMN %s.%s IS %s", tbl, quotePG(orig.Name), pgString(orig.Comment))
		}
	case "oracle":
		stmt = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tbl, quotePG(ch.Name))
		origDef := quotePG(orig.Name) + " " + orig.Type
		if !orig.Nullable {
			origDef += " NOT NULL"
		}
		if orig.HasDef {
			origDef += " DEFAULT " + formatDefault(orig.Default)
		}
		rollback = fmt.Sprintf("ALTER TABLE %s ADD (%s)", tbl, origDef)
	case "sqlserver":
		// If the column has a default constraint, drop it first
		var dropDefaultStmt string
		if orig.HasDef {
			dropDefaultStmt = fmt.Sprintf(
				"DECLARE @cn NVARCHAR(200); "+
					"SELECT @cn = name FROM sys.default_constraints "+
					"WHERE parent_object_id = OBJECT_ID('%s') AND parent_column_id = COLUMNPROPERTY(OBJECT_ID('%s'), '%s', 'ColumnId'); "+
					"EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @cn); ",
				tbl, tbl, ch.Name, tbl)
		}
		stmt = dropDefaultStmt + fmt.Sprintf("ALTER TABLE %s DROP COLUMN [%s]", tbl, ch.Name)
		origDef := "[" + orig.Name + "] " + orig.Type
		if !orig.Nullable {
			origDef += " NOT NULL"
		}
		rollback = fmt.Sprintf("ALTER TABLE %s ADD %s", tbl, origDef)
	default:
		stmt = fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tbl, quotePG(ch.Name))
		origDef := quotePG(orig.Name) + " " + orig.Type
		rollback = fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tbl, origDef)
	}
	return stmt, rollback, warnings, highRisk, nil
}

func (s *TableManagerService) buildRenameColumn(dbType, tbl string, ch ColumnChange) (string, string, error) {
	if ch.Name == "" || ch.NewName == "" {
		return "", "", fmt.Errorf("rename requires name and new_name")
	}
	var stmt, rollback string
	if dbType == "mysql" {
		stmt = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tbl, quoteMySQL(ch.Name), quoteMySQL(ch.NewName))
		rollback = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tbl, quoteMySQL(ch.NewName), quoteMySQL(ch.Name))
	} else {
		stmt = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tbl, quotePG(ch.Name), quotePG(ch.NewName))
		rollback = fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", tbl, quotePG(ch.NewName), quotePG(ch.Name))
	}
	return stmt, rollback, nil
}

func (s *TableManagerService) buildAddIndex(dbType, tbl string, ch IndexChange, curCols map[string]TableColumn) (string, string, error) {
	if ch.Name == "" {
		return "", "", fmt.Errorf("index name required")
	}
	if len(ch.Columns) == 0 {
		return "", "", fmt.Errorf("index must have at least one column")
	}
	var cols string
	if dbType == "mysql" {
		cols = strings.Join(quoteList(ch.Columns, quoteMySQL), ", ")
	} else if dbType == "oracle" {
		// Oracle stores unquoted identifiers as UPPERCASE by default.
		// Columns must be quoted with uppercase to match the actual DB names.
		cols = strings.Join(quoteList(ch.Columns, func(n string) string {
			return quotePG(strings.ToUpper(n))
		}), ", ")
	} else if dbType == "sqlserver" {
		cols = strings.Join(quoteList(ch.Columns, func(n string) string {
			return "[" + strings.ReplaceAll(n, "]", "]]") + "]"
		}), ", ")
	} else {
		cols = strings.Join(quoteList(ch.Columns, quotePG), ", ")
	}
	typ := strings.ToUpper(ch.Type)
	var stmt string
	switch typ {
	case "UNIQUE":
		stmt = fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
	case "UNIQUE CLUSTERED":
		if dbType != "sqlserver" {
			return "", "", fmt.Errorf("UNIQUE CLUSTERED index is SQL Server only")
		}
		stmt = fmt.Sprintf("CREATE UNIQUE CLUSTERED INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
	case "CLUSTERED":
		if dbType == "sqlserver" {
			stmt = fmt.Sprintf("CREATE CLUSTERED INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
		} else if dbType == "oracle" {
			stmt = fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
		} else {
			return "", "", fmt.Errorf("CLUSTERED index is not supported for %s", dbType)
		}
	case "FULLTEXT":
		if dbType != "mysql" {
			return "", "", fmt.Errorf("FULLTEXT index is MySQL only")
		}
		stmt = fmt.Sprintf("CREATE FULLTEXT INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
	case "SPATIAL":
		if dbType == "mysql" || dbType == "sqlserver" {
			// MySQL SPATIAL index requires all indexed columns to be NOT NULL
			if dbType == "mysql" {
				for _, col := range ch.Columns {
					if c, ok := curCols[col]; ok && c.Nullable {
						return "", "", fmt.Errorf("SPATIAL index requires column '%s' to be NOT NULL (MySQL limitation)", col)
					}
				}
			}
			stmt = fmt.Sprintf("CREATE SPATIAL INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
		} else {
			return "", "", fmt.Errorf("SPATIAL index is not supported for %s", dbType)
		}
	case "BITMAP":
		if dbType != "oracle" {
			return "", "", fmt.Errorf("BITMAP index is Oracle only")
		}
		stmt = fmt.Sprintf("CREATE BITMAP INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
	case "XML":
		if dbType != "sqlserver" {
			return "", "", fmt.Errorf("XML index is SQL Server only")
		}
		stmt = fmt.Sprintf("CREATE PRIMARY XML INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
	default:
		// PostgreSQL-specific index methods: HASH, GIST, GIN, SPGIST, BRIN
		if dbType == "postgres" || dbType == "postgresql" {
			upper := strings.ToUpper(ch.Type)
			switch upper {
			case "HASH", "GIST", "GIN", "SPGIST", "BRIN":
				stmt = fmt.Sprintf("CREATE INDEX %s ON %s USING %s (%s)", quoteIdx(dbType, ch.Name), tbl, strings.ToLower(ch.Type), cols)
			default:
				stmt = fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
			}
		} else {
			stmt = fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quoteIdx(dbType, ch.Name), tbl, cols)
		}
	}
	rollback := fmt.Sprintf("DROP INDEX %s", quoteIdx(dbType, ch.Name))
	// Extract schema from qualified table name "schema"."table"
	sch := ""
	if idx := strings.Index(tbl, "."); idx > 0 {
		sch = strings.Trim(tbl[:idx], "\"")
	}
	if dbType == "postgres" || dbType == "oracle" {
		if sch != "" {
			rollback = fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", quotePG(sch), quotePG(ch.Name))
		} else {
			rollback = fmt.Sprintf("DROP INDEX IF EXISTS %s", quotePG(ch.Name))
		}
	}
	// Add index comment if provided
	if ch.Comment != "" {
		if dbType == "mysql" {
			stmt += " COMMENT '" + strings.ReplaceAll(ch.Comment, "'", "''") + "'"
		} else if dbType == "postgres" {
			// PostgreSQL supports COMMENT ON INDEX
			qualifiedIdx := quotePG(ch.Name)
			if sch != "" {
				qualifiedIdx = quotePG(sch) + "." + qualifiedIdx
			}
			stmt += ";\nCOMMENT ON INDEX " + qualifiedIdx + " IS " + pgString(ch.Comment)
		}
	}
	return stmt, rollback, nil
}

func (s *TableManagerService) buildDropIndex(dbType, tbl, schema string, ch IndexChange, cur map[string]TableIndex) (string, string, []string, bool, error) {
	if ch.Name == "" {
		return "", "", nil, false, fmt.Errorf("index name required")
	}
	orig, exists := cur[ch.Name]
	if !exists {
		return "", "", nil, false, fmt.Errorf("index '%s' not found", ch.Name)
	}
	warnings := []string{fmt.Sprintf("DROP_INDEX %s: index will be removed", ch.Name)}
	highRisk := true

	var stmt, rollback string
	if dbType == "mysql" {
		stmt = fmt.Sprintf("DROP INDEX %s ON %s", quoteMySQL(ch.Name), tbl)
	} else if dbType == "postgres" {
		stmt = fmt.Sprintf("DROP INDEX IF EXISTS %s.%s", quotePG(schema), quotePG(ch.Name))
	} else if dbType == "oracle" {
		stmt = fmt.Sprintf("DROP INDEX %s.%s", quotePG(schema), quotePG(ch.Name))
	} else if dbType == "sqlserver" {
		stmt = fmt.Sprintf("DROP INDEX [%s] ON %s", ch.Name, tbl)
	} else {
		stmt = fmt.Sprintf("DROP INDEX %s", quotePG(ch.Name))
	}
	// Rollback: re-create
	cols := strings.Join(quoteList(orig.Columns, func(n string) string {
		if dbType == "mysql" {
			return quoteMySQL(n)
		}
		return quotePG(n)
	}), ", ")
	if orig.Type == "UNIQUE" {
		if dbType == "mysql" {
			rollback = fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", quoteMySQL(orig.Name), tbl, cols)
		} else {
			rollback = fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (%s)", quotePG(orig.Name), tbl, cols)
		}
	} else {
		if dbType == "mysql" {
			rollback = fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quoteMySQL(orig.Name), tbl, cols)
		} else {
			rollback = fmt.Sprintf("CREATE INDEX %s ON %s (%s)", quotePG(orig.Name), tbl, cols)
		}
	}
	return stmt, rollback, warnings, highRisk, nil
}

func (s *TableManagerService) buildIndexComment(dbType, tbl, schema string, ch IndexChange, cur map[string]TableIndex) (string, string, error) {
	if ch.Name == "" {
		return "", "", fmt.Errorf("index name required")
	}
	orig, exists := cur[ch.Name]
	if !exists {
		return "", "", fmt.Errorf("index '%s' not found", ch.Name)
	}
	var stmt, rollback string
	switch dbType {
	case "postgres", "postgresql":
		stmt = fmt.Sprintf("COMMENT ON INDEX %s.%s IS %s", quotePG(schema), quotePG(ch.Name), pgString(ch.Comment))
		rollback = fmt.Sprintf("COMMENT ON INDEX %s.%s IS %s", quotePG(schema), quotePG(ch.Name), pgString(orig.Comment))
	case "oracle":
		// Oracle does not support COMMENT ON INDEX
		return "", "", fmt.Errorf("Oracle does not support index comments")
	default:
		return "", "", fmt.Errorf("index comment not supported for %s", dbType)
	}
	return stmt, rollback, nil
}

func (s *TableManagerService) buildAddConstraint(dbType, tbl string, ch IndexChange) (string, string, error) {
	if ch.Name == "" {
		return "", "", fmt.Errorf("constraint name required")
	}
	// Re-use IndexChange fields: Name, Type (UNIQUE/FOREIGN KEY/CHECK), Columns, Comment holds ref info "ref_table(col)"
	typ := strings.ToUpper(ch.Type)
	var stmt, rollback string
	name := quoteIdx(dbType, ch.Name)
	switch typ {
	case "PRIMARY KEY":
		cols := strings.Join(quoteList(ch.Columns, func(n string) string {
			if dbType == "mysql" { return quoteMySQL(n) }
			if dbType == "sqlserver" { return "[" + strings.ReplaceAll(n, "]", "]]") + "]" }
			if dbType == "oracle" { return quotePG(strings.ToUpper(n)) }
			return quotePG(n)
		}), ", ")
		if dbType == "sqlserver" {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT [PK_%s] PRIMARY KEY (%s)", tbl, ch.Columns[0], cols)
		} else {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD PRIMARY KEY (%s)", tbl, cols)
		}
		rollback = "-- WARNING: cannot auto-rollback dropped PRIMARY KEY"
	case "UNIQUE":
		cols := strings.Join(quoteList(ch.Columns, func(n string) string {
			if dbType == "mysql" {
				return quoteMySQL(n)
			}
			if dbType == "oracle" {
				return quotePG(strings.ToUpper(n))
			}
			return quotePG(n)
		}), ", ")
		stmt = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s UNIQUE (%s)", tbl, name, cols)
		rollback = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tbl, name)
	case "FOREIGN KEY":
		if ch.Comment == "" {
			return "", "", fmt.Errorf("FK requires comment field formatted as 'ref_table(ref_col[:on_delete[:on_update]])'")
		}
		parts := strings.Split(ch.Comment, ":")
		ref := strings.TrimSpace(parts[0])
		onDel, onUpd := "NO ACTION", "NO ACTION"
		if len(parts) > 1 {
			onDel = strings.ToUpper(strings.TrimSpace(parts[1]))
		}
		if len(parts) > 2 {
			onUpd = strings.ToUpper(strings.TrimSpace(parts[2]))
		}
		// parse ref as "table(col)"
		refTable, refCol := parseRef(ref)
		if refTable == "" || refCol == "" {
			return "", "", fmt.Errorf("invalid FK reference: %s", ref)
		}
		var colQuoted, refT, refC string
		if dbType == "mysql" {
			colQuoted = strings.Join(quoteList(ch.Columns, quoteMySQL), ", ")
			refT = quoteMySQL(refTable)
			refC = quoteMySQL(refCol)
		} else {
			colQuoted = strings.Join(quoteList(ch.Columns, quotePG), ", ")
			refT = quotePG(refTable)
			refC = quotePG(refCol)
		}
		stmt = fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s (%s)", tbl, name, colQuoted, refT, refC)
		if onDel != "NO ACTION" {
			stmt += " ON DELETE " + onDel
		}
		if onUpd != "NO ACTION" {
			stmt += " ON UPDATE " + onUpd
		}
		rollback = fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tbl, name)
	default:
		return "", "", fmt.Errorf("unsupported constraint type: %s", typ)
	}
	return stmt, rollback, nil
}

func (s *TableManagerService) buildDropConstraint(dbType, tbl string, ch IndexChange) (string, string, error) {
	if ch.Name == "" {
		return "", "", fmt.Errorf("constraint name required")
	}
	if strings.ToUpper(ch.Name) == "PRIMARY" || strings.ToUpper(ch.Name) == "PRIMARY KEY" {
		var stmt string
		if dbType == "sqlserver" {
			stmt = fmt.Sprintf(
				"DECLARE @pk NVARCHAR(200); "+
					"SELECT @pk = name FROM sys.key_constraints WHERE type = 'PK' AND parent_object_id = OBJECT_ID('%s'); "+
					"EXEC('ALTER TABLE %s DROP CONSTRAINT ' + @pk)",
				tbl, tbl)
		} else {
			stmt = fmt.Sprintf("ALTER TABLE %s DROP PRIMARY KEY", tbl)
		}
		rollback := "-- WARNING: cannot auto-rollback dropped PRIMARY KEY"
		return stmt, rollback, nil
	}
	name := quoteIdx(dbType, ch.Name)
	stmt := fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tbl, name)
	rollback := fmt.Sprintf("-- WARNING: cannot auto-rollback dropped constraint %s (original definition not captured)", ch.Name)
	return stmt, rollback, nil
}

func (s *TableManagerService) buildTableComment(dbType, tbl string, newComment, oldComment string) (string, string, error) {
	var stmt, rollback string
	switch dbType {
	case "mysql":
		stmt = fmt.Sprintf("ALTER TABLE %s COMMENT %s", tbl, mysqlString(newComment))
		rollback = fmt.Sprintf("ALTER TABLE %s COMMENT %s", tbl, mysqlString(oldComment))
	case "postgres":
		stmt = fmt.Sprintf("COMMENT ON TABLE %s IS %s", tbl, pgString(newComment))
		rollback = fmt.Sprintf("COMMENT ON TABLE %s IS %s", tbl, pgString(oldComment))
	case "oracle":
		stmt = fmt.Sprintf("COMMENT ON TABLE %s IS '%s'", tbl, strings.ReplaceAll(newComment, "'", "''"))
		rollback = fmt.Sprintf("COMMENT ON TABLE %s IS '%s'", tbl, strings.ReplaceAll(oldComment, "'", "''"))
	case "sqlserver":
		// SQL Server uses sp_addextendedproperty; parse schema.table from tbl.
		// tbl can be [db].[schema].[table] (3-part) or [schema].[table] (2-part).
		parts := strings.Split(tbl, ".")
		var schemaName, tableNameOnly string
		if len(parts) >= 3 {
			schemaName = strings.Trim(parts[len(parts)-2], "[]")
			tableNameOnly = strings.Trim(parts[len(parts)-1], "[]")
		} else if len(parts) == 2 {
			schemaName = strings.Trim(parts[0], "[]")
			tableNameOnly = strings.Trim(parts[1], "[]")
		} else {
			schemaName = "dbo"
			tableNameOnly = strings.Trim(parts[0], "[]")
		}
		qualified := schemaName + "." + tableNameOnly
		safeComment := strings.ReplaceAll(newComment, "'", "''")
		safeOld := strings.ReplaceAll(oldComment, "'", "''")
		stmt = fmt.Sprintf("IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID('%s') AND minor_id = 0 AND name = 'MS_Description')\n"+
			"  EXEC sys.sp_dropextendedproperty @name=N'MS_Description', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s';\n"+
			"EXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s'",
			qualified, schemaName, tableNameOnly, safeComment, schemaName, tableNameOnly)
		rollback = fmt.Sprintf("IF EXISTS (SELECT 1 FROM sys.extended_properties WHERE major_id = OBJECT_ID('%s') AND minor_id = 0 AND name = 'MS_Description')\n"+
			"  EXEC sys.sp_dropextendedproperty @name=N'MS_Description', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s';\n"+
			"EXEC sys.sp_addextendedproperty @name=N'MS_Description', @value=N'%s', @level0type=N'SCHEMA', @level0name=N'%s', @level1type=N'TABLE', @level1name=N'%s'",
			qualified, schemaName, tableNameOnly, safeOld, schemaName, tableNameOnly)
	}
	return stmt, rollback, nil
}

func quoteIdx(dbType, name string) string {
	if dbType == "mysql" {
		return quoteMySQL(name)
	}
	if dbType == "sqlserver" {
		return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
	}
	return quotePG(name)
}

func quoteList(names []string, q func(string) string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = q(n)
	}
	return out
}

func parseRef(ref string) (string, string) {
	i := strings.Index(ref, "(")
	if i < 0 {
		return "", ""
	}
	if !strings.HasSuffix(ref, ")") {
		return "", ""
	}
	return strings.TrimSpace(ref[:i]), strings.TrimSpace(ref[i+1 : len(ref)-1])
}

func formatDefault(v string) string {
	if v == "" {
		return "''"
	}
	lower := strings.ToLower(v)
	switch lower {
	case "null", "current_timestamp", "now()", "true", "false", "current_date", "current_time":
		return v
	}
	// numeric
	if isNumeric(v) {
		return v
	}
	// already quoted
	if (strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) || (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) {
		return v
	}
	// expression like nextval(...)
	if strings.Contains(v, "(") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, r := range s {
		if r == '-' && i == 0 {
			continue
		}
		if r == '.' && !dot {
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toInt(s string) int64 {
	var v int64
	fmt.Sscanf(s, "%d", &v)
	return v
}

func isReservedWord(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case "select", "from", "where", "insert", "update", "delete", "drop", "table", "index",
		"column", "database", "schema", "view", "trigger", "procedure", "function", "group",
		"order", "having", "limit", "offset", "join", "union", "values", "set", "key", "user",
		"role", "grant", "revoke", "primary", "foreign", "unique", "check", "default", "null",
		"not", "and", "or", "like", "in", "between", "exists", "case", "when", "then", "else",
		"end", "create", "alter", "add", "constraint", "references", "on", "asc", "desc":
		return true
	}
	return false
}

// ExecuteViewDDL executes a view DDL statement (CREATE VIEW, ALTER VIEW, DROP VIEW)
// through the query service, returning column info on success.
func (s *TableManagerService) ExecuteViewDDL(dsID, schema, view, sql, database string) (map[string]interface{}, error) {
	driver, err := s.connectDriverForDB(dsID, database)
	if err != nil {
		return nil, fmt.Errorf("connect driver: %w", err)
	}
	defer driver.Close()

	result, err := driver.ExecuteQuery(sql, schema)
	if err != nil {
		return nil, fmt.Errorf("exec error: %w", err)
	}

	return map[string]interface{}{
		"success":   true,
		"columns":   result.Columns,
		"rows":      result.Rows,
		"total_rows": result.TotalRows,
		"duration":  result.Duration,
	}, nil
}
