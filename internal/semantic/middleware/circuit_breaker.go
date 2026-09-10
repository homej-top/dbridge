package middleware

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

// CircuitBreaker 熔断器。
type CircuitBreaker struct {
	state        string
	failureCount int64
	threshold    int64
	resetTimeout time.Duration
	lastFailure  time.Time
	mu           sync.Mutex
}

// NewCircuitBreaker 创建熔断器。
func NewCircuitBreaker(threshold int64, resetTimeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:        "closed",
		threshold:    threshold,
		resetTimeout: resetTimeout,
	}
}

// Allow 判断是否允许请求通过。
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "closed":
		return true
	case "open":
		if time.Since(cb.lastFailure) > cb.resetTimeout {
			cb.state = "half_open"
			return true
		}
		return false
	case "half_open":
		return true
	}
	return false
}

// RecordSuccess 记录成功。
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case "half_open":
		cb.state = "closed"
		cb.failureCount = 0
	case "closed":
		cb.failureCount = 0
	}
}

// RecordFailure 记录失败。
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.lastFailure = time.Now()

	switch cb.state {
	case "half_open":
		cb.state = "open"
		cb.failureCount = cb.threshold
	case "closed":
		cb.failureCount++
		if cb.failureCount >= cb.threshold {
			cb.state = "open"
		}
	}
}

// State 返回当前状态。
func (cb *CircuitBreaker) State() string {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	return cb.state
}

// CircuitBreakerEngine 熔断器中间件引擎。
type CircuitBreakerEngine struct {
	inner    engine.SemanticQueryEngine
	breaker  *CircuitBreaker
	fallback engine.SemanticQueryEngine
}

// NewCircuitBreakerEngine 创建熔断器中间件。
func NewCircuitBreakerEngine(inner engine.SemanticQueryEngine, breaker *CircuitBreaker, fallback engine.SemanticQueryEngine) *CircuitBreakerEngine {
	return &CircuitBreakerEngine{inner: inner, breaker: breaker, fallback: fallback}
}

// Query 执行查询，熔断时降级到 fallback。
func (s *CircuitBreakerEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	if !s.breaker.Allow() {
		if s.fallback != nil {
			return s.fallback.Query(ctx, req)
		}
		return nil, fmt.Errorf("circuit breaker is open, request rejected")
	}

	result, err := s.inner.Query(ctx, req)
	if err != nil {
		s.breaker.RecordFailure()
	} else {
		s.breaker.RecordSuccess()
	}

	return result, err
}

func (s *CircuitBreakerEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return s.inner.ListCubes(ctx, filter)
}

func (s *CircuitBreakerEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return s.inner.GetCube(ctx, name)
}

func (s *CircuitBreakerEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return s.inner.ListMeasures(ctx, cubeName)
}

func (s *CircuitBreakerEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return s.inner.ListDimensions(ctx, cubeName)
}

func (s *CircuitBreakerEngine) Capabilities() *engine.EngineCapabilities {
	return s.inner.Capabilities()
}

func (s *CircuitBreakerEngine) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}
