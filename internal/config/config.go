package config

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Server       ServerConfig       `mapstructure:"server"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Cache        CacheConfig        `mapstructure:"cache"`
	NATS         NATSConfig         `mapstructure:"nats"`
	JWT          JWTConfig          `mapstructure:"jwt"`
	Crypto       CryptoConfig       `mapstructure:"crypto"`
	CORS         CORSConfig         `mapstructure:"cors"`
	Log          LogConfig          `mapstructure:"log"`
	Sync         SyncConfig         `mapstructure:"sync"`
	Storage      StorageConfig      `mapstructure:"storage"`
}

type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	Env          string        `mapstructure:"env"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type DatabaseConfig struct {
	Type       string             `mapstructure:"type"`
	MySQL      MySQLConfig        `mapstructure:"mysql"`
	PostgreSQL PostgreSQLConfig   `mapstructure:"postgresql"`
	SQLite     SQLiteConfig       `mapstructure:"sqlite"`
}

type MySQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"`
	Charset         string        `mapstructure:"charset"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

type PostgreSQLConfig struct {
	Host            string        `mapstructure:"host"`
	Port            int           `mapstructure:"port"`
	Database        string        `mapstructure:"database"`
	Username        string        `mapstructure:"username"`
	Password        string        `mapstructure:"password"`
	SSLMode         string        `mapstructure:"ssl_mode"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	ConnMaxIdleTime time.Duration `mapstructure:"conn_max_idle_time"`
}

type SQLiteConfig struct {
	Path string `mapstructure:"path"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
	PoolSize int    `mapstructure:"pool_size"`
}

// CacheConfig 统一缓存配置：backend 支持 redis | local（缺省 local）。
// DefaultTTL 单位：秒（int，避免 yaml duration 解析成纳秒）。
// FallbackLocal 仅初始化阶段降级；集群多实例建议 false。
// LocalMaxItems 仅 local 后端生效；0 = 不限制（依赖 TTL 清理），>0 = 超限淘汰。
type CacheConfig struct {
	Backend       string      `mapstructure:"backend"`
	DefaultTTL    int         `mapstructure:"default_ttl"`
	FallbackLocal bool        `mapstructure:"fallback_local"`
	LocalMaxItems int         `mapstructure:"local_max_items"`
	Redis         RedisConfig `mapstructure:"redis"`
}

type NATSConfig struct {
	URL         string        `mapstructure:"url"`
	JetStream   bool          `mapstructure:"jetstream"`
	MaxMsgSize  int           `mapstructure:"max_msg_size"`
	MsgRetention time.Duration `mapstructure:"msg_retention"`
}

type JWTConfig struct {
	Secret    string `mapstructure:"secret"`
	ExpiresIn int    `mapstructure:"expires_in"`
}

type CryptoConfig struct {
	KeyID string `mapstructure:"key_id"`
}

type CORSConfig struct {
	Enabled          bool     `mapstructure:"enabled"`
	AllowedOrigins   []string `mapstructure:"allowed_origins"`
	AllowedMethods   []string `mapstructure:"allowed_methods"`
	AllowedHeaders   []string `mapstructure:"allowed_headers"`
	ExposedHeaders   []string `mapstructure:"exposed_headers"`
	AllowCredentials bool     `mapstructure:"allow_credentials"`
	MaxAge           int      `mapstructure:"max_age"`
}

type LogConfig struct {
	Level    string `mapstructure:"level"`
	Format   string `mapstructure:"format"`
	Output   string `mapstructure:"output"`
	FilePath string `mapstructure:"file_path"`
}

type SyncConfig struct {
	SmallTableThreshold int64           `mapstructure:"small_table_threshold"`
	BatchSize           int64           `mapstructure:"batch_size"`
	DDLTimeout          time.Duration   `mapstructure:"ddl_timeout"`
	LockTimeout         time.Duration   `mapstructure:"lock_timeout"`
	MaxRetry            int             `mapstructure:"max_retry"`
	RetryIntervals      []time.Duration `mapstructure:"retry_intervals"`
}

// ─── Storage Config ─────────────────────────────────────────────────

type StorageConfig struct {
	TemporaryProfile string              `mapstructure:"temporary_profile" yaml:"temporary_profile"`
	Limits           StorageLimitsConfig `mapstructure:"limits" yaml:"limits"`
}

type StorageLimitsConfig struct {
	MaxSingleFile    int64    `mapstructure:"max_single_file" yaml:"max_single_file"`
	TotalQuota       int64    `mapstructure:"total_quota" yaml:"total_quota"`
	MaxDirDepth      int      `mapstructure:"max_dir_depth" yaml:"max_dir_depth"`
	UploadAllowTypes []string `mapstructure:"upload_allow_types" yaml:"upload_allow_types"`
}

type LocalStorageConfig struct {
	RootDir string `mapstructure:"root_dir" json:"root_dir" yaml:"root_dir"`
}

type S3StorageConfig struct {
	Endpoint        string `mapstructure:"endpoint" json:"endpoint" yaml:"endpoint"`
	Region          string `mapstructure:"region" json:"region" yaml:"region"`
	Bucket          string `mapstructure:"bucket" json:"bucket" yaml:"bucket"`
	AccessKeyID     string `mapstructure:"access_key_id" json:"access_key_id" yaml:"access_key_id"`
	SecretAccessKey string `mapstructure:"secret_access_key" json:"secret_access_key" yaml:"secret_access_key"`
	UsePathStyle    bool   `mapstructure:"use_path_style" json:"use_path_style" yaml:"use_path_style"`
	DisableSSL      bool   `mapstructure:"disable_ssl" json:"disable_ssl" yaml:"disable_ssl"`
	Prefix          string `mapstructure:"prefix" json:"prefix" yaml:"prefix"`
	SSEEnabled      bool   `mapstructure:"sse_enabled" json:"sse_enabled" yaml:"sse_enabled"`
}

func LoadConfig(configPath string) (*Config, error) {
	gotenv.Load()

	// 保存路径以便后续回写
	configFilePath = configPath

	v := viper.New()

	v.SetConfigFile(configPath)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.BindEnv("cache.redis.addr", "REDIS_ADDR")
	v.BindEnv("nats.url", "NATS_URL")
	v.BindEnv("database.type", "DB_TYPE")
	v.BindEnv("database.sqlite.path", "DB_SQLITE_PATH")
	v.BindEnv("jwt.secret", "DBRIDGE_JWT_SECRET")
	v.BindEnv("crypto.key_id", "DBRIDGE_CRYPTO_KEY")

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// Validate checks the config for security issues.
func (c *Config) Validate() []string {
	var warnings []string
	isProd := c.Server.Env == "production"

	// JWT secret validation
	if c.JWT.Secret == "" || c.JWT.Secret == "change-me-in-production" {
		msg := "JWT Secret 未设置或使用了默认值，请通过环境变量 DBRIDGE_JWT_SECRET 设置"
		if isProd {
			log.Fatalf("【安全错误】%s", msg)
		}
		warnings = append(warnings, msg)
	} else if len(c.JWT.Secret) < 32 {
		msg := fmt.Sprintf("JWT Secret 长度过短 (%d 字符)，建议至少 32 字符", len(c.JWT.Secret))
		warnings = append(warnings, msg)
	}

	// Storage config validation
	warnings = append(warnings, c.Storage.Validate()...)

	return warnings
}

// Validate 校验存储配置
func (s *StorageConfig) Validate() []string {
	var warnings []string
	// 存储实例已迁移至数据库管理，配置文件仅保留系统级参数（limits/temporary_profile）
	// 无需校验 profiles
	return warnings
}

// ─── Config Persistence ───────────────────────────────────────────

var configFilePath string

// SaveConfig 将当前配置写回 YAML 文件
func SaveConfig(cfg *Config) error {
	if configFilePath == "" {
		return fmt.Errorf("config: no config path")
	}
	// 读取原始 YAML 内容
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return fmt.Errorf("config: read file: %w", err)
	}

	// 序列化 storage 段为 YAML
	storageYAML, err := yaml.Marshal(map[string]interface{}{
		"storage": cfg.Storage,
	})
	if err != nil {
		return fmt.Errorf("config: marshal storage: %w", err)
	}

	// 解析原始配置为通用 map，替换 storage 段
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("config: unmarshal: %w", err)
	}
	var storageRaw map[string]interface{}
	if err := yaml.Unmarshal(storageYAML, &storageRaw); err != nil {
		return fmt.Errorf("config: unmarshal storage: %w", err)
	}
	raw["storage"] = storageRaw["storage"]

	// 写回文件
	out, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Errorf("config: marshal output: %w", err)
	}
	return os.WriteFile(configFilePath, out, 0644)
}
