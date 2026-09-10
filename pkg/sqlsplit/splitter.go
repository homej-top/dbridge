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
var goDelimiterRe = regexp.MustCompile(`(?i)^GO\s*$`)


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

// SplitSQL splits SQL into individual statements with GoSQLX validation.
// dialect controls string-aware splitting (e.g. MySQL backslash escapes, MSSQL GO).
// Pass "" for default behavior (standard SQL, no GO delimiter).
func SplitSQL(sqlText string, dialect string) ([]string, error) {
	r, err := SplitSQLDetailed(sqlText, dialect)
	if err != nil {
		return nil, err
	}
	return r.Statements, nil
}

// SplitSQLDetailed splits and classifies with GoSQLX tokenizer validation.
func SplitSQLDetailed(sqlText string, dialect string) (*SplitResult, error) {
	// Step 1: delimiter-based buffered splitting
	rawStmts, err := splitByDelimiter(sqlText, dialect)
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

// splitByDelimiter splits SQL by delimiter, handling BEGIN/END blocks,
// DELIMITER changes, string literals, and dialect-specific batch separators.
func splitByDelimiter(sqlText string, dialect string) ([]string, error) {
	reader := strings.NewReader(sqlText)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	delimiter := ";"
	var buf strings.Builder
	blockDepth := 0
	// State variables must be outside the loop to persist across lines (multi-line strings).
	inSingleQuote := false
	inDoubleQuote := false
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

		// Update string state; skip delimiter check if inside a string.
		updateStringState(trimmed, dialect, &inSingleQuote, &inDoubleQuote)
		if inSingleQuote || inDoubleQuote {
			continue
		}

		// MSSQL GO batch delimiter (must be on its own line, outside strings).
		if dialect == "mssql" && goDelimiterRe.MatchString(trimmed) {
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				stmts = append(stmts, stmt)
			}
			buf.Reset()
			continue
		}

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


// updateStringState updates inSingleQuote/inDoubleQuote based on the content of a trimmed line.
// It correctly handles:
//   - Standard SQL: '' inside a single-quoted string is an escaped quote (not end of string)
//   - MySQL: \' ends/starts a string, \\ prevents the next char from being special
//   - Double-quoted identifiers (PostgreSQL, Oracle, standard SQL)
func updateStringState(line string, dialect string, inSingleQuote *bool, inDoubleQuote *bool) {
	isMySQL := dialect == "mysql" || dialect == "mariadb" || dialect == "oceanbase"

	i := 0
	for i < len(line) {
		c := line[i]

		if *inSingleQuote {
			if isMySQL && c == '\\' && i+1 < len(line) {
				i += 2 // skip escaped character
				continue
			}
			if c == '\'' {
				if isMySQL {
					*inSingleQuote = false
					i++
					continue
				}
				// Standard SQL: '' is an escaped quote inside the string
				if i+1 < len(line) && line[i+1] == '\'' {
					i += 2
					continue
				}
				*inSingleQuote = false
				i++
				continue
			}
			i++
			continue
		}

		if *inDoubleQuote {
			if c == '"' {
				if i+1 < len(line) && line[i+1] == '"' {
					i += 2 // "" escaped double-quote
					continue
				}
				*inDoubleQuote = false
				i++
				continue
			}
			i++
			continue
		}

		// Not inside any string
		switch c {
		case '\'':
			*inSingleQuote = true
		case '"':
			*inDoubleQuote = true
		case '-':
			if i+1 < len(line) && line[i+1] == '-' {
				return // rest of line is a comment
			}
		}
		i++
	}
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
func SplitStream(reader io.Reader, dialect string, fn func(stmt string) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	delimiter := ";"
	var buf strings.Builder
	blockDepth := 0
	inSingleQuote := false
	inDoubleQuote := false

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

		updateStringState(trimmed, dialect, &inSingleQuote, &inDoubleQuote)
		if inSingleQuote || inDoubleQuote {
			continue
		}

		if dialect == "mssql" && goDelimiterRe.MatchString(trimmed) {
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				if err := fn(stmt); err != nil {
					return err
				}
			}
			buf.Reset()
			continue
		}

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
				if err := fn(stmt); err != nil {
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
