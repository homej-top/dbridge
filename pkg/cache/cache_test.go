package cache

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var testCtx = context.Background()

func TestLocalCacheSuite(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		runCacheSuite(t, NewLocalCache(time.Minute, time.Minute, 0))
	})
	t.Run("with_max_items", func(t *testing.T) {
		runCacheSuite(t, NewLocalCache(time.Minute, time.Minute, 8))
	})
}

func TestRedisCacheSuite(t *testing.T) {
	if os.Getenv("TEST_REDIS") == "" {
		t.Skip("set TEST_REDIS=1 to run redis cache tests")
	}
	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "127.0.0.1:6379"
	}
	password := os.Getenv("TEST_REDIS_PASSWORD")
	db := 15
	if v := os.Getenv("TEST_REDIS_DB"); v != "" {
		fmt.Sscanf(v, "%d", &db)
	}
	rc, err := NewRedisCache(addr, password, db, 10)
	if err != nil {
		t.Fatalf("redis unavailable: %v", err)
	}
	defer rc.Close()
	rc.client.FlushDB(testCtx)
	runCacheSuite(t, rc)
}

// runCacheSuite 两套实现共用的用例集。
func runCacheSuite(t *testing.T, c Cache) {
	t.Run("set_get", func(t *testing.T) {
		if err := c.Set(testCtx, "k", []byte("v1"), time.Minute); err != nil {
			t.Fatalf("set: %v", err)
		}
		v, err := c.Get(testCtx, "k")
		if err != nil || string(v) != "v1" {
			t.Fatalf("get: v=%q err=%v", v, err)
		}
		// 未命中
		if _, err := c.Get(testCtx, "missing"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("ttl_expiry", func(t *testing.T) {
		if err := c.Set(testCtx, "k_exp", []byte("x"), 50*time.Millisecond); err != nil {
			t.Fatalf("set: %v", err)
		}
		time.Sleep(120 * time.Millisecond)
		if _, err := c.Get(testCtx, "k_exp"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound after ttl, got %v", err)
		}
	})

	t.Run("del_exists", func(t *testing.T) {
		c.Set(testCtx, "k_del", []byte("1"), time.Minute)
		ok, err := c.Exists(testCtx, "k_del")
		if err != nil || !ok {
			t.Fatalf("exists: %v %v", ok, err)
		}
		if err := c.Del(testCtx, "k_del"); err != nil {
			t.Fatalf("del: %v", err)
		}
		if ok, _ := c.Exists(testCtx, "k_del"); ok {
			t.Fatalf("key should be deleted")
		}
	})

	t.Run("del_by_prefix", func(t *testing.T) {
		for i := 0; i < 5; i++ {
			if err := c.Set(testCtx, fmt.Sprintf("report:ds:r%d", i), []byte("1"), time.Minute); err != nil {
				t.Fatalf("set: %v", err)
			}
		}
		c.Set(testCtx, "other:x", []byte("1"), time.Minute)
		if err := c.DelByPrefix(testCtx, "report:ds:"); err != nil {
			t.Fatalf("del_by_prefix: %v", err)
		}
		for i := 0; i < 5; i++ {
			if ok, _ := c.Exists(testCtx, fmt.Sprintf("report:ds:r%d", i)); ok {
				t.Fatalf("report key r%d should be deleted", i)
			}
		}
		if ok, _ := c.Exists(testCtx, "other:x"); !ok {
			t.Fatalf("non-matching key should remain")
		}
	})

	t.Run("ttl_command", func(t *testing.T) {
		c.Set(testCtx, "k_ttl", []byte("1"), time.Second)
		ttl, err := c.TTL(testCtx, "k_ttl")
		if err != nil {
			t.Fatalf("ttl: %v", err)
		}
		if ttl <= 0 || ttl > time.Second {
			t.Fatalf("unexpected ttl: %v", ttl)
		}
		if _, err := c.TTL(testCtx, "k_noexist"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("json_helpers", func(t *testing.T) {
		type obj struct {
			Name string `json:"name"`
			N    int    `json:"n"`
		}
		in := obj{Name: "张三", N: 42}
		if err := c.SetJSON(testCtx, "k_json", in, time.Minute); err != nil {
			t.Fatalf("setjson: %v", err)
		}
		var out obj
		if err := c.GetJSON(testCtx, "k_json", &out); err != nil {
			t.Fatalf("getjson: %v", err)
		}
		if out != in {
			t.Fatalf("json roundtrip mismatch: %+v", out)
		}
		if err := c.SetString(testCtx, "k_str", "hello", time.Minute); err != nil {
			t.Fatalf("setstring: %v", err)
		}
		if s, err := c.GetString(testCtx, "k_str"); err != nil || s != "hello" {
			t.Fatalf("getstring: %q %v", s, err)
		}
	})

	t.Run("lock_concurrency", func(t *testing.T) {
		const n = 20
		var winners int32
		var mu sync.Mutex
		var tokens []string
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				locked, token, err := c.Lock(testCtx, "lock:conc", time.Second)
				if err != nil {
					t.Errorf("lock err: %v", err)
					return
				}
				if locked {
					atomic.AddInt32(&winners, 1)
					mu.Lock()
					tokens = append(tokens, token)
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		if winners != 1 {
			t.Fatalf("expected exactly 1 winner, got %d", winners)
		}
		for _, tok := range tokens {
			c.Unlock(testCtx, "lock:conc", tok)
		}
	})

	t.Run("lock_ttl_reacquire", func(t *testing.T) {
		locked, token, err := c.Lock(testCtx, "lock:exp", 50*time.Millisecond)
		if err != nil || !locked {
			t.Fatalf("first lock failed: %v %v", locked, err)
		}
		time.Sleep(120 * time.Millisecond)
		locked2, token2, err := c.Lock(testCtx, "lock:exp", time.Second)
		if err != nil || !locked2 {
			t.Fatalf("reacquire after ttl failed: %v %v", locked2, err)
		}
		c.Unlock(testCtx, "lock:exp", token)
		c.Unlock(testCtx, "lock:exp", token2)
	})

	t.Run("lock_wrong_token", func(t *testing.T) {
		locked, token, err := c.Lock(testCtx, "lock:wrong", time.Second)
		if err != nil || !locked {
			t.Fatalf("lock failed: %v %v", locked, err)
		}
		if err := c.Unlock(testCtx, "lock:wrong", "bad-token"); err == nil {
			t.Fatalf("expected unlock token mismatch error")
		}
		if ok, _ := c.Exists(testCtx, "lock:wrong"); !ok {
			t.Fatalf("lock should still be held after wrong-token unlock")
		}
		if err := c.Unlock(testCtx, "lock:wrong", token); err != nil {
			t.Fatalf("unlock with correct token: %v", err)
		}
	})

	t.Run("ctx_cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(testCtx)
		cancel()
		if _, err := c.Get(ctx, "k"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if _, _, err := c.Lock(ctx, "k", time.Second); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled on lock, got %v", err)
		}
	})

	t.Run("set_permanent", func(t *testing.T) {
		// ttl<=0 => 永久缓存：可读、TTL()=0
		if err := c.Set(testCtx, "k_perm", []byte("p"), 0); err != nil {
			t.Fatalf("set: %v", err)
		}
		if v, err := c.Get(testCtx, "k_perm"); err != nil || string(v) != "p" {
			t.Fatalf("get: %q err=%v", v, err)
		}
		if ttl, err := c.TTL(testCtx, "k_perm"); err != nil || ttl != 0 {
			t.Fatalf("expected ttl=0 (no expiry), got ttl=%v err=%v", ttl, err)
		}
	})

	t.Run("del_missing", func(t *testing.T) {
		if err := c.Del(testCtx, "k_noexist_del"); err != nil {
			t.Fatalf("del missing should return nil, got %v", err)
		}
	})

	t.Run("exists_missing", func(t *testing.T) {
		if ok, err := c.Exists(testCtx, "k_noexist_exists"); err != nil || ok {
			t.Fatalf("expected false,nil got %v,%v", ok, err)
		}
	})

	t.Run("string_roundtrip", func(t *testing.T) {
		if err := c.SetString(testCtx, "k_str2", "中文值", time.Minute); err != nil {
			t.Fatalf("setstring: %v", err)
		}
		if s, err := c.GetString(testCtx, "k_str2"); err != nil || s != "中文值" {
			t.Fatalf("getstring: %q err=%v", s, err)
		}
	})

	t.Run("del_prefix_empty", func(t *testing.T) {
		if err := c.DelByPrefix(testCtx, ""); err != nil {
			t.Fatalf("empty prefix should be no-op, got %v", err)
		}
	})

	t.Run("lock_default_ttl", func(t *testing.T) {
		// ttl<=0 时使用默认 TTL，锁仍生效
		locked, token, err := c.Lock(testCtx, "lock:default", 0)
		if err != nil || !locked {
			t.Fatalf("lock with ttl=0 failed: locked=%v err=%v", locked, err)
		}
		locked2, _, _ := c.Lock(testCtx, "lock:default", time.Second)
		if locked2 {
			t.Fatalf("lock should still be held")
		}
		if err := c.Unlock(testCtx, "lock:default", token); err != nil {
			t.Fatalf("unlock: %v", err)
		}
	})

	t.Run("unlock_after_expiry", func(t *testing.T) {
		locked, token, err := c.Lock(testCtx, "lock:exp2", 50*time.Millisecond)
		if err != nil || !locked {
			t.Fatalf("lock failed: locked=%v err=%v", locked, err)
		}
		time.Sleep(120 * time.Millisecond)
		// 锁已过期，Unlock 应视为已释放（返回 nil，不报错）
		if err := c.Unlock(testCtx, "lock:exp2", token); err != nil {
			t.Fatalf("unlock after expiry should be nil, got %v", err)
		}
	})

	t.Run("concurrent_read_write", func(t *testing.T) {
		// 并发 Set/Get/Exists/Del 压力（配合 -race 检测数据竞争）
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				key := fmt.Sprintf("conc:%d", n%3)
				for k := 0; k < 50; k++ {
					_ = c.Set(testCtx, key, []byte(fmt.Sprintf("v-%d-%d", n, k)), time.Second)
					_, _ = c.Get(testCtx, key)
					_, _ = c.Exists(testCtx, key)
					_ = c.Del(testCtx, key)
				}
			}(i)
		}
		wg.Wait()
	})
}

// TestLocalMaxItemsEviction 仅本地后端：超限后按访问顺序淘汰最久未用项。
func TestLocalMaxItemsEviction(t *testing.T) {
	lc := NewLocalCache(time.Minute, time.Minute, 3)
	for i := 0; i < 4; i++ {
		if err := lc.Set(testCtx, fmt.Sprintf("k%d", i), []byte(fmt.Sprint(i)), time.Minute); err != nil {
			t.Fatalf("set: %v", err)
		}
	}
	// k0 最久未用，应被淘汰
	if _, err := lc.Get(testCtx, "k0"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("k0 should be evicted, got err=%v", err)
	}
	// 其余仍在
	for i := 1; i < 4; i++ {
		if _, err := lc.Get(testCtx, fmt.Sprintf("k%d", i)); err != nil {
			t.Fatalf("k%d should exist: %v", i, err)
		}
	}
}

// TestKeyHelpers Key 生成函数。
func TestKeyHelpers(t *testing.T) {
	if got := Key("report", "rpt_audit_daily"); got != "report:rpt_audit_daily" {
		t.Fatalf("Key mismatch: %q", got)
	}
	if got := KeyPrefix("report", "ds:abc"); got != "report:ds:abc:" {
		t.Fatalf("KeyPrefix mismatch: %q", got)
	}
}

// TestMetricsSnapshot 校验指标埋点（命中/未命中/错误均被记录，快照结构完整）。
func TestMetricsSnapshot(t *testing.T) {
	c := NewLocalCache(time.Minute, time.Minute, 0)
	_ = c.Set(testCtx, "m:1", []byte("1"), time.Minute)
	if _, err := c.Get(testCtx, "m:1"); err != nil { // 命中
		t.Fatalf("get: %v", err)
	}
	if _, err := c.Get(testCtx, "m:missing"); !errors.Is(err, ErrNotFound) { // 未命中
		t.Fatalf("expected ErrNotFound, got %v", err)
	}

	snap := Snapshot()
	if snap["backend"] == nil || snap["operations"] == nil || snap["fallback_total"] == nil {
		t.Fatalf("snapshot missing keys: %v", snap)
	}
	ops, _ := snap["operations"].(map[string]any)
	if _, ok := ops["get"]; !ok {
		t.Fatalf("expected 'get' op recorded, got %v", ops)
	}
	if _, ok := ops["set"]; !ok {
		t.Fatalf("expected 'set' op recorded")
	}
	if Backend() != BackendLocal {
		t.Fatalf("expected BackendLocal, got %d", Backend())
	}
}
