// Package cache 提供统一缓存接口与两种后端实现（Redis / 本地 go-cache）。
//
// 业务方只依赖本包暴露的接口（Cache），通过 cache.Get() 获取全局实例，
// 不感知底层实现。所有方法都必须处理 ctx 取消（含内存实现）。
//
// 设计约定：
//   - 未命中统一返回 ErrNotFound（errors.Is 判断）。
//   - 接口层 ttl<=0 表示永久缓存；业务层如需"关闭缓存"应自行判断 ttl<=0 并跳过读写，
//     不要将 ttl<=0 透传给本接口（两者语义不同）。
//   - 锁接口强制 token 语义：Lock 返回 token，Unlock 必须携带该 token，防止误释放。
package cache

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound 表示缓存未命中。
var ErrNotFound = errors.New("cache: key not found")

// Cache 统一缓存接口。
type Cache interface {
	// —— 基础操作 ——
	// Get 读取原始字节，未命中返回 ErrNotFound。
	Get(ctx context.Context, key string) ([]byte, error)
	// Set 写入原始字节；ttl<=0 表示永久缓存。
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Del 删除单个 key（不存在时返回 nil）。
	Del(ctx context.Context, key string) error
	// DelByPrefix 按前缀批量删除；尽力操作、非原子，仅用于低频运维/缓存失效场景，严禁高频路径调用。
	DelByPrefix(ctx context.Context, prefix string) error
	// Exists 判断 key 是否存在。
	Exists(ctx context.Context, key string) (bool, error)
	// TTL 返回剩余有效期；未命中返回 ErrNotFound；无过期时间返回 0。
	TTL(ctx context.Context, key string) (time.Duration, error)

	// —— 便捷类型方法（内部序列化，业务免手动编解码） ——
	GetString(ctx context.Context, key string) (string, error)
	SetString(ctx context.Context, key, value string, ttl time.Duration) error
	GetJSON(ctx context.Context, key string, out any) error
	SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error

	// —— 互斥锁（强制 token 语义） ——
	// 返回 locked=true 时，调用方必须保存 token 并调用 Unlock(ctx, key, token) 释放，禁止裸解锁。
	// 锁 TTL 应大于业务最大耗时（业务层需用 Context 超时保证执行时间 < TTL）；
	// 锁过期 ≠ 业务停止，业务超时由应用层自行管控。
	// 注意：LocalCache 的锁仅进程内互斥，多实例部署不生效；跨实例互斥必须使用 RedisCache。
	Lock(ctx context.Context, key string, ttl time.Duration) (locked bool, token string, err error)
	Unlock(ctx context.Context, key string, token string) error

	// Close 释放底层资源（Redis 连接池 / 本地无操作）。
	Close() error
}

// ctxDone 检查上下文是否已取消；已取消返回 ctx.Err()，否则返回 nil。
// 所有接口（含内存实现）都应在入口调用。
func ctxDone(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
