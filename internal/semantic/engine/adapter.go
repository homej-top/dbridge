package engine

import "context"

// Initializer 适配器初始化接口。
type Initializer interface {
	Init(ctx context.Context, config AdapterConfig) error
	Close() error
}

// AdapterConfig 适配器配置。
type AdapterConfig struct {
	Type   string        `json:"type"`
	Local  *LocalConfig  `json:"local,omitempty"`
	CubeJS *CubeJSConfig `json:"cubejs,omitempty"`
	Dbt    *DbtConfig    `json:"dbt,omitempty"`
	Looker *LookerConfig `json:"looker,omitempty"`
}

// LocalConfig 本地适配器配置。
type LocalConfig struct{}

// CubeJSConfig Cube.js 适配器配置。
type CubeJSConfig struct {
	APIURL   string `json:"api_url"`
	APIToken string `json:"api_token"`
}

// DbtConfig dbt Semantic Layer 配置。
type DbtConfig struct {
	Host          string `json:"host"`
	AccountID     string `json:"account_id"`
	ProjectID     string `json:"project_id"`
	APIToken      string `json:"api_token"`
	EnvironmentID string `json:"environment_id"`
}

// LookerConfig Looker 适配器配置。
type LookerConfig struct {
	BaseURL      string `json:"base_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// AdapterDeps 适配器共享依赖。
// 使用 interface{} 避免 engine 包对 service/repository 的循环依赖。
type AdapterDeps struct {
	QueryService interface{} // *service.QueryService
	DB           interface{} // *gorm.DB
	Logger       interface{} // *zap.Logger
	Cache        interface{} // cache.Cache
}
