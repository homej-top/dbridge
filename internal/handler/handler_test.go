package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dbridge/dbridge/internal/config"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("DB setup failed: %v", err)
	}
	repository.DB = db
	if err := repository.AutoMigrate(); err != nil {
		t.Fatalf("AutoMigrate failed: %v", err)
	}
}

func TestHealthHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := NewHealthHandler()
	r.GET("/health/live", h.Liveness)
	r.GET("/health/ready", h.Readiness)

	req := httptest.NewRequest("GET", "/health/live", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/health/ready", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthLoginMissingParams(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:    "0123456789abcdef0123456789abcdef",
			ExpiresIn: 3600,
		},
	}
	logger, _ := zap.NewDevelopment()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := NewAuthHandler(cfg, logger)
	r.POST("/auth/login", h.Login)

	// Empty JSON body → empty strings → service validation rejects
	body := strings.NewReader(`{}`)
	req := httptest.NewRequest("POST", "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Service returns "username and password are required" → 400
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestAuthLoginInvalidCredentials(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{
		JWT: config.JWTConfig{
			Secret:    "0123456789abcdef0123456789abcdef",
			ExpiresIn: 3600,
		},
	}
	logger, _ := zap.NewDevelopment()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := NewAuthHandler(cfg, logger)
	r.POST("/auth/login", h.Login)

	body := strings.NewReader(`{"username":"admin","password":"wrongpass"}`)
	req := httptest.NewRequest("POST", "/auth/login", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Password doesn't match bcrypt hash in DB → 401
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDataSourceList(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := NewDataSourceHandler(cfg, logger)
	r.GET("/data-sources", h.List)

	req := httptest.NewRequest("GET", "/data-sources", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"code":0`)
}

func TestDataSourceCreate(t *testing.T) {
	setupTestDB(t)
	gin.SetMode(gin.TestMode)

	// Init crypto key for password encryption
	if err := cryptoPkg.InitKey([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{}
	logger, _ := zap.NewDevelopment()

	w := httptest.NewRecorder()
	_, r := gin.CreateTestContext(w)

	h := NewDataSourceHandler(cfg, logger)
	r.POST("/data-sources", h.Create)

	body := strings.NewReader(`{
		"name": "test_db",
		"type": "mysql",
		"host": "localhost",
		"port": 3306,
		"database": "test",
		"username": "root",
		"password": "secret"
	}`)
	req := httptest.NewRequest("POST", "/data-sources", body)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Contains(t, w.Body.String(), `"code":0`)
}
