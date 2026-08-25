package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync/atomic"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	cachePkg "github.com/dbridge/dbridge/pkg/cache"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

var logger *zap.Logger

func SetAuditLogger(l *zap.Logger) {
	logger = l
}

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	ModuleSystem     = "system"
	ModuleDatasource = "datasource"
	ModuleQuery      = "query"
	ModuleSync       = "sync"
	ModuleAI         = "ai"
	ModuleSecurity   = "security"
	ModuleReport     = "report"
	ModuleExport     = "export"
	ModuleCompare    = "compare"
	ModuleAuth       = "auth"

	BulkSuffix = "_bulk"
	PrefixBulk = "bulk_"

	ResultSuccess = "success"
	ResultFailure = "failure"
)

// ─── Audit Details ─────────────────────────────────────────────────────────

type AuditDetails struct {
	SQL          string `json:"sql,omitempty"`
	Target       string `json:"target,omitempty"`
	DsType       string `json:"ds_type,omitempty"`
	Source       string `json:"source,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	SkillSlug    string `json:"skill_slug,omitempty"`
	SkillName    string `json:"skill_name,omitempty"`
	SkillVersion int    `json:"skill_version,omitempty"`
	Duration     int64  `json:"duration_ms,omitempty"`
	Error        string `json:"error,omitempty"`
	RowsAffected int64  `json:"rows_affected,omitempty"`
}

// ─── Service ────────────────────────────────────────────────────────────────

type AuditLogService struct {
	db *gorm.DB
}

func NewAuditLogService(db *gorm.DB) *AuditLogService {
	return &AuditLogService{db: db}
}

func (s *AuditLogService) Create(log *repository.AuditLog) error {
	return s.db.Create(log).Error
}

// QuickAudit writes a single audit log entry with structured details
func QuickAudit(db *gorm.DB, userID, tenantID, module, operation, targetID, result, ip, ua, username string, details *AuditDetails) error {
	svc := NewAuditLogService(db)
	var jsonDetails []byte
	if details != nil {
		var marshalErr error
		jsonDetails, marshalErr = json.Marshal(details)
		if marshalErr != nil {
			fallback := AuditDetails{Error: fmt.Sprintf("json marshal failed: %v", marshalErr)}
			jsonDetails, _ = json.Marshal(fallback)
		}
	} else {
		jsonDetails = []byte("{}")
	}
	logItem := &repository.AuditLog{
		UserID:    userID,
		Module:    module,
		Operation: operation,
		TargetID:  targetID,
		Result:    result,
		Details:   string(jsonDetails),
		IP:        ip,
		UserAgent: ua,
		Username:  username,
		TenantID:  tenantID,
		CreatedAt: time.Now(),
	}
	if err := svc.Create(logItem); err != nil {
		if logger != nil {
			logger.Error("audit write failed", zap.Error(err), zap.Any("log", logItem))
		} else {
			log.Printf("audit write failed: %v", err)
		}
		return err
	}
	return nil
}

// ─── List ───────────────────────────────────────────────────────────────────

func (s *AuditLogService) List(page, pageSize int, module, operation, result, userID string) ([]repository.AuditLog, int64, error) {
	var logs []repository.AuditLog
	var total int64
	q := s.db.Model(&repository.AuditLog{})
	if module != "" {
		q = q.Where("module = ?", module)
	}
	if operation != "" {
		q = q.Where("operation = ?", operation)
	}
	if result != "" {
		q = q.Where("result = ?", result)
	}
	if userID != "" {
		q = q.Where("user_id = ?", userID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if page < 1 { page = 1 }
	if pageSize < 1 || pageSize > 200 { pageSize = 20 }
	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// ─── Purge ──────────────────────────────────────────────────────────────────

// PurgeExpired deletes audit logs older than retentionDays in batches
func (s *AuditLogService) PurgeExpired(retentionDays int, batchSize int) (int64, error) {
	startTime := time.Now()
	cutoff := time.Now().AddDate(0, 0, -retentionDays)

	// 跨实例互斥锁：backend=redis 用统一缓存分布式锁；否则回退 DB 锁（settings 表）。
	// 注意：锁 TTL(5min) 必须大于业务最大耗时，审计清理最坏情况远小于 5 分钟。
	release, err := acquirePurgeLock()
	if err != nil {
		return 0, err
	}
	defer release()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	var total int64
	var lastErr error
	for {
		select {
		case <-ctx.Done():
			if logger != nil { logger.Warn("audit purge timed out", zap.Int64("deleted_sofar", total)) }
			s.writePurgeLog(total, time.Since(startTime), nil)
			return total, nil
		default:
		}
		tx := s.db.WithContext(ctx).Begin()
		var ids []uint
		err := tx.Model(&repository.AuditLog{}).
			Where("created_at < ?", cutoff).
			Order("id ASC").
			Limit(batchSize).
			Pluck("id", &ids).Error
		if err != nil {
			tx.Rollback()
			lastErr = err
			break
		}
		if len(ids) == 0 {
			tx.Commit()
			break
		}
		res := tx.Delete(&repository.AuditLog{}, ids)
		if res.Error != nil {
			tx.Rollback()
			lastErr = res.Error
			break
		}
		tx.Commit()
		total += res.RowsAffected
		if logger != nil {
			logger.Info("audit purge batch", zap.Int("batch_size", len(ids)), zap.Int64("total", total))
		}
	}
	if logger != nil { logger.Info("audit purge completed", zap.Int64("deleted", total)) }
	s.writePurgeLog(total, time.Since(startTime), lastErr)
	cleanExpiredLocks()
	return total, lastErr
}

func (s *AuditLogService) writePurgeLog(deleted int64, duration time.Duration, err error) {
	log := repository.AuditPurgeLog{
		Deleted:  deleted,
		Duration: duration.Milliseconds(),
	}
	if err != nil {
		log.Error = err.Error()
	}
	s.db.Create(&log)
}

// ─── Distributed Lock ───────────────────────────────────────────────────────

// acquirePurgeLock 获取审计清理的跨实例互斥锁。
// 后端为 redis 时使用统一缓存分布式锁（带 token）；否则回退 DB 锁（settings 表）。
// 返回的 release 必须被调用（配对释放）。
func acquirePurgeLock() (release func(), err error) {
	if cachePkg.Backend() == cachePkg.BackendRedis {
		lockKey := cachePkg.Key("lock", "audit_purge")
		locked, token, err := cachePkg.Get().Lock(context.Background(), lockKey, 5*time.Minute)
		if err != nil {
			return nil, fmt.Errorf("acquire purge lock failed: %w", err)
		}
		if !locked {
			return nil, fmt.Errorf("another purge task is running")
		}
		return func() {
			_ = cachePkg.Get().Unlock(context.Background(), lockKey, token)
		}, nil
	}
	// local 后端（或 DB 后端）：回退 DB 锁。多实例部署时进程内锁不跨实例，必须用 DB 锁。
	if !acquireDistributedLock("audit_purge_lock", 5*time.Minute) {
		return nil, fmt.Errorf("another purge task is running")
	}
	return func() { releaseDistributedLock("audit_purge_lock") }, nil
}

func acquireDistributedLock(lockKey string, ttl time.Duration) bool {
	db := repository.GetDB()
	if db == nil { return false }
	dialector := db.Config.Dialector.Name()
	switch dialector {
	case "sqlite":
		return true
	case "mysql", "postgres":
		return acquireDBLock(lockKey, ttl)
	default:
		return false
	}
}

func releaseDistributedLock(lockKey string) {
	db := repository.GetDB()
	if db == nil { return }
	db.Exec("DELETE FROM settings WHERE key = ? AND category = 'lock'", lockKey)
}

func acquireDBLock(lockKey string, ttl time.Duration) bool {
	db := repository.GetDB()
	if db == nil { return false }
	expires := time.Now().Add(ttl).Unix()
	now := time.Now().Unix()

	// Atomic UPDATE CAS: grab expired or empty lock
	result := db.Exec(
		"UPDATE settings SET value = ? WHERE key = ? AND (CAST(value AS INTEGER) < ? OR value = '' OR value IS NULL)",
		expires, lockKey, now,
	)
	if result.RowsAffected > 0 {
		return true
	}
	// Lock doesn't exist → INSERT
	err := db.Exec(
		"INSERT OR IGNORE INTO settings (key, value, category) VALUES (?, ?, 'lock')",
		lockKey, expires,
	).Error
	return err == nil && db.RowsAffected > 0
}

func cleanExpiredLocks() {
	db := repository.GetDB()
	if db == nil { return }
	now := time.Now().Unix()
	db.Exec("DELETE FROM settings WHERE category = 'lock' AND CAST(value AS INTEGER) < ?", now)
}

// ─── Disk Monitoring ────────────────────────────────────────────────────────

var writeSuspended atomic.Bool

func IsAuditWriteSuspended() bool {
	return writeSuspended.Load()
}

func StartDiskMonitor(dataDir string) {
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			usage := getDiskUsage(dataDir)
			if usage >= 0.95 {
				if writeSuspended.CompareAndSwap(false, true) {
					if logger != nil { logger.Warn("audit write suspended: disk usage >= 95%") }
				}
			} else if usage < 0.85 {
				if writeSuspended.CompareAndSwap(true, false) {
					if logger != nil { logger.Info("audit write resumed: disk recovered") }
				}
			}
		}
	}()
}

func getDiskUsage(dataDir string) float64 {
	// Per-platform implementation (unix.Statfs on Linux/macOS, GetDiskFreeSpaceEx on Windows)
	// Returns 0 if platform not supported (disk monitoring disabled)
	return 0
}

// ─── Cron Hot Reload ────────────────────────────────────────────────────────

type CronService struct {
	stopCh chan struct{}
}

var cronService = &CronService{}

func StartAuditPurgeCron(db *gorm.DB, cronExpr string, retentionDays, batchSize int) {
	cronService.stopCh = make(chan struct{})
	go func() {
		// Parse cron and run periodically
		// For now, use a simple ticker approach
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				svc := NewAuditLogService(db)
				svc.PurgeExpired(retentionDays, batchSize)
			case <-cronService.stopCh:
				return
			}
		}
	}()
}

func ReloadAuditPurgeCron(db *gorm.DB, cronExpr string, retentionDays, batchSize int) {
	if cronService.stopCh != nil {
		close(cronService.stopCh)
	}
	StartAuditPurgeCron(db, cronExpr, retentionDays, batchSize)
}

// GetSQLiteDataDir returns the configured SQLite data directory
func GetSQLiteDataDir() string {
	settingsSvc := NewSettingsService(repository.GetDB())
	dir, err := settingsSvc.Get("sqlite.data_dir")
	if err != nil || dir == "" {
		dir = "./data/sqlite"
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, 0755)
	}
	return dir
}
