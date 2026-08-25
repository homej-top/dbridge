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

// ViewHandler provides dedicated endpoints for view operations,
// separating them from the table endpoints for cleaner API design.
type ViewHandler struct {
	svc    *service.TableManagerService
	logger *zap.Logger
}

func NewViewHandler(cfg *config.Config, logger *zap.Logger) *ViewHandler {
	return &ViewHandler{
		svc:    service.NewTableManagerService(repository.GetDB()),
		logger: logger,
	}
}

// ─── Request types ─────────────────────────────────────────────────────────

type viewStructureRequest struct {
	DataSourceID string `json:"data_source_id" binding:"required"`
	Schema       string `json:"schema"`
	View         string `json:"view" binding:"required"`
	Database     string `json:"database"` // optional, for PG/MSSQL
}

type viewDDLRequest struct {
	DataSourceID string `json:"data_source_id" binding:"required"`
	Schema       string `json:"schema"`
	View         string `json:"view"`
	SQL          string `json:"sql" binding:"required"`
	Database     string `json:"database"` // optional, for PG/MSSQL
}

// ─── Handlers ──────────────────────────────────────────────────────────────

// Structure returns the column definitions and DDL for a view.
// Similar to the table structure endpoint but optimized for views
// (no indexes/constraints, columns come from the view's SELECT statement).
func (h *ViewHandler) Structure(c *gin.Context) {
	var req viewStructureRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.GetFullStructure(req.DataSourceID, req.Schema, req.View, req.Database)
	if err != nil {
		h.logger.Error("get view structure failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// Definition returns the CREATE VIEW DDL for the given view name.
func (h *ViewHandler) Definition(c *gin.Context) {
	var req viewStructureRequest
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

// ExecuteDDL runs a view DDL statement (CREATE VIEW, ALTER VIEW, DROP VIEW).
// This is the dedicated view DDL executor, separate from the generic query/ddl-exec endpoint.
func (h *ViewHandler) ExecuteDDL(c *gin.Context) {
	var req viewDDLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	result, err := h.svc.ExecuteViewDDL(req.DataSourceID, req.Schema, req.View, req.SQL, req.Database)
	if err != nil {
		h.logger.Error("execute view DDL failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}
