package ai_security

import (
	"fmt"

	"github.com/dbridge/dbridge/internal/repository"
)

// IsEnabled checks if a security feature is enabled via Settings table.
// Returns true if the setting doesn't exist (default enabled).
func IsEnabled(key string) bool {
	db := repository.GetDB()
	if db == nil {
		return true
	}
	var val string
	if err := db.Table("settings").Where("`key` = ?", key).Select("value").Scan(&val).Error; err != nil {
		return true
	}
	return val != "false"
}

// Feature keys
const (
	KeyRiskControl    = "ai_security.risk_control.enabled"
	KeyDataMask       = "ai_security.data_mask.enabled"
	KeyPLLM           = "ai_security.pllm.enabled"
	KeyInputSanitizer = "ai_security.input_sanitizer.enabled"
	KeyAudit          = "ai_security.audit.enabled"
	KeyRateLimit      = "ai_security.rate_limit.enabled"
)

// GetSetting returns a setting value or default
func GetSetting(key, defaultVal string) string {
	db := repository.GetDB()
	if db == nil {
		return defaultVal
	}
	var val string
	if err := db.Table("settings").Where("`key` = ?", key).Select("value").Scan(&val).Error; err != nil {
		return defaultVal
	}
	return val
}

// GetIntSetting returns an integer setting or default
func GetIntSetting(key string, defaultVal int) int {
	s := GetSetting(key, "")
	if s == "" {
		return defaultVal
	}
	var v int
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil {
		return defaultVal
	}
	return v
}

// SaveSetting persists a setting key-value pair
func SaveSetting(key, value string) error {
	db := repository.GetDB()
	if db == nil {
		return fmt.Errorf("database not initialized")
	}
	// Use GORM's Migrator to check: SQLite has AUTOINCREMENT primary keys
	if db.Config.Dialector != nil && db.Config.Dialector.Name() == "sqlite" {
		return db.Exec("INSERT OR REPLACE INTO settings (`key`, `value`, `category`, `is_secret`) VALUES (?, ?, 'ai_security', ?)",
			key, value, false).Error
	}
	return db.Exec(
		"INSERT INTO settings (`key`, `value`) VALUES (?, ?) ON CONFLICT(`key`) DO UPDATE SET `value` = ?",
		key, value, value,
	).Error
}

// SeedDefaults writes default security settings if they don't exist
func SeedDefaults() {
	defaults := map[string]string{
		KeyRiskControl:    "true",
		KeyDataMask:       "true",
		KeyPLLM:           "false",
		KeyInputSanitizer: "true",
		KeyAudit:          "true",
		KeyRateLimit:      "true",
		"ai_security.env": "dev",
	}
	for k, v := range defaults {
		if GetSetting(k, "") == "" {
			_ = SaveSetting(k, v)
		}
	}

	// Load mask rules into cache on startup
	RefreshMaskRules()
}

// SeedGlobalMaskRules writes built-in global mask rules (data_source_id="")
func SeedGlobalMaskRules() {
	db := repository.GetDB()
	if db == nil {
		return
	}
	builtin := []struct {
		ColumnName string
		MaskFunc   string
	}{
		{"*phone*|*mobile*|*tel*", "phone"},
		{"*email*|*mail*", "email"},
		{"*id_card*|*idcard*|*identity*", "id_card"},
		{"*password*|*passwd*|*pwd*|*secret*", "hash"},
		{"*bank*|*card*|*credit*", "bank_card"},
	}
	for _, r := range builtin {
		var count int64
		db.Model(&repository.MaskRuleConfig{}).
			Where("data_source_id = '' AND column_name = ? AND mask_func = ?", r.ColumnName, r.MaskFunc).
			Count(&count)
		if count > 0 {
			continue
		}
		db.Create(&repository.MaskRuleConfig{
			ID:         "mask_global_" + r.MaskFunc,
			ColumnName: r.ColumnName,
			MaskFunc:   r.MaskFunc,
			Enabled:    true,
		})
	}
}
