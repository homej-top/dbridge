package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/dbridge/dbridge/pkg/storage"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// TaskHandler handles import/export task HTTP endpoints
type TaskHandler struct {
	svc    *service.TaskService
	logger *zap.Logger
}

// NewTaskHandler creates a new TaskHandler
func NewTaskHandler(logger *zap.Logger) *TaskHandler {
	return &TaskHandler{
		svc:    service.NewTaskService(repository.GetDB()),
		logger: logger,
	}
}

// ─── Export Task Endpoints ──────────────────────────────────────────────────

// CreateExportTask POST /export-tasks
func (h *TaskHandler) CreateExportTask(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		DataSourceID string `json:"data_source_id" binding:"required"`
		DatabaseName string `json:"database_name"`
		SchemaName   string `json:"schema_name"`
		Config       struct {
			Export *repository.ExportConfig `json:"export"`
		} `json:"config"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	cfg := req.Config.Export
	if cfg == nil {
		cfg = &repository.ExportConfig{
			ExportScope:   "schema",
			ExportContent: "all",
			ExportFormat:  "sql",
		}
	}

	task, err := h.svc.CreateExportTask(req.Name, req.DataSourceID, req.DatabaseName, req.SchemaName, cfg, userID, tenantID)
	if err != nil {
		h.logger.Error("create export task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(task))
}

// CreateImportTask POST /import-tasks
// Supports two modes:
//   - JSON: {"name":...,"data_source_id":...,"config":{"import":{...}}}
//   - Multipart: JSON "data" field + "file" field for upload mode
func (h *TaskHandler) CreateImportTask(c *gin.Context) {
	var req struct {
		Name         string `json:"name" binding:"required"`
		DataSourceID string `json:"data_source_id" binding:"required"`
		DatabaseName string `json:"database_name"`
		SchemaName   string `json:"schema_name"`
		Config       struct {
			Import *repository.ImportConfig `json:"import"`
		} `json:"config"`
	}

	contentType := c.ContentType()

	// Try multipart first (upload mode with file)
	if strings.Contains(contentType, "multipart/form-data") {
		dataStr := c.PostForm("data")
		if dataStr != "" {
			if err := json.Unmarshal([]byte(dataStr), &req); err != nil {
				c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid data JSON: "+err.Error()))
				return
			}
		} else {
			// Fallback: bind individual fields from form
			req.Name = c.PostForm("name")
			req.DataSourceID = c.PostForm("data_source_id")
			req.DatabaseName = c.PostForm("database_name")
			req.SchemaName = c.PostForm("schema_name")
			configStr := c.PostForm("config")
			if configStr != "" {
				json.Unmarshal([]byte(configStr), &req.Config)
			}
		}

		// Handle file upload
		file, header, err := c.Request.FormFile("file")
		if err == nil {
			defer file.Close()

			// 确定使用的存储实例：优先使用前端指定的 profile，否则用绑定配置或默认实例
			profileName := ""
			if req.Config.Import != nil {
				profileName = req.Config.Import.StorageProfile
			}
			var st storage.FileStorage
			var basePath string
			if profileName != "" {
				st = storage.GetByName(profileName)
			}
			if st == nil {
				b := storage.ResolveModule(storage.ModuleImportExport)
				if b != nil && b.Storage != nil {
					st = b.Storage
					basePath = b.BasePath
				}
			}
			if st == nil {
				st = storage.Get()
			}
			if st == nil {
				c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "storage not available"))
				return
			}

			importBase := basePath
			if importBase == "" {
				importBase = "imports"
			}
			uploadPath := filepath.Join(importBase, time.Now().Format("2006/01"), req.Name+"_"+header.Filename)
			// 从 multipart 请求头读取真实的 Content-Type
			contentType := header.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}
			// 使用独立 context，避免 HTTP 请求超时中断大文件写入
			if _, err := st.Save(context.Background(), uploadPath, file, contentType); err != nil {
				h.logger.Error("upload import file failed", zap.Error(err))
				c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
				return
			}
			if req.Config.Import == nil {
				req.Config.Import = &repository.ImportConfig{ImportSource: "upload"}
			}
			req.Config.Import.ImportFilePath = uploadPath
			req.Config.Import.StorageProfile = profileName
		}
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
			return
		}
	}

	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	cfg := req.Config.Import
	if cfg == nil {
		cfg = &repository.ImportConfig{
			ImportSource:   "upload",
			ImportContent:  "all",
			ImportStrategy: "fail",
		}
	}

	task, err := h.svc.CreateImportTask(req.Name, req.DataSourceID, req.DatabaseName, req.SchemaName, cfg, userID, tenantID)
	if err != nil {
		h.logger.Error("create import task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(task))
}

// ─── Task CRUD Endpoints ────────────────────────────────────────────────────

// ListTasks GET /export-tasks or /import-tasks
func (h *TaskHandler) ListTasks(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	taskType := c.Query("task_type") // "export" or "import"

	tasks, err := h.svc.ListTasks(tenantID, taskType)
	if err != nil {
		h.logger.Error("list tasks failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	// Expand config to top-level fields for frontend compatibility
	result := make([]gin.H, len(tasks))
	for i, t := range tasks {
		result[i] = flattenTask(&t)
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"list":  result,
		"total": len(result),
	}))
}

// GetTask GET /export-tasks/:id or /import-tasks/:id
func (h *TaskHandler) GetTask(c *gin.Context) {
	id := c.Param("id")
	task, err := h.svc.GetTask(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "task not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(flattenTask(task)))
}

// DeleteTask DELETE /export-tasks/:id
func (h *TaskHandler) DeleteTask(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.DeleteTask(id); err != nil {
		h.logger.Error("delete task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── Execution Endpoints ────────────────────────────────────────────────────

// StartExecution POST /export-tasks/:id/start or /import-tasks/:id/start
func (h *TaskHandler) StartExecution(c *gin.Context) {
	taskID := c.Param("id")
	exec, err := h.svc.StartExecution(taskID)
	if err != nil {
		h.logger.Error("start execution failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(exec))
}

// CancelExecution POST /export-tasks/:id/cancel
func (h *TaskHandler) CancelExecution(c *gin.Context) {
	taskID := c.Param("id")
	// Get latest running execution
	exec, err := h.svc.GetLatestExecution(taskID)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "no execution found"))
		return
	}
	if err := h.svc.CancelExecution(exec.ID); err != nil {
		h.logger.Error("cancel execution failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "cancelled"}))
}

// ListExecutions GET /export-tasks/:id/executions
func (h *TaskHandler) ListExecutions(c *gin.Context) {
	taskID := c.Param("id")
	execs, err := h.svc.ListExecutions(taskID)
	if err != nil {
		h.logger.Error("list executions failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"list":  execs,
		"total": len(execs),
	}))
}

// GetExecution GET /export-tasks/:id/executions/:eid
func (h *TaskHandler) GetExecution(c *gin.Context) {
	eid := c.Param("eid")
	exec, err := h.svc.GetExecution(eid)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "execution not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(exec))
}

// GetExecutionLogs GET /export-tasks/:id/executions/:eid/logs
func (h *TaskHandler) GetExecutionLogs(c *gin.Context) {
	eid := c.Param("eid")
	exec, err := h.svc.GetExecution(eid)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "execution not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"status":        exec.Status,
		"progress":      exec.Progress,
		"log_text":      exec.LogText,
		"log_file_path": exec.LogFilePath,
		"error_msg":     exec.ErrorMsg,
		"started_at":    exec.StartedAt,
		"finished_at":   exec.FinishedAt,
	}))
}

// DownloadResult GET /export-tasks/:id/executions/:eid/download
func (h *TaskHandler) DownloadResult(c *gin.Context) {
	eid := c.Param("eid")
	exec, err := h.svc.GetExecution(eid)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "execution not found"))
		return
	}

	if exec.ResultFilePath == "" {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "no result file"))
		return
	}

	st := storage.Get()
	if st == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "storage not available"))
		return
	}

	reader, err := st.ReadStream(c.Request.Context(), exec.ResultFilePath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "file not found"))
		return
	}
	defer reader.Close()

	fileName := exec.ResultFileName
	if fileName == "" {
		fileName = filepath.Base(exec.ResultFilePath)
	}

	c.Header("Content-Type", "application/sql")
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	c.Header("Content-Length", strconv.FormatInt(exec.ResultFileSize, 10))

	io.Copy(c.Writer, reader)
}

// ─── Storage File Browse ────────────────────────────────────────────────────

// BrowseStorageFiles GET /storage-files?profile=xxx&path=xxx
func (h *TaskHandler) BrowseStorageFiles(c *gin.Context) {
	profile := c.Query("profile")
	dirPath := c.Query("path")

	files, err := h.svc.BrowseStorageFiles(profile, dirPath)
	if err != nil {
		h.logger.Error("browse storage files failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"current_path": dirPath,
		"files":        files,
	}))
}

// ─── Helpers ────────────────────────────────────────────────────────────────

// flattenTask expands Task.Config JSON to top-level fields for frontend compatibility
func flattenTask(t *repository.Task) gin.H {
	m := gin.H{
		"id":             t.ID,
		"name":           t.Name,
		"task_type":      t.TaskType,
		"data_source_id": t.DataSourceID,
		"database_name":  t.DatabaseName,
		"schema_name":    t.SchemaName,
		"status":         t.Status,
		"tenant_id":      t.TenantID,
		"created_by":     t.CreatedBy,
		"created_at":     t.CreatedAt,
		"updated_at":     t.UpdatedAt,
	}

	switch t.TaskType {
	case "export":
		cfg, err := t.GetExportConfig()
		if err == nil && cfg != nil {
			m["export_scope"] = cfg.ExportScope
			m["export_content"] = cfg.ExportContent
			m["export_format"] = cfg.ExportFormat
			m["export_batch_size"] = cfg.ExportBatchSize
			m["export_tables"] = cfg.ExportTables
			m["storage_profile"] = cfg.StorageProfile
			m["storage_path"] = cfg.StoragePath
			// Old field compat
			m["export_type"] = cfg.ExportScope
		}
	case "import":
		cfg, err := t.GetImportConfig()
		if err == nil && cfg != nil {
			m["import_source"] = cfg.ImportSource
			m["import_content"] = cfg.ImportContent
			m["import_strategy"] = cfg.ImportStrategy
			m["skip_safety_check"] = cfg.SkipSafetyCheck
			m["import_file_name"] = cfg.ImportFileName
			m["import_file_path"] = cfg.ImportFilePath
			m["storage_profile"] = cfg.StorageProfile
			m["source_ds_id"] = cfg.SourceDSID
			m["source_database"] = cfg.SourceDatabase
			m["source_schema"] = cfg.SourceSchema
			m["source_tables"] = cfg.SourceTables
			m["target_schema"] = coalesce(cfg.TargetSchema, t.SchemaName)
			m["target_database"] = coalesce(cfg.TargetDatabase, t.DatabaseName)
		}
	}
	return m
}

// coalesce returns the first non-empty string
func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
