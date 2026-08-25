package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{}
}

func (h *HealthHandler) Liveness(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"status": "alive",
	})
}

func (h *HealthHandler) Readiness(c *gin.Context) {
	// TODO: Check DB, Redis, NATS connectivity
	c.JSON(http.StatusOK, gin.H{
		"status": "ready",
	})
}
