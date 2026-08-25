package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestSyncStructure_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	tests := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"missing table", `{"source_ds":"a","target_ds":"b","action":"create"}`},
		{"missing action", `{"source_ds":"a","target_ds":"b","table":"users"}`},
		{"missing source_ds", `{"target_ds":"b","table":"users","action":"create"}`},
		{"missing target_ds", `{"source_ds":"a","table":"users","action":"create"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, r := gin.CreateTestContext(w)
			r.POST("/compare/sync-structure", h.SyncStructure)

			req := httptest.NewRequest("POST", "/compare/sync-structure", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), `"code":1000`)
		})
	}
}

func TestSyncStructure_ValidParams_NoConnection(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/compare/sync-structure", h.SyncStructure)

	body := `{"source_ds":"nonexistent","target_ds":"nonexistent","table":"users","action":"create"}`
	req := httptest.NewRequest("POST", "/compare/sync-structure", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Should fail because data source doesn't exist in DB
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":1004`)
}

func TestSyncData_MissingParams(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	tests := []struct {
		name string
		body string
	}{
		{"empty body", `{}`},
		{"missing table", `{"source_ds":"a","target_ds":"b"}`},
		{"missing source_ds", `{"target_ds":"b","table":"users"}`},
		{"missing target_ds", `{"source_ds":"a","table":"users"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			_, r := gin.CreateTestContext(w)
			r.POST("/compare/sync-data", h.SyncData)

			req := httptest.NewRequest("POST", "/compare/sync-data", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
			assert.Contains(t, w.Body.String(), `"code":1000`)
		})
	}
}

func TestSyncData_ValidParams_NoConnection(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/compare/sync-data", h.SyncData)

	body := `{
		"source_ds":"nonexistent",
		"target_ds":"nonexistent",
		"table":"users",
		"options":{"mode":"full"}
	}`
	req := httptest.NewRequest("POST", "/compare/sync-data", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), `"code":1004`)
}

func TestSyncData_WithAllOptions(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/compare/sync-data", h.SyncData)

	body := `{
		"source_ds":"nonexistent",
		"target_ds":"nonexistent",
		"table":"users",
		"options":{
			"truncate_target":true,
			"sync_id":false,
			"mode":"diff",
			"check_fields":["id","email"],
			"sync_columns":["name","email"],
			"selected_rows":[]
		}
	}`
	req := httptest.NewRequest("POST", "/compare/sync-data", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Fails at connection (expected), but validates JSON binding succeeded
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestSyncStructure_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/compare/sync-structure", h.SyncStructure)

	req := httptest.NewRequest("POST", "/compare/sync-structure", strings.NewReader(`{invalid json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestSyncData_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	h := NewCompareHandler(cfg, logger)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)
	r.POST("/compare/sync-data", h.SyncData)

	req := httptest.NewRequest("POST", "/compare/sync-data", strings.NewReader(`not json`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
