package middleware

import (
	"context"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

// MetricsCollector 指标采集接口。
type MetricsCollector interface {
	RecordQuery(engine string, duration time.Duration, err error)
	RecordCacheHit(engine string)
	RecordCacheMiss(engine string)
}

// NopMetricsCollector 空实现。
type NopMetricsCollector struct{}

func NewNopMetricsCollector() *NopMetricsCollector { return &NopMetricsCollector{} }

func (n *NopMetricsCollector) RecordQuery(string, time.Duration, error) {}
func (n *NopMetricsCollector) RecordCacheHit(string)                    {}
func (n *NopMetricsCollector) RecordCacheMiss(string)                   {}

// MetricsQueryEngine 指标采集中间件。
type MetricsQueryEngine struct {
	inner   engine.SemanticQueryEngine
	metrics MetricsCollector
}

// NewMetricsQueryEngine 创建指标采集中间件。
func NewMetricsQueryEngine(inner engine.SemanticQueryEngine, m MetricsCollector) *MetricsQueryEngine {
	return &MetricsQueryEngine{inner: inner, metrics: m}
}

// Query 执行查询并记录指标。
func (s *MetricsQueryEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	start := time.Now()

	result, err := s.inner.Query(ctx, req)

	duration := time.Since(start)
	s.metrics.RecordQuery(req.Engine, duration, err)

	if result != nil {
		if result.CacheHit {
			s.metrics.RecordCacheHit(req.Engine)
		} else {
			s.metrics.RecordCacheMiss(req.Engine)
		}
	}

	return result, err
}

func (s *MetricsQueryEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return s.inner.ListCubes(ctx, filter)
}

func (s *MetricsQueryEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return s.inner.GetCube(ctx, name)
}

func (s *MetricsQueryEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return s.inner.ListMeasures(ctx, cubeName)
}

func (s *MetricsQueryEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return s.inner.ListDimensions(ctx, cubeName)
}

func (s *MetricsQueryEngine) Capabilities() *engine.EngineCapabilities {
	return s.inner.Capabilities()
}

func (s *MetricsQueryEngine) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}
