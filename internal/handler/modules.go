package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// ─── Dashboard Tab ──────────────────────────────────────────────────────────

type DashboardTab struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Type      string `json:"type"` // "overview" | "dashboard_view"
	ViewID    string `json:"view_id"`
	SortOrder int    `json:"sort_order"`
}

type DashboardHandler struct {
	svc    *service.DashboardService
	logger *zap.Logger
}

func NewDashboardHandler(cfg *config.Config, logger *zap.Logger) *DashboardHandler {
	return &DashboardHandler{
		svc:    service.NewDashboardService(repository.GetDB()),
		logger: logger,
	}
}

func (h *DashboardHandler) Stats(c *gin.Context) {
	stats, err := h.svc.GetStats()
	if err != nil {
		h.logger.Error("get dashboard stats failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(stats))
}

// GetTabs returns the current user's dashboard tab configuration
func (h *DashboardHandler) GetTabs(c *gin.Context) {
	key := "dashboard.tabs." + c.GetString("user_id")
	defaultTabs := `[{"id":"tab_overview","title":"系统概览","type":"overview","view_id":"","sort_order":0}]`
	settingsSvc := service.NewSettingsService(repository.GetDB())
	raw, err := settingsSvc.Get(key)
	if err != nil || raw == "" {
		raw = defaultTabs
	}
	var tabs []DashboardTab
	if err := json.Unmarshal([]byte(raw), &tabs); err != nil {
		h.logger.Warn("failed to parse dashboard tabs, using defaults", zap.Error(err))
		json.Unmarshal([]byte(defaultTabs), &tabs)
	}
	c.JSON(http.StatusOK, model.SuccessResponse(tabs))
}

// SaveTabs saves the current user's dashboard tab configuration
func (h *DashboardHandler) SaveTabs(c *gin.Context) {
	var tabs []DashboardTab
	if err := c.ShouldBindJSON(&tabs); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "无效的 Tab 配置格式"))
		return
	}
	if len(tabs) == 0 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "至少保留一个 Tab"))
		return
	}
	hasOverview := false
	for i, t := range tabs {
		if t.Type != "overview" && t.Type != "dashboard_view" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "Tab 类型必须是 overview 或 dashboard_view"))
			return
		}
		if t.Type == "dashboard_view" && t.ViewID == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "dashboard_view 类型的 Tab 必须指定 ViewID"))
			return
		}
		if len([]rune(t.Title)) > 30 {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "Tab 标题不能超过 30 个字符"))
			return
		}
		if t.ID == "" {
			t.ID = uuid.New().String()
		}
		if t.Type == "overview" {
			hasOverview = true
		}
		tabs[i] = t
	}
	if !hasOverview {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "系统概览 Tab 不可删除"))
		return
	}
	key := "dashboard.tabs." + c.GetString("user_id")
	raw, err := json.Marshal(tabs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "序列化失败"))
		return
	}
	settingsSvc := service.NewSettingsService(repository.GetDB())
	if err := settingsSvc.Set(key, string(raw), "dashboard", false); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "保存失败"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"saved": true}))
}

type QueryHandler struct {
	svc      *service.QueryService
	dsSvc    *service.DataSourceService
	auditSvc *service.AuditLogService
	logger   *zap.Logger
}

func NewQueryHandler(cfg *config.Config, logger *zap.Logger) *QueryHandler {
	return &QueryHandler{
		svc:      service.NewQueryService(repository.GetDB()),
		dsSvc:    service.NewDataSourceService(repository.GetDB()),
		auditSvc: service.NewAuditLogService(repository.GetDB()),
		logger:   logger,
	}
}

func (h *QueryHandler) Execute(c *gin.Context) {
	var req service.QueryInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.Execute(req)
	if err != nil {
		h.logger.Error("query execution failed", zap.Error(err))
		h.logQuery(c, req, 0, false, err.Error())
		c.JSON(http.StatusOK, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	h.logQuery(c, req, result.Duration, true, "")
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// GetDDL retrieves the CREATE TABLE/VIEW DDL for a given table
func (h *QueryHandler) GetDDL(c *gin.Context) {
	dataSourceID := c.Query("data_source_id")
	schema := c.Query("schema")
	table := c.Query("table")

	if dataSourceID == "" || table == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "data_source_id and table are required"))
		return
	}

	ddl, err := h.dsSvc.GetDDL(dataSourceID, schema, table)
	if err != nil {
		h.logger.Error("get DDL failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"ddl": ddl,
	}))
}

// ─── SQL Type Classification ──────────────────────────────────────────────

// classifySQL returns the SQL category: ddl, dml, dql, dcl, tcl
func classifySQL(sql string) string {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	if strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "SHOW") ||
		strings.HasPrefix(upper, "DESCRIBE") || strings.HasPrefix(upper, "DESC") ||
		strings.HasPrefix(upper, "EXPLAIN") || strings.HasPrefix(upper, "WITH") {
		return "dql"
	}
	if strings.HasPrefix(upper, "INSERT") || strings.HasPrefix(upper, "UPDATE") ||
		strings.HasPrefix(upper, "DELETE") || strings.HasPrefix(upper, "MERGE") {
		return "dml"
	}
	if strings.HasPrefix(upper, "CREATE") || strings.HasPrefix(upper, "ALTER") ||
		strings.HasPrefix(upper, "DROP") || strings.HasPrefix(upper, "TRUNCATE") {
		return "ddl"
	}
	if strings.HasPrefix(upper, "GRANT") || strings.HasPrefix(upper, "REVOKE") {
		return "dcl"
	}
	if strings.HasPrefix(upper, "COMMIT") || strings.HasPrefix(upper, "ROLLBACK") ||
		strings.HasPrefix(upper, "SAVEPOINT") {
		return "tcl"
	}
	// Internal markers for multi-step DDL operations (e.g. SQL Server schema rename)
	if strings.HasPrefix(upper, "-- @RENAME_SCHEMA") {
		return "ddl"
	}
	return "dql" // default to DQL
}

// executeTyped executes SQL with type validation
func (h *QueryHandler) executeTyped(c *gin.Context, expectedType string) {
	var req service.QueryInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	actualType := classifySQL(req.SQL)
	if actualType != expectedType {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError,
			fmt.Sprintf("SQL type mismatch: expected %s, got %s", expectedType, actualType)))
		return
	}
	result, err := h.svc.Execute(req)
	if err != nil {
		h.logger.Error("typed query failed", zap.String("type", expectedType), zap.Error(err))
		h.logQuery(c, req, 0, false, err.Error())
		c.JSON(http.StatusOK, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	h.logQuery(c, req, result.Duration, true, "")
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *QueryHandler) ExecuteDQL(c *gin.Context) { h.executeTyped(c, "dql") }
func (h *QueryHandler) ExecuteDML(c *gin.Context) { h.executeTyped(c, "dml") }
func (h *QueryHandler) ExecuteDDL(c *gin.Context) { h.executeTyped(c, "ddl") }
func (h *QueryHandler) ExecuteDCL(c *gin.Context) { h.executeTyped(c, "dcl") }
func (h *QueryHandler) ExecuteTCL(c *gin.Context) { h.executeTyped(c, "tcl") }

func (h *QueryHandler) logQuery(c *gin.Context, req service.QueryInput, duration int64, success bool, errMsg string) {
	result := service.ResultSuccess
	if !success { result = service.ResultFailure }
	service.QuickAudit(repository.GetDB(),
		c.GetString("user_id"), c.GetString("tenant_id"),
		service.ModuleQuery, "query_execute", req.DataSourceID,
		result, c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{
			SQL:      truncateSQL(req.SQL, 200),
			Target:   req.DataSourceID,
			Duration: duration,
			Error:    errMsg,
		})
}

func truncateSQL(sql string, maxLen int) string {
	if len(sql) <= maxLen {
		return sql
	}
	return sql[:maxLen] + "..."
}

type SyncTaskHandler struct {
	svc    *service.SyncTaskService
	logger *zap.Logger
}

func NewSyncTaskHandler(cfg *config.Config, logger *zap.Logger) *SyncTaskHandler {
	return &SyncTaskHandler{
		svc:    service.NewSyncTaskService(repository.GetDB()),
		logger: logger,
	}
}

func (h *SyncTaskHandler) List(c *gin.Context) {
	page := 1
	pageSize := 20
	if v := c.Query("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := c.Query("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			pageSize = ps
		}
	}

	tasks, total, err := h.svc.List(page, pageSize, c.GetString("tenant_id"))
	if err != nil {
		h.logger.Error("list sync tasks failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": tasks, "total": total}))
}

func (h *SyncTaskHandler) Get(c *gin.Context) {
	id := c.Param("id")
	task, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "task not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(task))
}

func (h *SyncTaskHandler) Create(c *gin.Context) {
	var req service.CreateSyncTaskInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	task, err := h.svc.Create(req, c.GetString("user_id"), c.GetString("tenant_id"))
	if err != nil {
		h.logger.Error("create sync task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(task))
}

func (h *SyncTaskHandler) Start(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.UpdateStatus(id, "running"); err != nil {
		h.logger.Error("start sync task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "started"}))
}

func (h *SyncTaskHandler) Stop(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.UpdateStatus(id, "stopped"); err != nil {
		h.logger.Error("stop sync task failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "stopped"}))
}

type CompareHandler struct {
	svc      *service.CompareService
	auditSvc *service.AuditLogService
	logger   *zap.Logger
}

func NewCompareHandler(cfg *config.Config, logger *zap.Logger) *CompareHandler {
	return &CompareHandler{
		svc:      service.NewCompareService(repository.GetDB()),
		auditSvc: service.NewAuditLogService(repository.GetDB()),
		logger:   logger,
	}
}

func (h *CompareHandler) CompareStructure(c *gin.Context) {
	var req struct {
		SourceDS        string `json:"source_ds" binding:"required"`
		SourceSchema    string `json:"source_schema"`
		SourceDatabase  string `json:"source_database"`
		TargetDS        string `json:"target_ds" binding:"required"`
		TargetSchema    string `json:"target_schema"`
		TargetDatabase  string `json:"target_database"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	objects, err := h.svc.CompareStructures(req.SourceDS, req.SourceSchema, req.SourceDatabase, req.TargetDS, req.TargetSchema, req.TargetDatabase)
	if err != nil {
		h.logger.Error("compare structure failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"objects": objects,
		"status":  "completed",
	}))
}

func (h *CompareHandler) GetTableData(c *gin.Context) {
	var req struct {
		DataSourceID string `json:"data_source_id" binding:"required"`
		Schema       string `json:"schema"`
		Database     string `json:"database"`
		Table        string `json:"table" binding:"required"`
		Page         int    `json:"page"`
		PageSize     int    `json:"page_size"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.GetTableData(req.DataSourceID, req.Schema, req.Table, req.Page, req.PageSize)
	if err != nil {
		h.logger.Error("get table data failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *CompareHandler) GetTableStructure(c *gin.Context) {
	var req struct {
		DataSourceID string `json:"data_source_id" binding:"required"`
		Schema       string `json:"schema"`
		Database     string `json:"database"`
		Table        string `json:"table" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.GetTableStructure(req.DataSourceID, req.Schema, req.Table)
	if err != nil {
		h.logger.Error("get table structure failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *CompareHandler) dsName(id string) string {
	var ds repository.DataSource
	if err := repository.GetDB().Select("name").Where("id = ?", id).First(&ds).Error; err != nil {
		return id
	}
	return ds.Name
}

func (h *CompareHandler) SyncStructure(c *gin.Context) {
	var req service.SyncStructureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.SyncStructure(req)
	if err != nil {
		h.logger.Error("sync structure failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	if !req.DryRun {
		sourceName := h.dsName(req.SourceDS)
		targetName := h.dsName(req.TargetDS)
		details, _ := json.Marshal(map[string]interface{}{
			"source":        sourceName,
			"source_id":     req.SourceDS,
			"source_schema": req.SourceSchema,
			"target":        targetName,
			"target_id":     req.TargetDS,
			"target_schema": req.TargetSchema,
			"table":         req.Table,
			"action":        req.Action,
			"ddl":           result.DDL,
			"success":       result.Success,
			"message":       result.Message,
			"operator":      c.GetString("username"),
		})
		if auditErr := h.auditSvc.Create(&repository.AuditLog{
			UserID:    c.GetString("user_id"),
			Operation: "sync_structure",
			Details:   string(details),
			IP:        c.ClientIP(),
			UserAgent: c.Request.UserAgent(),
			TenantID:  c.GetString("tenant_id"),
		}); auditErr != nil {
			h.logger.Error("failed to write audit log", zap.Error(auditErr))
		}
	}

	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *CompareHandler) SyncData(c *gin.Context) {
	var req service.SyncDataRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.SyncData(req)
	if err != nil {
		h.logger.Error("sync data failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	sourceName := h.dsName(req.SourceDS)
	targetName := h.dsName(req.TargetDS)
	details, _ := json.Marshal(map[string]interface{}{
		"source":        sourceName,
		"source_id":     req.SourceDS,
		"source_schema": req.SourceSchema,
		"target":        targetName,
		"target_id":     req.TargetDS,
		"target_schema": req.TargetSchema,
		"table":         req.Table,
		"mode":          req.Options.Mode,
		"options":       req.Options,
		"total_rows":    result.TotalRows,
		"synced_rows":   result.SyncedRows,
		"skipped_rows":  result.SkippedRows,
		"success":       result.Success,
		"errors":        result.Errors,
		"operator":      c.GetString("username"),
	})
	if auditErr := h.auditSvc.Create(&repository.AuditLog{
		UserID:    c.GetString("user_id"),
		Operation: "sync_data",
		Details:   string(details),
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
		TenantID:  c.GetString("tenant_id"),
	}); auditErr != nil {
		h.logger.Error("failed to write audit log", zap.Error(auditErr))
	}

	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

type AuditLogHandler struct {
	svc    *service.AuditLogService
	logger *zap.Logger
}

func NewAuditLogHandler(cfg *config.Config, logger *zap.Logger) *AuditLogHandler {
	return &AuditLogHandler{
		svc:    service.NewAuditLogService(repository.GetDB()),
		logger: logger,
	}
}

func (h *AuditLogHandler) List(c *gin.Context) {
	page := 1
	pageSize := 20
	module := c.Query("module")
	operation := c.Query("operation")
	result := c.Query("result")
	userID := c.Query("user_id")

	if v := c.Query("page"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			page = p
		}
	}
	if v := c.Query("page_size"); v != "" {
		if ps, err := strconv.Atoi(v); err == nil && ps > 0 {
			pageSize = ps
		}
	}

	logs, total, err := h.svc.List(page, pageSize, module, operation, result, userID)
	if err != nil {
		h.logger.Error("list audit logs failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// Batch query usernames from user table
	type nameMap map[string]string
	userIDs := make([]string, 0)
	seenUser := make(map[string]bool)
	targetIDs := make([]string, 0)
	seenTarget := make(map[string]bool)
	for _, l := range logs {
		if l.UserID != "" && !seenUser[l.UserID] {
			userIDs = append(userIDs, l.UserID)
			seenUser[l.UserID] = true
		}
		if l.TargetID != "" && !seenTarget[l.TargetID] {
			targetIDs = append(targetIDs, l.TargetID)
			seenTarget[l.TargetID] = true
		}
	}

	userNameMap := make(nameMap)
	targetNameMap := make(nameMap)
	db := repository.GetDB()

	if len(userIDs) > 0 {
		var users []repository.User
		db.Where("id IN ?", userIDs).Find(&users)
		for _, u := range users { userNameMap[u.ID] = u.Username }
	}
	if len(targetIDs) > 0 {
		// Try data_sources first (most common target)
		type dsRow struct{ ID, Name string }
		var dss []dsRow
		db.Table("data_sources").Where("id IN ?", targetIDs).Select("id, name").Scan(&dss)
		for _, d := range dss { targetNameMap[d.ID] = d.Name }
		// Also try reports
		type rpRow struct{ ID, Name string }
		var rps []rpRow
		db.Table("reports").Where("id IN ?", targetIDs).Select("id, name").Scan(&rps)
		for _, r := range rps { if targetNameMap[r.ID] == "" { targetNameMap[r.ID] = r.Name } }
	}

	// Enrich with username and target name
	type auditEnriched struct {
		repository.AuditLog
		UserName   string `json:"username"`
		TargetName string `json:"target_name"`
	}
	enriched := make([]auditEnriched, len(logs))
	for i, l := range logs {
		enriched[i] = auditEnriched{AuditLog: l, UserName: userNameMap[l.UserID], TargetName: targetNameMap[l.TargetID]}
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"list":  enriched,
		"total": total,
	}))
}

type SettingsHandler struct {
	svc      *service.SettingsService
	cfg      *config.Config
	logger   *zap.Logger
}

func NewSettingsHandler(cfg *config.Config, logger *zap.Logger) *SettingsHandler {
	return &SettingsHandler{
		svc:    service.NewSettingsService(repository.GetDB()),
		cfg:    cfg,
		logger: logger,
	}
}


func (h *SettingsHandler) Get(c *gin.Context) {
	// Load memory metrics
	memMetrics, _ := h.svc.GetByCategory("memory_metrics")

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"sync": h.cfg.Sync,
		"log": gin.H{
			"level":  h.cfg.Log.Level,
			"format": h.cfg.Log.Format,
		},
		"mcp_enabled":    h.svc.IsMCPEnabled(),
		"memory_metrics": memMetrics,
	}))
}

func (h *SettingsHandler) Update(c *gin.Context) {
	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	// Community edition: no user-updatable settings (AI config is Pro-only)
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"saved": true}))
}

func maskSecret(s string) string {
	if len(s) <= 6 {
		return ""
	}
	return s[:3] + "****" + s[len(s)-3:]
}

func isMasked(s string) bool {
	return len(s) >= 7 && s[3:7] == "****"
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

