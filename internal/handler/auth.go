package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type AuthHandler struct {
	svc    *service.AuthService
	logger *zap.Logger
}

func NewAuthHandler(cfg *config.Config, logger *zap.Logger) *AuthHandler {
	return &AuthHandler{
		svc:    service.NewAuthService(cfg),
		logger: logger,
	}
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req service.LoginInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid parameters"))
		return
	}

	output, err := h.svc.Login(req)
	if err != nil {
		h.logger.Error("login failed", zap.Error(err))
		// Validation errors return 400, auth failures return 401
		if err.Error() == "username and password are required" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		} else {
			c.JSON(http.StatusUnauthorized, model.ErrorResponse(model.CodeAuthFailed, err.Error()))
		}
		return
	}

	// Audit login
	service.QuickAudit(repository.GetDB(), output.User.ID, output.User.TenantID,
		service.ModuleSystem, "user_login", output.User.ID,
		service.ResultSuccess, c.ClientIP(), c.Request.UserAgent(), output.User.Username, nil)

	c.JSON(http.StatusOK, model.SuccessResponse(output))
}

func (h *AuthHandler) ChangePassword(c *gin.Context) {
	var req service.ChangePasswordInput
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "请输入旧密码和新密码（至少6位）"))
		return
	}

	userID, _ := c.Get("user_id")
	if userID == nil {
		c.JSON(http.StatusUnauthorized, model.ErrorResponse(model.CodeAuthFailed, "未登录"))
		return
	}

	if err := h.svc.ChangePassword(userID.(string), req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

type DataSourceHandler struct {
	svc    *service.DataSourceService
	logger *zap.Logger
}

func NewDataSourceHandler(cfg *config.Config, logger *zap.Logger) *DataSourceHandler {
	return &DataSourceHandler{
		svc:    service.NewDataSourceService(repository.GetDB()),
		logger: logger,
	}
}

func (h *DataSourceHandler) List(c *gin.Context) {
	if h.svc == nil {
		c.JSON(http.StatusServiceUnavailable, model.ErrorResponse(model.CodeServiceUnavailable, "database not available"))
		return
	}
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}

	tag := c.Query("tag")
	dataSources, err := h.svc.ListByTag(tenantID.(string), tag)
	if err != nil {
		h.logger.Error("list data sources failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"total": len(dataSources),
		"list":  dataSources,
	}))
}

func (h *DataSourceHandler) Get(c *gin.Context) {
	id := c.Param("id")
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}

	ds, err := h.svc.Get(id, tenantID.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "data source not found"))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(ds))
}

func (h *DataSourceHandler) Create(c *gin.Context) {
	var input service.CreateDSInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	// SQLite skips host/port/username validation
	if input.Type != "sqlite" {
		if input.Host == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "host is required"))
			return
		}
		if input.Port == 0 {
			input.Port = 3306
		}
		if input.Username == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "username is required"))
			return
		}
	}

	tenantID, _ := c.Get("tenant_id")
	if tenantID != nil {
		input.TenantID = tenantID.(string)
	}

	userID, _ := c.Get("user_id")
	if userID != nil {
		input.CreatedBy = userID.(string)
	}

	ds, err := h.svc.Create(input)
	if err != nil {
		h.logger.Error("create data source failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// Audit
	service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
		service.ModuleDatasource, "ds_create", ds.ID,
		service.ResultSuccess, c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{Target: input.Name, DsType: input.Type})

	c.JSON(http.StatusCreated, model.SuccessResponse(ds))
}

func (h *DataSourceHandler) Update(c *gin.Context) {
	id := c.Param("id")
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}

	var input service.CreateDSInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	ds, err := h.svc.Update(id, tenantID.(string), input)
	if err != nil {
		if errors.Is(err, service.ErrSystemDataSourceTypeLocked) {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
			return
		}
		h.logger.Error("update data source failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// Audit
	service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
		service.ModuleDatasource, "ds_update", id,
		service.ResultSuccess, c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{Target: ds.Name, DsType: ds.Type})
	c.JSON(http.StatusOK, model.SuccessResponse(ds))
}

func (h *DataSourceHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}

	if err := h.svc.Delete(id, tenantID.(string)); err != nil {
		if errors.Is(err, service.ErrSystemDataSourceProtected) {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
			return
		}
		h.logger.Error("delete data source failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *DataSourceHandler) TestConnection(c *gin.Context) {
	var input struct {
		ID                string `json:"id"`
		Name              string `json:"name"`
		Type              string `json:"type" binding:"required"`
		Host              string `json:"host"`
		Port              int    `json:"port"`
		Database          string `json:"database"`
		Username          string `json:"username"`
		Password          string `json:"password"`
		SSLMode           string `json:"ssl_mode"`
		ExtraConfig       string `json:"extra_config"`
		OracleService     string `json:"oracle_service"`
		OracleConnectMode string `json:"oracle_connect_mode"`
		OracleRole        string `json:"oracle_role"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	// SQLite skips host/port/username validation
	if input.Type != "sqlite" {
		if input.Host == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "host is required"))
			return
		}
		if input.Port == 0 {
			input.Port = 3306
		}
		if input.Username == "" {
			c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "username is required"))
			return
		}
	}

	// If editing and password is empty, load stored password from DB
	password := input.Password
	if password == "" && input.ID != "" {
		stored, err := h.svc.GetDecryptedPassword(input.ID)
		if err == nil {
			password = stored
		}
	}
	if password == "" && input.Type != "sqlite" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "password is required"))
		return
	}

	result, err := h.svc.TestConnection(service.CreateDSInput{
		Name:              input.Name,
		Type:              input.Type,
		Host:              input.Host,
		Port:              input.Port,
		Database:          input.Database,
		Username:          input.Username,
		Password:          password,
		SSLMode:           input.SSLMode,
		ExtraConfig:       input.ExtraConfig,
		OracleService:     input.OracleService,
		OracleConnectMode: input.OracleConnectMode,
		OracleRole:        input.OracleRole,
	})
	if err != nil {
		c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
			"connected": false,
			"error":     err.Error(),
		}))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"connected": true,
		"message":   result,
	}))
}

func (h *DataSourceHandler) GetSchema(c *gin.Context) {
	id := c.Param("id")

	schemas, err := h.svc.GetSchema(id)
	if err != nil {
		h.logger.Error("get schema failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(schemas))
}

func (h *DataSourceHandler) ListSchemaNames(c *gin.Context) {
	id := c.Param("id")

	names, err := h.svc.ListSchemaNames(id)
	if err != nil {
		h.logger.Error("list schema names failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(names))
}

func (h *DataSourceHandler) GetSchemaObjects(c *gin.Context) {
	id := c.Param("id")
	schemaName := c.Param("schema")

	if schemaName == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "schema name is required"))
		return
	}

	objects, err := h.svc.GetSchemaObjects(id, schemaName)
	if err != nil {
		h.logger.Error("get schema objects failed", zap.String("id", id), zap.String("schema", schemaName), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(objects))
}

func (h *DataSourceHandler) Export(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}

	var req struct {
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "请输入导出密码"))
		return
	}

	items, err := h.svc.ExportDataSources(tenantID.(string), req.Password)
	if err != nil {
		h.logger.Error("export data sources failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	data, _ := json.MarshalIndent(items, "", "  ")
	c.JSON(http.StatusOK, model.SuccessResponse(string(data)))
}

func (h *DataSourceHandler) Import(c *gin.Context) {
	tenantID, _ := c.Get("tenant_id")
	if tenantID == nil {
		tenantID = "default"
	}
	userID, _ := c.Get("user_id")
	createdBy := ""
	if userID != nil {
		createdBy = userID.(string)
	}

	var req struct {
		Items    []service.ExportItem `json:"items" binding:"required"`
		Password string               `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "请提供导入数据和导出密码"))
		return
	}

	result, err := h.svc.ImportDataSources(tenantID.(string), createdBy, req.Items, req.Password)
	if err != nil {
		h.logger.Error("import data sources failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// SchemaDetailList returns schema summaries (name, table_count, view_count, charset, collation)
func (h *DataSourceHandler) SchemaDetailList(c *gin.Context) {
	id := c.Param("id")

	items, err := h.svc.SchemaDetailList(id)
	if err != nil {
		h.logger.Error("schema detail list failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(items))
}

// GetColumnTypes returns the column type definitions for the given data source
func (h *DataSourceHandler) GetColumnTypes(c *gin.Context) {
	id := c.Param("id")

	types, err := h.svc.GetColumnTypes(id)
	if err != nil {
		h.logger.Error("get column types failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(types))
}

// GetIndexTypes returns the index type definitions for the given data source
func (h *DataSourceHandler) GetIndexTypes(c *gin.Context) {
	id := c.Param("id")
	types, err := h.svc.GetIndexTypes(id)
	if err != nil {
		h.logger.Error("get index types failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(types))
}

// TableList returns tables/views for a given schema with metadata
func (h *DataSourceHandler) TableList(c *gin.Context) {
	id := c.Param("id")
	schemaName := c.Param("schema")
	database := c.Query("database") // optional, for PG/MSSQL database context

	if schemaName == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "schema name is required"))
		return
	}

	items, err := h.svc.TableList(id, schemaName, database)
	if err != nil {
		h.logger.Error("table list failed", zap.String("id", id), zap.String("schema", schemaName), zap.String("database", database), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(items))
}

// GetTreeMetadata returns the tree hierarchy metadata for a data source.
func (h *DataSourceHandler) GetTreeMetadata(c *gin.Context) {
	id := c.Param("id")

	meta, err := h.svc.GetTreeMetadata(id)
	if err != nil {
		h.logger.Error("tree metadata failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(meta))
}

// ListDatabases returns the database list for a data source (PG/MSSQL).
func (h *DataSourceHandler) ListDatabases(c *gin.Context) {
	id := c.Param("id")

	dbs, err := h.svc.ListDatabases(id)
	if err != nil {
		h.logger.Error("list databases failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	// Return both name list (for backward compat) and full metadata
	names := make([]string, len(dbs))
	for i, db := range dbs {
		names[i] = db.Name
	}
	c.JSON(http.StatusOK, model.SuccessResponse(names))
}

// ListDatabaseSchemas returns schema names within a specific database (PG/MSSQL).
func (h *DataSourceHandler) ListDatabaseSchemas(c *gin.Context) {
	id := c.Param("id")
	db := c.Param("db")

	schemas, err := h.svc.ListDatabaseSchemas(id, db)
	if err != nil {
		h.logger.Error("list database schemas failed", zap.String("id", id), zap.String("db", db), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(schemas))
}
