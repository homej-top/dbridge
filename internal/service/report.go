package service

import (
	"fmt"

	"github.com/dbridge/dbridge/internal/repository"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Report represents a data report
type Report struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	Description  string  `json:"description"`
	DataSourceID string  `json:"data_source_id"`
	SchemaName   string  `json:"schema_name"`
	SQLContent   string  `json:"sql_content"`
	ChartType    string  `json:"chart_type"`
	ChartConfig  string  `json:"chart_config"`
	CacheTTL     int64   `json:"cache_ttl"`
	CategoryID   *string `json:"category_id"`
	UserID       string  `json:"user_id"`
	TenantID     string  `json:"tenant_id"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type ReportService struct {
	db *gorm.DB
}

func NewReportService(db *gorm.DB) *ReportService {
	return &ReportService{db: db}
}

type CreateReportInput struct {
	Name         string  `json:"name" binding:"required"`
	Description  string  `json:"description"`
	DataSourceID string  `json:"data_source_id" binding:"required"`
	SchemaName   string  `json:"schema_name"`
	SQLContent   string  `json:"sql_content" binding:"required"`
	ChartType    string  `json:"chart_type"`
	ChartConfig  string  `json:"chart_config"`
	CategoryID   *string `json:"category_id"`
}

type UpdateReportInput struct {
	Name         *string `json:"name"`
	Description  *string `json:"description"`
	DataSourceID *string `json:"data_source_id"`
	SchemaName   *string `json:"schema_name"`
	SQLContent   *string `json:"sql_content"`
	ChartType    *string `json:"chart_type"`
	ChartConfig  *string `json:"chart_config"`
	CategoryID   *string `json:"category_id"`
}

type GenerateSQLInput struct {
	DataSourceID string `json:"data_source_id" binding:"required"`
	Schema       string `json:"schema"`
	Description  string `json:"description" binding:"required"`
}

func mapReport(row map[string]interface{}) *Report {
	r := &Report{}
	mapStr(row, "id", &r.ID)
	mapStr(row, "name", &r.Name)
	mapStr(row, "description", &r.Description)
	mapStr(row, "data_source_id", &r.DataSourceID)
	mapStr(row, "schema_name", &r.SchemaName)
	mapStr(row, "sql_content", &r.SQLContent)
	mapStr(row, "chart_type", &r.ChartType)
	if r.ChartType == "" {
		r.ChartType = "table"
	}
	mapStr(row, "chart_config", &r.ChartConfig)
	if r.ChartConfig == "" {
		r.ChartConfig = "{}"
	}
	if v, ok := row["cache_ttl"]; ok && v != nil {
		fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &r.CacheTTL)
	} else {
		r.CacheTTL = 14400
	}
	if v, ok := row["category_id"]; ok && v != nil {
		s := toString(v)
		r.CategoryID = &s
	}
	mapStr(row, "user_id", &r.UserID)
	mapStr(row, "tenant_id", &r.TenantID)
	mapStr(row, "created_at", &r.CreatedAt)
	mapStr(row, "updated_at", &r.UpdatedAt)
	return r
}

func (s *ReportService) queryReports(query string, args ...interface{}) ([]*Report, error) {
	builder := repository.NewSQLBuilder()
	query = builder.Build(query, "created_at", "updated_at")
	rows, err := s.db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []*Report
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
		results = append(results, mapReport(row))
	}
	if results == nil {
		results = make([]*Report, 0)
	}
	return results, nil
}

// List returns reports, optionally filtered by category
func (s *ReportService) List(userID, tenantID string, categoryID *string) ([]*Report, error) {
	query := `SELECT id, name, description, data_source_id, schema_name, sql_content, chart_type, chart_config,
		COALESCE(cache_ttl, 14400) as cache_ttl, category_id, user_id, tenant_id, is_system,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM reports WHERE (user_id = ? OR user_id = 'system')`
	args := []interface{}{userID}
	if tenantID != "" {
		query += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if categoryID != nil && *categoryID != "" {
		query += " AND category_id = ?"
		args = append(args, *categoryID)
	}
	query += " ORDER BY updated_at DESC"
	return s.queryReports(query, args...)
}

// Get returns a single report by ID
func (s *ReportService) Get(id string) (*Report, error) {
	reports, err := s.queryReports(`SELECT id, name, description, data_source_id, schema_name, sql_content, chart_type, chart_config,
		COALESCE(cache_ttl, 14400) as cache_ttl, category_id, user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM reports WHERE id = ?`, id)
	if err != nil || len(reports) == 0 {
		return nil, fmt.Errorf("report not found: %s", id)
	}
	return reports[0], nil
}

// Create creates a new report
func (s *ReportService) Create(input CreateReportInput, userID, tenantID string) (*Report, error) {
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	chartType := input.ChartType
	if chartType == "" {
		chartType = "table"
	}
	chartConfig := input.ChartConfig
	if chartConfig == "" {
		chartConfig = "{}"
	}

	err := s.db.Exec(`INSERT INTO reports (id, name, description, data_source_id, schema_name, sql_content, chart_type, chart_config, cache_ttl, category_id, user_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 14400, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Description, input.DataSourceID, input.SchemaName, input.SQLContent,
		chartType, chartConfig, input.CategoryID, userID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Update updates a report
func (s *ReportService) Update(id string, input UpdateReportInput) (*Report, error) {
	report, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	if input.Name != nil {
		report.Name = *input.Name
	}
	if input.Description != nil {
		report.Description = *input.Description
	}
	if input.DataSourceID != nil {
		report.DataSourceID = *input.DataSourceID
	}
	if input.SchemaName != nil {
		report.SchemaName = *input.SchemaName
	}
	if input.SQLContent != nil {
		report.SQLContent = *input.SQLContent
	}
	if input.ChartType != nil {
		report.ChartType = *input.ChartType
	}
	if input.ChartConfig != nil {
		report.ChartConfig = *input.ChartConfig
	}
	if input.CategoryID != nil {
		report.CategoryID = input.CategoryID
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	err = s.db.Exec(`UPDATE reports SET name=?, description=?, data_source_id=?, schema_name=?, sql_content=?, chart_type=?, chart_config=?, category_id=?, updated_at=? WHERE id=?`,
		report.Name, report.Description, report.DataSourceID, report.SchemaName, report.SQLContent,
		report.ChartType, report.ChartConfig, report.CategoryID, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete deletes a report
func (s *ReportService) Delete(id string) error {
	return s.db.Exec(`DELETE FROM reports WHERE id = ?`, id).Error
}

// Execute executes the report's SQL and returns results
func (s *ReportService) Execute(id string) (*Report, *QueryOutput, error) {
	report, err := s.Get(id)
	if err != nil {
		return nil, nil, err
	}

	// We need QueryService to execute, but that creates circular deps.
	// Instead, return the report with SQL for the frontend to execute via /query endpoint.
	return report, nil, nil
}

// GenerateSQL generates SQL from a natural language description using AI
func (s *ReportService) GenerateSQL(input GenerateSQLInput) (string, error) {
	// This would call the AI service to generate SQL
	// For now, return a placeholder - the frontend handles this via /ai/chat
	return fmt.Sprintf("-- Generated SQL for: %s\nSELECT * FROM %s LIMIT 10;",
		strings.TrimSpace(input.Description), "table_name"), nil
}
