package adapter

import (
	"context"
	"fmt"

	"github.com/homej-top/dbridge/internal/semantic/engine"
	"github.com/homej-top/dbridge/internal/service"
	"gorm.io/gorm"
)

// LocalAdapter 本地语义层适配器。
type LocalAdapter struct {
	querySvc *service.QueryService
	db       *gorm.DB
	registry *LocalRegistry
	compiler *LocalCompiler
}

// NewLocalAdapter 无参构造，依赖通过 InjectDeps 注入。
func NewLocalAdapter() *LocalAdapter {
	return &LocalAdapter{}
}

// InjectDeps 注入共享依赖。
func (a *LocalAdapter) InjectDeps(deps *engine.AdapterDeps) {
	if deps.QueryService != nil {
		a.querySvc = deps.QueryService.(*service.QueryService)
	}
	if deps.DB != nil {
		a.db = deps.DB.(*gorm.DB)
	}
}

// Init 初始化适配器。
func (a *LocalAdapter) Init(ctx context.Context, config engine.AdapterConfig) error {
	if a.db == nil {
		return fmt.Errorf("local adapter: DB not injected")
	}
	a.registry = NewLocalRegistry(a.db)
	if err := a.registry.Load(); err != nil {
		return fmt.Errorf("local adapter: load cubes: %w", err)
	}
	a.compiler = NewLocalCompiler(a.registry)
	return nil
}

// Close 关闭适配器。
func (a *LocalAdapter) Close() error {
	return nil
}

// Capabilities 返回引擎能力。
func (a *LocalAdapter) Capabilities() *engine.EngineCapabilities {
	return &engine.EngineCapabilities{
		Name:                   "local",
		Version:                "1.0.0",
		SupportsCreate:         true,
		SupportsUpdate:         true,
		SupportsDelete:         true,
		SupportsPreAgg:         false,
		SupportsMultiSource:    true,
		SupportsJoin:           true,
		SupportsDynamicSQL:     false,
		SupportsDerivedMetrics: false,
		SupportedAggTypes:      []string{"count", "sum", "avg", "min", "max", "count_distinct"},
	}
}

// Ping 检查引擎连接。
func (a *LocalAdapter) Ping(ctx context.Context) error {
	return nil
}

// ─── SemanticQueryEngine ────────────────────────────────────────────────────

// Query 执行语义查询。
func (a *LocalAdapter) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	if len(req.DerivedMetrics) > 0 {
		return nil, fmt.Errorf("local adapter does not support derived metrics; use cubejs adapter or set engine_overrides")
	}

	compiled, err := a.compiler.Compile(req)
	if err != nil {
		return nil, fmt.Errorf("compile error: %w", err)
	}

	cubeName := extractCubeName(req.Measures[0])
	cube, ok := a.registry.GetCube(cubeName)
	if !ok {
		return nil, fmt.Errorf("cube not found: %s", cubeName)
	}

	dataSourceID := req.DataSourceID
	if dataSourceID == "" {
		dataSourceID = cube.DataSourceID
	}

	if a.querySvc == nil {
		return nil, fmt.Errorf("local adapter: QueryService not injected")
	}

	out, err := a.querySvc.Execute(service.QueryInput{
		DataSourceID: dataSourceID,
		SQL:          compiled.SQL,
		Schema:       cube.Schema,
		Database:     cube.Database,
		Category:     "data",
	})
	if err != nil {
		return nil, err
	}

	columnTypes := make([]string, len(out.Columns))
	for i := range columnTypes {
		columnTypes[i] = "string"
	}

	return &engine.QueryResult{
		Columns:     out.Columns,
		ColumnTypes: columnTypes,
		Rows:        out.Rows,
		TotalRows:   out.TotalRows,
		DurationMs:  out.Duration,
		SQL:         compiled.SQL,
		Engine:      "local",
	}, nil
}

// ListCubes 列出所有 Cube。
func (a *LocalAdapter) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return a.registry.ListCubes(filter), nil
}

// GetCube 获取 Cube 详情。
func (a *LocalAdapter) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	detail, ok := a.registry.GetCubeDetail(name)
	if !ok {
		return nil, fmt.Errorf("cube not found: %s", name)
	}
	return detail, nil
}

// ListMeasures 列出指定 Cube 的度量。
func (a *LocalAdapter) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	measures, ok := a.registry.ListMeasures(cubeName)
	if !ok {
		return nil, fmt.Errorf("cube not found: %s", cubeName)
	}
	return measures, nil
}

// ListDimensions 列出指定 Cube 的维度。
func (a *LocalAdapter) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	dims, ok := a.registry.ListDimensions(cubeName)
	if !ok {
		return nil, fmt.Errorf("cube not found: %s", cubeName)
	}
	return dims, nil
}

// ─── SemanticAdminEngine ────────────────────────────────────────────────────

// CreateCube 创建 Cube。
func (a *LocalAdapter) CreateCube(ctx context.Context, cube *engine.CubeDefinition) error {
	if cube.Version == 0 {
		cube.Version = 1
	}
	return a.registry.Save(cube)
}

// UpdateCube 更新 Cube。
func (a *LocalAdapter) UpdateCube(ctx context.Context, name string, cube *engine.CubeDefinition) error {
	existing, ok := a.registry.GetCube(name)
	if !ok {
		return fmt.Errorf("cube not found: %s", name)
	}
	cube.Version = existing.Version + 1
	cube.Name = name
	return a.registry.Save(cube)
}

// DeleteCube 删除 Cube。
func (a *LocalAdapter) DeleteCube(ctx context.Context, name string) error {
	return a.registry.Delete(name)
}
