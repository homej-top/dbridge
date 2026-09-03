package main

import (
	"context"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/handler"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	cachePkg "github.com/dbridge/dbridge/pkg/cache"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"github.com/dbridge/dbridge/pkg/logger"
	storagePkg "github.com/dbridge/dbridge/pkg/storage"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"go.uber.org/zap"
)

func main() {
	_ = godotenv.Load()

	// Load config
	cfg, err := config.LoadConfig("configs/config.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Init logger
	zapLogger, err := logger.Init(cfg.Log)
	if err != nil {
		log.Fatalf("Failed to init logger: %v", err)
	}
	defer zapLogger.Sync()

	sugar := zapLogger.Sugar()
	sugar.Infof("Starting DBridge server on port %d", cfg.Server.Port)

	// Validate config (security warnings/fatal)
	for _, w := range cfg.Validate() {
		sugar.Warnf("【配置警告】%s", w)
	}

	// Init crypto (32-byte key for AES-256-GCM)
	// Production: must set DBRIDGE_CRYPTO_KEY env var (hex-encoded 32 bytes)
	// Development: falls back to a default key with a warning
	var cryptoKey []byte
	if envKey := os.Getenv("DBRIDGE_CRYPTO_KEY"); envKey != "" {
		// Try hex decode first (recommended: 64 hex chars = 32 bytes)
		if decoded, err := hex.DecodeString(envKey); err == nil && len(decoded) == 32 {
			cryptoKey = decoded
		} else if len(envKey) == 32 {
			// Backward compat: treat as raw string bytes
			cryptoKey = []byte(envKey)
		} else {
			sugar.Fatalf("【安全错误】DBRIDGE_CRYPTO_KEY 无效: 需要 64 位十六进制字符或 32 位字符串")
		}
	} else {
		if cfg.Server.Env == "production" {
			sugar.Fatalf("【安全错误】生产环境必须设置 DBRIDGE_CRYPTO_KEY 环境变量 (openssl rand -hex 32)")
		}
		sugar.Warn("【安全警告】DBRIDGE_CRYPTO_KEY 未设置，使用不安全的默认密钥（仅限开发环境）")
		cryptoKey = []byte("0123456789abcdef0123456789abcdef")
	}
	// Legacy keys for backward compatibility with rust_react branch encrypted data
	legacyKeys := [][]byte{
		[]byte("0123456789abcdef0123456789abcdef"),
		[]byte("aaaaaaaaaaaabbbbbbbbbbbbcccccccc"),
	}
	if err := cryptoPkg.InitKeyWithLegacy(cryptoKey, legacyKeys); err != nil {
		sugar.Fatalf("加密模块初始化失败: %v", err)
	}
	sugar.Info("加密模块初始化成功")

	// Init database
	dbInitErr := repository.Init(cfg.Database)
	if dbInitErr != nil {
		sugar.Warnf("Failed to init database: %v", dbInitErr)
	} else {
		sugar.Info("Database connected")
		if err := repository.AutoMigrate(); err != nil {
			sugar.Warnf("Failed to auto migrate: %v", err)
		} else {
			sugar.Info("Database migrated")
			if err := repository.SeedDefaultAdmin(); err != nil {
				sugar.Warnf("Failed to seed default admin: %v", err)
			}
			// 确保系统数据源存在（幂等）
			dsSvc := service.NewDataSourceService(repository.GetDB())
			if err := dsSvc.EnsureSystemDataSource(cfg.Database); err != nil {
				sugar.Warnf("ensure system data source failed: %v", err)
			}
		}
	}
	defer repository.Close()

	// Init unified cache (backend: redis | local)
	if err := cachePkg.Init(cfg.Cache); err != nil {
		sugar.Fatalf("cache init failed: %v", err)
	}
	defer cachePkg.Close()

	// Init Storage from database
	if err := storagePkg.InitFromDB(repository.GetDB()); err != nil {
		sugar.Fatalf("Failed to init storage: %v", err)
	}
	sugar.Info("Storage initialized from database")

	// Set Gin mode
	if cfg.Server.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	// Create Gin router
	r := gin.Default()

	// CORS middleware (configurable)
	setupCORS(r, cfg, sugar)

	// Register health check routes
	healthHandler := handler.NewHealthHandler()
	r.GET("/health/live", healthHandler.Liveness)
	r.GET("/health/ready", healthHandler.Readiness)

	// Register API v1 routes (safe even if DB init failed)
	v1 := r.Group("/api/v1")
	if dbInitErr == nil {
		// Initialize connection pool manager
		initPoolManager(cfg, zapLogger)

		handler.RegisterRoutes(v1, cfg, zapLogger)
	} else {
		sugar.Warn("Database not initialized, API routes will return 503")
		// Register minimal routes for health check only
		v1.GET("/info", func(c *gin.Context) {
			c.JSON(503, gin.H{"error": "service unavailable", "reason": "database not initialized"})
		})
	}

	// Serve frontend static files (built React SPA)
	r.Static("/assets", "./web/dist/assets")
	r.StaticFile("/favicon.svg", "./web/dist/favicon.svg")
	r.StaticFile("/icons.svg", "./web/dist/icons.svg")
	r.NoRoute(func(c *gin.Context) {
		c.File("./web/dist/index.html")
	})

	// Create HTTP server
	srv := &http.Server{
		Addr:         ":" + strconv.Itoa(cfg.Server.Port),
		Handler:      r,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// Graceful shutdown
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			sugar.Fatalf("Server failed to start: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	sugar.Info("Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		sugar.Fatalf("Server forced to shutdown: %v", err)
	}

	// Shutdown storage profiles
	if mgr := storagePkg.GetManager(); mgr != nil {
		if err := mgr.Shutdown(); err != nil {
			sugar.Warnf("Storage shutdown: %v", err)
		}
		sugar.Info("Storage profiles closed")
	}

	// Shutdown connection pool manager
	poolCtx, poolCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer poolCancel()
	if err := service.ShutdownPoolManager(poolCtx); err != nil {
		sugar.Warnf("Pool manager shutdown: %v", err)
	}

	sugar.Info("Server exited")
}

// setupCORS configures CORS middleware from config file.
// In development, localhost:5170 is always allowed.
func setupCORS(r *gin.Engine, cfg *config.Config, sugar *zap.SugaredLogger) {
	if !cfg.CORS.Enabled {
		sugar.Info("CORS disabled")
		return
	}

	allowedOrigins := make(map[string]bool)
	for _, origin := range cfg.CORS.AllowedOrigins {
		allowedOrigins[origin] = true
	}
	// Development: always allow local Vite dev server
	if cfg.Server.Env == "development" {
		allowedOrigins["http://localhost:5170"] = true
		allowedOrigins["http://127.0.0.1:5170"] = true
	}

	methods := strings.Join(cfg.CORS.AllowedMethods, ", ")
	if methods == "" {
		methods = "GET, POST, PUT, DELETE, OPTIONS"
	}
	headers := strings.Join(cfg.CORS.AllowedHeaders, ", ")
	if headers == "" {
		headers = "Content-Type, Authorization"
	}

	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if origin != "" && allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Methods", methods)
			c.Header("Access-Control-Allow-Headers", headers)
			for _, h := range cfg.CORS.ExposedHeaders {
				c.Header("Access-Control-Expose-Headers", h)
			}
			if cfg.CORS.AllowCredentials {
				c.Header("Access-Control-Allow-Credentials", "true")
			}
			if cfg.CORS.MaxAge > 0 {
				c.Header("Access-Control-Max-Age", strconv.Itoa(cfg.CORS.MaxAge))
			}
		}
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})
	sugar.Infof("CORS enabled, allowed origins: %v", cfg.CORS.AllowedOrigins)
}

// initPoolManager initializes the global connection pool manager.
func initPoolManager(cfg *config.Config, logger *zap.Logger) {
	poolCfg := service.PoolConfig{
		MaxOpenConns:    20,
		MaxIdleConns:    5,
		ConnMaxLifetime: 300 * time.Second,
		ConnIdleTime:    60 * time.Second,
	}
	mgrCfg := service.ManagerConfig{
		PoolIdleTimeout: 300 * time.Second,
		CleanupInterval: 120 * time.Second,
	}
	perDB := map[string]service.PoolConfig{
		"oracle":    {MaxOpenConns: 15, MaxIdleConns: 8, ConnMaxLifetime: 600 * time.Second, ConnIdleTime: 60 * time.Second},
		"sqlserver": {MaxOpenConns: 15, MaxIdleConns: 5, ConnMaxLifetime: 300 * time.Second, ConnIdleTime: 60 * time.Second},
	}
	service.InitPoolManager(poolCfg, mgrCfg, perDB, logger)
}
