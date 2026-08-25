package cache

import (
	"sync"
	"time"
)

// 后端标识
const (
	BackendLocal = 0
	BackendRedis = 1
)

// latencyBounds 延迟分桶边界（秒）：<1ms, <10ms, <100ms, >=100ms
var latencyBounds = [3]time.Duration{time.Millisecond, 10 * time.Millisecond, 100 * time.Millisecond}

type opStats struct {
	total   int64
	hits    int64
	errs    int64
	latency [len(latencyBounds) + 1]int64
}

// Metrics 提供 pkg/cache 的运行时观测指标（互斥保护，无外部依赖）。
// 二期可导出到 Prometheus（对应指标名：
//
//	cache_backend{gauge} / cache_fallback_total / cache_operation_total{op,hit,err} / cache_latency_seconds{op,backend}）。
type Metrics struct {
	mu       sync.Mutex
	backend  int
	fallback int64
	ops      map[string]*opStats
}

var globalMetrics = &Metrics{ops: make(map[string]*opStats)}

func (m *Metrics) setBackend(b int) {
	m.mu.Lock()
	m.backend = b
	m.mu.Unlock()
}

func (m *Metrics) incFallback() {
	m.mu.Lock()
	m.fallback++
	m.mu.Unlock()
}

// record 记录一次操作：op 为操作名（get/set/del/del_prefix/exists/ttl/lock/unlock）。
func (m *Metrics) record(op string, hit, isErr bool, d time.Duration) {
	m.mu.Lock()
	s := m.ops[op]
	if s == nil {
		s = &opStats{}
		m.ops[op] = s
	}
	s.total++
	if hit {
		s.hits++
	}
	if isErr {
		s.errs++
	}
	i := 0
	for i < len(latencyBounds) && d >= latencyBounds[i] {
		i++
	}
	s.latency[i]++
	m.mu.Unlock()
}

// Backend 返回当前后端类型（BackendLocal / BackendRedis）。
func Backend() int {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()
	return globalMetrics.backend
}

// Snapshot 返回当前指标快照（供调试日志 / 未来 Prometheus 导出）。
func Snapshot() map[string]any {
	globalMetrics.mu.Lock()
	defer globalMetrics.mu.Unlock()
	out := map[string]any{
		"backend":        globalMetrics.backend,
		"fallback_total": globalMetrics.fallback,
	}
	ops := map[string]any{}
	for k, s := range globalMetrics.ops {
		ops[k] = map[string]any{
			"total":   s.total,
			"hits":    s.hits,
			"errs":    s.errs,
			"latency": s.latency,
		}
	}
	out["operations"] = ops
	return out
}
