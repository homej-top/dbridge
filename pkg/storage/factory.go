package storage

import "fmt"

// StorageFactoryConfig 多 Profile 工厂配置
type StorageFactoryConfig struct {
	DefaultProfile   string
	TemporaryProfile string
	Profiles         map[string]ProfileFactoryConfig
}

// ProfileFactoryConfig 单个 Profile 的工厂配置
type ProfileFactoryConfig struct {
	Backend string
	Local   LocalFactoryConfig
	S3      S3FactoryConfig
}

// LocalFactoryConfig 本地存储工厂配置
type LocalFactoryConfig struct {
	RootDir string
}

// S3FactoryConfig S3 存储工厂配置
type S3FactoryConfig struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	UsePathStyle    bool
	DisableSSL      bool
	Prefix          string
	SSEEnabled      bool
}

// createFileStorage 根据单个 Profile 配置创建 FileStorage 实例
func createFileStorage(cfg ProfileFactoryConfig) (FileStorage, error) {
	switch cfg.Backend {
	case "local":
		return NewLocalFileStorage(cfg.Local.RootDir)
	case "s3":
		return NewS3FileStorage(S3Config{
			Endpoint:        cfg.S3.Endpoint,
			Region:          cfg.S3.Region,
			Bucket:          cfg.S3.Bucket,
			AccessKeyID:     cfg.S3.AccessKeyID,
			SecretAccessKey: cfg.S3.SecretAccessKey,
			UsePathStyle:    cfg.S3.UsePathStyle,
			DisableSSL:      cfg.S3.DisableSSL,
			Prefix:          cfg.S3.Prefix,
			SSEEnabled:      cfg.S3.SSEEnabled,
		})
	default:
		return nil, fmt.Errorf("storage: unknown backend: %s", cfg.Backend)
	}
}

// buildSummary 构建 Profile 概要信息
func buildSummary(name string, cfg ProfileFactoryConfig) map[string]string {
	s := map[string]string{}
	switch cfg.Backend {
	case "local":
		s["root_dir"] = cfg.Local.RootDir
	case "s3":
		s["endpoint"] = cfg.S3.Endpoint
		s["bucket"] = cfg.S3.Bucket
		s["region"] = cfg.S3.Region
		s["prefix"] = cfg.S3.Prefix
	}
	return s
}
