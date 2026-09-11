package service

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/homej-top/dbridge/internal/repository"
	"gorm.io/gorm"
)

// DashboardReport represents a dashboard view configuration
type DashboardReport struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Layout      string `json:"layout"` // JSON array of layout items
	UserID      string `json:"user_id"`
	TenantID    string `json:"tenant_id"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type DashboardReportService struct {
	db *gorm.DB
}

func NewDashboardReportService(db *gorm.DB) *DashboardReportService {
	return &DashboardReportService{db: db}
}

type CreateDashboardInput struct {
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	Layout      string `json:"layout"`
}

type UpdateDashboardInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Layout      *string `json:"layout"`
}

func mapDashboard(row map[string]interface{}) *DashboardReport {
	d := &DashboardReport{}
	mapStr(row, "id", &d.ID)
	mapStr(row, "name", &d.Name)
	mapStr(row, "description", &d.Description)
	mapStr(row, "layout", &d.Layout)
	if d.Layout == "" {
		d.Layout = "[]"
	}
	mapStr(row, "user_id", &d.UserID)
	mapStr(row, "tenant_id", &d.TenantID)
	mapStr(row, "created_at", &d.CreatedAt)
	mapStr(row, "updated_at", &d.UpdatedAt)
	return d
}

func (s *DashboardReportService) queryDashboards(query string, args ...interface{}) ([]*DashboardReport, error) {
	builder := repository.NewSQLBuilder()
	query = builder.Build(query, "created_at", "updated_at")
	rows, err := s.db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []*DashboardReport
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, c := range cols {
			row[c] = vals[i]
		}
		results = append(results, mapDashboard(row))
	}
	if results == nil {
		results = make([]*DashboardReport, 0)
	}
	return results, nil
}

// List returns all dashboards for a user (including system dashboards)
func (s *DashboardReportService) List(userID, tenantID string) ([]*DashboardReport, error) {
	return s.queryDashboards(`SELECT id, name, description, layout, user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM dashboards WHERE (user_id = ? AND tenant_id = ?) OR user_id = 'system'
		ORDER BY updated_at DESC`, userID, tenantID)
}

// Get returns a single dashboard by ID
func (s *DashboardReportService) Get(id string) (*DashboardReport, error) {
	dashboards, err := s.queryDashboards(`SELECT id, name, description, layout, user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM dashboards WHERE id = ?`, id)
	if err != nil || len(dashboards) == 0 {
		return nil, fmt.Errorf("dashboard not found: %s", id)
	}
	return dashboards[0], nil
}

// Create creates a new dashboard
func (s *DashboardReportService) Create(input CreateDashboardInput, userID, tenantID string) (*DashboardReport, error) {
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	layout := input.Layout
	if layout == "" {
		layout = "[]"
	}

	err := s.db.Exec(`INSERT INTO dashboards (id, name, description, layout, user_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Description, layout, userID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Update updates a dashboard
func (s *DashboardReportService) Update(id string, input UpdateDashboardInput) (*DashboardReport, error) {
	d, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		d.Name = *input.Name
	}
	if input.Description != nil {
		d.Description = *input.Description
	}
	if input.Layout != nil {
		d.Layout = *input.Layout
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	err = s.db.Exec(`UPDATE dashboards SET name=?, description=?, layout=?, updated_at=? WHERE id=?`,
		d.Name, d.Description, d.Layout, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete deletes a dashboard
func (s *DashboardReportService) Delete(id string) error {
	return s.db.Exec(`DELETE FROM dashboards WHERE id = ?`, id).Error
}
