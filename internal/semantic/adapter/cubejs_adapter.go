package adapter

import (
	"context"
	"fmt"
	"strings"

	"github.com/homej-top/dbridge/internal/semantic/adapter/cubejs"
	"github.com/homej-top/dbridge/internal/semantic/engine"
)

// CubeJSAdapter Cube.js 语义层适配器。
type CubeJSAdapter struct {
	client *cubejs.Client
	config *engine.CubeJSConfig
}

// NewCubeJSAdapter 创建 Cube.js 适配器。
func NewCubeJSAdapter() *CubeJSAdapter {
	return &CubeJSAdapter{}
}

// Init 初始化适配器。
func (a *CubeJSAdapter) Init(ctx context.Context, config engine.AdapterConfig) error {
	if config.CubeJS == nil {
		return fmt.Errorf("cubejs config is required")
	}
	a.config = config.CubeJS

	if a.config.APIURL == "" {
		return fmt.Errorf("cubejs api_url is required")
	}

	a.client = cubejs.NewClient(a.config.APIURL, a.config.APIToken)
	return a.client.Ping(ctx)
}

// Close 关闭适配器。
func (a *CubeJSAdapter) Close() error {
	return nil
}

// Ping 检查连接。
func (a *CubeJSAdapter) Ping(ctx context.Context) error {
	return a.client.Ping(ctx)
}

// Capabilities 返回引擎能力。
func (a *CubeJSAdapter) Capabilities() *engine.EngineCapabilities {
	return &engine.EngineCapabilities{
		Name:                   "cubejs",
		Version:                "1.0.0",
		SupportsCreate:         false,
		SupportsUpdate:         false,
		SupportsDelete:         false,
		SupportsPreAgg:         true,
		SupportsMultiSource:    true,
		SupportsJoin:           true,
		SupportsDynamicSQL:     true,
		SupportsDerivedMetrics: false,
		SupportedAggTypes:      []string{"count", "sum", "avg", "min", "max", "count_distinct", "number", "running_total"},
	}
}

// ─── SemanticQueryEngine ────────────────────────────────────────────────────

// Query 执行语义查询。
func (a *CubeJSAdapter) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	cubeQuery := translateQuery(req)

	resp, err := a.client.Load(ctx, cubeQuery)
	if err != nil {
		return nil, err
	}

	return translateResult(resp, req)
}

// ListCubes 列出所有 Cube。
func (a *CubeJSAdapter) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	meta, err := a.client.Meta(ctx)
	if err != nil {
		return nil, err
	}

	var cubes []*engine.CubeMeta
	for i := range meta.Cubes {
		c := &meta.Cubes[i]
		if filter != nil && filter.Search != "" {
			search := strings.ToLower(filter.Search)
			if !strings.Contains(strings.ToLower(c.Name), search) &&
				!strings.Contains(strings.ToLower(c.Title), search) &&
				!strings.Contains(strings.ToLower(c.Description), search) {
				continue
			}
		}
		cubes = append(cubes, translateCubeMeta(c))
	}
	return cubes, nil
}

// GetCube 获取 Cube 详情。
func (a *CubeJSAdapter) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	meta, err := a.client.Meta(ctx)
	if err != nil {
		return nil, err
	}

	for i := range meta.Cubes {
		if meta.Cubes[i].Name == name {
			return translateCubeDetail(&meta.Cubes[i]), nil
		}
	}
	return nil, fmt.Errorf("cube not found: %s", name)
}

// ListMeasures 列出指定 Cube 的度量。
func (a *CubeJSAdapter) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	meta, err := a.client.Meta(ctx)
	if err != nil {
		return nil, err
	}

	for i := range meta.Cubes {
		if meta.Cubes[i].Name == cubeName {
			return extractMeasures(meta.Cubes[i].Measures), nil
		}
	}
	return nil, fmt.Errorf("cube not found: %s", cubeName)
}

// ListDimensions 列出指定 Cube 的维度。
func (a *CubeJSAdapter) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	meta, err := a.client.Meta(ctx)
	if err != nil {
		return nil, err
	}

	for i := range meta.Cubes {
		if meta.Cubes[i].Name == cubeName {
			return extractDimensions(meta.Cubes[i].Dimensions), nil
		}
	}
	return nil, fmt.Errorf("cube not found: %s", cubeName)
}

// ─── 查询转换 ────────────────────────────────────────────────────────────────

func translateQuery(req *engine.QueryRequest) *cubejs.Query {
	q := &cubejs.Query{
		Measures:   req.Measures,
		Dimensions: req.Dimensions,
		Limit:      req.Limit,
		Offset:     req.Offset,
	}

	if len(req.TimeDimensions) > 0 {
		q.TimeDimensions = make([]cubejs.TimeDimension, len(req.TimeDimensions))
		for i, td := range req.TimeDimensions {
			q.TimeDimensions[i] = cubejs.TimeDimension{
				Dimension:   td.Dimension,
				DateRange:   td.DateRange,
				Granularity: td.Granularity,
			}
		}
	}

	if len(req.Filters) > 0 {
		q.Filters = make([]cubejs.Filter, len(req.Filters))
		for i, f := range req.Filters {
			q.Filters[i] = cubejs.Filter{
				Member:   f.Member,
				Operator: f.Operator,
				Values:   f.Values,
			}
		}
	}

	if len(req.Order) > 0 {
		q.Order = make([][]string, len(req.Order))
		for i, o := range req.Order {
			q.Order[i] = []string{o.Member, o.Direction}
		}
	}

	return q
}

// ─── 结果转换 ────────────────────────────────────────────────────────────────

func translateResult(resp *cubejs.LoadResponse, req *engine.QueryRequest) (*engine.QueryResult, error) {
	columns := make([]string, 0, len(req.Measures)+len(req.Dimensions))
	columns = append(columns, req.Dimensions...)
	columns = append(columns, req.Measures...)

	for _, td := range req.TimeDimensions {
		columns = append(columns, td.Dimension)
	}

	rows := make([][]interface{}, 0, len(resp.Data))
	for _, dataRow := range resp.Data {
		row := make([]interface{}, len(columns))
		for i, col := range columns {
			row[i] = dataRow[col]
		}
		rows = append(rows, row)
	}

	columnTypes := make([]string, len(columns))
	for i := range columnTypes {
		columnTypes[i] = "string"
	}

	return &engine.QueryResult{
		Columns:     columns,
		ColumnTypes: columnTypes,
		Rows:        rows,
		TotalRows:   int64(len(rows)),
		Engine:      "cubejs",
	}, nil
}

// ─── 元数据映射 ──────────────────────────────────────────────────────────────

func translateCubeMeta(c *cubejs.Cube) *engine.CubeMeta {
	return &engine.CubeMeta{
		Name:        c.Name,
		DisplayName: c.Title,
		Description: c.Description,
		Measures:    extractMemberNames(c.Measures),
		Dimensions:  extractMemberNames(c.Dimensions),
		DataSource:  c.DataSource,
	}
}

func translateCubeDetail(c *cubejs.Cube) *engine.CubeDetail {
	detail := &engine.CubeDetail{
		CubeMeta: &engine.CubeMeta{
			Name:        c.Name,
			DisplayName: c.Title,
			Description: c.Description,
			DataSource:  c.DataSource,
		},
		Measures:   make([]*engine.MeasureDetail, len(c.Measures)),
		Dimensions: make([]*engine.DimensionDetail, len(c.Dimensions)),
	}

	for i := range c.Measures {
		m := &c.Measures[i]
		detail.Measures[i] = &engine.MeasureDetail{
			MeasureMeta: &engine.MeasureMeta{
				Name:        m.Name,
				DisplayName: m.Title,
				Description: m.Description,
				Type:        m.Type,
			},
			SQL: m.SQL,
		}
		detail.Measures[i].MeasureMeta.Name = m.Name
	}

	for i := range c.Dimensions {
		d := &c.Dimensions[i]
		detail.Dimensions[i] = &engine.DimensionDetail{
			DimensionMeta: &engine.DimensionMeta{
				Name:        d.Name,
				DisplayName: d.Title,
				Description: d.Description,
				Type:        d.Type,
			},
			SQL:        d.SQL,
			PrimaryKey: d.PrimaryKey,
		}
	}

	if len(c.Joins) > 0 {
		detail.Joins = make([]*engine.JoinDetail, len(c.Joins))
		for i := range c.Joins {
			j := &c.Joins[i]
			detail.Joins[i] = &engine.JoinDetail{
				JoinCubeName: j.Name,
				Relationship: j.Relationship,
				SQL:          j.SQL,
				JoinType:     j.JoinType,
			}
		}
	}

	measureNames := make([]string, len(c.Measures))
	for i, m := range c.Measures {
		measureNames[i] = m.Name
	}
	detail.CubeMeta.Measures = measureNames

	dimensionNames := make([]string, len(c.Dimensions))
	for i, d := range c.Dimensions {
		dimensionNames[i] = d.Name
	}
	detail.CubeMeta.Dimensions = dimensionNames

	return detail
}

func extractMeasures(members []cubejs.Member) []*engine.MeasureMeta {
	result := make([]*engine.MeasureMeta, len(members))
	for i := range members {
		m := &members[i]
		result[i] = &engine.MeasureMeta{
			Name:        m.Name,
			DisplayName: m.Title,
			Description: m.Description,
			Type:        m.Type,
		}
	}
	return result
}

func extractDimensions(members []cubejs.Member) []*engine.DimensionMeta {
	result := make([]*engine.DimensionMeta, len(members))
	for i := range members {
		d := &members[i]
		result[i] = &engine.DimensionMeta{
			Name:        d.Name,
			DisplayName: d.Title,
			Description: d.Description,
			Type:        d.Type,
		}
	}
	return result
}

func extractMemberNames(members []cubejs.Member) []string {
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	return names
}
