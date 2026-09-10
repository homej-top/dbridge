package health

import (
	"context"
	"sync"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

// HealthStatus 引擎健康状态。
type HealthStatus struct {
	Name       string        `json:"name"`
	Status     string        `json:"status"`
	LastCheck  time.Time     `json:"last_check"`
	Latency    time.Duration `json:"latency"`
	ErrorCount int64         `json:"error_count"`
	Message    string        `json:"message,omitempty"`
}

// HealthChecker 健康检查器。
type HealthChecker struct {
	mu       sync.RWMutex
	statuses map[string]*HealthStatus
	engines  map[string]engine.SemanticQueryEngine
}

// NewHealthChecker 创建健康检查器。
func NewHealthChecker(engines map[string]engine.SemanticQueryEngine) *HealthChecker {
	return &HealthChecker{
		statuses: make(map[string]*HealthStatus),
		engines:  engines,
	}
}

// CheckAll 检查所有引擎健康状态。
func (c *HealthChecker) CheckAll(ctx context.Context) map[string]*HealthStatus {
	c.mu.Lock()
	defer c.mu.Unlock()

	for name, eng := range c.engines {
		start := time.Now()
		err := eng.Ping(ctx)
		latency := time.Since(start)

		prev := c.statuses[name]
		status := &HealthStatus{
			Name:      name,
			LastCheck: time.Now(),
			Latency:   latency,
		}

		if err != nil {
			status.Status = "unhealthy"
			status.Message = err.Error()
			if prev != nil {
				status.ErrorCount = prev.ErrorCount + 1
			} else {
				status.ErrorCount = 1
			}
		} else if latency > 5*time.Second {
			status.Status = "degraded"
			if prev != nil {
				status.ErrorCount = prev.ErrorCount
			}
		} else {
			status.Status = "healthy"
			if prev != nil {
				status.ErrorCount = prev.ErrorCount
			}
		}

		c.statuses[name] = status
	}

	result := make(map[string]*HealthStatus, len(c.statuses))
	for k, v := range c.statuses {
		cp := *v
		result[k] = &cp
	}
	return result
}

// IsHealthy 判断指定引擎是否健康。
func (c *HealthChecker) IsHealthy(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	status, ok := c.statuses[name]
	return ok && status.Status == "healthy"
}

// OverallStatus 返回综合健康状态。
func (c *HealthChecker) OverallStatus() string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if len(c.statuses) == 0 {
		return "unknown"
	}

	hasUnhealthy := false
	hasDegraded := false
	for _, s := range c.statuses {
		switch s.Status {
		case "unhealthy":
			hasUnhealthy = true
		case "degraded":
			hasDegraded = true
		}
	}

	if hasUnhealthy {
		return "unhealthy"
	}
	if hasDegraded {
		return "degraded"
	}
	return "healthy"
}
