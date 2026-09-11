package middleware

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/homej-top/dbridge/internal/semantic/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

// mockEngine 测试用引擎。
type mockEngine struct {
	result  *engine.QueryResult
	err     error
	pingErr error
}

func (m *mockEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	return m.result, m.err
}
func (m *mockEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return nil, nil
}
func (m *mockEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return nil, nil
}
func (m *mockEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return nil, nil
}
func (m *mockEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return nil, nil
}
func (m *mockEngine) Capabilities() *engine.EngineCapabilities { return &engine.EngineCapabilities{} }
func (m *mockEngine) Ping(ctx context.Context) error           { return m.pingErr }

// ─── Logging Tests ─────────────────────────────────────────────────────────

func TestLoggingQueryEngine_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	inner := &mockEngine{result: &engine.QueryResult{TotalRows: 10, Engine: "local"}}
	eng := NewLoggingQueryEngine(inner, logger)

	result, err := eng.Query(context.Background(), &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	})

	assert.NoError(t, err)
	assert.Equal(t, int64(10), result.TotalRows)
}

func TestLoggingQueryEngine_Error(t *testing.T) {
	logger := zaptest.NewLogger(t)
	inner := &mockEngine{err: fmt.Errorf("query failed")}
	eng := NewLoggingQueryEngine(inner, logger)

	_, err := eng.Query(context.Background(), &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	})

	assert.Error(t, err)
}

// ─── Metrics Tests ─────────────────────────────────────────────────────────

type recordingMetrics struct {
	mu         sync.Mutex
	queries    int
	cacheHits  int
	cacheMiss  int
	lastEngine string
}

func (r *recordingMetrics) RecordQuery(engine string, duration time.Duration, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries++
	r.lastEngine = engine
}
func (r *recordingMetrics) RecordCacheHit(engine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheHits++
}
func (r *recordingMetrics) RecordCacheMiss(engine string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cacheMiss++
}

func TestMetricsQueryEngine_RecordQuery(t *testing.T) {
	metrics := &recordingMetrics{}
	inner := &mockEngine{result: &engine.QueryResult{Engine: "local"}}
	eng := NewMetricsQueryEngine(inner, metrics)

	_, err := eng.Query(context.Background(), &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	})

	assert.NoError(t, err)
	assert.Equal(t, 1, metrics.queries)
	assert.Equal(t, "local", metrics.lastEngine)
}

func TestMetricsQueryEngine_CacheHit(t *testing.T) {
	metrics := &recordingMetrics{}
	inner := &mockEngine{result: &engine.QueryResult{CacheHit: true, Engine: "local"}}
	eng := NewMetricsQueryEngine(inner, metrics)

	_, _ = eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})

	assert.Equal(t, 1, metrics.cacheHits)
	assert.Equal(t, 0, metrics.cacheMiss)
}

func TestMetricsQueryEngine_CacheMiss(t *testing.T) {
	metrics := &recordingMetrics{}
	inner := &mockEngine{result: &engine.QueryResult{CacheHit: false, Engine: "local"}}
	eng := NewMetricsQueryEngine(inner, metrics)

	_, _ = eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})

	assert.Equal(t, 0, metrics.cacheHits)
	assert.Equal(t, 1, metrics.cacheMiss)
}

// ─── Tracing Tests ─────────────────────────────────────────────────────────

type recordingSpan struct {
	tags map[string]interface{}
}

func (s *recordingSpan) SetTag(key string, value interface{}) Span {
	s.tags[key] = value
	return s
}
func (s *recordingSpan) Finish() {}

type recordingTracer struct {
	spans []*recordingSpan
}

func (t *recordingTracer) Start(ctx context.Context, operationName string) Span {
	span := &recordingSpan{tags: make(map[string]interface{})}
	t.spans = append(t.spans, span)
	return span
}

func TestTracedQueryEngine_SpanTags(t *testing.T) {
	tracer := &recordingTracer{}
	inner := &mockEngine{result: &engine.QueryResult{TotalRows: 5, DurationMs: 100, Engine: "local"}}
	eng := NewTracedQueryEngine(inner, tracer)

	_, _ = eng.Query(context.Background(), &engine.QueryRequest{
		Engine:     "local",
		Measures:   []string{"orders.count"},
		Dimensions: []string{"orders.status"},
	})

	assert.Len(t, tracer.spans, 1)
	span := tracer.spans[0]
	assert.Equal(t, "local", span.tags["engine"])
	assert.Equal(t, int64(5), span.tags["rows"])
	assert.Equal(t, int64(100), span.tags["duration_ms"])
}

func TestTracedQueryEngine_Error(t *testing.T) {
	tracer := &recordingTracer{}
	inner := &mockEngine{err: fmt.Errorf("boom")}
	eng := NewTracedQueryEngine(inner, tracer)

	_, _ = eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})

	assert.Len(t, tracer.spans, 1)
	assert.Equal(t, "boom", tracer.spans[0].tags["error"])
}

// ─── Circuit Breaker Tests ─────────────────────────────────────────────────

func TestCircuitBreaker_InitialState(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)
	assert.Equal(t, "closed", cb.State())
	assert.True(t, cb.Allow())
}

func TestCircuitBreaker_ClosedToOpen(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)

	cb.RecordFailure()
	assert.Equal(t, "closed", cb.State())
	cb.RecordFailure()
	assert.Equal(t, "closed", cb.State())
	cb.RecordFailure()
	assert.Equal(t, "open", cb.State())
	assert.False(t, cb.Allow())
}

func TestCircuitBreaker_OpenToHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Millisecond)

	cb.RecordFailure()
	assert.Equal(t, "open", cb.State())

	time.Sleep(20 * time.Millisecond)
	assert.True(t, cb.Allow())
	assert.Equal(t, "half_open", cb.State())
}

func TestCircuitBreaker_HalfOpenToClosed(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transitions to half_open
	cb.RecordSuccess()
	assert.Equal(t, "closed", cb.State())
}

func TestCircuitBreaker_HalfOpenToOpen(t *testing.T) {
	cb := NewCircuitBreaker(1, 10*time.Millisecond)

	cb.RecordFailure()
	time.Sleep(20 * time.Millisecond)
	cb.Allow() // transitions to half_open
	cb.RecordFailure()
	assert.Equal(t, "open", cb.State())
}

func TestCircuitBreaker_SuccessResetsCount(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Second)

	cb.RecordFailure()
	cb.RecordFailure()
	cb.RecordSuccess()
	cb.RecordFailure()
	assert.Equal(t, "closed", cb.State())
}

func TestCircuitBreakerEngine_Fallback(t *testing.T) {
	fallback := &mockEngine{result: &engine.QueryResult{Engine: "fallback", TotalRows: 99}}
	inner := &mockEngine{err: fmt.Errorf("inner failed")}
	cb := NewCircuitBreaker(1, time.Minute)
	eng := NewCircuitBreakerEngine(inner, cb, fallback)

	// First call: inner fails, breaker opens
	_, err := eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})
	assert.Error(t, err)
	assert.Equal(t, "open", cb.State())

	// Second call: breaker open, uses fallback
	result, err := eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})
	assert.NoError(t, err)
	assert.Equal(t, "fallback", result.Engine)
	assert.Equal(t, int64(99), result.TotalRows)
}

func TestCircuitBreakerEngine_NoFallback(t *testing.T) {
	inner := &mockEngine{err: fmt.Errorf("inner failed")}
	cb := NewCircuitBreaker(1, time.Minute)
	eng := NewCircuitBreakerEngine(inner, cb, nil)

	_, _ = eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})

	_, err := eng.Query(context.Background(), &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "circuit breaker is open")
}

// ─── Nop Implementations ───────────────────────────────────────────────────

func TestNopMetricsCollector(t *testing.T) {
	nop := NewNopMetricsCollector()
	nop.RecordQuery("test", time.Second, nil)
	nop.RecordCacheHit("test")
	nop.RecordCacheMiss("test")
}

func TestNopTracer(t *testing.T) {
	nop := NewNopTracer()
	span := nop.Start(context.Background(), "test")
	span.SetTag("key", "value")
	span.Finish()
}

// Ensure unused import is used
var _ = zap.NewNop
