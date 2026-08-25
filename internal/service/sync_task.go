package service

import (
	"errors"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SyncTaskService struct {
	db *gorm.DB
}

func NewSyncTaskService(db *gorm.DB) *SyncTaskService {
	return &SyncTaskService{db: db}
}

type CreateSyncTaskInput struct {
	Name        string `json:"name" binding:"required"`
	SourceDS    string `json:"source_ds" binding:"required"`
	TargetDS    string `json:"target_ds" binding:"required"`
	SourceTable string `json:"source_table" binding:"required"`
	TargetTable string `json:"target_table" binding:"required"`
	SyncMode    string `json:"sync_mode"`
}

func (s *SyncTaskService) List(page, pageSize int, tenantID string) ([]repository.SyncTask, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var tasks []repository.SyncTask
	var total int64

	q := s.db.Model(&repository.SyncTask{})
	if tenantID != "" {
		q = q.Where("tenant_id = ?", tenantID)
	}
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * pageSize
	if err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&tasks).Error; err != nil {
		return nil, 0, err
	}
	return tasks, total, nil
}

func (s *SyncTaskService) Get(id string) (*repository.SyncTask, error) {
	var task repository.SyncTask
	if err := s.db.Where("id = ?", id).First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *SyncTaskService) Create(input CreateSyncTaskInput, userID, tenantID string) (*repository.SyncTask, error) {
	if input.Name == "" || input.SourceDS == "" || input.TargetDS == "" {
		return nil, errors.New("missing required fields")
	}

	syncMode := input.SyncMode
	if syncMode == "" {
		syncMode = "full"
	}

	task := repository.SyncTask{
		ID:        uuid.New().String(),
		Name:      input.Name,
		SourceDS:  input.SourceDS,
		TargetDS:  input.TargetDS,
		SourceTable: input.SourceTable,
		TargetTable: input.TargetTable,
		SyncMode:  syncMode,
		Status:    "pending",
		Progress:  0,
		TenantID:  tenantID,
		CreatedBy: userID,
	}

	if err := s.db.Create(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *SyncTaskService) UpdateStatus(id, status string) error {
	updates := map[string]interface{}{"status": status}
	if status == "running" {
		now := time.Now()
		updates["last_sync_time"] = &now
	}
	return s.db.Model(&repository.SyncTask{}).Where("id = ?", id).Updates(updates).Error
}

func (s *SyncTaskService) Delete(id string) error {
	return s.db.Where("id = ?", id).Delete(&repository.SyncTask{}).Error
}
