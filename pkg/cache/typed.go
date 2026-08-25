package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// typedOps 提供便捷类型方法，委托给底层 Cache 实现。
// 具体实现（LocalCache / RedisCache）内嵌 typedOps，并在构造时设置 raw 指向自身。
type typedOps struct {
	raw Cache
}

func (t *typedOps) GetString(ctx context.Context, key string) (string, error) {
	data, err := t.raw.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (t *typedOps) SetString(ctx context.Context, key, value string, ttl time.Duration) error {
	return t.raw.Set(ctx, key, []byte(value), ttl)
}

func (t *typedOps) GetJSON(ctx context.Context, key string, out any) error {
	data, err := t.raw.Get(ctx, key)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (t *typedOps) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return t.raw.Set(ctx, key, data, ttl)
}

// recordOp 记录一次操作指标：hit=成功（Get 命中即成功），ErrNotFound 不计入错误。
func recordOp(op string, start time.Time, err error) {
	globalMetrics.record(op, err == nil, err != nil && !errors.Is(err, ErrNotFound), time.Since(start))
}
