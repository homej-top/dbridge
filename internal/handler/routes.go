package handler

import (
	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/middleware"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func RegisterRoutes(r *gin.RouterGroup, cfg *config.Config, logger *zap.Logger) {
	chainMW := func(base []gin.HandlerFunc, extra ...gin.HandlerFunc) []gin.HandlerFunc {
		result := make([]gin.HandlerFunc, 0, len(base)+len(extra))
		result = append(result, base...)
		result = append(result, extra...)
		return result
	}
	_ = chainMW // reserved for future Community routes

	authMW := middleware.AuthMiddleware(&cfg.JWT)

	protectedMW := []gin.HandlerFunc{authMW}

	adminOpMW := append(append([]gin.HandlerFunc{}, protectedMW...), middleware.RequireRole("admin", "operator"))
	adminMW := append(append([]gin.HandlerFunc{}, protectedMW...), middleware.RequireRole("admin"))

	// Dashboard routes (protected)
	dashboardHandler := NewDashboardHandler(cfg, logger)
	dashboard := r.Group("/dashboard")
	dashboard.Use(protectedMW...)
	{
		dashboard.GET("/stats", dashboardHandler.Stats)
		dashboard.GET("/tabs", dashboardHandler.GetTabs)
		dashboard.PUT("/tabs", dashboardHandler.SaveTabs)
	}

	// Auth routes
	authHandler := NewAuthHandler(cfg, logger)
	auth := r.Group("/auth")
	{
		auth.POST("/login", authHandler.Login)
	}

	authProtected := r.Group("/auth")
	authProtected.Use(protectedMW...)
	{
		authProtected.POST("/change-password", authHandler.ChangePassword)
	}

	// Data source routes (protected)
	db := repository.GetDB()
	if db == nil {
		logger.Warn("Database not initialized, data source routes will return 503")
	}
	dsHandler := &DataSourceHandler{
		svc:    service.NewDataSourceService(db),
		logger: logger,
	}
	ds := r.Group("/data-sources")
	ds.Use(protectedMW...)
	{
		ds.GET("", dsHandler.List)
		ds.POST("/export", dsHandler.Export)
		ds.POST("/import", dsHandler.Import)
		ds.GET("/:id", dsHandler.Get)
		ds.POST("", dsHandler.Create)
		ds.PUT("/:id", dsHandler.Update)
		ds.DELETE("/:id", dsHandler.Delete)
		ds.POST("/test", dsHandler.TestConnection)
		ds.GET("/:id/schema", dsHandler.GetSchema)
		ds.GET("/:id/schemas", dsHandler.ListSchemaNames)
		ds.GET("/:id/schemas/:schema/objects", dsHandler.GetSchemaObjects)
		ds.GET("/:id/schema-detail-list", dsHandler.SchemaDetailList)
		ds.GET("/:id/schemas/:schema/table-list", dsHandler.TableList)
		ds.GET("/:id/column-types", dsHandler.GetColumnTypes)
		ds.GET("/:id/index-types", dsHandler.GetIndexTypes)
		ds.GET("/:id/tree-metadata", dsHandler.GetTreeMetadata)
		ds.GET("/:id/databases", dsHandler.ListDatabases)
		ds.GET("/:id/databases/:db/schemas", dsHandler.ListDatabaseSchemas)
		ds.POST("/upload-sqlite", dsHandler.UploadSQLiteFile)
		ds.GET("/:id/download", dsHandler.DownloadSQLiteFile)
	}

	// Query routes (protected)
	queryHandler := NewQueryHandler(cfg, logger)
	query := r.Group("/query")
	query.Use(protectedMW...)
	{
		query.POST("", queryHandler.Execute)
		query.GET("/ddl", queryHandler.GetDDL)
		query.POST("/dql", queryHandler.ExecuteDQL)
		query.POST("/dml", queryHandler.ExecuteDML)
		query.POST("/ddl-exec", queryHandler.ExecuteDDL)
		query.POST("/dcl", queryHandler.ExecuteDCL)
		query.POST("/tcl", queryHandler.ExecuteTCL)
	}

	// Sync task routes (protected)
	syncHandler := NewSyncTaskHandler(cfg, logger)
	sync := r.Group("/sync-tasks")
	sync.Use(protectedMW...)
	{
		sync.GET("", syncHandler.List)
		sync.GET("/:id", syncHandler.Get)
		sync.POST("", syncHandler.Create)
		sync.POST("/:id/start", syncHandler.Start)
		sync.POST("/:id/stop", syncHandler.Stop)
	}

	// Compare routes (protected)
	compareHandler := NewCompareHandler(cfg, logger)
	compare := r.Group("/compare")
	compare.Use(protectedMW...)
	{
		compare.POST("/structure", compareHandler.CompareStructure)
		compare.POST("/sync-structure", middleware.RequireRole("admin", "operator"), compareHandler.SyncStructure)
		compare.POST("/sync-data", middleware.RequireRole("admin", "operator"), compareHandler.SyncData)
	}

	// Table structure management routes (protected)
	tableHandler := NewTableHandler(cfg, logger)
	table := r.Group("/table")
	table.Use(protectedMW...)
	{
		table.POST("/structure", tableHandler.Structure)
		table.POST("/preview-alter", tableHandler.PreviewAlter)
		table.POST("/alter", middleware.RequireRole("admin", "operator"), tableHandler.Alter)
	}

	// View routes (protected)
	viewHandler := NewViewHandler(cfg, logger)
	view := r.Group("/view")
	view.Use(protectedMW...)
	{
		view.POST("/structure", viewHandler.Structure)
		view.POST("/definition", viewHandler.Definition)
		view.POST("/ddl-exec", middleware.RequireRole("admin", "operator"), viewHandler.ExecuteDDL)
	}

	// Server management routes
	dsMgmtSvc := service.NewDataSourceService(db)
	serverMgrHandler := NewServerManagerHandler(dsMgmtSvc, logger)
	server := r.Group("/server")
	server.Use(protectedMW...)
	server.Use(middleware.RequireManagementTag())
	{
		server.GET("/:ds_id/info", serverMgrHandler.GetServerInfo)
		server.GET("/:ds_id/metrics", serverMgrHandler.GetMetrics)
		server.GET("/:ds_id/metrics/v2", serverMgrHandler.GetMetricsV2)
		server.GET("/:ds_id/databases", serverMgrHandler.ListDatabases)
		server.GET("/:ds_id/processes", serverMgrHandler.ListProcesses)
		server.GET("/:ds_id/users", serverMgrHandler.ListUsers)
		server.GET("/:ds_id/tablespaces", serverMgrHandler.ListTablespaces)
		server.POST("/:ds_id/databases", serverMgrHandler.CreateDatabase)
		server.DELETE("/:ds_id/databases/:name", serverMgrHandler.DropDatabase)
		server.POST("/:ds_id/users", serverMgrHandler.CreateUser)
		server.DELETE("/:ds_id/users/:name", serverMgrHandler.DropUser)
		server.PUT("/:ds_id/users/:name/grants", serverMgrHandler.GrantPrivileges)
		server.PUT("/:ds_id/users/:name/password", serverMgrHandler.AlterUserPassword)
		server.PUT("/:ds_id/users/:name/lock", serverMgrHandler.AlterUserLock)
		server.PUT("/:ds_id/users/:name/rename", serverMgrHandler.AlterUserRename)
		server.PUT("/:ds_id/users/:name/schema", serverMgrHandler.AlterUserDefaultSchema)
		server.GET("/:ds_id/users/:name/privileges", serverMgrHandler.GetUserPrivileges)
		server.POST("/:ds_id/users/:name/privileges", serverMgrHandler.ApplyUserPrivileges)
		server.GET("/:ds_id/capability", serverMgrHandler.GetCapability)
		server.GET("/:ds_id/roles", serverMgrHandler.ListRoles)
		server.POST("/:ds_id/roles", serverMgrHandler.CreateRole)
		server.GET("/:ds_id/roles/:name/details", serverMgrHandler.GetRolePrivileges)
		server.PUT("/:ds_id/roles/:name/attribute", serverMgrHandler.AlterRoleAttribute)
		server.DELETE("/:ds_id/roles/:name", serverMgrHandler.DropRole)
		server.POST("/:ds_id/roles/:name/members", serverMgrHandler.AddRoleMember)
		server.DELETE("/:ds_id/roles/:name/members/:member", serverMgrHandler.RemoveRoleMember)

		// ─── MSSQL Login Management ───
		server.GET("/:ds_id/logins", serverMgrHandler.ListLogins)
		server.POST("/:ds_id/logins", serverMgrHandler.CreateLogin)
		server.GET("/:ds_id/logins/:name", serverMgrHandler.GetLoginDetail)
		server.PUT("/:ds_id/logins/:name", serverMgrHandler.AlterLogin)
		server.DELETE("/:ds_id/logins/:name", serverMgrHandler.DropLogin)

		// ─── MSSQL Database User Management ───
		server.GET("/:ds_id/database/:db/users", serverMgrHandler.ListDatabaseUsers)
		server.POST("/:ds_id/database/:db/users", serverMgrHandler.CreateDatabaseUser)
		server.DELETE("/:ds_id/database/:db/users/:name", serverMgrHandler.DropDatabaseUser)
		server.POST("/:ds_id/logins/:name/batch-map-users", serverMgrHandler.BatchCreateDatabaseUsers)

		// ─── MSSQL Orphaned Users ───
		server.GET("/:ds_id/database/:db/orphaned-users", serverMgrHandler.DetectOrphanedUsers)
		server.POST("/:ds_id/database/:db/orphaned-users/:name/fix", serverMgrHandler.FixOrphanedUser)

		// ─── MSSQL Effective Permissions ───
		server.POST("/:ds_id/database/:db/effective-permissions", serverMgrHandler.GetEffectivePermissions)

		// ─── MSSQL Guest Compliance ───
		server.GET("/:ds_id/database/:db/guest-status", serverMgrHandler.CheckGuestStatus)
		server.POST("/:ds_id/database/:db/disable-guest", serverMgrHandler.DisableGuest)
	}

	// Audit log routes (protected, admin only)
	auditHandler := NewAuditLogHandler(cfg, logger)
	audit := r.Group("/audit-logs")
	audit.Use(adminOpMW...)
	{
		audit.GET("", auditHandler.List)
	}

	// Settings routes (protected, admin only)
	settingsHandler := NewSettingsHandler(cfg, logger)
	settings := r.Group("/settings")
	settings.Use(adminMW...)
	{
		settings.GET("", settingsHandler.Get)
		settings.PUT("", settingsHandler.Update)
	}
}
