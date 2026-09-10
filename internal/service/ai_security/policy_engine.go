package ai_security

import (
	"fmt"
	"strconv"
)

// PolicyConfig holds risk thresholds loaded from Settings
type PolicyConfig struct {
	Env           string  `json:"env"`
	L1MaxRows     int64   `json:"l1_max_rows"`
	L2MaxRows     int64   `json:"l2_max_rows"`
	L2MaxSec      float64 `json:"l2_max_sec"`
	MaxConcurrent int     `json:"max_concurrent"`
	QPSLimit      int     `json:"qps_limit"`
	DefaultMode   string  `json:"default_mode"`
}

// LoadPolicyConfig loads thresholds for a specific environment
func LoadPolicyConfig(env string) *PolicyConfig {
	if env == "" {
		env = GetSetting("ai_security.env", "dev")
	}
	return &PolicyConfig{
		Env:           env,
		L1MaxRows:     int64(GetIntSetting("ai_security."+env+".l1_max_rows", 10000)),
		L2MaxRows:     int64(GetIntSetting("ai_security."+env+".l2_max_rows", 50000)),
		L2MaxSec:      getFloatSetting("ai_security."+env+".l2_max_sec", 10),
		MaxConcurrent: GetIntSetting("ai_security."+env+".max_concurrent", 5),
		QPSLimit:      GetIntSetting("ai_security."+env+".qps_limit", 10),
		DefaultMode:   GetSetting("ai_security."+env+".default_mode", "approval"),
	}
}

// LoadPolicyConfigByDS loads thresholds for a data source's environment
func LoadPolicyConfigByDS(dsID string) *PolicyConfig {
	return LoadPolicyConfig(GetDataSourceEnv(dsID))
}

func getFloatSetting(key string, defaultVal float64) float64 {
	s := GetSetting(key, "")
	if s == "" {
		return defaultVal
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultVal
	}
	return v
}

// ClassifyRiskWithPolicy classifies SQL using EXPLAIN + threshold-based policy.
// Falls back to rule-based ClassifyRisk if EXPLAIN is unavailable.
func ClassifyRiskWithPolicy(dsID, sql string) (RiskLevel, string) {
	// First: fast rule-based check for DDL/system tables
	level, reason := ClassifyRisk(sql)
	if level == RiskL3 {
		return level, reason // DDL/system access is always L3
	}

	// For non-L3: attempt EXPLAIN-based analysis with thresholds
	cfg := LoadPolicyConfigByDS(dsID)

	// Try EXPLAIN to get row estimate
	rowsEstimate, err := estimateRows(cfg, dsID, sql)
	if err != nil {
		// EXPLAIN unavailable or limits disabled → use rule-based result
		if level == RiskL2 {
			return RiskL2, reason
		}
		return RiskL1, reason
	}

	// Apply threshold-based classification
	if rowsEstimate > cfg.L2MaxRows {
		return RiskL3, "预估扫描行数(" + strconv.FormatInt(rowsEstimate, 10) + ")超过L2上限(" + strconv.FormatInt(cfg.L2MaxRows, 10) + ")"
	}
	if rowsEstimate >= cfg.L1MaxRows {
		return RiskL2, "预估扫描行数(" + strconv.FormatInt(rowsEstimate, 10) + ")超过L1上限(" + strconv.FormatInt(cfg.L1MaxRows, 10) + ")"
	}
	return RiskL1, "预估扫描行数" + strconv.FormatInt(rowsEstimate, 10) + "，安全范围内"
}

// estimateRows attempts to get estimated row count via EXPLAIN.
// Returns -1 if EXPLAIN is unavailable or rows is unlimited (cfg has 0 or negative).
func estimateRows(cfg *PolicyConfig, dsID, sql string) (int64, error) {
	// Rule: 0 or negative means unlimited — skip EXPLAIN
	if cfg.L1MaxRows <= 0 && cfg.L2MaxRows <= 0 {
		return -1, fmt.Errorf("row limits disabled (L1=%d, L2=%d)", cfg.L1MaxRows, cfg.L2MaxRows)
	}

	result, err := ExplainEstimate(dsID, sql)
	if err != nil {
		return -1, err
	}
	if result == nil || result.RowsEstimate < 0 {
		return -1, fmt.Errorf("could not determine row estimate")
	}
	return result.RowsEstimate, nil
}
