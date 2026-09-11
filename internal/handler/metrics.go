package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/homej-top/dbridge/internal/service"
)

type MetricsHandler struct {
	logger interface{}
}

func NewMetricsHandler() *MetricsHandler {
	return &MetricsHandler{}
}

func (h *MetricsHandler) PoolMetrics(c *gin.Context) {
	mgr := service.PoolManager()
	if mgr == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "pool manager not initialized"})
		return
	}
	c.JSON(http.StatusOK, mgr.Stats())
}
