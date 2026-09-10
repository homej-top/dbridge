package storage

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

type BindingResolver struct {
	db      *gorm.DB
	cache   bindingCache
	sfGroup singleflight.Group
}

type ResolvedBinding struct {
	ProfileCode string
	BasePath    string
	Storage     FileStorage
}

type bindingCache struct {
	mu       sync.RWMutex
	entries  map[string]*ResolvedBinding
	loadedAt time.Time
	ttl      time.Duration
}

func newBindingCache(ttl time.Duration) *bindingCache {
	return &bindingCache{
		entries: make(map[string]*ResolvedBinding),
		ttl:     ttl,
	}
}

func (c *bindingCache) get(key string) (*ResolvedBinding, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if time.Since(c.loadedAt) > c.ttl {
		return nil, false
	}
	b, ok := c.entries[key]
	return b, ok
}

func (c *bindingCache) set(key string, b *ResolvedBinding) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = b
	c.loadedAt = time.Now()
}

func (c *bindingCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*ResolvedBinding)
}

func NewBindingResolver(db *gorm.DB) *BindingResolver {
	return &BindingResolver{
		db:    db,
		cache: *newBindingCache(5 * time.Minute),
	}
}

func (r *BindingResolver) Resolve(moduleCode string) *ResolvedBinding {
	if b, ok := r.cache.get(moduleCode); ok && b != nil {
		return b
	}

	result, _, _ := r.sfGroup.Do(moduleCode, func() (interface{}, error) {
		if b, ok := r.cache.get(moduleCode); ok && b != nil {
			return b, nil
		}
		b := r.loadFromDB(moduleCode)
		if b == nil {
			b = &ResolvedBinding{Storage: Get()}
		}
		r.cache.set(moduleCode, b)
		return b, nil
	})
	return result.(*ResolvedBinding)
}

func (r *BindingResolver) ResolvePath(moduleCode string, relativePath string) (*ResolvedBinding, string) {
	b := r.Resolve(moduleCode)
	full := relativePath
	if b.BasePath != "" {
		full = filepath.Join(b.BasePath, relativePath)
	}
	return b, full
}

func (r *BindingResolver) Invalidate() {
	r.cache.clear()
}

func (r *BindingResolver) loadFromDB(moduleCode string) *ResolvedBinding {
	if r.db == nil {
		return nil
	}

	type bindingRow struct {
		ProfileCode string
		BasePath    string
	}
	var row bindingRow
	err := r.db.Table("storage_bindings").
		Select("profile_code, base_path").
		Where("module_code = ?", moduleCode).
		Scan(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		log.Printf("binding_resolver: loadFromDB failed for module %q: %v, falling back to default", moduleCode, err)
		return nil
	}
	if row.ProfileCode == "" {
		return nil
	}

	st := GetByCode(row.ProfileCode)
	if st == nil {
		log.Printf("binding_resolver: profile %q not found for module %q, falling back to default", row.ProfileCode, moduleCode)
		return nil
	}
	return &ResolvedBinding{
		ProfileCode: row.ProfileCode,
		BasePath:    row.BasePath,
		Storage:     st,
	}
}

func ValidateBasePath(p string) (string, error) {
	if strings.ContainsRune(p, '\x00') {
		return "", fmt.Errorf("base_path must not contain null bytes")
	}
	cleaned := filepath.Clean(p)
	if filepath.IsAbs(cleaned) {
		return "", fmt.Errorf("base_path must be relative")
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("base_path must not contain ..")
	}
	if strings.ContainsAny(cleaned, "*?[]<>:|\"\\") {
		return "", fmt.Errorf("base_path contains illegal characters")
	}
	return cleaned, nil
}
