package service

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

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

// safeString converts []byte to string, replacing invalid UTF-8 sequences
func safeString(b []byte) string {
	s := string(b)
	if !utf8.ValidString(s) {
		return strings.ToValidUTF8(s, "?")
	}
	return s
}

type QueryService struct {
	db *gorm.DB
}

func NewQueryService(db *gorm.DB) *QueryService {
	return &QueryService{db: db}
}

type QueryInput struct {
	DataSourceID     string `json:"data_source_id" binding:"required"`
	SQL              string `json:"sql" binding:"required"`
	Schema           string `json:"schema"`
	Database         string `json:"database"` // optional, for PG/MSSQL to target a specific database
	Page             int    `json:"page"`
	PageSize         int    `json:"page_size"`
}

type QueryOutput struct {
	Columns   []string        `json:"columns"`
	Rows      [][]interface{} `json:"rows"`
	TotalRows int64           `json:"total_rows"`
	Duration  int64           `json:"duration"` // milliseconds
}

func (s *QueryService) Execute(input QueryInput) (*QueryOutput, error) {
	driver, err := s.connectDriver(context.Background(), input.DataSourceID, input.Database)
	if err != nil {
		return nil, err
	}
	defer driver.Close()

	lower := strings.ToLower(strings.TrimSpace(input.SQL))
	isSelect := strings.HasPrefix(lower, "select") ||
		strings.HasPrefix(lower, "show ") ||
		strings.HasPrefix(lower, "describe ") ||
		strings.HasPrefix(lower, "desc ") ||
		strings.HasPrefix(lower, "explain ")

	start := time.Now()

	// Handle SQL Server @RENAME_SCHEMA marker
	if matches := sqlserverRenameSchemaPattern.FindStringSubmatch(strings.TrimSpace(input.SQL)); matches != nil {
		return s.executeSQLServerSchemaRename(driver, matches[1], matches[2], start)
	}

	if isSelect {
		return s.executePagedQuery(driver, input, start)
	}

	result, err := driver.ExecuteQuery(input.SQL, input.Schema)
	if err != nil {
		return nil, fmt.Errorf("exec error: %w", err)
	}

	duration := time.Since(start).Milliseconds()
	return &QueryOutput{
		Columns:   result.Columns,
		Rows:      result.Rows,
		TotalRows: result.TotalRows,
		Duration:  duration,
	}, nil
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

// executePagedQuery handles SELECT queries with optional server-side pagination.
func (s *QueryService) executePagedQuery(driver drivers.DatabaseDriver, input QueryInput, start time.Time) (*QueryOutput, error) {
	query := strings.TrimRight(strings.TrimSpace(input.SQL), ";")
	dbType := driver.DBType()

	// If page/pagesize not specified, run directly
	if input.Page <= 0 || input.PageSize <= 0 {
		result, err := driver.ExecuteQuery(query, input.Schema)
		if err != nil {
			return nil, fmt.Errorf("query error: %w", err)
		}
		return &QueryOutput{
			Columns:   result.Columns,
			Rows:      result.Rows,
			TotalRows: result.TotalRows,
			Duration:  time.Since(start).Milliseconds(),
		}, nil
	}

	// COUNT(*) for total — strip ORDER BY to avoid SQL Server subquery error
	countQuery := query
	upperQ := strings.ToUpper(query)
	if idx := strings.LastIndex(upperQ, "ORDER BY"); idx >= 0 {
		// Strip ORDER BY clause (keep everything before it)
		countQuery = strings.TrimSpace(query[:idx])
		// Also strip trailing semicolon if present
		countQuery = strings.TrimRight(countQuery, ";")
	}
	countSQL := fmt.Sprintf("SELECT COUNT(*) FROM (%s) AS cnt", countQuery)
	if dbType == "oracle" || dbType == "sqlserver" {
		countSQL = fmt.Sprintf("SELECT COUNT(*) FROM (%s) cnt", countQuery)
	}
	countResult, err := driver.ExecuteQuery(countSQL, input.Schema)
	if err != nil {
		return nil, fmt.Errorf("count query error: %w", err)
	}
	var total int64
	if len(countResult.Rows) > 0 && len(countResult.Rows[0]) > 0 {
		switch v := countResult.Rows[0][0].(type) {
		case int64:
			total = v
		case float64:
			total = int64(v)
		case string:
			total, _ = strconv.ParseInt(v, 10, 64)
		case []byte:
			total, _ = strconv.ParseInt(string(v), 10, 64)
		}
	}

	// Paginated query
	offset := (input.Page - 1) * input.PageSize
	if dbType == "oracle" || dbType == "sqlserver" {
		if dbType == "sqlserver" && !strings.Contains(strings.ToUpper(query), "ORDER BY") {
			query = query + " ORDER BY (SELECT NULL)"
		}
		query = fmt.Sprintf("%s OFFSET %d ROWS FETCH NEXT %d ROWS ONLY", query, offset, input.PageSize)
	} else {
		query = fmt.Sprintf("%s LIMIT %d OFFSET %d", query, input.PageSize, offset)
	}

	result, err := driver.ExecuteQuery(query, input.Schema)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}

	return &QueryOutput{
		Columns:   result.Columns,
		Rows:      result.Rows,
		TotalRows: total,
		Duration:  time.Since(start).Milliseconds(),
	}, nil
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
	}, nil
}
