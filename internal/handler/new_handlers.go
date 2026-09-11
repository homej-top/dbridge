package handler

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/homej-top/dbridge/internal/config"
	"github.com/homej-top/dbridge/internal/model"
	"github.com/homej-top/dbridge/internal/repository"
	"github.com/homej-top/dbridge/internal/service"
	cachePkg "github.com/homej-top/dbridge/pkg/cache"
	"github.com/homej-top/dbridge/pkg/storage"
	"go.uber.org/zap"
)

// ─── Script Handler ────────────────────────────────────────────────────────


type ScriptHandler struct {
	svc    *service.ScriptService
	logger *zap.Logger
}

func NewScriptHandler(logger *zap.Logger) *ScriptHandler {
	return &ScriptHandler{
		svc:    service.NewScriptService(repository.GetDB()),
		logger: logger,
	}
}

func (h *ScriptHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	keyword := c.Query("keyword")

	scripts, err := h.svc.List(userID, tenantID, keyword)
	if err != nil {
		h.logger.Error("list scripts failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": scripts, "total": len(scripts)}))
}

func (h *ScriptHandler) Tree(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	tree, err := h.svc.Tree(userID, tenantID)
	if err != nil {
		h.logger.Error("get script tree failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(tree))
}

func (h *ScriptHandler) Get(c *gin.Context) {
	id := c.Param("id")
	script, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "script not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(script))
}

func (h *ScriptHandler) Create(c *gin.Context) {
	var input service.CreateScriptInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	script, err := h.svc.Create(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create script failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(script))
}

func (h *ScriptHandler) CreateFolder(c *gin.Context) {
	var input service.CreateFolderInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	folder, err := h.svc.CreateFolder(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create folder failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(folder))
}

func (h *ScriptHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input service.UpdateScriptInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	script, err := h.svc.Update(id, input)
	if err != nil {
		h.logger.Error("update script failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(script))
}

func (h *ScriptHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(id); err != nil {
		h.logger.Error("delete script failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ScriptHandler) Move(c *gin.Context) {
	id := c.Param("id")
	var input service.MoveNodeInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	script, err := h.svc.Move(id, input)
	if err != nil {
		h.logger.Error("move script failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(script))
}

// ─── Script File System Handlers ──────────────────────────────────────────

func (h *ScriptHandler) resolveScriptStorage() (storage.FileStorage, string, bool) {
	b := storage.ResolveModule(storage.ModuleScript)
	var st storage.FileStorage
	basePath := "scripts"
	if b != nil && b.Storage != nil {
		st = b.Storage
		basePath = b.BasePath
		if basePath == "" {
			basePath = "scripts"
		}
	} else {
		st = storage.Get()
	}
	if st == nil {
		return nil, "", false
	}
	return st, basePath, true
}

// FsList lists directory contents within the script storage binding
func (h *ScriptHandler) FsList(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	dir := c.DefaultQuery("dir", "")
	// Validate path to prevent directory traversal attacks
	if dir != "" {
		var err error
		dir, err = validatePath(dir)
		if err != nil {
			h.logger.Error("script fs list: invalid path", zap.String("dir", dir), zap.Error(err))
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid path: "+err.Error()))
			return
		}
	}
	// Prepend basePath to ensure we only list files within the scripts directory
	fullDir := basePath
	if dir != "" {
		fullDir = basePath + "/" + dir
	}
	result, err := st.List(c.Request.Context(), fullDir, &storage.ListOptions{
		Page:     1,
		PageSize: 500,
	})
	if err != nil {
		h.logger.Error("script fs list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	// Strip basePath prefix from paths so frontend sees relative paths
	for i := range result.Files {
		result.Files[i].Path = strings.TrimPrefix(result.Files[i].Path, basePath+"/")
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// FsRead reads a file's text content from script storage
func (h *ScriptHandler) FsRead(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	// Validate path to prevent directory traversal attacks
	validatedPath, err := validatePath(filePath)
	if err != nil {
		h.logger.Error("script fs read: invalid path", zap.String("path", filePath), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid path: "+err.Error()))
		return
	}
	// Prepend basePath to ensure we only read files within the scripts directory
	fullPath := basePath + "/" + validatedPath
	data, err := st.Read(c.Request.Context(), fullPath)
	if err != nil {
		h.logger.Error("script fs read failed", zap.Error(err))
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "file not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"path":    filePath,
		"content": string(data),
	}))
}

// FsSave creates or overwrites a file in script storage
func (h *ScriptHandler) FsSave(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	var req struct {
		Path    string `json:"path" binding:"required"`
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	// Validate path to prevent directory traversal attacks
	validatedPath, err := validatePath(req.Path)
	if err != nil {
		h.logger.Error("script fs save: invalid path", zap.String("path", req.Path), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid path: "+err.Error()))
		return
	}
	// Prepend basePath to ensure we only save files within the scripts directory
	fullPath := basePath + "/" + validatedPath
	reader := strings.NewReader(req.Content)
	_, err = st.Save(c.Request.Context(), fullPath, reader, "text/plain")
	if err != nil {
		h.logger.Error("script fs save failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// FsMkdir creates a directory in script storage
func (h *ScriptHandler) FsMkdir(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	// Validate path to prevent directory traversal attacks
	validatedPath, err := validatePath(req.Path)
	if err != nil {
		h.logger.Error("script fs mkdir: invalid path", zap.String("path", req.Path), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid path: "+err.Error()))
		return
	}
	// Prepend basePath to ensure we only create directories within the scripts directory
	fullPath := basePath + "/" + validatedPath
	if err := st.Mkdir(c.Request.Context(), fullPath); err != nil {
		h.logger.Error("script fs mkdir failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(nil))
}

// FsDelete deletes a file or directory in script storage
func (h *ScriptHandler) FsDelete(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	// Validate path to prevent directory traversal attacks
	validatedPath, err := validatePath(filePath)
	if err != nil {
		h.logger.Error("script fs delete: invalid path", zap.String("path", filePath), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid path: "+err.Error()))
		return
	}
	isDir := c.Query("is_dir") == "true"
	// Prepend basePath to ensure we only delete files within the scripts directory
	fullPath := basePath + "/" + validatedPath
	if isDir {
		if err := st.RemoveDir(c.Request.Context(), fullPath); err != nil {
			h.logger.Error("script fs remove dir failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
	} else {
		if err := st.Delete(c.Request.Context(), fullPath); err != nil {
			h.logger.Error("script fs delete failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// FsRename renames/moves a file or directory in script storage
func (h *ScriptHandler) FsRename(c *gin.Context) {
	st, basePath, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	var req struct {
		OldPath string `json:"old_path" binding:"required"`
		NewPath string `json:"new_path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	// Validate paths to prevent directory traversal attacks
	validatedOldPath, err := validatePath(req.OldPath)
	if err != nil {
		h.logger.Error("script fs rename: invalid old path", zap.String("old_path", req.OldPath), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid old path: "+err.Error()))
		return
	}
	validatedNewPath, err := validatePath(req.NewPath)
	if err != nil {
		h.logger.Error("script fs rename: invalid new path", zap.String("new_path", req.NewPath), zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid new path: "+err.Error()))
		return
	}
	// Prepend basePath to ensure we only rename files within the scripts directory
	fullOldPath := basePath + "/" + validatedOldPath
	fullNewPath := basePath + "/" + validatedNewPath
	if err := st.Rename(c.Request.Context(), fullOldPath, fullNewPath); err != nil {
		h.logger.Error("script fs rename failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── Report Handler ────────────────────────────────────────────────────────

type ReportHandler struct {
	svc      *service.ReportService
	querySvc *service.QueryService
	logger   *zap.Logger
}

func NewReportHandler(logger *zap.Logger, cfg *config.Config) *ReportHandler {
	return &ReportHandler{
		svc:      service.NewReportService(repository.GetDB()),
		querySvc: service.NewQueryService(repository.GetDB()),
		logger:   logger,
	}
}

func (h *ReportHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")
	var categoryID *string
	if v := c.Query("category_id"); v != "" {
		categoryID = &v
	}

	reports, err := h.svc.List(userID, tenantID, categoryID)
	if err != nil {
		h.logger.Error("list reports failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": reports, "total": len(reports)}))
}

func (h *ReportHandler) Get(c *gin.Context) {
	id := c.Param("id")
	report, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "report not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(report))
}

func (h *ReportHandler) Create(c *gin.Context) {
	var input service.CreateReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	report, err := h.svc.Create(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create report failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(report))
}

func (h *ReportHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input service.UpdateReportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	report, err := h.svc.Update(id, input)
	if err != nil {
		h.logger.Error("update report failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	// 报表内容/配置变更后主动失效缓存，避免陈旧数据
	invalidateReportCache(c.Request.Context(), id)
	c.JSON(http.StatusOK, model.SuccessResponse(report))
}

func (h *ReportHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(id); err != nil {
		h.logger.Error("delete report failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	// 删除后清理缓存
	invalidateReportCache(c.Request.Context(), id)
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// invalidateReportCache 报表更新/删除后主动失效缓存（普通报表 + 系统报表两个 key），
// 避免 TTL 未到期时读到陈旧数据。缓存不存在时删除为无副作用操作。
func invalidateReportCache(ctx context.Context, id string) {
	c := cachePkg.Get()
	_ = c.Del(ctx, cachePkg.Key("report", id))
	_ = c.Del(ctx, cachePkg.Key("report", "system:"+id))
}

func (h *ReportHandler) Execute(c *gin.Context) {
	id := c.Param("id")
	report, err := h.svc.Get(id)
	if err != nil {
		h.logger.Error("execute report failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// 报表结果缓存（cache_ttl<=0 时跳过缓存，直接执行 SQL）
	cacheKey := cachePkg.Key("report", id)
	if report.CacheTTL > 0 {
		var cached *service.QueryOutput
		if err := cachePkg.Get().GetJSON(c.Request.Context(), cacheKey, &cached); err == nil && cached != nil {
			c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
				"report":    report,
				"result":    cached,
				"cached_at": time.Now().Format("2006-01-02 15:04:05"),
			}))
			return
		}
	}

	result, err := h.querySvc.Execute(service.QueryInput{
		DataSourceID:       report.DataSourceID,
		SQL:                report.SQLContent,
		Schema:             report.SchemaName,
		SubscriptionStatus: c.GetString("subscription_status"),
	})
	if err != nil {
		c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
			"report": report,
			"error":  err.Error(),
		}))
		return
	}

	// 写缓存（ttl<=0 不写）
	if report.CacheTTL > 0 {
		if err := cachePkg.Get().SetJSON(c.Request.Context(), cacheKey, result, time.Duration(report.CacheTTL)*time.Second); err != nil {
			h.logger.Warn("report cache set failed", zap.Error(err))
		}
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"report": report,
		"result": result,
	}))
}

// ─── Report Category Handler ──────────────────────────────────────────────

type ReportCategoryHandler struct {
	svc    *service.ReportCategoryService
	logger *zap.Logger
}

func NewReportCategoryHandler(logger *zap.Logger) *ReportCategoryHandler {
	return &ReportCategoryHandler{
		svc:    service.NewReportCategoryService(repository.GetDB()),
		logger: logger,
	}
}

func (h *ReportCategoryHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	tree, err := h.svc.List(userID, tenantID)
	if err != nil {
		h.logger.Error("list categories failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(tree))
}

func (h *ReportCategoryHandler) Create(c *gin.Context) {
	var input service.CreateCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	cat, err := h.svc.Create(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create category failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(cat))
}

func (h *ReportCategoryHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input service.UpdateCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	cat, err := h.svc.Update(id, input)
	if err != nil {
		h.logger.Error("update category failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(cat))
}

func (h *ReportCategoryHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(id); err != nil {
		h.logger.Error("delete category failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ReportCategoryHandler) Move(c *gin.Context) {
	id := c.Param("id")
	var input service.MoveCategoryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	cat, err := h.svc.Move(id, input)
	if err != nil {
		h.logger.Error("move category failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(cat))
}

// ─── Dashboard Report Handler ──────────────────────────────────────────────

type DashboardReportHandler struct {
	svc    *service.DashboardReportService
	logger *zap.Logger
}

func NewDashboardReportHandler(logger *zap.Logger) *DashboardReportHandler {
	return &DashboardReportHandler{
		svc:    service.NewDashboardReportService(repository.GetDB()),
		logger: logger,
	}
}

func (h *DashboardReportHandler) List(c *gin.Context) {
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	dashboards, err := h.svc.List(userID, tenantID)
	if err != nil {
		h.logger.Error("list dashboards failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": dashboards, "total": len(dashboards)}))
}

func (h *DashboardReportHandler) Get(c *gin.Context) {
	id := c.Param("id")
	dashboard, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "dashboard not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(dashboard))
}

func (h *DashboardReportHandler) Create(c *gin.Context) {
	var input service.CreateDashboardInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	dashboard, err := h.svc.Create(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create dashboard failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(dashboard))
}

func (h *DashboardReportHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var input service.UpdateDashboardInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	dashboard, err := h.svc.Update(id, input)
	if err != nil {
		h.logger.Error("update dashboard failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(dashboard))
}

func (h *DashboardReportHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	if err := h.svc.Delete(id); err != nil {
		h.logger.Error("delete dashboard failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}
