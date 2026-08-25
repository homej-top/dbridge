package service

import (
	"github.com/dbridge/dbridge/internal/repository"
	"gorm.io/gorm"
)

type SettingsService struct {
	db *gorm.DB
}

func NewSettingsService(db *gorm.DB) *SettingsService {
	return &SettingsService{db: db}
}

func (s *SettingsService) Get(key string) (string, error) {
	var setting repository.Setting
	if err := s.db.Where("`key` = ?", key).First(&setting).Error; err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (s *SettingsService) Set(key, value, category string, isSecret bool) error {
	var setting repository.Setting
	result := s.db.Where("`key` = ?", key).First(&setting)
	if result.Error != nil && result.Error != gorm.ErrRecordNotFound {
		return result.Error
	}

	if result.RowsAffected > 0 {
		return s.db.Model(&setting).Updates(map[string]interface{}{
			"value":     value,
			"category":  category,
			"is_secret": isSecret,
		}).Error
	}

	setting = repository.Setting{
		Key:      key,
		Value:    value,
		Category: category,
		IsSecret: isSecret,
	}
	return s.db.Create(&setting).Error
}

func (s *SettingsService) GetByCategory(category string) (map[string]string, error) {
	var settings []repository.Setting
	if err := s.db.Where("category = ?", category).Find(&settings).Error; err != nil {
		return nil, err
	}
	result := make(map[string]string, len(settings))
	for _, st := range settings {
		result[st.Key] = st.Value
	}
	return result, nil
}

func (s *SettingsService) GetAIConfig() (map[string]string, error) {
	return s.GetByCategory("ai")
}

func (s *SettingsService) SetAIConfig(cfg map[string]string) error {
	for k, v := range cfg {
		isSecret := k == "ai_api_key"
		if err := s.Set(k, v, "ai", isSecret); err != nil {
			return err
		}
	}
	return nil
}

// DeleteConfig removes a setting by key
func (s *SettingsService) DeleteConfig(key string) error {
	return s.db.Where("`key` = ?", key).Delete(&repository.Setting{}).Error
}

// IsMCPEnabled returns whether the MCP server is enabled.
func (s *SettingsService) IsMCPEnabled() bool {
	val, err := s.Get("mcp_enabled")
	return err == nil && val == "true"
}

// SetMCPEnabled enables or disables the MCP server.
func (s *SettingsService) SetMCPEnabled(enabled bool) error {
	v := "false"
	if enabled {
		v = "true"
	}
	return s.Set("mcp_enabled", v, "mcp", false)
}
