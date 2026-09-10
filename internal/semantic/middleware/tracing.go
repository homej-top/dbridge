package middleware

import (
	"context"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

// Tracer 链路追踪接口。
type Tracer interface {
	Start(ctx context.Context, operationName string) Span
}

// Span 追踪跨度接口。
type Span interface {
	SetTag(key string, value interface{}) Span
	Finish()
}

// NopTracer 空实现。
type NopTracer struct{}

func NewNopTracer() *NopTracer { return &NopTracer{} }

func (n *NopTracer) Start(ctx context.Context, operationName string) Span {
	return &nopSpan{}
}

type nopSpan struct{}

func (n *nopSpan) SetTag(string, interface{}) Span { return n }
func (n *nopSpan) Finish()                         {}

// TracedQueryEngine 链路追踪中间件。
type TracedQueryEngine struct {
	inner  engine.SemanticQueryEngine
	tracer Tracer
}

// NewTracedQueryEngine 创建链路追踪中间件。
func NewTracedQueryEngine(inner engine.SemanticQueryEngine, tracer Tracer) *TracedQueryEngine {
	return &TracedQueryEngine{inner: inner, tracer: tracer}
}

// Query 执行查询并记录追踪信息。
func (s *TracedQueryEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	span := s.tracer.Start(ctx, "semantic.query")
	defer span.Finish()

	span.SetTag("engine", req.Engine)
	span.SetTag("measures", req.Measures)
	span.SetTag("dimensions", req.Dimensions)

	result, err := s.inner.Query(ctx, req)
	if err != nil {
		span.SetTag("error", err.Error())
	} else {
		span.SetTag("rows", result.TotalRows)
		span.SetTag("duration_ms", result.DurationMs)
		span.SetTag("cache_hit", result.CacheHit)
	}

	return result, err
}

func (s *TracedQueryEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return s.inner.ListCubes(ctx, filter)
}

func (s *TracedQueryEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return s.inner.GetCube(ctx, name)
}

func (s *TracedQueryEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return s.inner.ListMeasures(ctx, cubeName)
}

func (s *TracedQueryEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return s.inner.ListDimensions(ctx, cubeName)
}

func (s *TracedQueryEngine) Capabilities() *engine.EngineCapabilities {
	return s.inner.Capabilities()
}

func (s *TracedQueryEngine) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}
