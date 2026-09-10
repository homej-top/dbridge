package ai_security

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/dbridge/dbridge/internal/repository"
)

// DBConnector is a function that provides a pooled *sql.DB for a given data source.
// It is set by the service package at init time to avoid circular imports.
var DBConnector func(ctx context.Context, ds *repository.DataSource) (*sql.DB, error)

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

	if DBConnector == nil {
		return nil, fmt.Errorf("DBConnector not initialized")
	}
	db, err := DBConnector(context.Background(), ds)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	// 连接由连接池管理，不关闭

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
