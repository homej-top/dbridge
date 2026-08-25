package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/pkg/storage"
	"gorm.io/gorm"
)

// DBExportService handles direct database export/import operations
type DBExportService struct {
	db      *gorm.DB
	storage storage.FileStorage
}

func NewDBExportService(db *gorm.DB, storage storage.FileStorage) *DBExportService {
	return &DBExportService{db: db, storage: storage}
}

// ─── Input Types ──────────────────────────────────────────────────────────

type DBExportInput struct {
	DsID             string   `json:"ds_id" binding:"required"`
	Schema           string   `json:"schema" binding:"required"`
	TargetDBType     string   `json:"target_db_type" binding:"required"`
	Tables           []string `json:"tables"`
	IncludeStructure bool     `json:"include_structure"`
	IncludeData      bool     `json:"include_data"`
	BatchSize        int      `json:"batch_size"`
	ExportFormat     string   `json:"export_format"`
}

type DBImportInput struct {
	DsID   string `json:"ds_id" binding:"required"`
	Schema string `json:"schema"`
	SQL    string `json:"sql" binding:"required"`
	Tables []string `json:"tables"`
}

// ─── Export ───────────────────────────────────────────────────────────────

// Export generates a SQL/DDL export file for the given data source
func (s *DBExportService) Export(input DBExportInput) ([]byte, string, error) {
	if input.BatchSize <= 0 {
		input.BatchSize = 500
	}
	if input.ExportFormat == "" {
		input.ExportFormat = "sql"
	}

	// Get data source
	var ds repository.DataSource
	if err := s.db.Where("id = ?", input.DsID).First(&ds).Error; err != nil {
		return nil, "", fmt.Errorf("data source not found: %w", err)
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("-- DBridge Export\n-- Source: %s (%s)\n-- Schema: %s\n-- Target DB Type: %s\n-- Generated: %s\n\n",
		ds.Name, ds.Type, input.Schema, input.TargetDBType, time.Now().Format("2006-01-02 15:04:05")))

	// Determine which tables to export
	tables := input.Tables
	if len(tables) == 0 {
		// Would list all tables via driver
		output.WriteString("-- No tables specified, use driver to list all tables\n")
	}

	// For each table, export structure and/or data
	for _, table := range tables {
		if input.IncludeStructure {
			output.WriteString(fmt.Sprintf("\n-- ========================================\n"))
			output.WriteString(fmt.Sprintf("-- Table: %s\n", table))
			output.WriteString(fmt.Sprintf("-- ========================================\n\n"))

			// TODO: Get DDL via driver
			output.WriteString(fmt.Sprintf("-- DDL for %s (requires driver integration)\n\n", table))
		}

		if input.IncludeData {
			output.WriteString(fmt.Sprintf("-- Data for %s (requires driver integration)\n\n", table))
		}
	}

	result := []byte(output.String())
	fileName := fmt.Sprintf("%s_%s_%s.sql",
		strings.ReplaceAll(ds.Name, " ", "_"),
		input.Schema,
		time.Now().Format("20060102_150405"))

	return result, fileName, nil
}

// ─── Import ───────────────────────────────────────────────────────────────

// Import executes a SQL script on the target data source
func (s *DBExportService) Import(input DBImportInput) (map[string]interface{}, error) {
	// Get data source
	var ds repository.DataSource
	if err := s.db.Where("id = ?", input.DsID).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found: %w", err)
	}

	result := map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Import to %s (%s) requires driver integration", ds.Name, ds.Type),
	}

	return result, nil
}

// ─── Single Table Import ──────────────────────────────────────────────────

type ImportSingleInput struct {
	TargetDSID   string `json:"target_ds_id" binding:"required"`
	TargetSchema string `json:"target_schema"`
	TargetTable  string `json:"target_table" binding:"required"`
	SQL          string `json:"sql" binding:"required"`
	Strategy     string `json:"strategy"` // fail, skip, update, replace
}

type ImportSingleResult struct {
	Success      bool     `json:"success"`
	TotalRows    int64    `json:"total_rows"`
	ImportedRows int64    `json:"imported_rows"`
	SkippedRows  int64    `json:"skipped_rows"`
	Errors       []string `json:"errors"`
}

// ImportSingle imports a single table using the provided SQL
func (s *DBExportService) ImportSingle(input ImportSingleInput) (*ImportSingleResult, error) {
	if input.Strategy == "" {
		input.Strategy = "fail"
	}

	// Get target data source
	var ds repository.DataSource
	if err := s.db.Where("id = ?", input.TargetDSID).First(&ds).Error; err != nil {
		return nil, fmt.Errorf("data source not found: %w", err)
	}

	_ = ds // Driver integration needed for actual execution

	return &ImportSingleResult{
		Success:      true,
		TotalRows:    0,
		ImportedRows: 0,
		SkippedRows:  0,
		Errors:       []string{},
	}, fmt.Errorf("driver integration required for single table import")
}

// ─── Import from File ─────────────────────────────────────────────────────

type ImportUploadInput struct {
	TargetDSID   string `json:"target_ds_id" binding:"required"`
	TargetSchema string `json:"target_schema"`
	TargetTable  string `json:"target_table"`
	Strategy     string `json:"strategy"`
}

// ImportUpload handles an uploaded SQL file import
func (s *DBExportService) ImportUpload(input ImportUploadInput, fileData []byte) (*ImportSingleResult, error) {
	return s.ImportSingle(ImportSingleInput{
		TargetDSID:   input.TargetDSID,
		TargetSchema: input.TargetSchema,
		TargetTable:  input.TargetTable,
		SQL:          string(fileData),
		Strategy:     input.Strategy,
	})
}

// ─── Manifest ─────────────────────────────────────────────────────────────

type ExportManifest struct {
	SourceName   string   `json:"source_name"`
	SourceType   string   `json:"source_type"`
	TargetType   string   `json:"target_type"`
	Tables       []string `json:"tables"`
	ExportedAt   string   `json:"exported_at"`
	Format       string   `json:"format"`
	IncludeDDL   bool     `json:"include_ddl"`
	IncludeData  bool     `json:"include_data"`
	BatchSize    int      `json:"batch_size"`
}

func (s *DBExportService) GenerateManifest(input DBExportInput, dsName string) ([]byte, error) {
	manifest := ExportManifest{
		SourceName:  dsName,
		SourceType:  input.TargetDBType, // actual type would come from driver
		TargetType:  input.TargetDBType,
		Tables:      input.Tables,
		ExportedAt:  time.Now().Format(time.RFC3339),
		Format:      input.ExportFormat,
		IncludeDDL:  input.IncludeStructure,
		IncludeData: input.IncludeData,
		BatchSize:   input.BatchSize,
	}
	return json.MarshalIndent(manifest, "", "  ")
}
