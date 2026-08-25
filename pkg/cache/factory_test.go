package cache

import (
	"testing"
	"time"

	"github.com/dbridge/dbridge/internal/config"
)

// TestInitFallbackLocal backend=redis 不可达 + fallback_local=true → 降级 local，不报错。
func TestInitFallbackLocal(t *testing.T) {
	cfg := config.CacheConfig{
		Backend:       "redis",
		Redis:         config.RedisConfig{Addr: "127.0.0.1:1"}, // 不可达端口，快速失败
		FallbackLocal: true,
	}
	if err := Init(cfg); err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if Backend() != BackendLocal {
		t.Fatalf("expected backend local after fallback, got %d", Backend())
	}
}

// TestInitRedisFailsWithoutFallback backend=redis 不可达 + fallback_local=false → 返回错误。
func TestInitRedisFailsWithoutFallback(t *testing.T) {
	cfg := config.CacheConfig{
		Backend:       "redis",
		Redis:         config.RedisConfig{Addr: "127.0.0.1:1"},
		FallbackLocal: false,
	}
	if err := Init(cfg); err == nil {
		t.Fatalf("expected error when redis unavailable and no fallback")
	}
}

// TestGetBeforeInit Get 在 Init 前调用返回本地兜底，不 panic。
func TestGetBeforeInit(t *testing.T) {
	c := Get()
	if c == nil {
		t.Fatalf("Get() returned nil")
	}
	if err := c.Set(testCtx, "preinit", []byte("1"), 0); err != nil {
		t.Fatalf("set: %v", err)
	}
}

// TestFactoryRoundtrip 工厂链路：Init(local) → Get() → SetJSON/GetJSON 全链路可用。
func TestFactoryRoundtrip(t *testing.T) {
	if err := Init(config.CacheConfig{Backend: "local"}); err != nil {
		t.Fatalf("init: %v", err)
	}
	c := Get()
	if c == nil {
		t.Fatalf("Get() returned nil")
	}
	key := Key("test", "roundtrip")
	if err := c.SetJSON(testCtx, key, map[string]any{"a": 1, "b": "x"}, time.Minute); err != nil {
		t.Fatalf("setjson: %v", err)
	}
	var out map[string]any
	if err := c.GetJSON(testCtx, key, &out); err != nil {
		t.Fatalf("getjson: %v", err)
	}
	if out["a"] != float64(1) || out["b"] != "x" {
		t.Fatalf("roundtrip mismatch: %v", out)
	}
	if Backend() != BackendLocal {
		t.Fatalf("expected local backend, got %d", Backend())
	}
}
