package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// unlockScript 校验 token 后删除锁，防止误删其它持有者的锁。
// 返回：1=删除成功；0=token 不匹配；-1=key 不存在（锁已过期/释放）。
var unlockScript = redis.NewScript(`
if redis.call("exists", KEYS[1]) == 0 then
	return -1
end
if redis.call("get", KEYS[1]) == ARGV[1] then
	return redis.call("del", KEYS[1])
end
return 0
`)

// delPrefixScript 服务端 SCAN + UNLINK 批量删除；UNLINK 异步释放，避免阻塞 Redis 单线程。
// 仅限低频运维/缓存失效场景。
var delPrefixScript = redis.NewScript(`
local cursor = "0"
local count = 0
repeat
	local result = redis.call("SCAN", cursor, "MATCH", ARGV[1], "COUNT", 1000)
	cursor = result[1]
	local keys = result[2]
	for i = 1, #keys do
		redis.call("UNLINK", keys[i])
		count = count + 1
	end
until cursor == "0"
return count
`)

// RedisCache 基于 go-redis 的分布式缓存/锁实现（跨实例共享）。
type RedisCache struct {
	typedOps
	client *redis.Client
}

// NewRedisCache 创建 Redis 缓存并做启动 Ping 校验。
func NewRedisCache(addr, password string, db, poolSize int) (*RedisCache, error) {
	if addr == "" {
		return nil, fmt.Errorf("cache: redis addr is empty")
	}
	opts := &redis.Options{Addr: addr, Password: password, DB: db}
	if poolSize > 0 {
		opts.PoolSize = poolSize
	}
	client := redis.NewClient(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("cache: failed to connect redis: %w", err)
	}
	rc := &RedisCache{client: client}
	rc.typedOps.raw = rc
	return rc, nil
}

// ─── 基础操作 ──────────────────────────────────────────────────────────────

func (r *RedisCache) Get(ctx context.Context, key string) (data []byte, err error) {
	start := time.Now()
	defer func() { recordOp("get", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return nil, e
	}
	data, err = r.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, ErrNotFound
	}
	return data, err
}

func (r *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) (err error) {
	start := time.Now()
	defer func() { recordOp("set", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	// ttl<=0 时 expire 传 0 => redis SET 无过期（永久缓存）
	return r.client.Set(ctx, key, value, ttl).Err()
}

func (r *RedisCache) Del(ctx context.Context, key string) (err error) {
	start := time.Now()
	defer func() { recordOp("del", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	return r.client.Del(ctx, key).Err()
}

// DelByPrefix 服务端 Lua SCAN + UNLINK；尽力操作、非原子，仅限低频缓存失效场景。
func (r *RedisCache) DelByPrefix(ctx context.Context, prefix string) (err error) {
	start := time.Now()
	defer func() { recordOp("del_prefix", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	if prefix == "" {
		return nil
	}
	_, err = delPrefixScript.Run(ctx, r.client, []string{}, prefix+"*").Result()
	return err
}

func (r *RedisCache) Exists(ctx context.Context, key string) (found bool, err error) {
	start := time.Now()
	defer func() { recordOp("exists", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return false, e
	}
	n, err := r.client.Exists(ctx, key).Result()
	return n > 0, err
}

func (r *RedisCache) TTL(ctx context.Context, key string) (ttl time.Duration, err error) {
	start := time.Now()
	defer func() { recordOp("ttl", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return 0, e
	}
	ttl, err = r.client.TTL(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	switch ttl {
	case -2 * time.Nanosecond: // key 不存在
		return 0, ErrNotFound
	case -1 * time.Nanosecond: // 无过期时间
		return 0, nil
	}
	return ttl, nil
}

// ─── 分布式锁 ──────────────────────────────────────────────────────────────

func (r *RedisCache) Lock(ctx context.Context, key string, ttl time.Duration) (locked bool, token string, err error) {
	start := time.Now()
	defer func() { recordOp("lock", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return false, "", e
	}
	if ttl <= 0 {
		ttl = lockDefaultTTL
	}
	token = randomToken()
	ok, err := r.client.SetNX(ctx, key, token, ttl).Result()
	if err != nil {
		return false, "", err
	}
	if !ok {
		return false, "", nil // 已被其它持有者占用
	}
	return true, token, nil
}

func (r *RedisCache) Unlock(ctx context.Context, key string, token string) (err error) {
	start := time.Now()
	defer func() { recordOp("unlock", start, err) }()
	if e := ctxDone(ctx); e != nil {
		return e
	}
	n, err := unlockScript.Run(ctx, r.client, []string{key}, token).Int64()
	if err != nil {
		return err
	}
	if n == 0 {
		// token 不匹配，禁止误删
		return errors.New("cache: unlock token mismatch")
	}
	return nil // -1（已过期/释放）或 1（删除成功）均视为已释放
}

func (r *RedisCache) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}
