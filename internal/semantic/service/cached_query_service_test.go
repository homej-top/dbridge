package service

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/engine"
	cachePkg "github.com/dbridge/dbridge/pkg/cache"
)

type mockEngine struct {
	queryCount atomic.Int32
	result     *engine.QueryResult
	err        error
}

func (m *mockEngine) Query(_ context.Context, _ *engine.QueryRequest) (*engine.QueryResult, error) {
	m.queryCount.Add(1)
	return m.result, m.err
}
func (m *mockEngine) ListCubes(_ context.Context, _ *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return nil, nil
}
func (m *mockEngine) GetCube(_ context.Context, _ string) (*engine.CubeDetail, error) { return nil, nil }
func (m *mockEngine) ListMeasures(_ context.Context, _ string) ([]*engine.MeasureMeta, error) {
	return nil, nil
}
func (m *mockEngine) ListDimensions(_ context.Context, _ string) ([]*engine.DimensionMeta, error) {
	return nil, nil
}
func (m *mockEngine) Capabilities() *engine.EngineCapabilities { return &engine.EngineCapabilities{} }
func (m *mockEngine) Ping(_ context.Context) error             { return nil }

func newTestCache() cachePkg.Cache {
	return cachePkg.NewLocalCache(5*time.Minute, time.Minute, 0)
}

func TestCachedQueryService_CacheMiss(t *testing.T) {
	inner := &mockEngine{
		result: &engine.QueryResult{
			Columns:   []string{"count"},
			Rows:      [][]interface{}{{42}},
			TotalRows: 1,
			Engine:    "local",
		},
	}
	svc := NewCachedQueryService(inner, newTestCache(), time.Minute)

	req := &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	}

	result, err := svc.Query(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CacheHit {
		t.Fatal("expected cache miss on first query")
	}
	if inner.queryCount.Load() != 1 {
		t.Fatalf("expected 1 inner query call, got %d", inner.queryCount.Load())
	}
}

func TestCachedQueryService_CacheHit(t *testing.T) {
	inner := &mockEngine{
		result: &engine.QueryResult{
			Columns:   []string{"count"},
			Rows:      [][]interface{}{{42}},
			TotalRows: 1,
			Engine:    "local",
		},
	}
	svc := NewCachedQueryService(inner, newTestCache(), time.Minute)

	req := &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	}

	ctx := context.Background()
	_, _ = svc.Query(ctx, req)

	result, err := svc.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.CacheHit {
		t.Fatal("expected cache hit on second query")
	}
	if inner.queryCount.Load() != 1 {
		t.Fatalf("expected 1 inner query call (cached), got %d", inner.queryCount.Load())
	}
}

func TestCachedQueryService_Invalidate(t *testing.T) {
	inner := &mockEngine{
		result: &engine.QueryResult{
			Columns:   []string{"count"},
			Rows:      [][]interface{}{{42}},
			TotalRows: 1,
			Engine:    "local",
		},
	}
	svc := NewCachedQueryService(inner, newTestCache(), time.Minute)

	req := &engine.QueryRequest{
		Engine:   "local",
		Measures: []string{"orders.count"},
	}

	ctx := context.Background()
	_, _ = svc.Query(ctx, req)

	if err := svc.Invalidate(ctx); err != nil {
		t.Fatalf("invalidate error: %v", err)
	}

	result, err := svc.Query(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CacheHit {
		t.Fatal("expected cache miss after invalidate")
	}
	if inner.queryCount.Load() != 2 {
		t.Fatalf("expected 2 inner query calls after invalidate, got %d", inner.queryCount.Load())
	}
}

func TestCachedQueryService_DifferentRequestsDifferentKeys(t *testing.T) {
	inner := &mockEngine{
		result: &engine.QueryResult{
			Columns:   []string{"count"},
			Rows:      [][]interface{}{{42}},
			TotalRows: 1,
			Engine:    "local",
		},
	}
	svc := NewCachedQueryService(inner, newTestCache(), time.Minute)

	ctx := context.Background()
	req1 := &engine.QueryRequest{Engine: "local", Measures: []string{"orders.count"}}
	req2 := &engine.QueryRequest{Engine: "local", Measures: []string{"orders.sum"}}

	_, _ = svc.Query(ctx, req1)
	result, _ := svc.Query(ctx, req2)

	if result.CacheHit {
		t.Fatal("different requests should not share cache")
	}
	if inner.queryCount.Load() != 2 {
		t.Fatalf("expected 2 inner query calls, got %d", inner.queryCount.Load())
	}
}

func TestCachedQueryService_PassthroughMethods(t *testing.T) {
	inner := &mockEngine{}
	svc := NewCachedQueryService(inner, newTestCache(), time.Minute)

	ctx := context.Background()
	if _, err := svc.ListCubes(ctx, nil); err != nil {
		t.Fatalf("ListCubes: %v", err)
	}
	if _, err := svc.GetCube(ctx, "test"); err != nil {
		t.Fatalf("GetCube: %v", err)
	}
	if _, err := svc.ListMeasures(ctx, "test"); err != nil {
		t.Fatalf("ListMeasures: %v", err)
	}
	if _, err := svc.ListDimensions(ctx, "test"); err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	if caps := svc.Capabilities(); caps == nil {
		t.Fatal("Capabilities returned nil")
	}
	if err := svc.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}
