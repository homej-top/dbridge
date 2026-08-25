package storage

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var globalManager *ProfileManager

// InitFromDB 从数据库加载所有启用的存储实例。
// 若数据库为空，则种子一个默认的本地存储实例。
func InitFromDB(db *gorm.DB) error {
	manager := NewProfileManager()

	if db != nil {
		if err := db.AutoMigrate(&StorageInstanceRecord{}); err != nil {
			return fmt.Errorf("storage: migrate table: %w", err)
		}

		// 种子默认 local 实例（若表为空，仅写 DB，内存注册由加载循环完成）
		if err := ensureDefaultLocalStorage(db); err != nil {
			log.Printf("storage: seed default local storage: %v", err)
		}

		// 从 DB 加载所有启用的实例
		var instances []StorageInstanceRecord
		if err := db.Where("enabled = ?", true).Order("sort_order ASC").Find(&instances).Error; err != nil {
			return fmt.Errorf("storage: query instances: %w", err)
		}

		for _, inst := range instances {
			fs, err := createFromJSON(inst.Backend, inst.ConfigJSON)
			if err != nil {
				log.Printf("storage: init instance '%s' (%s) failed: %v", inst.Code, inst.Name, err)
				continue
			}
			summary := summaryFromRecord(inst)
			if err := manager.Register(inst.Name, inst.Code, inst.Backend, fs, summary); err != nil {
				log.Printf("storage: register instance '%s' failed: %v", inst.Code, err)
				continue
			}
		}

		// 设置第一个为默认
		if len(instances) > 0 {
			manager.SetDefault(instances[0].Name)
		}
	} else {
		// 无数据库（测试场景）：直接创建内存 local 实例
		fs, err := NewLocalFileStorage("./data/files")
		if err == nil {
			manager.Register("local", "local", "local", fs, map[string]string{"root_dir": "./data/files"})
			manager.SetDefault("local")
		}
	}

	globalManager = manager
	manager.StartHealthCheck(5 * time.Minute)
	return nil
}

// ensureDefaultLocalStorage 确保 storage_instances 表至少有一个默认 local 实例。
// 只写数据库（内存注册由后续的 DB 加载循环完成），使用 ON CONFLICT 防并发。
func ensureDefaultLocalStorage(db *gorm.DB) error {
	// 检查是否已有实例
	var count int64
	db.Model(&StorageInstanceRecord{}).Count(&count)
	if count > 0 {
		return nil
	}

	candidates := []string{"./data/files"}
	candidates = append(candidates, filepath.Join(os.TempDir(), "dbridge", "files"))
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		candidates = append(candidates, filepath.Join(home, ".dbridge", "files"))
	}

	var lastErr error
	for _, root := range candidates {
		// 验证目录可创建
		if _, err := NewLocalFileStorage(root); err != nil {
			lastErr = err
			continue
		}

		cfgJSON := fmt.Sprintf(`{"root_dir":%q}`, root)
		err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&StorageInstanceRecord{
			Name: "local", Code: "local", Backend: "local", Enabled: true,
			ConfigJSON: cfgJSON, SortOrder: 0,
		}).Error
		if err != nil {
			lastErr = err
			continue
		}

		log.Printf("storage: default local storage seeded at '%s'", root)
		return nil
	}
	return lastErr
}

func createFromJSON(backend, configJSON string) (FileStorage, error) {
	return CreateFromJSONString(backend, configJSON)
}

// CreateFromJSONString 从 JSON 字符串创建 FileStorage（公开方法）
func CreateFromJSONString(backend, configJSON string) (FileStorage, error) {
	switch backend {
	case "local":
		var cfg LocalFactoryConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		return NewLocalFileStorage(cfg.RootDir)
	case "s3":
		var cfg S3FactoryConfig
		if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
			return nil, err
		}
		return NewS3FileStorage(S3Config{
			Endpoint:        cfg.Endpoint,
			Region:          cfg.Region,
			Bucket:          cfg.Bucket,
			AccessKeyID:     cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey,
			UsePathStyle:    cfg.UsePathStyle,
			DisableSSL:      cfg.DisableSSL,
			Prefix:          cfg.Prefix,
			SSEEnabled:      cfg.SSEEnabled,
		})
	default:
		return nil, fmt.Errorf("storage: unknown backend: %s", backend)
	}
}

func summaryFromRecord(inst StorageInstanceRecord) map[string]string {
	s := map[string]string{}
	if inst.Backend == "local" {
		var cfg LocalFactoryConfig
		json.Unmarshal([]byte(inst.ConfigJSON), &cfg)
		s["root_dir"] = cfg.RootDir
	} else {
		var cfg S3FactoryConfig
		json.Unmarshal([]byte(inst.ConfigJSON), &cfg)
		s["endpoint"] = cfg.Endpoint
		s["bucket"] = cfg.Bucket
		s["region"] = cfg.Region
	}
	return s
}

// StorageInstanceRecord 数据库记录（避免循环依赖）
type StorageInstanceRecord struct {
	Name       string `gorm:"type:varchar(100);not null"`
	Code       string `gorm:"type:varchar(50);uniqueIndex;not null"`
	Backend    string `gorm:"type:varchar(20);not null;default:local"`
	Enabled    bool   `gorm:"default:true"`
	ConfigJSON string `gorm:"type:text;not null"`
	SortOrder  int    `gorm:"default:0"`
}

func (StorageInstanceRecord) TableName() string { return "storage_instances" }

// Deprecated: 旧 Init 保留兼容，新代码使用 InitFromDB
func Init(cfg StorageFactoryConfig) error {
	return InitFromDB(nil)
}

// Get 返回默认 FileStorage（兼容旧代码）
func Get() FileStorage {
	if globalManager == nil {
		return nil
	}
	fs := globalManager.GetDefault()
	if fs == nil {
		for _, p := range globalManager.ListProfiles() {
			if p.Enabled {
				return globalManager.Get(p.Name)
			}
		}
	}
	return fs
}

// GetByName 按名称获取存储实例
func GetByName(name string) FileStorage {
	if globalManager == nil {
		return nil
	}
	return globalManager.Get(name)
}

// GetByCode 按代码获取存储实例
func GetByCode(code string) FileStorage {
	if globalManager == nil {
		return nil
	}
	return globalManager.GetByCode(code)
}

// GetManager 返回全局 ProfileManager
func GetManager() *ProfileManager { return globalManager }

// Reset 重置（仅测试用）
func Reset() {
	if globalManager != nil {
		_ = globalManager.Shutdown()
	}
	globalManager = nil
}

// EnsureDataDir ensures the local data directory exists
func EnsureDataDir(path string) error {
	return os.MkdirAll(path, 0755)
}
