package handler

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/dbridge/dbridge/pkg/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// FileManagerHandler 文件管理 HTTP Handler
type FileManagerHandler struct {
	getStorage func() storage.FileStorage
	cfg        *config.Config
	logger     *zap.Logger
}

// NewFileManagerHandler 创建文件管理 Handler
func NewFileManagerHandler(getStorage func() storage.FileStorage, cfg *config.Config, logger *zap.Logger) *FileManagerHandler {
	if getStorage == nil {
		getStorage = storage.Get
	}
	return &FileManagerHandler{getStorage: getStorage, cfg: cfg, logger: logger}
}

// requireStorage 获取存储实例；不可用时返回 503 并返回 false，避免 nil 解引用 panic
func (h *FileManagerHandler) requireStorage(c *gin.Context) (storage.FileStorage, bool) {
	st := h.getStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return nil, false
	}
	return st, true
}

// List 列出目录内容
func (h *FileManagerHandler) List(c *gin.Context) {
	profile := c.Query("profile")
	var st storage.FileStorage
	if profile != "" {
		mgr := storage.GetManager()
		if mgr != nil {
			st = mgr.GetByCode(profile)
		}
		if st == nil {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeResourceNotFound, "存储实例不存在或已禁用"))
			return
		}
	} else {
		st = h.getStorage()
		if st == nil {
			c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
			return
		}
	}
	dir := c.DefaultQuery("dir", "")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "50"))
	search := c.Query("search")
	recursive := c.Query("recursive") == "true"
	nextToken := c.Query("next_token")

	opts := &storage.ListOptions{
		Page:      page,
		PageSize:  pageSize,
		Search:    search,
		Recursive: recursive,
		NextToken: nextToken,
	}
	result, err := st.List(c.Request.Context(), dir, opts)
	if err != nil {
		if os.IsNotExist(err) {
			c.JSON(http.StatusOK, model.SuccessResponse(&storage.ListResult{
				Files: []storage.FileInfo{}, Total: 0,
			}))
			return
		}
		h.logger.Error("list files failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// Tree 获取目录树
func (h *FileManagerHandler) Tree(c *gin.Context) {
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	dir := c.DefaultQuery("dir", "")
	depth, _ := strconv.Atoi(c.DefaultQuery("depth", "5"))

	// 读取全局最大深度配置
	if h.cfg != nil && h.cfg.Storage.Limits.MaxDirDepth > 0 {
		if depth == 0 || depth > h.cfg.Storage.Limits.MaxDirDepth {
			depth = h.cfg.Storage.Limits.MaxDirDepth
		}
	}
	if depth == -1 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "depth=-1 is forbidden"))
		return
	}

	nodes, err := st.Tree(c.Request.Context(), dir, depth)
	if err != nil {
		// S3 后端 depth 超限时自动降级为 3 重试
		var treeErr *storage.ErrTreeDepthExceeded
		if errors.As(err, &treeErr) {
			nodes, err = st.Tree(c.Request.Context(), dir, 3)
		}
	}
	if err != nil {
		h.logger.Error("tree failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nodes))
}

// Upload 上传文件
func (h *FileManagerHandler) Upload(c *gin.Context) {
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	dir := c.PostForm("dir")
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "file required"))
		return
	}
	defer file.Close()

	// 文件名清洗 + 类型白名单校验
	safeName := sanitizeFileName(header.Filename)
	if !h.isUploadAllowed(safeName) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError,
			"文件类型不允许上传，允许的类型: "+strings.Join(h.cfg.Storage.Limits.UploadAllowTypes, ", ")))
		return
	}

	// 单文件大小校验
	if h.cfg.Storage.Limits.MaxSingleFile > 0 && header.Size > h.cfg.Storage.Limits.MaxSingleFile {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError,
			fmt.Sprintf("文件大小 %d 超过限制 %d", header.Size, h.cfg.Storage.Limits.MaxSingleFile)))
		return
	}

	targetPath := safeName
	if dir != "" {
		targetPath = strings.TrimRight(dir, "/") + "/" + safeName
	}

	contentType := header.Header.Get("Content-Type")
	info, err := st.Save(c.Request.Context(), targetPath, file, contentType)
	if err != nil {
		h.logger.Error("upload failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(info))
}

// Download 下载文件
func (h *FileManagerHandler) Download(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	reader, err := st.ReadStream(c.Request.Context(), path)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "file not found"))
		return
	}

	// 获取文件信息以设置 Content-Type
	info, _ := st.Stat(c.Request.Context(), path)
	contentType := "application/octet-stream"
	if info != nil && info.ContentType != "" {
		contentType = info.ContentType
	}

	// io.Pipe 桥接：goroutine 中关闭原始 reader，避免双重关闭竞争
	pr, pw := io.Pipe()
	go func() {
		defer reader.Close()
		defer pw.Close()
		_, _ = io.Copy(pw, reader)
	}()

	c.Header("Content-Disposition", "attachment; filename=\""+filepath.Base(path)+"\"")
	c.DataFromReader(http.StatusOK, info.Size, contentType, pr, nil)
}

// Stat 获取文件信息
func (h *FileManagerHandler) Stat(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	info, err := st.Stat(c.Request.Context(), path)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(info))
}

// Mkdir 创建目录
func (h *FileManagerHandler) Mkdir(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	if err := st.Mkdir(c.Request.Context(), req.Path); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(nil))
}

// Rename 重命名/移动
func (h *FileManagerHandler) Rename(c *gin.Context) {
	var req struct {
		OldPath string `json:"old_path" binding:"required"`
		NewPath string `json:"new_path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	if err := st.Rename(c.Request.Context(), req.OldPath, req.NewPath); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// Delete 删除文件
func (h *FileManagerHandler) Delete(c *gin.Context) {
	path := c.Query("path")
	if path == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	if err := st.Delete(c.Request.Context(), path); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// RemoveDir 删除目录
func (h *FileManagerHandler) RemoveDir(c *gin.Context) {
	var req struct {
		Path string `json:"path" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	h.logger.Warn("RemoveDir called", zap.String("path", req.Path), zap.String("user_id", c.GetString("user_id")))
	if err := st.RemoveDir(c.Request.Context(), req.Path); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// DeleteBatch 批量删除
func (h *FileManagerHandler) DeleteBatch(c *gin.Context) {
	var req struct {
		Paths []string `json:"paths" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	st, ok := h.requireStorage(c)
	if !ok {
		return
	}
	if err := st.DeleteBatch(c.Request.Context(), req.Paths); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── Profile 管理 ──────────────────────────────────────────────────

// ListProfiles 列出所有 Profile
func (h *FileManagerHandler) ListProfiles(c *gin.Context) {
	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}
	profiles := mgr.ListProfiles()
	c.JSON(http.StatusOK, model.SuccessResponse(profiles))
}

// SetDefaultProfile 切换默认 Profile
func (h *FileManagerHandler) SetDefaultProfile(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}

	previous := mgr.GetDefaultName()
	if err := mgr.SetDefault(req.Name); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	h.logger.Info("default storage profile changed",
		zap.String("previous", previous),
		zap.String("current", req.Name),
		zap.String("user_id", c.GetString("user_id")),
		zap.String("client_ip", c.ClientIP()),
		zap.String("user_agent", c.Request.UserAgent()))

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"previous": previous,
		"current":  req.Name,
	}))
}

// ─── 动态 Profile 管理（仅 admin） ────────────────────────────────

// CreateProfile 动态新增 Profile
func (h *FileManagerHandler) CreateProfile(c *gin.Context) {
	var req struct {
		Name    string                     `json:"name" binding:"required"`
		Code    string                     `json:"code"`
		Backend string                     `json:"backend" binding:"required"`
		Local   *config.LocalStorageConfig `json:"local,omitempty"`
		S3      *config.S3StorageConfig    `json:"s3,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}

	// 检查是否已存在
	if existing := mgr.Get(req.Name); existing != nil {
		c.JSON(http.StatusConflict, model.ErrorResponse(model.CodeParamError, "profile already exists"))
		return
	}

	// 根据 backend 类型创建 FileStorage
	var fs storage.FileStorage
	var summary map[string]string
	switch req.Backend {
	case "local":
		if req.Local == nil || req.Local.RootDir == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "local.root_dir required"))
			return
		}
		var err error
		fs, err = storage.NewLocalFileStorage(req.Local.RootDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		summary = map[string]string{"root_dir": req.Local.RootDir}
	case "s3":
		if req.S3 == nil || req.S3.Bucket == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "s3.bucket required"))
			return
		}
		var err error
		fs, err = storage.NewS3FileStorage(storage.S3Config{
			Endpoint:        req.S3.Endpoint,
			Region:          req.S3.Region,
			Bucket:          req.S3.Bucket,
			AccessKeyID:     req.S3.AccessKeyID,
			SecretAccessKey: req.S3.SecretAccessKey,
			UsePathStyle:    req.S3.UsePathStyle,
			DisableSSL:      req.S3.DisableSSL,
			Prefix:          req.S3.Prefix,
			SSEEnabled:      req.S3.SSEEnabled,
		})
		if err != nil {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError,
				"S3 连接失败: "+err.Error()+"。HTTP 端点需 disable_ssl=true，MinIO 需 use_path_style=true"))
			return
		}
		summary = map[string]string{
			"endpoint": req.S3.Endpoint,
			"bucket":   req.S3.Bucket,
			"region":   req.S3.Region,
		}
	default:
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "unsupported backend: "+req.Backend))
		return
	}

	code := req.Code
	if code == "" {
		code = req.Name
	}
	if err := mgr.Register(req.Name, code, req.Backend, fs, summary); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// 持久化到数据库
	configJSON, _ := json.Marshal(buildProfileConfig(req.Backend, req.Local, req.S3))
	db := repository.GetDB()
	if db != nil {
		instance := repository.StorageInstance{
			ID:         uuid.New().String(),
			Name:       req.Name,
			Code:       code,
			Backend:    req.Backend,
			Enabled:    true,
			ConfigJSON: string(configJSON),
		}
		if err := db.Create(&instance).Error; err != nil {
			h.logger.Warn("failed to persist profile to db", zap.Error(err))
		}
	}

	h.logger.Info("profile created", zap.String("name", req.Name), zap.String("code", code), zap.String("backend", req.Backend))
	c.JSON(http.StatusCreated, model.SuccessResponse(gin.H{"name": req.Name, "code": code, "backend": req.Backend}))
}

func buildProfileConfig(backend string, local *config.LocalStorageConfig, s3 *config.S3StorageConfig) map[string]interface{} {
	if backend == "local" && local != nil {
		return map[string]interface{}{"root_dir": local.RootDir}
	}
	if backend == "s3" && s3 != nil {
		return map[string]interface{}{
			"endpoint":          s3.Endpoint,
			"region":            s3.Region,
			"bucket":            s3.Bucket,
			"access_key_id":     s3.AccessKeyID,
			"secret_access_key": s3.SecretAccessKey,
			"use_path_style":    s3.UsePathStyle,
			"disable_ssl":       s3.DisableSSL,
			"prefix":            s3.Prefix,
			"sse_enabled":       s3.SSEEnabled,
		}
	}
	return nil
}

// DeleteProfile 动态删除 Profile（admin）
func (h *FileManagerHandler) DeleteProfile(c *gin.Context) {
	name := c.Param("name")
	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}

	// 1. 检查是否只剩一个启用的实例
	profiles := mgr.ListProfiles()
	enabledCount := 0
	for _, p := range profiles {
		if p.Enabled {
			enabledCount++
		}
	}
	if enabledCount <= 1 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "至少保留一个存储实例"))
		return
	}

	// 2. 检查该实例下是否有文件（带 3 秒超时，防止 List 阻塞）
	if fs := mgr.Get(name); fs != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()
		result, _ := fs.List(ctx, "", &storage.ListOptions{Recursive: true, PageSize: 1})
		if result != nil && len(result.Files) > 0 {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "该存储实例下存在文件，请先迁移或清理后再删除"))
			return
		}
	}

	// 3. 如果删除的是默认实例，先切换到另一个
	if mgr.GetDefaultName() == name {
		for _, p := range profiles {
			if p.Name != name && p.Enabled {
				if err := mgr.SetDefault(p.Name); err == nil {
					h.logger.Info("default storage switched before delete", zap.String("from", name), zap.String("to", p.Name))
					break
				}
			}
		}
	}

	if err := mgr.Unregister(name); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	// 从数据库删除
	db := repository.GetDB()
	if db != nil {
		// 级联清理：将引用该实例的绑定重置为默认实例
		var defaultInst repository.StorageInstance
		if err := db.Where("enabled = ?", true).Order("sort_order ASC").First(&defaultInst).Error; err == nil {
			db.Model(&repository.StorageBinding{}).
				Where("profile_code = ?", name).
				Updates(map[string]interface{}{
					"profile_code": defaultInst.Code,
					"updated_at":   time.Now(),
				})
		}
		db.Where("code = ?", name).Or("name = ?", name).Delete(&repository.StorageInstance{})
		storage.InvalidateBindingCache()
	}

	h.logger.Info("profile deleted", zap.String("name", name))
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// UpdateProfile 修改 Profile（admin）
func (h *FileManagerHandler) UpdateProfile(c *gin.Context) {
	name := c.Param("name")
	var req struct {
		Backend string                     `json:"backend"`
		Local   *config.LocalStorageConfig `json:"local,omitempty"`
		S3      *config.S3StorageConfig    `json:"s3,omitempty"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}

	// 确定 code（保持不变，除非未设置）
	code := name

	// 1. 用新配置创建临时 FileStorage（验证路径/连接有效性）
	var newFS storage.FileStorage
	var summary map[string]string
	var err error
	switch req.Backend {
	case "local":
		if req.Local == nil || req.Local.RootDir == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "local.root_dir required"))
			return
		}
		newFS, err = storage.NewLocalFileStorage(req.Local.RootDir)
		summary = map[string]string{"root_dir": req.Local.RootDir}
	case "s3":
		if req.S3 == nil || req.S3.Bucket == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "s3.bucket required"))
			return
		}
		newFS, err = storage.NewS3FileStorage(storage.S3Config{
			Endpoint:        req.S3.Endpoint,
			Region:          req.S3.Region,
			Bucket:          req.S3.Bucket,
			AccessKeyID:     req.S3.AccessKeyID,
			SecretAccessKey: req.S3.SecretAccessKey,
			UsePathStyle:    req.S3.UsePathStyle,
			DisableSSL:      req.S3.DisableSSL,
			Prefix:          req.S3.Prefix,
			SSEEnabled:      req.S3.SSEEnabled,
		})
		summary = map[string]string{
			"endpoint": req.S3.Endpoint, "bucket": req.S3.Bucket, "region": req.S3.Region,
		}
	default:
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "unsupported backend: "+req.Backend))
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "配置无效: "+err.Error()))
		return
	}

	// 2. 健康检查
	if checker, ok := newFS.(storage.HealthChecker); ok {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		if hErr := checker.HealthCheck(ctx); hErr != nil {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "配置验证失败: "+hErr.Error()))
			return
		}
	}

	// 3. 更新 DB
	db := repository.GetDB()
	if db != nil {
		var inst repository.StorageInstance
		if err := db.Where("code = ?", name).Or("name = ?", name).First(&inst).Error; err == nil {
			cfgJSON := buildProfileConfig(req.Backend, req.Local, req.S3)
			configJSON, _ := json.Marshal(cfgJSON)
			db.Model(&inst).Updates(map[string]interface{}{
				"backend":     req.Backend,
				"config_json": string(configJSON),
			})
		}
	}

	// 4. 原子替换内存实例
	if err := mgr.Replace(name, code, req.Backend, newFS, summary); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	h.logger.Info("profile updated", zap.String("name", name), zap.String("backend", req.Backend))
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"name": name, "backend": req.Backend}))
}

// TestProfile 测试 Profile 连通性
func (h *FileManagerHandler) TestProfile(c *gin.Context) {
	name := c.Param("name")
	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}
	fs := mgr.Get(name)
	if fs == nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "profile not found"))
		return
	}
	checker, ok := fs.(storage.HealthChecker)
	if !ok {
		c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "unknown", "message": "不支持连通性测试"}))
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	if err := checker.HealthCheck(ctx); err != nil {
		c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "disconnected", "message": err.Error()}))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"status": "connected", "message": "连接正常"}))
}

// ToggleProfile 切换 Profile 启用/停用状态
func (h *FileManagerHandler) ToggleProfile(c *gin.Context) {
	name := c.Param("name")
	db := repository.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "database not available"))
		return
	}
	var inst repository.StorageInstance
	if err := db.Where("code = ?", name).First(&inst).Error; err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "instance not found"))
		return
	}
	inst.Enabled = !inst.Enabled
	if err := db.Save(&inst).Error; err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	// 同步 ProfileManager
	mgr := storage.GetManager()
	if mgr != nil {
		if inst.Enabled {
			// 重新加载实例
			fs, err := createFSFromInstance(inst)
			if err == nil {
				mgr.Register(inst.Name, inst.Code, inst.Backend, fs, nil)
			}
		} else {
			mgr.Unregister(inst.Name)
		}
	}
	h.logger.Info("profile toggled", zap.String("code", name), zap.Bool("enabled", inst.Enabled))
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"enabled": inst.Enabled}))
}

func createFSFromInstance(inst repository.StorageInstance) (storage.FileStorage, error) {
	return storage.CreateFromJSONString(inst.Backend, inst.ConfigJSON)
}

// ─── 存储配额 ────────────────────────────────────────────────────

// GetQuota 查询当前存储用量与配额
func (h *FileManagerHandler) GetQuota(c *gin.Context) {
	limits := h.cfg.Storage.Limits

	st, ok := h.requireStorage(c)
	if !ok {
		return
	}

	// 计算当前用量
	ctx := c.Request.Context()
	usage := int64(0)

	// 遍历默认存储的所有文件统计大小
	result, err := st.List(ctx, "", &storage.ListOptions{Recursive: true, PageSize: 10000})
	if err != nil {
		// 递归统计失败就返回配额信息但无用量
		c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
			"usage_bytes":     0,
			"quota_bytes":     limits.TotalQuota,
			"unlimited":       limits.TotalQuota == 0,
			"max_single_file": limits.MaxSingleFile,
			"estimation_only": true,
		}))
		return
	}
	for _, f := range result.Files {
		if !f.IsDir {
			usage += f.Size
		}
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"usage_bytes":     usage,
		"quota_bytes":     limits.TotalQuota,
		"unlimited":       limits.TotalQuota == 0,
		"usage_percent":   usagePercent(usage, limits.TotalQuota),
		"max_single_file": limits.MaxSingleFile,
		"file_count":      len(result.Files),
	}))
}

func usagePercent(used, quota int64) float64 {
	if quota <= 0 {
		return 0
	}
	pct := float64(used) / float64(quota) * 100
	if pct > 100 {
		pct = 100
	}
	return float64(int(pct*10)) / 10 // 保留1位小数
}

// ─── 批量下载打包 ────────────────────────────────────────────────

// BatchDownload 批量打包下载（本地：流式 zip）
func (h *FileManagerHandler) BatchDownload(c *gin.Context) {
	var req struct {
		Paths []string `json:"paths" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	if len(req.Paths) == 0 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "paths required"))
		return
	}

	st, ok := h.requireStorage(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	// 本地存储：流式 zip 打包
	if _, isLocal := st.(*storage.LocalFileStorage); isLocal {
		c.Header("Content-Disposition", "attachment; filename=\"batch-download.zip\"")
		c.Header("Content-Type", "application/zip")

		zw := zip.NewWriter(c.Writer)
		defer zw.Close()

		for _, path := range req.Paths {
			reader, err := st.ReadStream(ctx, path)
			if err != nil {
				h.logger.Warn("batch download: skip file", zap.String("path", path), zap.Error(err))
				continue
			}
			entry, err := zw.Create(filepath.Base(path))
			if err != nil {
				reader.Close()
				continue
			}
			io.Copy(entry, reader)
			reader.Close()
		}
		return
	}

	// S3 存储：串行下载后打包
	c.Header("Content-Disposition", "attachment; filename=\"batch-download.zip\"")
	c.Header("Content-Type", "application/zip")

	zw := zip.NewWriter(c.Writer)
	defer zw.Close()

	for _, path := range req.Paths {
		reader, err := st.ReadStream(ctx, path)
		if err != nil {
			continue
		}
		entry, err := zw.Create(filepath.Base(path))
		if err != nil {
			reader.Close()
			continue
		}
		io.Copy(entry, reader)
		reader.Close()
	}
}

// sanitizeFileName 文件名清洗：过滤危险字符
func sanitizeFileName(name string) string {
	name = filepath.Base(name)
	result := make([]byte, 0, len(name))
	for _, ch := range []byte(name) {
		switch {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '.', ch == '-', ch == '_', ch == ' ':
			result = append(result, ch)
		default:
			if ch >= 0x80 {
				result = append(result, ch)
			} else {
				result = append(result, '_')
			}
		}
	}
	if len(result) == 0 {
		return "unnamed"
	}
	sanitized := strings.TrimSpace(string(result))
	sanitized = strings.ReplaceAll(sanitized, " ", "_")
	return sanitized
}

// isUploadAllowed 检查文件扩展名是否在白名单中
func (h *FileManagerHandler) isUploadAllowed(filename string) bool {
	allowTypes := h.cfg.Storage.Limits.UploadAllowTypes
	if len(allowTypes) == 0 {
		return true // 空白名单 = 不限制
	}
	ext := strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		return false
	}
	for _, allowed := range allowTypes {
		if strings.ToLower(allowed) == ext {
			return true
		}
	}
	return false
}

// ListBindings 查询所有业务模块的存储绑定配置
func (h *FileManagerHandler) ListBindings(c *gin.Context) {
	db := repository.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "database not available"))
		return
	}

	var bindings []repository.StorageBinding
	if err := db.Order("module_code ASC").Find(&bindings).Error; err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	mgr := storage.GetManager()
	type bindingResponse struct {
		ID               string `json:"id"`
		ModuleCode       string `json:"module_code"`
		ModuleName       string `json:"module_name"`
		ProfileCode      string `json:"profile_code"`
		BasePath         string `json:"base_path"`
		ProfileAvailable bool   `json:"profile_available"`
	}

	result := make([]bindingResponse, 0, len(bindings))
	for _, b := range bindings {
		available := false
		if mgr != nil {
			available = mgr.GetByCode(b.ProfileCode) != nil
		}
		result = append(result, bindingResponse{
			ID:               b.ID,
			ModuleCode:       b.ModuleCode,
			ModuleName:       b.ModuleName,
			ProfileCode:      b.ProfileCode,
			BasePath:         b.BasePath,
			ProfileAvailable: available,
		})
	}

	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// UpdateBinding 更新指定业务模块的存储绑定配置
func (h *FileManagerHandler) UpdateBinding(c *gin.Context) {
	moduleCode := c.Param("module_code")

	var req struct {
		ProfileCode string `json:"profile_code" binding:"required"`
		BasePath    string `json:"base_path"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid request: "+err.Error()))
		return
	}

	cleanedPath, err := storage.ValidateBasePath(req.BasePath)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "base_path invalid: "+err.Error()))
		return
	}

	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}
	if mgr.GetByCode(req.ProfileCode) == nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "存储实例不存在或已禁用"))
		return
	}

	db := repository.GetDB()
	if db == nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeServiceUnavailable, "database not available"))
		return
	}

	var binding repository.StorageBinding
	if err := db.Where("module_code = ?", moduleCode).First(&binding).Error; err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "绑定记录不存在"))
		return
	}

	binding.ProfileCode = req.ProfileCode
	binding.BasePath = cleanedPath
	if err := db.Save(&binding).Error; err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	storage.InvalidateBindingCache()

	h.logger.Info("binding updated",
		zap.String("module", moduleCode),
		zap.String("profile", req.ProfileCode),
		zap.String("base_path", cleanedPath),
	)

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"module_code":  binding.ModuleCode,
		"profile_code": binding.ProfileCode,
		"base_path":    binding.BasePath,
	}))
}

type transferRequest struct {
	SourceProfile string `json:"source_profile" binding:"required"`
	SourcePath    string `json:"source_path" binding:"required"`
	TargetProfile string `json:"target_profile"`
	TargetPath    string `json:"target_path"`
	Overwrite     bool   `json:"overwrite"`
}

func (h *FileManagerHandler) resolveTransferStorage(req *transferRequest) (src storage.FileStorage, dst storage.FileStorage, dstPath string, errCode int, errMsg string) {
	mgr := storage.GetManager()
	if mgr == nil {
		return nil, nil, "", http.StatusServiceUnavailable, "storage not initialized"
	}

	src = mgr.GetByCode(req.SourceProfile)
	if src == nil {
		return nil, nil, "", http.StatusBadRequest, "源存储实例不存在或已禁用"
	}

	if req.TargetProfile == "" {
		req.TargetProfile = req.SourceProfile
	}
	dst = mgr.GetByCode(req.TargetProfile)
	if dst == nil {
		return nil, nil, "", http.StatusBadRequest, "目标存储实例不存在或已禁用"
	}

	if req.TargetPath == "" {
		req.TargetPath = req.SourcePath
	}

	return src, dst, req.TargetPath, 0, ""
}

// CopyFiles 复制文件或目录
func (h *FileManagerHandler) CopyFiles(c *gin.Context) {
	var req transferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	src, dst, dstPath, errCode, errMsg := h.resolveTransferStorage(&req)
	if errCode != 0 {
		c.JSON(errCode, model.ErrorResponse(model.CodeParamError, errMsg))
		return
	}

	ctx := c.Request.Context()
	ts := storage.NewTransferService()
	defer ts.Close()

	stat, err := src.Stat(ctx, req.SourcePath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "源路径不存在"))
		return
	}

	if stat.IsDir {
		result, err := ts.CopyDir(ctx, src, req.SourcePath, dst, dstPath, req.Overwrite)
		if err != nil {
			h.logger.Error("copy dir failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
			"files", "copy_dir", req.SourcePath, "success", c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
			&service.AuditDetails{Extra: map[string]interface{}{"source_profile": req.SourceProfile, "target_profile": req.TargetProfile, "target_path": dstPath, "transferred": result.Transferred, "failed": result.Failed}})
		c.JSON(http.StatusOK, model.SuccessResponse(result))
	} else {
		err := ts.CopyFile(ctx, src, req.SourcePath, dst, dstPath, req.Overwrite)
		if err != nil {
			h.logger.Error("copy file failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
			"files", "copy_file", req.SourcePath, "success", c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
			&service.AuditDetails{Extra: map[string]interface{}{"source_profile": req.SourceProfile, "target_profile": req.TargetProfile, "target_path": dstPath}})
		c.JSON(http.StatusOK, model.SuccessResponse(&storage.TransferResult{
			TotalFiles:       1,
			Transferred:      1,
			TotalBytes:       stat.Size,
			TransferredBytes: stat.Size,
			Duration:         0,
		}))
	}
}

// MoveFiles 移动文件或目录
func (h *FileManagerHandler) MoveFiles(c *gin.Context) {
	var req transferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	src, dst, dstPath, errCode, errMsg := h.resolveTransferStorage(&req)
	if errCode != 0 {
		c.JSON(errCode, model.ErrorResponse(model.CodeParamError, errMsg))
		return
	}

	ctx := c.Request.Context()
	ts := storage.NewTransferService()
	defer ts.Close()

	stat, err := src.Stat(ctx, req.SourcePath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "源路径不存在"))
		return
	}

	if stat.IsDir {
		result, err := ts.MoveDir(ctx, src, req.SourcePath, dst, dstPath, req.Overwrite)
		if err != nil {
			h.logger.Error("move dir failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
			"files", "move_dir", req.SourcePath, "success", c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
			&service.AuditDetails{Extra: map[string]interface{}{"source_profile": req.SourceProfile, "target_profile": req.TargetProfile, "target_path": dstPath, "transferred": result.Transferred, "failed": result.Failed}})
		c.JSON(http.StatusOK, model.SuccessResponse(result))
	} else {
		err := ts.MoveFile(ctx, src, req.SourcePath, dst, dstPath, req.Overwrite)
		if err != nil {
			h.logger.Error("move file failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
			"files", "move_file", req.SourcePath, "success", c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
			&service.AuditDetails{Extra: map[string]interface{}{"source_profile": req.SourceProfile, "target_profile": req.TargetProfile, "target_path": dstPath}})
		c.JSON(http.StatusOK, model.SuccessResponse(&storage.TransferResult{
			TotalFiles:       1,
			Transferred:      1,
			TotalBytes:       stat.Size,
			TransferredBytes: stat.Size,
			Duration:         0,
		}))
	}
}

type syncRequest struct {
	SourceProfile string                   `json:"source_profile" binding:"required"`
	SourcePath    string                   `json:"source_path" binding:"required"`
	TargetProfile string                   `json:"target_profile"`
	TargetPath    string                   `json:"target_path"`
	Conflict      storage.ConflictStrategy `json:"conflict"`
	Mode          storage.SyncMode         `json:"mode"`
	DryRun        bool                     `json:"dry_run"`
	Filter        *storage.SyncFilter      `json:"filter"`
}

// SyncFiles 同步目录
func (h *FileManagerHandler) SyncFiles(c *gin.Context) {
	var req syncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	mgr := storage.GetManager()
	if mgr == nil {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "storage not initialized"))
		return
	}

	src := mgr.GetByCode(req.SourceProfile)
	if src == nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "源存储实例不存在或已禁用"))
		return
	}

	if req.TargetProfile == "" {
		req.TargetProfile = req.SourceProfile
	}
	dst := mgr.GetByCode(req.TargetProfile)
	if dst == nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "目标存储实例不存在或已禁用"))
		return
	}

	if req.TargetPath == "" {
		req.TargetPath = req.SourcePath
	}

	ctx := c.Request.Context()
	ts := storage.NewTransferService()
	defer ts.Close()

	opts := storage.SyncOptions{
		Conflict: req.Conflict,
		Mode:     req.Mode,
		DryRun:   req.DryRun,
		Filter:   req.Filter,
	}

	if req.DryRun {
		preview, err := ts.ComputeSyncPreview(ctx, src, req.SourcePath, dst, req.TargetPath, opts)
		if err != nil {
			h.logger.Error("sync preview failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		c.JSON(http.StatusOK, model.SuccessResponse(preview))
		return
	}

	result, err := ts.SyncDir(ctx, src, req.SourcePath, dst, req.TargetPath, opts)
	if err != nil {
		h.logger.Error("sync failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
		"files", "sync", req.SourcePath, "success", c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{Extra: map[string]interface{}{"source_profile": req.SourceProfile, "target_profile": req.TargetProfile, "target_path": req.TargetPath, "mode": string(req.Mode), "conflict": string(req.Conflict), "transferred": result.Transferred, "failed": result.Failed}})
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}
