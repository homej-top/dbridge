package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

type Claims struct {
	UserID   string `json:"user_id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	TenantID string `json:"tenant_id"`
	jwt.RegisteredClaims
}

func GenerateJWT(cfg *config.JWTConfig, userID, username, role, tenantID string) (string, error) {
	claims := Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		TenantID: tenantID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(cfg.ExpiresIn) * time.Second)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.Secret))
}

func AuthMiddleware(cfg *config.JWTConfig) gin.HandlerFunc {
	return func(c *gin.Context) {
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, model.ErrorResponse(model.CodeAuthFailed, "missing authorization header"))
			c.Abort()
			return
		}

		tokenString := strings.TrimPrefix(authHeader, "Bearer ")
		if tokenString == authHeader {
			c.JSON(http.StatusUnauthorized, model.ErrorResponse(model.CodeAuthFailed, "invalid authorization format"))
			c.Abort()
			return
		}

		claims := &Claims{}
		token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
			return []byte(cfg.Secret), nil
		})

		if err != nil || !token.Valid {
			c.JSON(http.StatusUnauthorized, model.ErrorResponse(model.CodeAuthFailed, "invalid or expired token"))
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Set("tenant_id", claims.TenantID)
		c.Next()
	}
}

func RequireRole(roles ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		userRole, exists := c.Get("role")
		if !exists {
			c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "no role in context"))
			c.Abort()
			return
		}

		for _, r := range roles {
			if userRole == r {
				c.Next()
				return
			}
		}

		c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "insufficient permissions"))
		c.Abort()
	}
}

func RequirePermission(permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Simple role-to-permission mapping
		rolePerms := map[string][]string{
			"admin":   {"*"},
			"operator": {"data_source:read", "data_source:write", "sync:read", "sync:write", "query:execute"},
			"developer": {"data_source:read", "sync:read", "sync:write", "query:execute"},
			"viewer":  {"data_source:read", "sync:read"},
		}

		userRole, _ := c.Get("role")
		role := userRole.(string)

		allowed, ok := rolePerms[role]
		if !ok {
			c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "unknown role"))
			c.Abort()
			return
		}

		// Admin has all permissions
		for _, a := range allowed {
			if a == "*" {
				c.Next()
				return
			}
		}

		for _, p := range permissions {
			found := false
			for _, a := range allowed {
				if a == p {
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusForbidden, model.ErrorResponse(model.CodePermissionDenied, "permission denied: "+p))
				c.Abort()
				return
			}
		}

		c.Next()
	}
}
