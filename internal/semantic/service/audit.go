package service

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

const ModuleSemantic = "semantic"

// AuditEntry 语义查询审计条目。
type AuditEntry struct {
	UserID       string
	TenantID     string
	Username     string
	Engine       string
	DataSourceID string
	Measures     []string
	Dimensions   []string
	Filters      []byte
	GeneratedSQL string
	DurationMs   int64
	RowCount     int64
	Status       string
	ErrorMessage string
	CallerType   string
	IP           string
	UserAgent    string
}

// AuditLogger 语义查询审计日志记录器。
type AuditLogger struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewAuditLogger 创建审计日志记录器。
func NewAuditLogger(db *gorm.DB, logger *zap.Logger) *AuditLogger {
	return &AuditLogger{db: db, logger: logger}
}

// Log 记录一次语义查询审计。
func (a *AuditLogger) Log(ctx context.Context, entry AuditEntry) {
	details := map[string]interface{}{
		"engine":        entry.Engine,
		"data_source":   entry.DataSourceID,
		"measures":      entry.Measures,
		"dimensions":    entry.Dimensions,
		"generated_sql": entry.GeneratedSQL,
		"duration_ms":   entry.DurationMs,
		"row_count":     entry.RowCount,
		"caller_type":   entry.CallerType,
	}
	if len(entry.Filters) > 0 {
		var filters interface{}
		if err := json.Unmarshal(entry.Filters, &filters); err == nil {
			details["filters"] = filters
		}
	}
	if entry.ErrorMessage != "" {
		details["error"] = entry.ErrorMessage
	}

	jsonDetails, err := json.Marshal(details)
	if err != nil {
		jsonDetails = []byte("{}")
	}

	record := &repository.AuditLog{
		UserID:    entry.UserID,
		TenantID:  entry.TenantID,
		Module:    ModuleSemantic,
		Operation: "query",
		Result:    entry.Status,
		Details:   string(jsonDetails),
		IP:        entry.IP,
		UserAgent: entry.UserAgent,
		Username:  entry.Username,
		CreatedAt: time.Now(),
	}

	if err := a.db.WithContext(ctx).Create(record).Error; err != nil {
		if a.logger != nil {
			a.logger.Error("semantic audit write failed", zap.Error(err))
		}
	}
}
