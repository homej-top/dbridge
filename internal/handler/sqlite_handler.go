package handler

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/dbridge/dbridge/internal/model"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	storagePkg "github.com/dbridge/dbridge/pkg/storage"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// UploadSQLiteFile handles .db file upload for SQLite data sources
func (h *DataSourceHandler) UploadSQLiteFile(c *gin.Context) {
	name := c.PostForm("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "name required"))
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "file required"))
		return
	}
	defer file.Close()

	// Validate extension
	if !strings.HasSuffix(strings.ToLower(header.Filename), ".db") {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "仅支持 .db 文件"))
		return
	}

	// Validate size (max 2GB)
	const maxSize = 2 * 1024 * 1024 * 1024
	if header.Size > maxSize {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "文件不能超过 2GB"))
		return
	}

	// Validate file header (SQLite magic)
	magic := make([]byte, 16)
	if _, err := io.ReadFull(file, magic); err != nil {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "无法读取文件"))
		return
	}
	if string(magic) != "SQLite format 3\x00" {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError, "非有效的 SQLite 数据库文件"))
		return
	}

	// SQLite 数据源强制使用本地存储 Profile（S3 模式仅支持只读）
	st := storagePkg.Get()
	if _, isLocal := st.(*storagePkg.LocalFileStorage); !isLocal {
		c.JSON(http.StatusBadRequest, model.ErrorResponse(model.CodeParamError,
			"当前默认存储为 S3/云存储，SQLite 需要本地存储。请切换默认 Profile 为 local 后重试"))
		return
	}

	// 使用 storage 抽象保存文件，MultiReader 将魔数拼回
	safeName := sanitizeSQLiteFileName(header.Filename)
	targetPath := "sqlite/" + safeName

	exists, _ := st.Exists(c.Request.Context(), targetPath)
	if exists {
		c.JSON(http.StatusConflict, model.ErrorResponse(model.CodeParamError, "文件已存在，请更换名称"))
		return
	}

	combinedReader := io.MultiReader(bytes.NewReader(magic), file)
	_, saveErr := st.Save(c.Request.Context(), targetPath, combinedReader, "application/x-sqlite3")
	if saveErr != nil {
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "保存文件失败"))
		return
	}

	// 对于本地存储，获取绝对路径用于 SQLite 连接
	destPath := targetPath
	if localSt, ok := st.(*storagePkg.LocalFileStorage); ok {
		resolved, _ := localSt.Resolve(targetPath)
		if resolved != "" {
			destPath = resolved
		}
	}

	// Create data source record
	dsID := uuid.New().String()
	ds := repository.DataSource{
		ID:        dsID,
		Name:      name,
		Type:      "sqlite",
		Host:      destPath,
		Port:      0,
		Database:  "",
		Username:  "",
		Password:  "",
		TenantID:  c.GetString("tenant_id"),
		CreatedBy: c.GetString("user_id"),
	}
	if err := repository.GetDB().Create(&ds).Error; err != nil {
		st.Delete(c.Request.Context(), targetPath)
		c.JSON(http.StatusInternalServerError, model.ErrorResponse(model.CodeDatabaseError, "创建数据源失败"))
		return
	}

	// Audit
	service.QuickAudit(repository.GetDB(), c.GetString("user_id"), c.GetString("tenant_id"),
		service.ModuleDatasource, "ds_create", dsID,
		service.ResultSuccess, c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{Target: name, DsType: "sqlite"})

	c.JSON(http.StatusCreated, model.SuccessResponse(ds))
}

// DownloadSQLiteFile downloads the .db file for a SQLite data source
func (h *DataSourceHandler) DownloadSQLiteFile(c *gin.Context) {
	id := c.Param("id")

	var ds repository.DataSource
	if err := repository.GetDB().Where("id = ? AND type = ?", id, "sqlite").First(&ds).Error; err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "SQLite 数据源不存在"))
		return
	}

	userID := c.GetString("user_id")
	if ds.CreatedBy != "" && ds.CreatedBy != userID {
		c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodeAuthFailed, "无权下载"))
		return
	}

	// 通过 storage 流式读取
	st := storagePkg.Get()
	// Host 字段存储的是路径；如果是本地存储，可能是绝对路径；需要转回相对路径
	storagePath := ds.Host
	if localSt, ok := st.(*storagePkg.LocalFileStorage); ok {
		// 尝试从绝对路径推导相对路径
		storagePath = strings.TrimPrefix(ds.Host, localSt.BasePath)
		storagePath = strings.TrimLeft(storagePath, "/\\")
	}

	reader, err := st.ReadStream(c.Request.Context(), storagePath)
	if err != nil {
		c.JSON(http.StatusNotFound, model.ErrorResponse(model.CodeResourceNotFound, "文件不存在"))
		return
	}
	defer reader.Close()

	// Audit
	service.QuickAudit(repository.GetDB(), userID, c.GetString("tenant_id"),
		service.ModuleDatasource, "ds_download", id,
		service.ResultSuccess, c.ClientIP(), c.Request.UserAgent(), c.GetString("username"),
		&service.AuditDetails{Target: ds.Name})

	c.Header("Content-Disposition", "attachment; filename=\""+ds.Name+".db\"")
	c.DataFromReader(http.StatusOK, -1, "application/x-sqlite3", reader, nil)
}

// sanitizeSQLiteFileName removes unsafe characters, specific to SQLite .db files
func sanitizeSQLiteFileName(name string) string {
	// strip path
	if idx := strings.LastIndex(name, "/"); idx >= 0 {
		name = name[idx+1:]
	}
	if idx := strings.LastIndex(name, "\\"); idx >= 0 {
		name = name[idx+1:]
	}
	result := make([]byte, 0, len(name))
	for _, ch := range []byte(name) {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') || ch == '.' || ch == '-' || ch == '_' {
			result = append(result, ch)
		}
	}
	if len(result) == 0 {
		return fmt.Sprintf("upload_%s.db", uuid.New().String()[:8])
	}
	return string(result)
}
