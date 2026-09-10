package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/dbridge/dbridge/internal/service/agentsdkwrap"
	cachePkg "github.com/dbridge/dbridge/pkg/cache"
	"github.com/dbridge/dbridge/pkg/storage"
	"github.com/gin-gonic/gin"
	"github.com/opentoys/agentsdk/skill"
	"github.com/opentoys/agentsdk/vfs"
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
	st, _, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	dir := c.DefaultQuery("dir", "")
	result, err := st.List(c.Request.Context(), dir, &storage.ListOptions{
		Page:     1,
		PageSize: 500,
	})
	if err != nil {
		h.logger.Error("script fs list failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// FsRead reads a file's text content from script storage
func (h *ScriptHandler) FsRead(c *gin.Context) {
	st, _, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	data, err := st.Read(c.Request.Context(), filePath)
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
	st, _, ok := h.resolveScriptStorage()
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
	reader := strings.NewReader(req.Content)
	_, err := st.Save(c.Request.Context(), req.Path, reader, "text/plain")
	if err != nil {
		h.logger.Error("script fs save failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// FsMkdir creates a directory in script storage
func (h *ScriptHandler) FsMkdir(c *gin.Context) {
	st, _, ok := h.resolveScriptStorage()
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
	if err := st.Mkdir(c.Request.Context(), req.Path); err != nil {
		h.logger.Error("script fs mkdir failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(nil))
}

// FsDelete deletes a file or directory in script storage
func (h *ScriptHandler) FsDelete(c *gin.Context) {
	st, _, ok := h.resolveScriptStorage()
	if !ok {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "没有可用的存储实例"))
		return
	}
	filePath := c.Query("path")
	if filePath == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "path required"))
		return
	}
	isDir := c.Query("is_dir") == "true"
	if isDir {
		if err := st.RemoveDir(c.Request.Context(), filePath); err != nil {
			h.logger.Error("script fs remove dir failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
	} else {
		if err := st.Delete(c.Request.Context(), filePath); err != nil {
			h.logger.Error("script fs delete failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// FsRename renames/moves a file or directory in script storage
func (h *ScriptHandler) FsRename(c *gin.Context) {
	st, _, ok := h.resolveScriptStorage()
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
	if err := st.Rename(c.Request.Context(), req.OldPath, req.NewPath); err != nil {
		h.logger.Error("script fs rename failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── AI Skill Handler ──────────────────────────────────────────────────────

type AISkillHandler struct {
	svc    *service.AISkillService
	logger *zap.Logger
}

func NewAISkillHandler(logger *zap.Logger) *AISkillHandler {
	return &AISkillHandler{
		svc:    service.NewAISkillService(repository.GetDB()),
		logger: logger,
	}
}

func (h *AISkillHandler) List(c *gin.Context) {
	tenantID := c.GetString("tenant_id")
	category := c.Query("category")

	skills, err := h.svc.List(tenantID, category)
	if err != nil {
		h.logger.Error("list ai skills failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": skills, "total": len(skills)}))
}

func (h *AISkillHandler) Get(c *gin.Context) {
	id := c.Param("id")
	skill, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(skill))
}

func (h *AISkillHandler) Create(c *gin.Context) {
	var input service.CreateAISkillInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	userID := c.GetString("user_id")
	tenantID := c.GetString("tenant_id")

	skill, err := h.svc.Create(input, userID, tenantID)
	if err != nil {
		h.logger.Error("create ai skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(skill))
}

func (h *AISkillHandler) Update(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	if !h.ensureWritePerm(c, existing) {
		return
	}
	var input service.UpdateAISkillInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	skill, err := h.svc.Update(id, input)
	if err != nil {
		h.logger.Error("update ai skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(skill))
}

func (h *AISkillHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	if !h.ensureWritePerm(c, existing) {
		return
	}
	refCount, err := h.svc.CountAgentRefs(id)
	if err != nil {
		h.logger.Error("count agent refs failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	if refCount > 0 {
		c.JSON(http.StatusConflict, model.ErrorResponse(model.CodeParamError, fmt.Sprintf("该技能被 %d 个 Agent 引用，无法删除", refCount)))
		return
	}
	if err := h.svc.Delete(id); err != nil {
		h.logger.Error("delete ai skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *AISkillHandler) ToggleActive(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	// Built-in skills can be enabled/disabled by any authenticated user;
	// custom skills still require author/admin ownership.
	if !existing.IsBuiltin && !h.ensureWritePerm(c, existing) {
		return
	}
	skill, err := h.svc.ToggleActive(id)
	if err != nil {
		h.logger.Error("toggle ai skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(skill))
}

// ensureWritePerm enforces built-in read-only and author/admin ownership.
func (h *AISkillHandler) ensureWritePerm(c *gin.Context, skill *service.AISkill) bool {
	if skill.IsBuiltin {
		c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "内置技能不可修改"))
		return false
	}
	role := c.GetString("role")
	userID := c.GetString("user_id")
	if role == "admin" || skill.AuthorID == userID {
		return true
	}
	c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "无权限修改该技能"))
	return false
}

// Preview returns the rendered SKILL.md for a skill.
func (h *AISkillHandler) Preview(c *gin.Context) {
	id := c.Param("id")
	skill, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	md, err := agentsdkwrap.BuildSkillMarkdown(skill.ToRepository())
	if err != nil {
		h.logger.Error("render skill markdown failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"markdown": md}))
}

// Test runs a skill in isolation with read-only tools and returns its trace.
func (h *AISkillHandler) Test(c *gin.Context) {
	id := c.Param("id")
	skill, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	var req struct {
		Prompt       string            `json:"prompt"`
		Variables    map[string]string `json:"variables"`
		DataSourceID string            `json:"data_source_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	prompt := applyTemplateVars(skill.PromptTemplate, req.Variables)
	if strings.TrimSpace(req.Prompt) != "" {
		prompt = req.Prompt
	}

	agent := &repository.Agent{
		ID:           "agent_skill_test",
		Name:         "Skill Test",
		Model:        "deepseek-chat",
		SystemPrompt: skill.SystemPrompt,
		Tools:        `["list_tables","execute_query","get_table_structure","list_datasources"]`,
		Skills:       fmt.Sprintf(`["%s"]`, skill.ID),
		TenantID:     skill.TenantID,
	}

	var mu sync.Mutex
	trace := make([]map[string]string, 0)
	onToolCall := func(name, args string) {
		mu.Lock()
		defer mu.Unlock()
		trace = append(trace, map[string]string{"name": name, "args": args})
	}

	start := time.Now()
	runner, err := agentsdkwrap.NewRunner(c.Request.Context(), agent,
		service.NewDataSourceService(repository.GetDB()),
		service.NewQueryService(repository.GetDB()),
		service.NewTableManagerService(repository.GetDB()),
		service.NewCompareService(repository.GetDB()),
		nil,
		onToolCall)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	result, runErr := runner.Run(c.Request.Context(), prompt)
	duration := time.Since(start).Milliseconds()

	resp := gin.H{
		"skill":       gin.H{"id": skill.ID, "slug": skill.Slug, "name": skill.Name, "version": skill.Version},
		"selected":    true,
		"tool_trace":  trace,
		"result":      result,
		"duration_ms": duration,
		"report":      buildSkillReport(skill, trace, result, duration, runErr),
	}
	if runErr != nil {
		resp["error"] = runErr.Error()
	}
	c.JSON(http.StatusOK, model.SuccessResponse(resp))
}

// Versions lists the version history of a skill.
func (h *AISkillHandler) Versions(c *gin.Context) {
	id := c.Param("id")
	if _, err := h.svc.Get(id); err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	versions, err := h.svc.GetVersions(id)
	if err != nil {
		h.logger.Error("list skill versions failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"list": versions, "total": len(versions)}))
}

// Rollback restores a skill to a historical version.
func (h *AISkillHandler) Rollback(c *gin.Context) {
	id := c.Param("id")
	existing, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	if !h.ensureWritePerm(c, existing) {
		return
	}
	var req struct {
		Version int `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	if req.Version <= 0 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "version 必须大于 0"))
		return
	}
	skill, err := h.svc.Rollback(id, req.Version)
	if err != nil {
		h.logger.Error("rollback skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(skill))
}

// Export downloads a skill as a ZIP package (SKILL.md + scripts/resources).
func (h *AISkillHandler) Export(c *gin.Context) {
	id := c.Param("id")
	skill, err := h.svc.Get(id)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "skill not found"))
		return
	}
	md, err := agentsdkwrap.BuildSkillMarkdown(skill.ToRepository())
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	dir := skill.Slug
	if dir == "" {
		dir = skill.ID
	}
	files := map[string][]byte{dir + "/SKILL.md": []byte(md)}
	addFiles := func(raw string) error {
		if strings.TrimSpace(raw) == "" || raw == "[]" {
			return nil
		}
		var entries []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return err
		}
		for _, e := range entries {
			p := path.Clean(strings.TrimSpace(e.Path))
			if p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) {
				continue
			}
			files[dir+"/"+p] = []byte(e.Content)
		}
		return nil
	}
	if err := addFiles(skill.ScriptFiles); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	if err := addFiles(skill.Resources); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	var buf bytes.Buffer
	if err := vfs.CreateZip(&buf, files); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, dir))
	c.Data(http.StatusOK, "application/zip", buf.Bytes())
}

// Import creates a skill from an uploaded ZIP package.
func (h *AISkillHandler) Import(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "缺少上传文件 file"))
		return
	}
	fh, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer fh.Close()
	data, err := io.ReadAll(fh)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	rawFiles, err := vfs.ParseZip(data)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "无法解析 ZIP"))
		return
	}
	fileMap := make(map[string][]byte, len(rawFiles))
	mem := vfs.NewMem()
	for p, content := range rawFiles {
		key := filepath.ToSlash(p)
		fileMap[key] = content
		_ = mem.WriteFile(key, content)
	}
	pkgs, err := skill.ParseSkillPackages(mem)
	if err != nil || len(pkgs) == 0 {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "ZIP 中未找到有效技能包（缺少 SKILL.md）"))
		return
	}
	pkg := pkgs[0]
	prompt, system := splitSkillBody(pkg.Body)
	input := service.CreateAISkillInput{
		Name:           pkg.Meta.Name,
		Description:    pkg.Meta.Description,
		PromptTemplate: prompt,
		SystemPrompt:   system,
		Tools:          marshalStringList(pkg.Meta.AllowedTools),
	}
	input.ScriptFiles, input.Resources = collectImportResources(pkg, fileMap)
	if strings.TrimSpace(input.Name) == "" {
		input.Name = "导入技能"
	}

	skill, err := h.svc.Create(input, c.GetString("user_id"), c.GetString("tenant_id"))
	if err != nil {
		h.logger.Error("import ai skill failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusCreated, model.SuccessResponse(skill))
}

func collectImportResources(pkg *skill.SkillPackage, fileMap map[string][]byte) (scripts, resources string) {
	type entry struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	var s, r []entry
	for _, rel := range pkg.Resources.Scripts {
		full := path.Join(pkg.Path, rel)
		if c, ok := fileMap[full]; ok {
			s = append(s, entry{Path: rel, Content: string(c)})
		}
	}
	others := append(append(append([]string{}, pkg.Resources.References...), pkg.Resources.Assets...), pkg.Resources.Templates...)
	for _, rel := range others {
		full := path.Join(pkg.Path, rel)
		if c, ok := fileMap[full]; ok {
			r = append(r, entry{Path: rel, Content: string(c)})
		}
	}
	scriptsJSON, _ := json.Marshal(s)
	resourcesJSON, _ := json.Marshal(r)
	return string(scriptsJSON), string(resourcesJSON)
}

func splitSkillBody(body string) (prompt, system string) {
	marker := "\n\n## System Instructions\n"
	if idx := strings.Index(body, marker); idx >= 0 {
		return body[:idx], strings.TrimSpace(body[idx+len(marker):])
	}
	return body, ""
}

func marshalStringList(list []string) string {
	if len(list) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(list)
	return string(b)
}

func buildSkillReport(skill *service.AISkill, trace []map[string]string, result string, duration int64, runErr error) string {
	var b strings.Builder
	b.WriteString("## 技能试运行报告\n\n")
	b.WriteString(fmt.Sprintf("- 技能：%s（%s）v%d\n", skill.Name, skill.Slug, skill.Version))
	b.WriteString(fmt.Sprintf("- 耗时：%dms\n", duration))
	if runErr != nil {
		b.WriteString(fmt.Sprintf("- 状态：失败（%s）\n", runErr.Error()))
	} else {
		b.WriteString("- 状态：成功\n")
	}
	b.WriteString("\n### 工具调用轨迹\n\n")
	if len(trace) == 0 {
		b.WriteString("- （无工具调用）\n")
	} else {
		for i, t := range trace {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, t["name"]))
		}
	}
	b.WriteString("\n### 输出\n\n")
	if strings.TrimSpace(result) == "" {
		b.WriteString("（无输出）\n")
	} else {
		b.WriteString(result)
	}
	return b.String()
}

func applyTemplateVars(tpl string, vars map[string]string) string {
	for k, v := range vars {
		tpl = strings.ReplaceAll(tpl, "{{"+k+"}}", v)
	}
	return tpl
}

// ─── Report Handler ────────────────────────────────────────────────────────

type ReportHandler struct {
	svc      *service.ReportService
	querySvc *service.QueryService
	aiSvc    *service.AIService
	logger   *zap.Logger
}

func NewReportHandler(logger *zap.Logger, cfg *config.Config) *ReportHandler {
	return &ReportHandler{
		svc:      service.NewReportService(repository.GetDB()),
		querySvc: service.NewQueryService(repository.GetDB()),
		aiSvc:    service.NewAIService(repository.GetDB(), cfg.AI),
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

func (h *ReportHandler) GenerateSQL(c *gin.Context) {
	var input service.GenerateSQLInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	prompt := fmt.Sprintf("Based on the following natural language description, generate a valid SQL SELECT query. Return ONLY the SQL query without any explanation or markdown formatting.\n\nDescription: %s", input.Description)
	resp, err := h.aiSvc.Chat(c.Request.Context(), service.ChatRequest{
		DataSourceID: input.DataSourceID,
		Schema:       input.Schema,
		Messages: []service.Message{
			{Role: "user", Content: prompt},
		},
	})
	if err != nil {
		h.logger.Error("generate SQL failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	sql := strings.TrimSpace(resp.Content)
	// Strip markdown code blocks if present
	sql = strings.TrimPrefix(sql, "```sql")
	sql = strings.TrimPrefix(sql, "```")
	sql = strings.TrimSuffix(sql, "```")
	sql = strings.TrimSpace(sql)
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"sql": sql}))
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
