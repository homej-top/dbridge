package ai_security

import (
	"context"
	"fmt"
	"strings"

	"github.com/dbridge/dbridge/internal/repository"
)

// RiskLevel classifies SQL operations
type RiskLevel string

const (
	RiskL1 RiskLevel = "L1" // 低风险：自动执行
	RiskL2 RiskLevel = "L2" // 中风险：静默审批+熔断
	RiskL3 RiskLevel = "L3" // 高风险：强制审批
)

// DDL keywords that indicate high-risk operations
var ddlKeywords = []string{
	"DROP TABLE", "DROP DATABASE", "DROP VIEW", "DROP INDEX",
	"DROP ",
	"TRUNCATE TABLE", "TRUNCATE ",
	"ALTER TABLE", "ALTER COLUMN",
	"GRANT", "REVOKE",
}

// Unsafe write patterns (no WHERE clause)
var unsafeWritePatterns = []string{
	"UPDATE ", "DELETE FROM ",
}

// System schema prefixes
var systemSchemas = []string{
	"mysql.", "pg_catalog.",
	"master.", "msdb.", "tempdb.",
}

// ClassifyRisk determines the risk level of a SQL statement.
// This is a fast, rule-based classification that doesn't require EXPLAIN.
// For production use, combine with EXPLAIN-based row estimation.
func ClassifyRisk(sql string) (RiskLevel, string) {
	if !IsEnabled(KeyRiskControl) {
		return RiskL1, "risk control disabled"
	}

	upper := strings.ToUpper(strings.TrimSpace(sql))

	// Check system schema access
	for _, schema := range systemSchemas {
		if strings.Contains(upper, strings.ToUpper(schema)) {
			return RiskL3, fmt.Sprintf("访问系统表: %s", schema)
		}
	}

	// Check DDL
	for _, keyword := range ddlKeywords {
		if strings.Contains(upper, keyword) {
			return RiskL3, fmt.Sprintf("包含高危DDL: %s", keyword)
		}
	}

	// Check unsafe writes (no WHERE)
	for _, pattern := range unsafeWritePatterns {
		if strings.Contains(upper, pattern) && !strings.Contains(upper, "WHERE") {
			return RiskL3, fmt.Sprintf("无WHERE条件的写操作: %s", pattern)
		}
	}

	// Check DML with WHERE (could be L2)
	if strings.HasPrefix(upper, "UPDATE") || strings.HasPrefix(upper, "DELETE") {
		return RiskL2, "带条件的写操作"
	}

	if strings.HasPrefix(upper, "INSERT") {
		return RiskL2, "数据插入操作"
	}

	// SELECT, SHOW, DESCRIBE, EXPLAIN → L1
	return RiskL1, "只读查询"
}

// GenerateApprovalCard creates a structured approval card for the frontend
type ApprovalCard struct {
	RiskLevel    string `json:"risk_level"`
	SQL          string `json:"sql"`
	Operation    string `json:"operation"`
	Reason       string `json:"reason"`
	EstimateRows int64  `json:"estimate_rows"`
}

func BuildApprovalCard(sql string, risk RiskLevel, reason string) *ApprovalCard {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	op := "SELECT"
	for _, kw := range []string{"UPDATE", "DELETE", "INSERT", "DROP", "TRUNCATE", "ALTER", "CREATE"} {
		if strings.HasPrefix(upper, kw) {
			op = kw
			break
		}
	}
	return &ApprovalCard{
		RiskLevel: string(risk),
		SQL:       sql,
		Operation: op,
		Reason:    reason,
	}
}

// ValidateApprovalToken checks if an approval token is valid
func ValidateApprovalToken(ctx context.Context, token, sql string) bool {
	// TODO: implement proper token validation with expiration
	return token != "" && strings.Contains(token, ":approved:")
}


// GetDataSourceEnv returns the environment (dev/test/prod) for a data source
func GetDataSourceEnv(dsID string) string {
    db := repository.GetDB()
    if db == nil { return "dev" }
    var env string
    db.Table("data_sources").Where("id = ?", dsID).Select("env").Scan(&env)
    if env == "" { env = "dev" }
    return env
}
