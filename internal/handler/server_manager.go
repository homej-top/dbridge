package handler

import (
	"net/http"

	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/dbridge/dbridge/internal/service/drivers"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type ServerManagerHandler struct {
	dsSvc  *service.DataSourceService
	logger *zap.Logger
}

func NewServerManagerHandler(dsSvc *service.DataSourceService, logger *zap.Logger) *ServerManagerHandler {
	return &ServerManagerHandler{dsSvc: dsSvc, logger: logger}
}

// GetServerInfo returns basic server information
func (h *ServerManagerHandler) GetServerInfo(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	info, err := driver.GetServerInfo(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(info))
}

// GetMetrics returns current server metrics
func (h *ServerManagerHandler) GetMetrics(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	metrics, err := driver.GetMetrics(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(metrics))
}

// GetMetricsV2 returns structured monitoring metrics
func (h *ServerManagerHandler) GetMetricsV2(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	metrics, err := driver.GetMetricsV2(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(metrics))
}

// ListDatabases returns database list for a server
func (h *ServerManagerHandler) ListDatabases(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	dbs, err := driver.ListDatabases()
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	if dbs == nil {
		dbs = []drivers.DatabaseInfo{}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"databases": dbs}))
}

func (h *ServerManagerHandler) CreateDatabase(c *gin.Context) {
	dsID := c.Param("ds_id")
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	if err := driver.CreateDatabase(req.Name); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) DropDatabase(c *gin.Context) {
	dsID := c.Param("ds_id")
	dbName := c.Param("name")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	if err := driver.DropDatabase(dbName); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) CreateUser(c *gin.Context) {
	dsID := c.Param("ds_id")
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	if err := driver.CreateUser(req.Username, req.Password); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) DropUser(c *gin.Context) {
	dsID := c.Param("ds_id")
	userName := c.Param("name")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	if err := driver.DropUser(userName); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) GrantPrivileges(c *gin.Context) {
	dsID := c.Param("ds_id")
	userName := c.Param("name")
	var req struct {
		Database string   `json:"database"`
		Privileges []string `json:"privileges"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	if err := driver.GrantPrivileges(userName, req.Database, req.Privileges); err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// GetCapability returns version and capability flags
func (h *ServerManagerHandler) GetCapability(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	cs, err := driver.DetectCapability()
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(cs))
}

// ListRoles returns role list
func (h *ServerManagerHandler) ListRoles(c *gin.Context) {
	dsID := c.Param("ds_id")
	database := c.Query("database")
	var driver drivers.DatabaseDriver
	var err error
	if database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	roles, err := driver.ListRoles()
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	if roles == nil { roles = []drivers.RoleInfo{} }
	c.JSON(http.StatusOK, model.SuccessResponse(roles))
}

func (h *ServerManagerHandler) CreateRole(c *gin.Context) {
	dsID := c.Param("ds_id")
	var req struct{ Name string `json:"name"`; Database string `json:"database"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	var driver drivers.DatabaseDriver
	var err error
	if req.Database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, req.Database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.CreateRole(req.Name); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) DropRole(c *gin.Context) {
	dsID := c.Param("ds_id"); roleName := c.Param("name")
	database := c.Query("database")
	var driver drivers.DatabaseDriver
	var err error
	if database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.DropRole(roleName); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) AddRoleMember(c *gin.Context) {
	dsID := c.Param("ds_id"); roleName := c.Param("name")
	var req struct{ Member string `json:"member"`; Database string `json:"database"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	var driver drivers.DatabaseDriver
	var err error
	if req.Database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, req.Database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AddRoleMember(roleName, req.Member); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

func (h *ServerManagerHandler) RemoveRoleMember(c *gin.Context) {
	dsID := c.Param("ds_id"); roleName := c.Param("name"); member := c.Param("member")
	database := c.Query("database")
	var driver drivers.DatabaseDriver
	var err error
	if database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.RemoveRoleMember(roleName, member); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}


// GetUserPrivileges returns detailed privileges for a user
func (h *ServerManagerHandler) GetUserPrivileges(c *gin.Context) {
	dsID := c.Param("ds_id")
	userName := c.Param("name")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()
	privs, err := driver.GetUserPrivileges(userName)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}
	roles, _ := driver.GetUserRoles(userName)
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"privileges": privs, "roles": roles}))
}

// ApplyUserPrivileges applies privilege changes (supports Dry-Run)
func (h *ServerManagerHandler) ApplyUserPrivileges(c *gin.Context) {
	dsID := c.Param("ds_id")
	userName := c.Param("name")
	var req struct {
		Database string                    `json:"database"`
		Changes  []drivers.PrivilegeDelta  `json:"changes"`
		DryRun   bool                      `json:"dry_run"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	var driver drivers.DatabaseDriver
	var err error
	if req.Database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, req.Database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if len(req.Changes) > 0 { req.Changes[0].DryRun = req.DryRun }
	result, err := driver.ApplyPrivilegeChanges(userName, req.Changes)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// ListProcesses returns current process/session list
func (h *ServerManagerHandler) ListProcesses(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, ds, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	processes, err := driver.ListProcesses(ds.Type)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	if processes == nil {
		processes = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(processes))
}

// ListUsers returns user list for a server
func (h *ServerManagerHandler) ListUsers(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, ds, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	users, err := driver.ListUsers(ds.Type)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	if users == nil {
		users = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(users))
}

// ListTablespaces returns tablespace info (Oracle/PG)
func (h *ServerManagerHandler) ListTablespaces(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, ds, err := h.dsSvc.Connect(dsID)
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	defer driver.Close()

	result, err := driver.ListTablespaces(ds.Type)
	if err != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	if result == nil {
		result = []map[string]interface{}{}
	}
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// AlterUserPassword changes a user password
func (h *ServerManagerHandler) AlterUserPassword(c *gin.Context) {
	dsID := c.Param("ds_id"); userName := c.Param("name")
	var req struct{ Password string `json:"password"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterUserPassword(userName, req.Password); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// AlterUserLock locks or unlocks a user
func (h *ServerManagerHandler) AlterUserLock(c *gin.Context) {
	dsID := c.Param("ds_id"); userName := c.Param("name")
	var req struct{ Lock bool `json:"lock"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterUserLock(userName, req.Lock); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// AlterUserRename renames a user
func (h *ServerManagerHandler) AlterUserRename(c *gin.Context) {
	dsID := c.Param("ds_id"); userName := c.Param("name")
	var req struct{ NewName string `json:"new_name"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterUserRename(userName, req.NewName); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// AlterUserDefaultSchema changes default schema
func (h *ServerManagerHandler) AlterUserDefaultSchema(c *gin.Context) {
	dsID := c.Param("ds_id"); userName := c.Param("name")
	var req struct{ Schema string `json:"schema"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterUserDefaultSchema(userName, req.Schema); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// GetRolePrivileges returns role details with privileges and members
func (h *ServerManagerHandler) GetRolePrivileges(c *gin.Context) {
	dsID := c.Param("ds_id"); roleName := c.Param("name")
	database := c.Query("database")
	var driver drivers.DatabaseDriver
	var err error
	if database != "" {
		driver, err = h.dsSvc.ConnectForDB(dsID, database)
	} else {
		driver, _, err = h.dsSvc.Connect(dsID)
	}
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	privs, _ := driver.GetRolePrivileges(roleName)
	if privs == nil { privs = []drivers.PrivilegeEntry{} }
	parentRoles, _ := driver.GetParentRoles(roleName)
	if parentRoles == nil { parentRoles = []drivers.ParentRoleInfo{} }
	membersInfo, _ := driver.GetRoleMembers(roleName)
	if membersInfo == nil { membersInfo = []drivers.MemberRoleInfo{} }
	inherit, _ := driver.GetRoleInherit(roleName)
	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"privileges":   privs,
		"parent_roles": parentRoles,
		"members":      membersInfo,
		"inherit":      inherit,
	}))
}

// AlterRoleAttribute changes a role attribute
func (h *ServerManagerHandler) AlterRoleAttribute(c *gin.Context) {
	dsID := c.Param("ds_id"); roleName := c.Param("name")
	var req struct{ Attribute string `json:"attribute"`; Value string `json:"value"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterRoleAttribute(roleName, req.Attribute, req.Value); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── MSSQL Login Management Handlers ────────────────────────────────────

// ListLogins returns SQL Server instance-level login list
func (h *ServerManagerHandler) ListLogins(c *gin.Context) {
	dsID := c.Param("ds_id")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()

	logins, err := driver.ListLogins()
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	if logins == nil { logins = []drivers.MSSQLLogin{} }
	c.JSON(http.StatusOK, model.SuccessResponse(logins))
}

// CreateLogin creates a new SQL Server login with optional server roles and DB user mappings
func (h *ServerManagerHandler) CreateLogin(c *gin.Context) {
	dsID := c.Param("ds_id")
	var req drivers.CreateLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.CreateLogin(req); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// GetLoginDetail returns login details including server roles and DB user mappings
func (h *ServerManagerHandler) GetLoginDetail(c *gin.Context) {
	dsID := c.Param("ds_id"); loginName := c.Param("name")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	detail, err := driver.GetLoginDetail(loginName)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(detail))
}

// AlterLogin modifies a login (password, enable/disable, rename, unlock)
func (h *ServerManagerHandler) AlterLogin(c *gin.Context) {
	dsID := c.Param("ds_id"); loginName := c.Param("name")
	var req drivers.AlterLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.AlterLogin(loginName, req); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// DropLogin deletes a login with optional cascaded user removal
func (h *ServerManagerHandler) DropLogin(c *gin.Context) {
	dsID := c.Param("ds_id"); loginName := c.Param("name")
	cascade := c.Query("cascade") == "true"
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	result, err := driver.DropLogin(loginName, cascade)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// ─── MSSQL Database User Management Handlers ────────────────────────────

// ListDatabaseUsers returns users in a specific database
func (h *ServerManagerHandler) ListDatabaseUsers(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	users, err := driver.ListDatabaseUsers(database)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	if users == nil { users = []drivers.MSSQLDatabaseUser{} }
	c.JSON(http.StatusOK, model.SuccessResponse(users))
}

// CreateDatabaseUser creates a database user
func (h *ServerManagerHandler) CreateDatabaseUser(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	var req drivers.CreateDBUserRequest
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.CreateDatabaseUser(database, req); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// DropDatabaseUser deletes a database user
func (h *ServerManagerHandler) DropDatabaseUser(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db"); userName := c.Param("name")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.DropDatabaseUser(database, userName); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// BatchCreateDatabaseUsers creates DB user mappings for a login across multiple databases
func (h *ServerManagerHandler) BatchCreateDatabaseUsers(c *gin.Context) {
	dsID := c.Param("ds_id"); loginName := c.Param("name")
	var req struct{ Mappings []drivers.DBUserMapping `json:"mappings"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.BatchCreateDatabaseUsers(loginName, req.Mappings); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── MSSQL Orphaned Users Handlers ──────────────────────────────────────

// DetectOrphanedUsers detects orphaned database users
func (h *ServerManagerHandler) DetectOrphanedUsers(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	users, err := driver.DetectOrphanedUsers(database)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	if users == nil { users = []drivers.OrphanedUser{} }
	c.JSON(http.StatusOK, model.SuccessResponse(users))
}

// FixOrphanedUser fixes an orphaned user by re-linking to a login
func (h *ServerManagerHandler) FixOrphanedUser(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db"); userName := c.Param("name")
	var req struct{ LoginName string `json:"login_name"` }
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.FixOrphanedUser(database, userName, req.LoginName); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

// ─── MSSQL Effective Permissions Handler ───────────────────────────────

// GetEffectivePermissions calculates effective permissions for a principal on an object
func (h *ServerManagerHandler) GetEffectivePermissions(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	var req struct {
		PrincipalName string `json:"principal_name" binding:"required"`
		ObjectType    string `json:"object_type"`
		ObjectName    string `json:"object_name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	result, err := driver.GetEffectivePermissions(database, req.PrincipalName, req.ObjectType, req.ObjectName)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// ─── MSSQL Guest Compliance Handlers ────────────────────────────────────

// CheckGuestStatus returns guest user status for compliance check
func (h *ServerManagerHandler) CheckGuestStatus(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	status, err := driver.CheckGuestStatus(database)
	if err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(status))
}

// DisableGuest disables the guest user in a database
func (h *ServerManagerHandler) DisableGuest(c *gin.Context) {
	dsID := c.Param("ds_id"); database := c.Param("db")
	driver, _, err := h.dsSvc.Connect(dsID)
	if err != nil { c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error())); return }
	defer driver.Close()
	if err := driver.DisableGuest(database); err != nil { c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error())); return }
	c.JSON(http.StatusOK, model.SuccessResponse(nil))
}

