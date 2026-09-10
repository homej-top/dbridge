package engine

// CubeMeta Cube 元数据摘要。
type CubeMeta struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Measures    []string `json:"measures"`
	Dimensions  []string `json:"dimensions"`
	DataSource  string   `json:"data_source"`
}

// CubeDetail Cube 完整详情。
type CubeDetail struct {
	*CubeMeta
	SQLTable        string             `json:"sql_table,omitempty"`
	SQLQuery        string             `json:"sql_query,omitempty"`
	Measures        []*MeasureDetail   `json:"measures"`
	Dimensions      []*DimensionDetail `json:"dimensions"`
	Joins           []*JoinDetail      `json:"joins,omitempty"`
	PreAggregations []*PreAggDetail    `json:"pre_aggregations,omitempty"`
}

// MeasureMeta 度量元数据摘要。
type MeasureMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// MeasureDetail 度量完整详情。
type MeasureDetail struct {
	*MeasureMeta
	SQL          string                 `json:"sql"`
	Filters      []*Filter              `json:"filters,omitempty"`
	DrillMembers []string               `json:"drill_members,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// DimensionMeta 维度元数据摘要。
type DimensionMeta struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Description string `json:"description"`
	Type        string `json:"type"`
}

// DimensionDetail 维度完整详情。
type DimensionDetail struct {
	*DimensionMeta
	SQL             string                 `json:"sql"`
	PrimaryKey      bool                   `json:"primary_key,omitempty"`
	TimeGranularity string                 `json:"time_granularity,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// JoinDetail JOIN 详情。
type JoinDetail struct {
	JoinCubeName string `json:"join_cube_name"`
	Relationship string `json:"relationship"`
	SQL          string `json:"sql"`
	JoinType     string `json:"join_type"`
}

// PreAggDetail 预聚合详情。
// 注意：预聚合功能仅 CubeJSAdapter 支持，LocalAdapter 的 PreAggregations 字段始终为空。
type PreAggDetail struct {
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Measures      []string `json:"measures"`
	Dimensions    []string `json:"dimensions"`
	TimeDimension string   `json:"time_dimension"`
	Granularity   string   `json:"granularity"`
	Status        string   `json:"status"`
	RowCount      int64    `json:"row_count"`
}

// CubeFilter Cube 列表过滤。
type CubeFilter struct {
	Search       string `json:"search,omitempty"`
	DataSourceID string `json:"data_source_id,omitempty"`
}

// CubeDefinition Cube 创建/更新定义。
type CubeDefinition struct {
	Name           string             `json:"name"`
	DisplayName    string             `json:"display_name"`
	Description    string             `json:"description"`
	DataSourceID   string             `json:"data_source_id"`
	Database       string             `json:"database,omitempty"`
	Schema         string             `json:"schema,omitempty"`
	SQLTable       string             `json:"sql_table,omitempty"`
	SQLQuery       string             `json:"sql_query,omitempty"`
	Measures       []*MeasureDetail   `json:"measures"`
	Dimensions     []*DimensionDetail `json:"dimensions"`
	Joins          []*JoinDetail      `json:"joins,omitempty"`
	PreAggregations []*PreAggDetail   `json:"pre_aggregations,omitempty"`
	Version        int                `json:"version"`
	UpstreamTables []string           `json:"upstream_tables,omitempty"`
}
