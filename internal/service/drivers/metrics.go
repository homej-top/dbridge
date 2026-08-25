package drivers

import "time"

// ServerMetricsV2 provides structured database monitoring metrics
type ServerMetricsV2 struct {
	Connections      ConnectionMetrics           `json:"connections"`
	Throughput       ThroughputMetrics           `json:"throughput"`
	BufferCache      CacheMetrics                `json:"buffer_cache"`
	Locks            LockMetrics                 `json:"locks"`
	Storage          StorageMetrics              `json:"storage"`
	Replication      *ReplicationMetrics         `json:"replication,omitempty"`
	DatabaseSpecific map[string]interface{}      `json:"database_specific"`
	DBType           string                      `json:"db_type"`
	CollectedAt      time.Time                   `json:"collected_at"`
	CostMs           int64                       `json:"cost_ms"`
	Warnings         []string                    `json:"warnings,omitempty"`
}

type ConnectionMetrics struct {
	Total          int     `json:"total"`
	Active         int     `json:"active"`
	Idle           int     `json:"idle,omitempty"`
	Waiting        int     `json:"waiting,omitempty"`
	MaxConnections int     `json:"max_connections"`
	UsagePercent   float64 `json:"usage_percent"`
}

type ThroughputMetrics struct {
	QuestionsTotal int64   `json:"questions_total"`
	QPS            float64 `json:"qps,omitempty"`
	CommitTotal    int64   `json:"commit_total,omitempty"`
	RollbackTotal  int64   `json:"rollback_total,omitempty"`
	SlowQueries    int     `json:"slow_queries"`
}

type CacheMetrics struct {
	HitRate    float64 `json:"hit_rate"`
	TotalMB    float64 `json:"total_mb,omitempty"`
	DirtyPages int     `json:"dirty_pages,omitempty"`
}

type LockMetrics struct {
	Deadlocks        int `json:"deadlocks"`
	LockWaits        int `json:"lock_waits"`
	LongTransactions int `json:"long_transactions"`
	BlockedSessions  int `json:"blocked_sessions"`
}

type StorageMetrics struct {
	Tablespaces []TablespaceMetric `json:"tablespaces,omitempty"`
}

type TablespaceMetric struct {
	Name      string  `json:"name"`
	SizeMB    float64 `json:"size_mb"`
	UsedMB    float64 `json:"used_mb,omitempty"`
	FreeMB    float64 `json:"free_mb,omitempty"`
	UsagePct  float64 `json:"usage_pct,omitempty"`
	MaxSizeMB float64 `json:"max_size_mb,omitempty"`
}

type ReplicationMetrics struct {
	LagSeconds       float64 `json:"lag_seconds"`
	IOThreadRunning  *bool   `json:"io_thread_running,omitempty"`
	SQLThreadRunning *bool   `json:"sql_thread_running,omitempty"`
	DanglingSlots    *int    `json:"dangling_slots,omitempty"`
}
