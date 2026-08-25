package service

import (
	"context"
	"crypto/md5"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"sync"

	_ "modernc.org/sqlite"
	"sync/atomic"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service/drivers"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
)

// validDrivers is a whitelist of supported driver names.
var validDrivers = map[string]bool{
	"mysql": true, "postgres": true, "oracle": true, "sqlserver": true, "sqlite": true,
}

// PoolConfig maps to database/sql SetXXX methods for a single *sql.DB.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnIdleTime    time.Duration
}

// ManagerConfig controls the pool-cache lifecycle.
type ManagerConfig struct {
	PoolIdleTimeout time.Duration // how long before an unused pool is removed from cache
	CleanupInterval time.Duration
}

// PoolEntry is a cached *sql.DB instance.
type PoolEntry struct {
	DB       *sql.DB
	Driver   string
	LastUsed time.Time
	PoolKey  string
}

// PoolStat exposes runtime metrics for a single pool.
type PoolStat struct {
	PoolKey      string `json:"pool_key"`
	Driver       string `json:"driver"`
	LastUsed     string `json:"last_used"`
	OpenConns    int    `json:"open_conns"`
	InUse        int    `json:"in_use"`
	Idle         int    `json:"idle"`
	WaitCount    int64  `json:"wait_count"`
	WaitDuration string `json:"wait_duration"`
}

// PoolManagerStats is the top-level stats payload.
type PoolManagerStats struct {
	TotalPools int                 `json:"total_pools"`
	Pools      map[string]PoolStat `json:"pools"`
}

// ConnectionPoolManager caches *sql.DB instances keyed by PoolKey,
// providing reuse across requests. It does not manage individual TCP
// connections — that is handled by database/sql.
type ConnectionPoolManager struct {
	mu       sync.RWMutex
	pools    map[string]*PoolEntry
	sf       singleflight.Group
	cfg      PoolConfig
	mgrCfg   ManagerConfig
	perDB    map[string]PoolConfig
	logger   *zap.Logger
	stopCh   chan struct{}
	stopOnce sync.Once
	shutdown atomic.Bool
}

// NewConnectionPoolManager creates the manager and starts the cleanup loop.
func NewConnectionPoolManager(cfg PoolConfig, mgrCfg ManagerConfig, perDB map[string]PoolConfig, logger *zap.Logger) *ConnectionPoolManager {
	m := &ConnectionPoolManager{
		pools:  make(map[string]*PoolEntry),
		cfg:    cfg,
		mgrCfg: mgrCfg,
		perDB:  perDB,
		logger: logger,
		stopCh: make(chan struct{}),
	}
	go m.cleanupLoop()
	return m
}

// PoolKey returns a stable, opaque identifier for a (driverType, dsn) pair.
func PoolKey(driverType, dsn string) string {
	h := md5.Sum([]byte(driverType + ":" + dsn))
	return fmt.Sprintf("%x", h)
}

// GetWithContext returns a cached *sql.DB or creates one.
// sql.Open only allocates the struct; the first real network I/O happens in PingContext.
func (m *ConnectionPoolManager) GetWithContext(ctx context.Context, poolKey, driverName, dsn string) (*sql.DB, error) {
	if m.shutdown.Load() {
		return nil, fmt.Errorf("pool manager is shutting down")
	}
	if !validDrivers[driverName] {
		return nil, fmt.Errorf("unsupported driver: %s", driverName)
	}

	// fast path — cache hit
	m.mu.RLock()
	entry, exists := m.pools[poolKey]
	m.mu.RUnlock()
	if exists {
		entry.LastUsed = time.Now()
		m.logger.Debug("pool cache hit", zap.String("poolKey", poolKey))
		return entry.DB, nil
	}

	m.logger.Info("pool cache miss, creating new pool",
		zap.String("driver", driverName),
		zap.String("poolKey", poolKey))

	// singleflight prevents thundering-herd on the same key
	ch := m.sf.DoChan(poolKey, func() (interface{}, error) {
		// double-check inside singleflight
		m.mu.RLock()
		e, ok := m.pools[poolKey]
		m.mu.RUnlock()
		if ok {
			return e.DB, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		cfg := m.getConfig(driverName)
		db, err := sql.Open(driverName, dsn)
		if err != nil {
			return nil, err
		}
		db.SetMaxOpenConns(cfg.MaxOpenConns)
		db.SetMaxIdleConns(cfg.MaxIdleConns)
		db.SetConnMaxLifetime(cfg.ConnMaxLifetime)
		db.SetConnMaxIdleTime(cfg.ConnIdleTime)

		timeout := 3 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			if d := time.Until(deadline); d > 0 && d < timeout {
				timeout = d
			}
		}
		pingCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
		defer cancel()
		if err := db.PingContext(pingCtx); err != nil {
			db.Close()
			m.logger.Error("pool init ping failed",
				zap.String("driver", driverName),
				zap.String("poolKey", poolKey),
				zap.Error(err))
			m.sf.Forget(poolKey)
			return nil, fmt.Errorf("ping %s: %w", driverName, err)
		}

		m.logger.Info("new pool created",
			zap.String("driver", driverName),
			zap.String("poolKey", poolKey))

		m.mu.Lock()
		m.pools[poolKey] = &PoolEntry{
			DB:       db,
			Driver:   driverName,
			LastUsed: time.Now(),
			PoolKey:  poolKey,
		}
		m.mu.Unlock()
		return db, nil
	})

	select {
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*sql.DB), nil
	case <-ctx.Done():
		m.sf.Forget(poolKey)
		return nil, ctx.Err()
	}
}

// Evict removes a pool from the cache. It does NOT close the underlying *sql.DB
// so that in-flight requests are not disrupted. Old connections die naturally
// via SetConnMaxLifetime.
func (m *ConnectionPoolManager) Evict(poolKey string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exist := m.pools[poolKey]; exist {
		delete(m.pools, poolKey)
		m.logger.Info("pool evicted", zap.String("poolKey", poolKey))
	}
}

// Stats returns approximate per-pool metrics.
func (m *ConnectionPoolManager) Stats() PoolManagerStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	pools := make(map[string]PoolStat, len(m.pools))
	for k, e := range m.pools {
		s := e.DB.Stats()
		pools[k] = PoolStat{
			PoolKey:      k,
			Driver:       e.Driver,
			LastUsed:     e.LastUsed.UTC().Format(time.RFC3339),
			OpenConns:    s.OpenConnections,
			InUse:        s.InUse,
			Idle:         s.Idle,
			WaitCount:    s.WaitCount,
			WaitDuration: s.WaitDuration.String(),
		}
	}
	return PoolManagerStats{TotalPools: len(pools), Pools: pools}
}

// Shutdown closes all cached pools and stops the cleanup loop.
func (m *ConnectionPoolManager) Shutdown(ctx context.Context) error {
	m.shutdown.Store(true)
	m.stopOnce.Do(func() { close(m.stopCh) })

	done := make(chan struct{})
	go func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		for key, entry := range m.pools {
			entry.DB.Close()
			delete(m.pools, key)
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// cleanupLoop periodically removes pools that have been idle too long.
func (m *ConnectionPoolManager) cleanupLoop() {
	ticker := time.NewTicker(m.mgrCfg.CleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			m.cleanup()
		case <-m.stopCh:
			return
		}
	}
}

func (m *ConnectionPoolManager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for key, entry := range m.pools {
		if now.Sub(entry.LastUsed) > m.mgrCfg.PoolIdleTimeout {
			// Only delete the cache entry; do NOT call db.Close()
			// so in-flight requests are not broken.
			delete(m.pools, key)
		}
	}
}

func (m *ConnectionPoolManager) getConfig(driverName string) PoolConfig {
	if override, ok := m.perDB[driverName]; ok {
		return override
	}
	return m.cfg
}

// ─── Global singleton ──────────────────────────────────────────────────────

var globalPoolManager *ConnectionPoolManager

// InitPoolManager creates the global pool manager. Call once during startup.
func InitPoolManager(cfg PoolConfig, mgrCfg ManagerConfig, perDB map[string]PoolConfig, logger *zap.Logger) {
	globalPoolManager = NewConnectionPoolManager(cfg, mgrCfg, perDB, logger)
}

// ShutdownPoolManager shuts down the global pool manager.
func ShutdownPoolManager(ctx context.Context) error {
	if globalPoolManager != nil {
		return globalPoolManager.Shutdown(ctx)
	}
	return nil
}

// PoolManager returns the global manager (may be nil before InitPoolManager).
func PoolManager() *ConnectionPoolManager {
	return globalPoolManager
}

// ─── DSN builders (URI format, url.QueryEscape) ────────────────────────────

func driverNameOf(dbType string) string {
	switch dbType {
	case "mysql", "mariadb", "oceanbase":
		return "mysql"
	case "postgres", "postgresql":
		return "postgres"
	case "oracle":
		return "oracle"
	case "sqlserver":
		return "sqlserver"
	case "sqlite":
		return "sqlite"
	}
	return ""
}

// BuildDSN builds a URI-format DSN for the given data source.
func BuildDSN(ds repository.DataSource, pwd string) string {
	switch ds.Type {
	case "mysql", "mariadb", "oceanbase":
		return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?timeout=10s&readTimeout=30s",
			url.QueryEscape(ds.Username), url.QueryEscape(pwd),
			ds.Host, ds.Port, url.QueryEscape(ds.Database))
	case "postgres", "postgresql":
		db := ds.Database
		if db == "" {
			db = "postgres"
		}
		return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=disable",
			url.QueryEscape(ds.Username), url.QueryEscape(pwd),
			ds.Host, ds.Port, url.QueryEscape(db))
	case "oracle":
		svc := ds.Database
		if svc == "" {
			svc = "ORCLPDB1"
		}
		if ds.ExtraConfig != "" {
			var extra map[string]string
			if json.Unmarshal([]byte(ds.ExtraConfig), &extra) == nil {
				if s, ok := extra["oracle_service"]; ok && s != "" {
					svc = s
				}
			}
		}
		return fmt.Sprintf("oracle://%s:%s@%s:%d/%s",
			url.QueryEscape(ds.Username), url.QueryEscape(pwd),
			ds.Host, ds.Port, url.QueryEscape(svc))
	case "sqlserver":
		return fmt.Sprintf("sqlserver://%s:%s@%s:%d?database=%s&encrypt=disable&connection+timeout=10",
			url.QueryEscape(ds.Username), url.QueryEscape(pwd),
			ds.Host, ds.Port, url.QueryEscape(ds.Database))
	case "sqlite":
		return fmt.Sprintf("file:%s?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", ds.Host)
	}
	return ""
}

// ConnectDriver creates a DatabaseDriver through the global pool manager.
// ctx must be the HTTP request context; do NOT pass context.Background().
func ConnectDriver(ctx context.Context, ds repository.DataSource, pwd, database string) (drivers.DatabaseDriver, error) {
	tmpDS := ds // value copy to avoid mutating the original
	if database != "" && database != tmpDS.Database {
		tmpDS.Database = database
	}

	dsn := BuildDSN(tmpDS, pwd)
	driverName := driverNameOf(tmpDS.Type)

	poolKey := PoolKey(driverName, dsn)
	db, err := globalPoolManager.GetWithContext(ctx, poolKey, driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("acquire connection pool: %w", err)
	}

	cfg := drivers.DriverConfig{
		Host:           tmpDS.Host,
		Port:           tmpDS.Port,
		Username:       tmpDS.Username,
		Password:       pwd,
		Database:       tmpDS.Database,
		MaxConnections: 10,
		DB:             db,
	}

	// Oracle-specific extra config
	if tmpDS.Type == "oracle" && tmpDS.ExtraConfig != "" {
		var extra map[string]string
		if json.Unmarshal([]byte(tmpDS.ExtraConfig), &extra) == nil {
			if v, ok := extra["oracle_service"]; ok {
				cfg.OracleService = v
			}
			if v, ok := extra["connect_mode"]; ok {
				cfg.OracleConnectMode = v
			}
			if v, ok := extra["role"]; ok {
				cfg.OracleRole = v
			}
		}
	}

	driver, _, err := drivers.CreateDriver(tmpDS.Type, cfg)
	return driver, err
}
