package engine

// QueryRequest 统一查询请求。
type QueryRequest struct {
	Engine       string            `json:"engine,omitempty"`
	DataSourceID string            `json:"data_source_id,omitempty"`
	Measures     []string          `json:"measures"`
	Dimensions   []string          `json:"dimensions,omitempty"`
	TimeDimensions []TimeDimensionQuery `json:"time_dimensions,omitempty"`
	Filters      []Filter          `json:"filters,omitempty"`
	Order        []OrderItem       `json:"order,omitempty"`
	Limit        int               `json:"limit,omitempty"`
	Offset       int               `json:"offset,omitempty"`
	DerivedMetrics []DerivedMetric `json:"derived_metrics,omitempty"`
	EngineOverrides map[string]string `json:"engine_overrides,omitempty"`
	UserContext  map[string]string `json:"user_context,omitempty"`
}

// DerivedMetric 派生指标。
type DerivedMetric struct {
	Name        string     `json:"name"`
	Type        MetricType `json:"type"`
	Numerator   string     `json:"numerator,omitempty"`
	Denominator string     `json:"denominator,omitempty"`
	Expression  string     `json:"expression,omitempty"`
	CompareType string     `json:"compare_type,omitempty"`
}

// MetricType 指标类型。
type MetricType string

const (
	MetricTypeRatio       MetricType = "ratio"
	MetricTypeDerived     MetricType = "derived"
	MetricTypeTimeCompare MetricType = "time_compare"
)

// TimeDimensionQuery 时间维度查询。
type TimeDimensionQuery struct {
	Dimension   string   `json:"dimension"`
	DateRange   []string `json:"date_range"`
	Granularity string   `json:"granularity"`
}

// Filter 过滤条件。
type Filter struct {
	Member   string   `json:"member"`
	Operator string   `json:"operator"`
	Values   []string `json:"values"`
}

// OrderItem 排序项。
type OrderItem struct {
	Member    string `json:"member"`
	Direction string `json:"direction"`
}

// QueryResult 查询结果。
type QueryResult struct {
	Columns     []string        `json:"columns"`
	ColumnTypes []string        `json:"column_types"`
	Rows        [][]interface{} `json:"rows"`
	TotalRows   int64           `json:"total_rows"`
	DurationMs  int64           `json:"duration_ms"`
	SQL         string          `json:"sql,omitempty"`
	CacheHit    bool            `json:"cache_hit,omitempty"`
	Engine      string          `json:"engine"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}
