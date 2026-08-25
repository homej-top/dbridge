package service

import (
	"github.com/dbridge/dbridge/internal/repository"
	"gorm.io/gorm"
)

type DashboardService struct {
	db *gorm.DB
}

func NewDashboardService(db *gorm.DB) *DashboardService {
	return &DashboardService{db: db}
}

type DashboardStats struct {
	DataSourceCount int64   `json:"data_source_count"`
	SyncTaskCount   int64   `json:"sync_task_count"`
	QueryCount      int64   `json:"query_count"`
	SuccessRate     float64 `json:"success_rate"`
	AuditLogCount   int64   `json:"audit_log_count"`
	RunningSyncs    int64   `json:"running_syncs"`
}

func (s *DashboardService) GetStats() (*DashboardStats, error) {
	stats := &DashboardStats{}

	if s.db == nil {
		return stats, nil
	}

	s.db.Model(&repository.DataSource{}).Count(&stats.DataSourceCount)
	s.db.Model(&repository.SyncTask{}).Count(&stats.SyncTaskCount)
	s.db.Model(&repository.SyncTask{}).Where("status = ?", "running").Count(&stats.RunningSyncs)
	s.db.Model(&repository.AuditLog{}).Count(&stats.AuditLogCount)

	var queryTotal int64
	var querySuccess int64
	s.db.Model(&repository.AuditLog{}).Where("operation = ?", "query_execute").Count(&queryTotal)
	s.db.Model(&repository.AuditLog{}).Where("operation = ? AND details LIKE ?", "query_execute", `%"success":true%`).Count(&querySuccess)

	stats.QueryCount = queryTotal
	if queryTotal > 0 {
		stats.SuccessRate = float64(querySuccess) / float64(queryTotal) * 100
	} else {
		stats.SuccessRate = 100
	}

	return stats, nil
}
