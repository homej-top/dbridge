package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service/drivers"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"gorm.io/gorm"
)

// schemaNamePattern validates schema/database/user names to prevent SQL injection.
// Allows letters, digits, underscores, dots (for qualified names) and brackets (SQL Server).
var schemaNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_.\[\]-]*$`)

// alterDatabaseRenamePattern detects ALTER DATABASE ... RENAME TO for PostgreSQL.
// PostgreSQL does not allow renaming the currently connected database, so we must
// switch to the "postgres" maintenance database before executing this DDL.
var alterDatabaseRenamePattern = regexp.MustCompile(`(?i)^ALTER\s+DATABASE\s+["\x60\[]?[\w]+["\x60\]]?\s+RENAME\s+TO`)

// sqlserverRenameSchemaPattern detects the marker for SQL Server schema rename.
// Format: -- @RENAME_SCHEMA [old_name] TO [new_name]
var sqlserverRenameSchemaPattern = regexp.MustCompile(`^--\s*@RENAME_SCHEMA\s+\[([^\]]+)\]\s+TO\s+\[([^\]]+)\]`)

// validateSchemaName checks that a schema name is safe for use in SQL identifiers.
func validateSchemaName(schema string) error {
	if schema == "" {
		return nil
	}
	if len(schema) > 128 {
		return fmt.Errorf("schema 名称过长: %d 字符", len(schema))
	}
	if !schemaNamePattern.MatchString(schema) {
		return fmt.Errorf("无效的 schema 名称 (仅允许字母、数字、下划线、点): %s", schema)
	}
	return nil
}

type QueryService struct {
	db *gorm.DB
}

func NewQueryService(db *gorm.DB) *QueryService {
	return &QueryService{db: db}
}

type QueryInput struct {
	DataSourceID       string `json:"data_source_id" binding:"required"`
	SQL                string `json:"sql" binding:"required"`
	Schema             string `json:"schema"`
	Database           string `json:"database"`
	Page               int    `json:"page"`
	PageSize           int    `json:"page_size"`
	Category           string `json:"category"` // "data" | "meta" | "other" — classified by frontend
	SubscriptionStatus string `json:"-"`        // subscription status from middleware
}

type QueryOutput struct {
	Columns      []string        `json:"columns"`
	Rows         [][]interface{} `json:"rows"`
	TotalRows    int64           `json:"total_rows"`
	Duration     int64           `json:"duration"` // milliseconds
	Mode         string          `json:"mode"`     // "data" | "meta" | "message"
	Truncated    bool            `json:"truncated,omitempty"`
	AffectedRows int64           `json:"affected_rows,omitempty"`
}

func (s *QueryService) Execute(input QueryInput) (*QueryOutput, error) {
	driver, err := s.connectDriver(context.Background(), input.DataSourceID, input.Database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	start := time.Now()

	// Handle SQL Server @RENAME_SCHEMA marker
	if matches := sqlserverRenameSchemaPattern.FindStringSubmatch(strings.TrimSpace(input.SQL)); matches != nil {
		return s.executeSQLServerSchemaRename(driver, matches[1], matches[2], start)
	}

	// Unified execution path - let the driver decide how to execute
	result, err := driver.ExecuteQuery(input.SQL, input.Schema)
	if err != nil {
		return nil, fmt.Errorf("execution error: %w", err)
	}

	// Determine mode based on actual result structure
	mode := s.determineMode(result)

	return &QueryOutput{
		Columns:      result.Columns,
		Rows:         result.Rows,
		TotalRows:    result.TotalRows,
		Duration:     time.Since(start).Milliseconds(),
		Mode:         mode,
		AffectedRows: result.AffectedRows,
	}, nil
}

// determineMode automatically selects the display mode based on query result structure
func (s *QueryService) determineMode(result *drivers.QueryResult) string {
	// Case 1: Has columns → table mode (SELECT, SHOW, EXEC with results, empty tables, etc.)
	if len(result.Columns) > 0 {
		// Check if it's a single-column message format (from Exec fallback)
		if len(result.Columns) == 1 && result.Columns[0] == "result" && len(result.Rows) == 1 {
			return "message"
		}
		return "data"
	}

	// Case 2: No columns but has affected rows → message mode (DDL/DML)
	if result.AffectedRows > 0 {
		return "message"
	}

	// Case 3: Empty result → message mode
	return "message"
}

// connectDriver creates a DatabaseDriver through the global pool manager.
func (s *QueryService) connectDriver(ctx context.Context, dataSourceID, database string) (drivers.DatabaseDriver, error) {
	var ds repository.DataSource
	if err := s.db.Where("id = ?", dataSourceID).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found")
	}
	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt password")
	}
	// PostgreSQL: ALTER DATABASE RENAME must run from a different database
	if (ds.Type == "postgres" || ds.Type == "postgresql") && database != "" && alterDatabaseRenamePattern.MatchString(database) {
		database = "postgres"
	}
	return ConnectDriver(ctx, ds, pwd, database)
}

// executeSQLServerSchemaRename renames a SQL Server schema.
func (s *QueryService) executeSQLServerSchemaRename(driver drivers.DatabaseDriver, oldName, newName string, start time.Time) (*QueryOutput, error) {
	// 1. Create new schema
	_, err := driver.ExecuteQuery(fmt.Sprintf("CREATE SCHEMA [%s]", newName), "")
	if err != nil {
		return nil, fmt.Errorf("failed to create new schema: %w", err)
	}
	// 2. Transfer all objects from old to new schema
	transferSQL := fmt.Sprintf(`DECLARE @sql NVARCHAR(MAX) = '';
SELECT @sql = @sql + 'ALTER SCHEMA [%s] TRANSFER [' + s.name + '].[' + o.name + ']; '
FROM sys.objects o
JOIN sys.schemas s ON o.schema_id = s.schema_id
WHERE s.name = '%s' AND o.type IN ('U','V','P','FN','IF','TF');
EXEC sp_executesql @sql;`, newName, oldName)
	_, err = driver.ExecuteQuery(strings.TrimSpace(transferSQL), "")
	if err != nil {
		return nil, fmt.Errorf("failed to transfer objects: %w", err)
	}
	// 3. Drop old schema
	_, err = driver.ExecuteQuery(fmt.Sprintf("DROP SCHEMA [%s]", oldName), "")
	if err != nil {
		return nil, fmt.Errorf("failed to drop old schema: %w", err)
	}
	duration := time.Since(start).Milliseconds()
	return &QueryOutput{
		Columns:   []string{"result"},
		Rows:      [][]interface{}{{"Schema renamed successfully"}},
		TotalRows: 1,
		Duration:  duration,
		Mode:      "message",
	}, nil
}
