package handler

import (
	"net/http"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type TableHandler struct {
	svc    *service.TableManagerService
	logger *zap.Logger
}

func NewTableHandler(cfg *config.Config, logger *zap.Logger) *TableHandler {
	return &TableHandler{
		svc:    service.NewTableManagerService(repository.GetDB()),
		logger: logger,
	}
}

type structureRequest struct {
	DataSourceID string `json:"data_source_id" binding:"required"`
	Schema       string `json:"schema"`
	Table        string `json:"table" binding:"required"`
	Database     string `json:"database"` // optional, for PG/MSSQL database context
}

func (h *TableHandler) Structure(c *gin.Context) {
	var req structureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.GetFullStructure(req.DataSourceID, req.Schema, req.Table, req.Database)
	if err != nil {
		h.logger.Error("get table structure failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *TableHandler) PreviewAlter(c *gin.Context) {
	var req service.AlterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	req.DryRun = true
	result, err := h.svc.PreviewAlter(req)
	if err != nil {
		h.logger.Error("preview alter failed", zap.Error(err))
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

func (h *TableHandler) Alter(c *gin.Context) {
	var req service.AlterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.ExecuteAlter(req,
		c.GetString("username"),
		c.GetString("user_id"),
		c.GetString("tenant_id"),
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	if err != nil {
		h.logger.Error("execute alter failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

type viewDefRequest struct {
	DataSourceID string `json:"data_source_id" binding:"required"`
	Schema       string `json:"schema"`
	View         string `json:"view" binding:"required"`
	Database     string `json:"database"`
}

func (h *TableHandler) ViewDefinition(c *gin.Context) {
	var req viewDefRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	def, err := h.svc.GetViewDefinition(req.DataSourceID, req.Schema, req.View, req.Database)
	if err != nil {
		h.logger.Error("get view definition failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(map[string]string{"definition": def}))
}
