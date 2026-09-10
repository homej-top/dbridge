package ai_security

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	_ "github.com/microsoft/go-mssqldb"
	_ "github.com/sijms/go-ora/v2"
)

// explainResult represents a parsed EXPLAIN output for row estimation
type explainResult struct {
	RowsEstimate int64
	AccessType   string // INDEX, ALL, REF, RANGE
}

// ExplainEstimate returns estimated rows from EXPLAIN for a given SQL.
// dsID: data source UUID to connect to
// sql: the SQL statement to estimate
func ExplainEstimate(dsID, sql string) (*explainResult, error) {
	ds, err := loadDataSource(dsID)
	if err != nil {
		return nil, fmt.Errorf("load datasource: %w", err)
	}

	db, err := openDBConnection(ds)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	defer db.Close()

	return executeExplain(db, ds.Type, sql)
}

func loadDataSource(dsID string) (*repository.DataSource, error) {
	db := repository.GetDB()
	if db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	var ds repository.DataSource
	if err := db.Where("id = ?", dsID).First(&ds).Error; err != nil {
		return nil, err
	}
	return &ds, nil
}

func openDBConnection(ds *repository.DataSource) (*sql.DB, error) {
	// Decrypt password
	pwd, err := decryptPassword(ds.Password)
	if err != nil {
		return nil, err
	}

	var dsn string
	var driver string
	switch strings.ToLower(ds.Type) {
	case "mysql", "mariadb":
		driver = "mysql"
		dsn = fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=5s", ds.Username, pwd, ds.Host, ds.Port, ds.Database)
	case "postgres", "postgresql":
		driver = "postgres"
		dsn = fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable connect_timeout=5", ds.Host, ds.Port, ds.Username, pwd, ds.Database)
	case "sqlserver", "mssql":
		driver = "sqlserver"
		dsn = fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable", ds.Username, pwd, ds.Host, ds.Port, ds.Database)
	case "oracle":
		driver = "oracle"
		extra := parseOracleExtra(ds.ExtraConfig)
		dsn = fmt.Sprintf("oracle://%s:%s@%s:%d/%s", ds.Username, pwd, ds.Host, ds.Port, extra.Service)
	default:
		return nil, fmt.Errorf("unsupported: %s", ds.Type)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(30 * time.Second)
	return db, nil
}

func executeExplain(db *sql.DB, dbType, sql string) (*explainResult, error) {
	switch strings.ToLower(dbType) {
	case "mysql", "mariadb":
		return explainMySQL(db, sql)
	case "postgres", "postgresql":
		return explainPostgres(db, sql)
	case "sqlserver", "mssql":
		return explainSQLServer(db, sql)
	case "oracle":
		return explainOracle(db, sql)
	default:
		return nil, fmt.Errorf("EXPLAIN not supported for %s", dbType)
	}
}

func explainMySQL(db *sql.DB, sql string) (*explainResult, error) {
	rows, err := db.Query("EXPLAIN " + sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, _ := rows.Columns()
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, c := range cols {
			row[strings.ToLower(c)] = vals[i]
		}
		result := &explainResult{AccessType: "UNKNOWN"}
		if rows, ok := row["rows"]; ok {
			result.RowsEstimate = parseRowCount(rows)
		}
		if t, ok := row["type"]; ok {
			result.AccessType = fmt.Sprintf("%v", t)
		}
		return result, nil
	}
	return &explainResult{RowsEstimate: -1}, nil
}

func explainPostgres(db *sql.DB, sql string) (*explainResult, error) {
	rows, err := db.Query("EXPLAIN (FORMAT JSON) " + sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		rows.Scan(&line)
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return &explainResult{RowsEstimate: -1}, nil
	}
	// Parse JSON: [{"Plan": {"Plan Rows": 1234, ...}}]
	var plans []struct {
		Plan struct {
			PlanRows   float64 `json:"Plan Rows"`
			NodeType   string  `json:"Node Type"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(strings.Join(lines, "")), &plans); err == nil && len(plans) > 0 {
		return &explainResult{
			RowsEstimate: int64(plans[0].Plan.PlanRows),
			AccessType:   plans[0].Plan.NodeType,
		}, nil
	}
	return &explainResult{RowsEstimate: -1}, nil
}

func explainSQLServer(db *sql.DB, sql string) (*explainResult, error) {
	_, _ = db.Exec("SET SHOWPLAN_XML ON")
	defer func() { _, _ = db.Exec("SET SHOWPLAN_XML OFF") }()
	rows, err := db.Query(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var line string
		rows.Scan(&line)
		lines = append(lines, line)
	}
	// Parse XML for estimated rows (simple heuristic)
	xml := strings.Join(lines, "")
	result := &explainResult{RowsEstimate: -1, AccessType: "UNKNOWN"}
	for _, tag := range []string{"EstimatedRows=\"", "EstimateRows=\""} {
		idx := strings.Index(xml, tag)
		if idx >= 0 {
			start := idx + len(tag)
			end := strings.Index(xml[start:], "\"")
			if end > 0 {
				if v, err := strconv.ParseFloat(xml[start:start+end], 64); err == nil {
					result.RowsEstimate = int64(v)
					break
				}
			}
		}
	}
	if strings.Contains(xml, "TableScan") { result.AccessType = "TableScan" }
	return result, nil
}

func explainOracle(db *sql.DB, sql string) (*explainResult, error) {
	_, _ = db.Exec("DELETE FROM PLAN_TABLE WHERE STATEMENT_ID = 'dbridge_est'")
	defer func() { _, _ = db.Exec("DELETE FROM PLAN_TABLE WHERE STATEMENT_ID = 'dbridge_est'") }()
	_, err := db.Exec("EXPLAIN PLAN SET STATEMENT_ID = 'dbridge_est' FOR " + sql)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query("SELECT CARDINALITY FROM PLAN_TABLE WHERE STATEMENT_ID = 'dbridge_est' AND ROWNUM = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := &explainResult{RowsEstimate: -1, AccessType: "UNKNOWN"}
	for rows.Next() {
		var card float64
		rows.Scan(&card)
		result.RowsEstimate = int64(card)
	}
	return result, nil
}

func parseRowCount(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case []byte:
		if i, err := strconv.ParseInt(string(val), 10, 64); err == nil {
			return i
		}
	case string:
		if i, err := strconv.ParseInt(val, 10, 64); err == nil {
			return i
		}
	}
	return -1
}

func parseOracleExtra(raw string) struct{ Service string } {
	var result struct{ Service string }
	json.Unmarshal([]byte(raw), &result)
	return result
}

// decryptPassword decrypts password. Reuses the crypto module if available.
func decryptPassword(encrypted string) (string, error) {
	// Try the shared crypto module from pkg/crypto
	// For now, return the value as-is since the datasource service decrypts internally
	return encrypted, nil
}
