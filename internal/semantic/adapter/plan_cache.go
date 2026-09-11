package adapter

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	cachePkg "github.com/homej-top/dbridge/pkg/cache"
)

// CompiledPlan 编译后的查询计划。
type CompiledPlan struct {
	SQL         string        `json:"sql"`
	Args        []interface{} `json:"args"`
	ColumnNames []string      `json:"column_names"`
}

// PlanCache 查询计划缓存。
type PlanCache struct {
	cache cachePkg.Cache
	ttl   time.Duration
}

// NewPlanCache 创建查询计划缓存。
func NewPlanCache(c cachePkg.Cache, ttl time.Duration) *PlanCache {
	return &PlanCache{cache: c, ttl: ttl}
}

// GetOrCompile 先查缓存，未命中则编译并缓存。
func (pc *PlanCache) GetOrCompile(ctx context.Context, key string, compile func() (*CompiledPlan, error)) (*CompiledPlan, error) {
	cacheKey := pc.cacheKey(key)

	var plan CompiledPlan
	if err := pc.cache.GetJSON(ctx, cacheKey, &plan); err == nil {
		return &plan, nil
	}

	compiled, err := compile()
	if err != nil {
		return nil, err
	}

	_ = pc.cache.SetJSON(ctx, cacheKey, compiled, pc.ttl)
	return compiled, nil
}

func (pc *PlanCache) cacheKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return cachePkg.Key("semantic_plan", fmt.Sprintf("%x", sum))
}
