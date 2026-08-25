package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig(t *testing.T) {
	// Create temp config file
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
server:
  port: 9090
  env: test
  read_timeout: 10s
  write_timeout: 10s
database:
  type: sqlite
  sqlite:
    path: /tmp/test.db
cache:
  backend: local
  default_ttl: 300
  local_max_items: 1000
  redis:
    addr: localhost:6379
    pool_size: 10
jwt:
  secret: test-secret
  expires_in: 3600
log:
  level: debug
  format: console
  output: stdout
`
	if err := os.WriteFile(cfgPath, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.Env != "test" {
		t.Errorf("Server.Env = %q, want %q", cfg.Server.Env, "test")
	}
	if cfg.Database.Type != "sqlite" {
		t.Errorf("Database.Type = %q, want %q", cfg.Database.Type, "sqlite")
	}
	if cfg.Cache.Redis.PoolSize != 10 {
		t.Errorf("Cache.Redis.PoolSize = %d, want 10", cfg.Cache.Redis.PoolSize)
	}
	if cfg.Cache.Backend != "local" {
		t.Errorf("Cache.Backend = %q, want %q", cfg.Cache.Backend, "local")
	}
	if cfg.Cache.DefaultTTL != 300 {
		t.Errorf("Cache.DefaultTTL = %d, want 300", cfg.Cache.DefaultTTL)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	_, err := LoadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Error("expected error for missing config file")
	}
}
