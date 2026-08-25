// Package sqlsplit provides SQL statement splitting with GoSQLX validation + delimiter-based fallback.
// Supports DDL/DML classification, dangerous statement detection, and DELIMITER state handling.
package sqlsplit

import (
	"bufio"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/ajitpratap0/GoSQLX/pkg/sql/tokenizer"
)

type Dialect string

const (
	MySQL    Dialect = "mysql"
	Postgres Dialect = "postgresql"
	MSSQL    Dialect = "mssql"
	Oracle   Dialect = "oracle"
	SQLite   Dialect = "sqlite"
)

func DialectFromDBType(dbType string) Dialect {
	switch strings.ToLower(dbType) {
	case "mysql", "mariadb", "oceanbase":
		return MySQL
	case "postgres", "postgresql":
		return Postgres
	case "sqlserver", "mssql":
		return MSSQL
	case "oracle":
		return Oracle
	case "sqlite":
		return SQLite
	default:
		return MySQL
	}
}

var delimiterRe = regexp.MustCompile(`(?i)^DELIMITER\s+([^\s;]+)`)


// StmtType classifies SQL statements
type StmtType int

const (
	StmtTypeDDL   StmtType = 1
	StmtTypeDML   StmtType = 2
	StmtTypeOther StmtType = 3
)

// SplitResult holds classified split results
type SplitResult struct {
	Statements   []string
	StmtTypes    []StmtType
	DDL          []string
	DML          []string
	HasDanger    bool
	DangerStmts  []string
	Degraded     bool // true if GoSQLX validation failed on any statement
	DegradedMsgs []string
}

// SplitSQL splits SQL into individual statements with GoSQLX validation
func SplitSQL(sqlText string) ([]string, error) {
	r, err := SplitSQLDetailed(sqlText)
	if err != nil {
		return nil, err
	}
	return r.Statements, nil
}

// SplitSQLDetailed splits and classifies with GoSQLX tokenizer validation
func SplitSQLDetailed(sqlText string) (*SplitResult, error) {
	// Step 1: delimiter-based buffered splitting
	rawStmts, err := splitByDelimiter(sqlText)
	if err != nil {
		return nil, err
	}

	result := &SplitResult{}

	// Step 2: GoSQLX validation per statement
	tkz := tokenizer.GetTokenizer()
	defer tokenizer.PutTokenizer(tkz)

	for _, stmt := range rawStmts {
		if stmt == "" {
			continue
		}
		result.Statements = append(result.Statements, stmt)
		result.StmtTypes = append(result.StmtTypes, classifyStmtType(stmt))
		classifyStatement(stmt, result)

		// Validate tokenization — marks degraded if GoSQLX fails
		if _, tokErr := tkz.Tokenize([]byte(stmt)); tokErr != nil {
			result.Degraded = true
			result.DegradedMsgs = append(result.DegradedMsgs,
				fmt.Sprintf("GoSQLX validation: %v (statement: %.60s...)", tokErr, stmt))
		}
	}

	return result, nil
}

// splitByDelimiter splits SQL by delimiter, handling BEGIN/END blocks and DELIMITER changes
func splitByDelimiter(sqlText string) ([]string, error) {
	reader := strings.NewReader(sqlText)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	delimiter := ";"
	var buf strings.Builder
	blockDepth := 0
	var stmts []string

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}

		if matches := delimiterRe.FindStringSubmatch(trimmed); len(matches) > 1 {
			delimiter = strings.TrimSpace(matches[1])
			continue
		}

		buf.WriteString(line)
		buf.WriteByte('\n')

		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "BEGIN") || (strings.HasPrefix(upper, "CREATE") && strings.Contains(upper, "BEGIN")) {
			blockDepth++
		}
		if strings.HasPrefix(upper, "END") && !strings.Contains(upper, "CASE") {
			blockDepth--
		}

		if blockDepth <= 0 && strings.HasSuffix(trimmed, delimiter) {
			stmt := strings.TrimSpace(buf.String())
			stmt = strings.TrimSuffix(stmt, delimiter)
			stmt = strings.TrimSpace(stmt)
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			buf.Reset()
		}
	}

	if buf.Len() > 0 {
		stmt := strings.TrimSpace(buf.String())
		stmt = strings.TrimSuffix(stmt, delimiter)
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			stmts = append(stmts, stmt)
		}
	}

	return stmts, scanner.Err()
}


func classifyStmtType(stmt string) StmtType {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	ddlPrefixes := []string{"CREATE ", "ALTER ", "DROP ", "TRUNCATE ", "RENAME "}
	for _, p := range ddlPrefixes {
		if strings.HasPrefix(upper, p) { return StmtTypeDDL }
	}
	dmlPrefixes := []string{"INSERT ", "UPDATE ", "DELETE ", "SELECT ", "MERGE ", "REPLACE "}
	for _, p := range dmlPrefixes {
		if strings.HasPrefix(upper, p) { return StmtTypeDML }
	}
	return StmtTypeOther
}

func classifyStatement(stmt string, result *SplitResult) {
	upper := strings.ToUpper(strings.TrimSpace(stmt))
	ddlPrefixes := []string{
		"CREATE TABLE", "CREATE INDEX", "CREATE VIEW", "CREATE TRIGGER",
		"CREATE PROCEDURE", "CREATE FUNCTION", "CREATE SCHEMA", "CREATE DATABASE",
		"ALTER TABLE", "ALTER INDEX", "ALTER VIEW",
		"DROP TABLE", "DROP INDEX", "DROP VIEW", "DROP TRIGGER",
		"DROP PROCEDURE", "DROP FUNCTION", "DROP SCHEMA", "DROP DATABASE",
		"TRUNCATE TABLE", "RENAME TABLE",
	}
	for _, p := range ddlPrefixes {
		if strings.HasPrefix(upper, p) {
			result.DDL = append(result.DDL, stmt)
			if strings.HasPrefix(upper, "DROP ") || strings.HasPrefix(upper, "TRUNCATE ") {
				result.HasDanger = true
				result.DangerStmts = append(result.DangerStmts, stmt)
			}
			return
		}
	}
	dmlPrefixes := []string{"INSERT ", "UPDATE ", "DELETE ", "SELECT ", "MERGE ", "REPLACE "}
	for _, p := range dmlPrefixes {
		if strings.HasPrefix(upper, p) {
			result.DML = append(result.DML, stmt)
			return
		}
	}
}

// SplitStream reads SQL from reader and calls fn for each complete statement.
// Uses delimiter-based buffered reading — does NOT load full file into memory.
func SplitStream(reader io.Reader, fn func(stmt string) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	delimiter := ";"
	var buf strings.Builder
	blockDepth := 0

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		if matches := delimiterRe.FindStringSubmatch(trimmed); len(matches) > 1 {
			delimiter = strings.TrimSpace(matches[1])
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "BEGIN") {
			blockDepth++
		}
		if strings.HasPrefix(upper, "END") && !strings.Contains(upper, "CASE") {
			blockDepth--
		}
		if blockDepth <= 0 && strings.HasSuffix(trimmed, delimiter) {
			stmt := strings.TrimSpace(buf.String())
			stmt = strings.TrimSuffix(stmt, delimiter)
			stmt = strings.TrimSpace(stmt)
			if stmt != "" {
				finalStmt := stmt // value copy before Reset
				if err := fn(finalStmt); err != nil {
					return err
				}
			}
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		stmt := strings.TrimSpace(buf.String())
		stmt = strings.TrimSuffix(stmt, delimiter)
		stmt = strings.TrimSpace(stmt)
		if stmt != "" {
			if err := fn(stmt); err != nil {
				return err
			}
		}
	}
	return scanner.Err()
}

// CheckDangerousSQL scans for dangerous operations
func CheckDangerousSQL(stmts []string) []string {
	var dangerous []string
	patterns := []string{"DROP DATABASE", "DROP TABLE", "TRUNCATE TABLE", "DROP SCHEMA", "DROP ALL"}
	for _, stmt := range stmts {
		upper := strings.ToUpper(strings.TrimSpace(stmt))
		for _, p := range patterns {
			if strings.HasPrefix(upper, p) {
				dangerous = append(dangerous, stmt)
				break
			}
		}
	}
	return dangerous
}

// FormatDangerSummary formats dangerous statement summary
func FormatDangerSummary(dangerous []string) string {
	if len(dangerous) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("检测到 %d 条危险语句:\n", len(dangerous)))
	for i, stmt := range dangerous {
		if i >= 5 {
			sb.WriteString(fmt.Sprintf("... 还有 %d 条\n", len(dangerous)-5))
			break
		}
		display := stmt
		if len(display) > 100 {
			display = display[:100] + "..."
		}
		sb.WriteString(fmt.Sprintf("  - %s\n", display))
	}
	return sb.String()
}
