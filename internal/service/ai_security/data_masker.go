package ai_security

import (
	"context"
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	cachePkg "github.com/dbridge/dbridge/pkg/cache"
)

// MaskRule defines a masking rule
type MaskRule struct {
	FieldPattern string `json:"field_pattern"`
	MaskFunc     string `json:"mask_func"`
}

// DefaultMaskRules are built-in rules applied globally
var DefaultMaskRules = []MaskRule{
	{FieldPattern: `(?i).*phone.*|.*mobile.*|.*tel.*`, MaskFunc: "phone"},
	{FieldPattern: `(?i).*email.*|.*mail.*`, MaskFunc: "email"},
	{FieldPattern: `(?i).*id_card.*|.*idcard.*|.*identity.*`, MaskFunc: "id_card"},
	{FieldPattern: `(?i).*password.*|.*passwd.*|.*pwd.*|.*secret.*`, MaskFunc: "hash"},
	{FieldPattern: `(?i).*bank.*|.*card.*|.*credit.*`, MaskFunc: "bank_card"},
	{FieldPattern: `(?i).*salary.*|.*wage.*|.*bonus.*`, MaskFunc: "hash"},
}

// maskRuleCache 脱敏规则专用进程内缓存（统一缓存接口的本地后端）。
// 说明：规则按"列"高频读取，跨实例一致性要求低，刻意使用进程内缓存，
// 避免逐列访问 Redis 带来的性能开销。
var maskRuleCache = cachePkg.NewLocalCache(30*time.Second, time.Minute, 10)

// maskRulesCacheKey 规则缓存 key。
func maskRulesCacheKey() string { return cachePkg.Key("maskrules", "global") }

// RefreshMaskRules reloads user-configured rules from the database
func RefreshMaskRules() {
	db := repository.GetDB()
	if db == nil {
		return
	}
	var rules []repository.MaskRuleConfig
	db.Where("enabled = ?", true).Find(&rules)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = maskRuleCache.SetJSON(ctx, maskRulesCacheKey(), rules, 30*time.Second)
}

// loadMaskRules 从缓存读取用户规则；未命中时刷新（查询 DB 并回填缓存）。
func loadMaskRules() []repository.MaskRuleConfig {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	var rules []repository.MaskRuleConfig
	if err := maskRuleCache.GetJSON(ctx, maskRulesCacheKey(), &rules); err == nil && len(rules) > 0 {
		return rules
	}
	RefreshMaskRules()
	_ = maskRuleCache.GetJSON(ctx, maskRulesCacheKey(), &rules)
	return rules
}

// ApplyMaskRules masks sensitive data in query results.
// Each row is map[string]interface{} with column name as key.
func ApplyMaskRules(columns []string, rows [][]interface{}) [][]interface{} {
	if !IsEnabled(KeyDataMask) {
		return rows
	}
	if columns == nil || rows == nil {
		return rows
	}

	// 每列只计算一次 mask 函数，避免逐格重复读取缓存/匹配正则
	maskFuncs := make([]string, len(columns))
	for j := range columns {
		maskFuncs[j] = getMaskFunc(columns[j])
	}

	result := make([][]interface{}, len(rows))
	for i, row := range rows {
		result[i] = make([]interface{}, len(row))
		for j, val := range row {
			maskFunc := maskFuncs[j]
			if maskFunc != "" && val != nil {
				result[i][j] = applyMask(maskFunc, fmt.Sprintf("%v", val))
			} else {
				result[i][j] = val
			}
		}
	}
	return result
}

func getMaskFunc(colName string) string {
	// 用户自定义规则（DB 配置，更高优先级）
	for _, rule := range loadMaskRules() {
		if matched, _ := regexp.MatchString("(?i)"+rule.ColumnName, colName); matched {
			return rule.MaskFunc
		}
	}

	// 内置默认规则
	for _, rule := range DefaultMaskRules {
		if matched, _ := regexp.MatchString(rule.FieldPattern, colName); matched {
			return rule.MaskFunc
		}
	}
	return ""
}

func applyMask(funcName, value string) string {
	switch funcName {
	case "phone":
		return maskPhone(value)
	case "email":
		return maskEmail(value)
	case "id_card":
		return maskIDCard(value)
	case "bank_card":
		return maskBankCard(value)
	case "hash":
		h := sha256.Sum256([]byte(value))
		return fmt.Sprintf("%x", h[:4])
	default:
		return "***"
	}
}

// maskTail 保留字符串末尾 n 位；不足 n 位时整体打码，避免切片越界。
func maskTail(s string, n int) string {
	if len(s) < n {
		return "****"
	}
	return "****" + s[len(s)-n:]
}

func maskPhone(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(cleaned) < 7 {
		return maskTail(cleaned, 4)
	}
	return cleaned[:3] + "****" + cleaned[len(cleaned)-4:]
}

func maskEmail(s string) string {
	parts := strings.SplitN(s, "@", 2)
	if len(parts) != 2 {
		return "***@" + parts[len(parts)-1]
	}
	if len(parts[0]) <= 2 {
		return "***@" + parts[1]
	}
	return string(parts[0][0]) + "***@" + parts[1]
}

func maskIDCard(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' || r == 'X' || r == 'x' {
			return r
		}
		return -1
	}, s)
	if len(cleaned) < 8 {
		return maskTail(cleaned, 4)
	}
	return cleaned[:4] + "****" + cleaned[len(cleaned)-4:]
}

func maskBankCard(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	if len(cleaned) < 8 {
		return maskTail(cleaned, 4)
	}
	return cleaned[:4] + "****" + cleaned[len(cleaned)-4:]
}
