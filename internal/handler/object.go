package handler

import (
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/homej-top/dbridge/internal/model"
	"github.com/homej-top/dbridge/internal/service"
	"github.com/homej-top/dbridge/internal/service/drivers"
	"go.uber.org/zap"
)

var dangerousPatternsRe = []*regexp.Regexp{
	regexp.MustCompile(`^\s*GRANT\b`),
	regexp.MustCompile(`^\s*REVOKE\b`),
	regexp.MustCompile(`^\s*ALTER\s+USER\b`),
	regexp.MustCompile(`^\s*ALTER\s+ROLE\b`),
	regexp.MustCompile(`^\s*CREATE\s+USER\b`),
	regexp.MustCompile(`^\s*DROP\s+USER\b`),
	regexp.MustCompile(`^\s*TRUNCATE\b`),
	regexp.MustCompile(`^\s*DELETE\b`),
	regexp.MustCompile(`^\s*UPDATE\b`),
	regexp.MustCompile(`^\s*INSERT\b`),
}

var (
	blockCommentRe  = regexp.MustCompile(`(?s)/\*.*?\*/`)
	stringLiteralRe = regexp.MustCompile(`'(\\'|''|[^'\\])*'`)
)

func validateDDLType(ddl string, allowedPrefixes []string) error {
	trimmed := strings.TrimSpace(strings.ToUpper(stripSQLComments(stripStringLiterals(ddl))))
	for _, prefix := range allowedPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return nil
		}
	}
	return fmt.Errorf("DDL type not allowed, only: %s", strings.Join(allowedPrefixes, ", "))
}

func validateNoDangerousSql(ddl string) error {
	cleaned := stripStringLiterals(ddl)
	cleaned = stripSQLComments(cleaned)
	upper := strings.ToUpper(cleaned)

	stmts := splitTopLevelStatements(upper)
	for _, stmt := range stmts {
		for _, re := range dangerousPatternsRe {
			if re.MatchString(stmt) {
				return fmt.Errorf("DDL contains disallowed statement: %s", strings.TrimSpace(stmt))
			}
		}
	}
	return nil
}

func splitTopLevelStatements(upper string) []string {
	var stmts []string
	depth := 0
	start := 0
	for i := 0; i < len(upper); i++ {
		if i+5 <= len(upper) && upper[i:i+5] == "BEGIN" && (i+5 >= len(upper) || !isIdentChar(upper[i+5])) {
			depth++
		} else if i+3 <= len(upper) && upper[i:i+3] == "END" && (i+3 >= len(upper) || !isIdentChar(upper[i+3])) {
			if depth > 0 {
				depth--
			}
		} else if upper[i] == ';' && depth == 0 {
			stmts = append(stmts, upper[start:i])
			start = i + 1
		}
	}
	if start < len(upper) {
		remaining := strings.TrimSpace(upper[start:])
		if remaining != "" {
			stmts = append(stmts, upper[start:])
		}
	}
	return stmts
}

func isIdentChar(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
}

func stripSQLComments(sql string) string {
	sql = blockCommentRe.ReplaceAllString(sql, " ")
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	sql = strings.Join(lines, "\n")
	sql = drivers.StripDelimiter(sql)
	return sql
}

func stripStringLiterals(sql string) string {
	return stringLiteralRe.ReplaceAllString(sql, "''")
}

// GetSupportedObjectTypes returns the object types supported by the current data source
func (h *DataSourceHandler) GetSupportedObjectTypes(c *gin.Context) {
	id := c.Param("id")

	types, err := h.svc.GetSupportedObjectTypes(id)
	if err != nil {
		h.logger.Error("get supported object types failed", zap.String("id", id), zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"types": types}))
}

// ListObjectsByType returns a paginated list of database objects of a given type
func (h *DataSourceHandler) ListObjectsByType(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")
	database := c.Query("database") // Optional: for multi-database support (PostgreSQL, etc.)

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "100"))
	if page < 1 {
		page = 1
	}
	if pageSize > 500 {
		pageSize = 500
	}
	if pageSize < 1 {
		pageSize = 100
	}

	opts := drivers.ListOptions{
		Page:     page,
		PageSize: pageSize,
		Keyword:  c.Query("keyword"),
		Database: database, // Pass database parameter to driver
	}

	result, err := h.svc.ListObjectsByType(id, schema, objectType, opts)
	if err != nil {
		h.logger.Error("list objects failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(result))
}

// GetObjectDetail returns detailed information about a specific database object
func (h *DataSourceHandler) GetObjectDetail(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")
	name := c.Param("name")
	database := c.Query("database")

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	var detail map[string]interface{}
	var err error
	if database != "" {
		detail, err = h.svc.GetObjectDetailForDB(id, schema, objectType, name, database)
	} else {
		detail, err = h.svc.GetObjectDetail(id, schema, objectType, name)
	}
	if err != nil {
		h.logger.Error("get object detail failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(detail))
}

// CreateObject creates a new database object by executing DDL
func (h *DataSourceHandler) CreateObject(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	var input struct {
		DDL      string `json:"ddl" binding:"required"`
		Database string `json:"database"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "ddl is required"))
		return
	}

	if err := validateDDLType(input.DDL, []string{"CREATE"}); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}
	if err := validateNoDangerousSql(input.DDL); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	var message string
	var err error
	if input.Database != "" {
		message, err = h.svc.CreateObjectForDB(id, schema, objectType, drivers.StripDelimiter(input.DDL), input.Database)
	} else {
		message, err = h.svc.CreateObject(id, schema, objectType, drivers.StripDelimiter(input.DDL))
	}
	if err != nil {
		h.logger.Error("create object failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"message": message}))
}

// AlterObject modifies an existing database object
func (h *DataSourceHandler) AlterObject(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")
	name := c.Param("name")

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	var input struct {
		DDL      string `json:"ddl" binding:"required"`
		Force    bool   `json:"force"`
		Database string `json:"database"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "ddl is required"))
		return
	}

	if err := validateNoDangerousSql(input.DDL); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, err.Error()))
		return
	}

	if !input.Force {
		deps, err := h.svc.CheckDependencies(id, schema, objectType, name)
		if err != nil {
			h.logger.Error("check dependencies failed", zap.Error(err))
			c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
			return
		}
		if len(deps) > 0 {
			c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
				"warning":         "object is depended on by other objects, modification may break dependencies",
				"dependencies":    deps,
				"require_confirm": true,
			}))
			return
		}
	}

	ddls, count, err := h.svc.AlterObjectForDB(id, schema, objectType, name, drivers.StripDelimiter(input.DDL), input.Database)
	if err != nil {
		h.logger.Error("alter object failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"message":    fmt.Sprintf("%s object modified", objectType),
		"statements": count,
		"ddls":       ddls,
	}))
}

// DropObject deletes a database object
func (h *DataSourceHandler) DropObject(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")
	name := c.Param("name")

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	var input struct {
		Force    bool   `json:"force"`
		Database string `json:"database"`
	}
	_ = c.ShouldBindJSON(&input)

	err := h.svc.DropObjectForDB(id, schema, objectType, name, input.Force, input.Database)
	if err != nil {
		var depWarning *service.DependencyWarning
		if errors.As(err, &depWarning) {
			c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
				"warning":         fmt.Sprintf("object %s is depended on by %d other objects", name, len(depWarning.Dependencies)),
				"dependencies":    depWarning.Dependencies,
				"require_confirm": true,
			}))
			return
		}
		h.logger.Error("drop object failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"message": "object deleted"}))
}

// RefreshMatView refreshes a materialized view
func (h *DataSourceHandler) RefreshMatView(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	name := c.Param("name")

	var input struct {
		Mode     string `json:"mode" binding:"required"` // normal, concurrently, with-no-data
		Database string `json:"database"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "mode is required"))
		return
	}

	if input.Mode != "normal" && input.Mode != "concurrently" && input.Mode != "with-no-data" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid mode: must be 'normal', 'concurrently', or 'with-no-data'"))
		return
	}

	err := h.svc.RefreshMatViewForDB(id, schema, name, input.Mode, input.Database)
	if err != nil {
		h.logger.Error("refresh materialized view failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{"message": "materialized view refreshed"}))
}

// GetCreateTemplate returns a DDL template for creating a new object
func (h *DataSourceHandler) GetCreateTemplate(c *gin.Context) {
	id := c.Param("id")
	schema := c.Param("schema")
	objectType := c.Param("type")
	objectName := c.Query("object_name")

	if !drivers.IsValidObjectType(objectType) {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "invalid object type: "+objectType))
		return
	}

	template, err := h.svc.GetCreateTemplate(id, schema, objectType, objectName)
	if err != nil {
		h.logger.Error("get create template failed", zap.Error(err))
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, err.Error()))
		return
	}

	c.JSON(http.StatusOK, model.SuccessResponse(gin.H{
		"template":    template,
		"object_type": objectType,
		"schema":      schema,
		"object_name": objectName,
	}))
}
