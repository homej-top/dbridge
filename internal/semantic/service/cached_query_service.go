package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/homej-top/dbridge/internal/semantic/engine"
	cachePkg "github.com/homej-top/dbridge/pkg/cache"
)

const semanticCachePrefix = "semantic"

// CachedQueryService 缓存装饰器，包装 SemanticQueryEngine 实现查询结果缓存。
type CachedQueryService struct {
	inner engine.SemanticQueryEngine
	cache cachePkg.Cache
	ttl   time.Duration
}

// NewCachedQueryService 创建缓存装饰器。
func NewCachedQueryService(inner engine.SemanticQueryEngine, c cachePkg.Cache, ttl time.Duration) *CachedQueryService {
	return &CachedQueryService{inner: inner, cache: c, ttl: ttl}
}

// Query 先查缓存，未命中则查询并缓存结果。
func (s *CachedQueryService) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	key, err := s.hashKey(req)
	if err != nil {
		return s.inner.Query(ctx, req)
	}

	var cached engine.QueryResult
	if err := s.cache.GetJSON(ctx, key, &cached); err == nil {
		cached.CacheHit = true
		return &cached, nil
	}

	result, err := s.inner.Query(ctx, req)
	if err != nil {
		return nil, err
	}

	if setErr := s.cache.SetJSON(ctx, key, result, s.ttl); setErr != nil && s.cache != nil {
		// 缓存写入失败不影响查询结果返回
		_ = setErr
	}

	return result, nil
}

// ListCubes 透传到内部引擎。
func (s *CachedQueryService) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return s.inner.ListCubes(ctx, filter)
}

// GetCube 透传到内部引擎。
func (s *CachedQueryService) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return s.inner.GetCube(ctx, name)
}

// ListMeasures 透传到内部引擎。
func (s *CachedQueryService) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return s.inner.ListMeasures(ctx, cubeName)
}

// ListDimensions 透传到内部引擎。
func (s *CachedQueryService) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return s.inner.ListDimensions(ctx, cubeName)
}

// Capabilities 透传到内部引擎。
func (s *CachedQueryService) Capabilities() *engine.EngineCapabilities {
	return s.inner.Capabilities()
}

// Ping 透传到内部引擎。
func (s *CachedQueryService) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}

// Invalidate 清除所有语义查询缓存。
func (s *CachedQueryService) Invalidate(ctx context.Context) error {
	return s.cache.DelByPrefix(ctx, cachePkg.Key(semanticCachePrefix, ""))
}

func (s *CachedQueryService) hashKey(req *engine.QueryRequest) (string, error) {
	data, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("hash key marshal: %w", err)
	}
	sum := sha256.Sum256(data)
	return cachePkg.Key(semanticCachePrefix, fmt.Sprintf("%x", sum)), nil
}
