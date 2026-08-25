package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

func testJWTConfig() *config.JWTConfig {
	return &config.JWTConfig{
		Secret:    "test-secret-key-32-bytes-long!!",
		ExpiresIn: 3600,
	}
}

func TestGenerateJWT(t *testing.T) {
	cfg := testJWTConfig()
	token, err := GenerateJWT(cfg, "user-1", "admin", "admin", "tenant-1")
	if err != nil {
		t.Fatalf("GenerateJWT failed: %v", err)
	}
	if token == "" {
		t.Fatal("token should not be empty")
	}
}

func TestAuthMiddlewareValidToken(t *testing.T) {
	cfg := testJWTConfig()
	token, err := GenerateJWT(cfg, "user-1", "admin", "admin", "tenant-1")
	if err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", AuthMiddleware(cfg), func(c *gin.Context) {
		userID, _ := c.Get("user_id")
		role, _ := c.Get("role")
		c.JSON(200, gin.H{"user_id": userID, "role": role})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddlewareMissingHeader(t *testing.T) {
	cfg := testJWTConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", AuthMiddleware(cfg), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAuthMiddlewareInvalidToken(t *testing.T) {
	cfg := testJWTConfig()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/test", AuthMiddleware(cfg), func(c *gin.Context) {
		c.JSON(200, gin.H{"ok": true})
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRequireRole(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		userRole     string
		allowedRoles []string
		expectCode   int
	}{
		{"admin allowed", "admin", []string{"admin"}, http.StatusOK},
		{"admin denied", "viewer", []string{"admin"}, http.StatusForbidden},
		{"multiple roles allowed", "operator", []string{"admin", "operator"}, http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.GET("/test", func(c *gin.Context) {
				c.Set("role", tt.userRole)
				c.Next()
			}, RequireRole(tt.allowedRoles...), func(c *gin.Context) {
				c.JSON(200, gin.H{"ok": true})
			})

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.expectCode {
				t.Errorf("expected %d, got %d", tt.expectCode, w.Code)
			}
		})
	}
}

func TestJWTClaimsParsing(t *testing.T) {
	cfg := testJWTConfig()
	tokenStr, err := GenerateJWT(cfg, "u1", "testuser", "developer", "t1")
	if err != nil {
		t.Fatal(err)
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(t *jwt.Token) (interface{}, error) {
		return []byte(cfg.Secret), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !token.Valid {
		t.Fatal("token should be valid")
	}

	if claims.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", claims.UserID, "u1")
	}
	if claims.Role != "developer" {
		t.Errorf("Role = %q, want %q", claims.Role, "developer")
	}
	if claims.TenantID != "t1" {
		t.Errorf("TenantID = %q, want %q", claims.TenantID, "t1")
	}
}
