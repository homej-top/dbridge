package engine

import "context"

// SemanticQueryEngine 查询引擎接口，所有适配器必须实现。
type SemanticQueryEngine interface {
	Query(ctx context.Context, req *QueryRequest) (*QueryResult, error)
	ListCubes(ctx context.Context, filter *CubeFilter) ([]*CubeMeta, error)
	GetCube(ctx context.Context, name string) (*CubeDetail, error)
	ListMeasures(ctx context.Context, cubeName string) ([]*MeasureMeta, error)
	ListDimensions(ctx context.Context, cubeName string) ([]*DimensionMeta, error)
	Capabilities() *EngineCapabilities
	Ping(ctx context.Context) error
}

// SemanticAdminEngine 管理引擎接口，仅支持运行时 Cube 管理的适配器实现（如 LocalAdapter）。
type SemanticAdminEngine interface {
	SemanticQueryEngine
	CreateCube(ctx context.Context, cube *CubeDefinition) error
	UpdateCube(ctx context.Context, name string, cube *CubeDefinition) error
	DeleteCube(ctx context.Context, name string) error
}

// EngineCapabilities 引擎能力描述。
type EngineCapabilities struct {
	Name                   string   `json:"name"`
	Version                string   `json:"version"`
	SupportsCreate         bool     `json:"supports_create"`
	SupportsUpdate         bool     `json:"supports_update"`
	SupportsDelete         bool     `json:"supports_delete"`
	SupportsPreAgg         bool     `json:"supports_pre_agg"`
	SupportsMultiSource    bool     `json:"supports_multi_source"`
	SupportsJoin           bool     `json:"supports_join"`
	SupportsDynamicSQL     bool     `json:"supports_dynamic_sql"`
	SupportsDerivedMetrics bool     `json:"supports_derived_metrics"`
	SupportedAggTypes      []string `json:"supported_agg_types"`
}
