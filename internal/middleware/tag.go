package middleware

import (
	"net/http"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/gin-gonic/gin"
)

// RequireManagementTag 校验数据源是否拥有 database_management 标签
func RequireManagementTag() gin.HandlerFunc {
	return func(c *gin.Context) {
		dsID := c.Param("ds_id")
		if dsID == "" {
			c.Next()
			return
		}

		db := repository.GetDB()
		if db == nil {
			c.Next()
			return
		}

		// Check if enforcement is enabled
		var settings repository.Setting
		if err := db.Where("`key` = ?", "server_management.require_management_tag").First(&settings).Error; err == nil && settings.Value == "false" {
			c.Next()
			return
		}

		// Verify data source has database_management tag
		var ds repository.DataSource
		if err := db.Where("id = ?", dsID).First(&ds).Error; err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 4005, "message": "数据源不存在"})
			c.Abort()
			return
		}

		if !hasTag(ds.Tags, "database_management") {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    4003,
				"message": "该数据源未授权数据库管理操作",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}

func hasTag(tags, tag string) bool {
	if tags == "" {
		return false
	}
	start := 0
	for i := 0; i <= len(tags); i++ {
		if i == len(tags) || tags[i] == ',' {
			if tags[start:i] == tag {
				return true
			}
			start = i + 1
		}
	}
	return false
}
