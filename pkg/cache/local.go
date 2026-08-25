package cache

import (
	"container/list"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	gocache "github.com/patrickmn/go-cache"
)

// lockDefaultTTL 锁未显式指定 TTL 时的兜底（锁必须有界，避免死锁）。
const lockDefaultTTL = 30 * time.Second

// LocalCache 基于 go-cache 的进程内缓存实现。
// 注意：
//   - 缓存仅本进程可见，多实例部署各实例不一致；
//   - Lock/Unlock 仅进程内互斥，不跨实例。
type LocalCache struct {
	typedOps
	cache *gocache.Cache

	// 可选上限淘汰（LocalMaxItems>0 时启用）：按访问顺序淘汰最久未用项
	mu       sync.Mutex
	maxItems int
	order    map[string]*list.Element
	lru      *list.List
}

// NewLocalCache 创建本地缓存。
// defaultTTL：未显式指定 TTL 项的默认有效期；cleanupInterval：过期清理周期；
// maxItems：0 表示不限制（依赖 TTL 清理）。
func NewLocalCache(defaultTTL time.Duration, cleanupInterval time.Duration, maxItems int) *LocalCache {
	if cleanupInterval <= 0 {
		cleanupInterval = time.Minute
	}
	lc := &LocalCache{
		cache:    gocache.New(defaultTTL, cleanupInterval),
		maxItems: maxItems,
	}
	if maxItems > 0 {
		lc.order = make(map[string]*list.Element)
		lc.lru = list.New()
	}
	lc.typedOps.raw = lc
	return lc
}

// ─── 基础操作 ──────────────────────────────────────────────────────────────

func (l *LocalCache) Get(ctx context.Context, key string) (data []byte, err error) {
	start := time.Now()
	defer func() { recordOp("get", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return nil, e
	}
	v, ok := l.cache.Get(key)
	if !ok {
		return nil, ErrNotFound
	}
	if b, ok := v.([]byte); ok {
		l.touch(key)
		return b, nil
	}
	return nil, fmt.Errorf("cache: local value type mismatch for key %q", key)
}

func (l *LocalCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) (err error) {
	start := time.Now()
	defer func() { recordOp("set", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	exp := ttl
	if exp <= 0 {
		exp = gocache.NoExpiration
	}
	l.cache.Set(key, value, exp)
	if l.maxItems > 0 {
		l.mu.Lock()
		l.touchLocked(key)
		for l.lru.Len() > l.maxItems {
			l.evictOldestLocked()
		}
		l.mu.Unlock()
	}
	return nil
}

func (l *LocalCache) Del(ctx context.Context, key string) (err error) {
	start := time.Now()
	defer func() { recordOp("del", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	l.cache.Delete(key)
	if l.maxItems > 0 {
		l.mu.Lock()
		if el, ok := l.order[key]; ok {
			l.lru.Remove(el)
			delete(l.order, key)
		}
		l.mu.Unlock()
	}
	return nil
}

// DelByPrefix 遍历删除前缀匹配的 key；尽力操作、非原子。
func (l *LocalCache) DelByPrefix(ctx context.Context, prefix string) (err error) {
	start := time.Now()
	defer func() { recordOp("del_prefix", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	if prefix == "" {
		return nil
	}
	items := l.cache.Items()
	for k := range items {
		if strings.HasPrefix(k, prefix) {
			l.cache.Delete(k)
			if l.maxItems > 0 {
				l.mu.Lock()
				if el, ok := l.order[k]; ok {
					l.lru.Remove(el)
					delete(l.order, k)
				}
				l.mu.Unlock()
			}
		}
	}
	return nil
}

func (l *LocalCache) Exists(ctx context.Context, key string) (found bool, err error) {
	start := time.Now()
	defer func() { recordOp("exists", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return false, e
	}
	_, ok := l.cache.Get(key)
	return ok, nil
}

func (l *LocalCache) TTL(ctx context.Context, key string) (ttl time.Duration, err error) {
	start := time.Now()
	defer func() { recordOp("ttl", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return 0, e
	}
	items := l.cache.Items()
	item, ok := items[key]
	if !ok {
		return 0, ErrNotFound
	}
	if item.Expiration == 0 {
		return 0, nil // 无过期时间
	}
	ttl = time.Duration(item.Expiration - time.Now().UnixNano())
	if ttl < 0 {
		return 0, ErrNotFound // 已过期（尚未被清理）
	}
	return ttl, nil
}

// ─── 互斥锁（进程内） ───────────────────────────────────────────────────────

func (l *LocalCache) Lock(ctx context.Context, key string, ttl time.Duration) (locked bool, token string, err error) {
	start := time.Now()
	defer func() { recordOp("lock", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return false, "", e
	}
	if ttl <= 0 {
		ttl = lockDefaultTTL
	}
	token = randomToken()
	// go-cache Add 语义 = SETNX：key 已存在则返回错误
	if e := l.cache.Add(key, token, ttl); e != nil {
		return false, "", nil // 已被占用
	}
	return true, token, nil
}

func (l *LocalCache) Unlock(ctx context.Context, key string, token string) (err error) {
	start := time.Now()
	defer func() { recordOp("unlock", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	v, ok := l.cache.Get(key)
	if !ok {
		return nil // 锁已过期/被释放
	}
	if s, ok := v.(string); ok && s == token {
		l.cache.Delete(key)
		return nil
	}
	return errors.New("cache: unlock token mismatch")
}

func (l *LocalCache) Close() error { return nil }

// ─── LRU 辅助（maxItems>0 时启用） ─────────────────────────────────────────

func (l *LocalCache) touch(key string) {
	if l.maxItems == 0 {
		return
	}
	l.mu.Lock()
	l.touchLocked(key)
	l.mu.Unlock()
}

func (l *LocalCache) touchLocked(key string) {
	if el, ok := l.order[key]; ok {
		l.lru.MoveToFront(el)
		return
	}
	l.order[key] = l.lru.PushFront(key)
}

func (l *LocalCache) evictOldestLocked() {
	el := l.lru.Back()
	if el == nil {
		return
	}
	key := el.Value.(string)
	l.lru.Remove(el)
	delete(l.order, key)
	l.cache.Delete(key)
}

// randomToken 生成随机锁 token（crypto/rand，失败时退化为时间戳）。
func randomToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err == nil {
		return hex.EncodeToString(b)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
