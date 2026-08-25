package cache

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/dbridge/dbridge/internal/config"
)

var (
	mu          sync.RWMutex
	defaultCache Cache
)

// cleanupInterval go-cache 过期清理周期。
const cleanupInterval = 5 * time.Minute

// Init 根据配置初始化全局缓存；backend 支持 "redis" | "local"（缺省 local）。
//
// 降级策略（仅初始化阶段）：
//   - backend=redis && Redis Ping 失败 && FallbackLocal=true → 降级为 LocalCache，输出 ERROR 日志并记录降级事件；
//   - backend=redis && Ping 失败 && FallbackLocal=false → 返回错误，启动失败。
//
// 运行时 Redis 断线/抖动：一期不做自动切换，直接返回错误。
func Init(cfg config.CacheConfig) error {
	backend := strings.ToLower(strings.TrimSpace(cfg.Backend))
	if backend == "" {
		backend = "local"
	}
	defaultTTL := time.Duration(cfg.DefaultTTL) * time.Second

	switch backend {
	case "redis":
		rc, err := NewRedisCache(cfg.Redis.Addr, cfg.Redis.Password, cfg.Redis.DB, cfg.Redis.PoolSize)
		if err != nil {
			if cfg.FallbackLocal {
				log.Printf("[ERROR] cache: redis init failed (%v), fallback to local cache", err)
				globalMetrics.incFallback()
				lc := NewLocalCache(defaultTTL, cleanupInterval, cfg.LocalMaxItems)
				mu.Lock()
				defaultCache = lc
				mu.Unlock()
				globalMetrics.setBackend(BackendLocal)
				log.Printf("cache init success, backend=local(fallback), fallback_local=true, local_max_items=%d", cfg.LocalMaxItems)
				return nil
			}
			return err
		}
		mu.Lock()
		defaultCache = rc
		mu.Unlock()
		globalMetrics.setBackend(BackendRedis)
		log.Printf("cache init success, backend=redis, fallback_local=%v", cfg.FallbackLocal)
	case "local":
		lc := NewLocalCache(defaultTTL, cleanupInterval, cfg.LocalMaxItems)
		mu.Lock()
		defaultCache = lc
		mu.Unlock()
		globalMetrics.setBackend(BackendLocal)
		log.Printf("cache init success, backend=local, fallback_local=%v, local_max_items=%d", cfg.FallbackLocal, cfg.LocalMaxItems)
	default:
		return fmt.Errorf("cache: unknown backend %q (expect redis|local)", cfg.Backend)
	}
	return nil
}

// Get 返回全局缓存实例；Init 前调用返回 LocalCache 兜底，避免 nil panic。
func Get() Cache {
	mu.RLock()
	c := defaultCache
	mu.RUnlock()
	if c == nil {
		lc := NewLocalCache(0, cleanupInterval, 0)
		mu.Lock()
		if defaultCache == nil {
			defaultCache = lc
		}
		c = defaultCache
		mu.Unlock()
		globalMetrics.setBackend(BackendLocal)
	}
	return c
}

// Close 关闭并释放资源。
func Close() error {
	mu.RLock()
	c := defaultCache
	mu.RUnlock()
	if c == nil {
		return nil
	}
	return c.Close()
}
