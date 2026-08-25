package storage

import (
	"context"
	"fmt"
	"io"
	"log"
	"sync"
	"time"
)

// HealthChecker 健康检查接口，FileStorage 可选实现
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// ProfileManager 管理多套 FileStorage 实例
type ProfileManager struct {
	mu             sync.RWMutex
	profiles       map[string]FileStorage
	profileInfo    map[string]ProfileInfo
	codeToName     map[string]string
	defaultName    string
	healthStop     chan struct{}
	healthInterval time.Duration
	healthLogger   func(format string, args ...interface{})
}

// NewProfileManager 创建管理器
func NewProfileManager() *ProfileManager {
	return &ProfileManager{
		profiles:    make(map[string]FileStorage),
		profileInfo: make(map[string]ProfileInfo),
		codeToName:  make(map[string]string),
	}
}

// Register 注册一个 Profile 实例
func (m *ProfileManager) Register(name, code, backend string, fs FileStorage, summary map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profiles[name]; ok {
		return fmt.Errorf("storage: profile '%s' already registered", name)
	}
	if code != "" {
		if existing, ok := m.codeToName[code]; ok {
			return fmt.Errorf("storage: code '%s' already used by profile '%s'", code, existing)
		}
		m.codeToName[code] = name
	}
	m.profiles[name] = fs
	m.profileInfo[name] = ProfileInfo{
		Name:    name,
		Code:    code,
		Backend: backend,
		Enabled: true,
		Summary: summary,
	}
	return nil
}

// SetDefault 设置默认 Profile
func (m *ProfileManager) SetDefault(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profiles[name]; !ok {
		return fmt.Errorf("storage: profile '%s' not found", name)
	}
	m.defaultName = name
	return nil
}

// GetDefault 获取默认的 FileStorage 实例
func (m *ProfileManager) GetDefault() FileStorage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[m.defaultName]
}

// Get 按名称获取 FileStorage 实例
func (m *ProfileManager) Get(name string) FileStorage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.profiles[name]
}

// GetByCode 按代码获取 FileStorage 实例
func (m *ProfileManager) GetByCode(code string) FileStorage {
	m.mu.RLock()
	defer m.mu.RUnlock()
	name := m.codeToName[code]
	if name == "" {
		return nil
	}
	return m.profiles[name]
}

// ListProfiles 列出所有 Profile 信息
func (m *ProfileManager) ListProfiles() []ProfileInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]ProfileInfo, 0, len(m.profileInfo))
	for name, info := range m.profileInfo {
		info.IsDefault = (name == m.defaultName)
		result = append(result, info)
	}
	return result
}

// GetDefaultName 返回当前默认 Profile 名称
func (m *ProfileManager) GetDefaultName() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.defaultName
}

// StartHealthCheck 启动健康探活协程，每 interval 对所有 Profile 执行探活
func (m *ProfileManager) StartHealthCheck(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	m.healthInterval = interval
	m.healthStop = make(chan struct{})
	if m.healthLogger == nil {
		m.healthLogger = func(format string, args ...interface{}) {
			log.Printf("[storage-health] "+format, args...)
		}
	}

	go func() {
		ticker := time.NewTicker(m.healthInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.runHealthCheck()
			case <-m.healthStop:
				return
			}
		}
	}()
}

// SetHealthLogger 设置健康检查日志输出
func (m *ProfileManager) SetHealthLogger(logger func(format string, args ...interface{})) {
	m.healthLogger = logger
}

func (m *ProfileManager) runHealthCheck() {
	m.mu.RLock()
	// 快照当前所有 profile 引用
	profiles := make(map[string]FileStorage, len(m.profiles))
	for name, fs := range m.profiles {
		profiles[name] = fs
	}
	m.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for name, fs := range profiles {
		if checker, ok := fs.(HealthChecker); ok {
			if err := checker.HealthCheck(ctx); err != nil {
				m.healthLogger("profile [%s] health check FAILED: %v", name, err)
			} else {
				m.healthLogger("profile [%s] health check OK", name)
			}
		}
	}
}

// StopHealthCheck 停止健康探活协程
func (m *ProfileManager) StopHealthCheck() {
	if m.healthStop != nil {
		close(m.healthStop)
		m.healthStop = nil
	}
}

// Unregister 注销一个 Profile 实例（不能删除当前默认）
func (m *ProfileManager) Unregister(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if name == m.defaultName {
		return fmt.Errorf("storage: cannot unregister default profile '%s', switch default first", name)
	}
	fs, ok := m.profiles[name]
	if !ok {
		return fmt.Errorf("storage: profile '%s' not found", name)
	}
	if closer, ok := fs.(io.Closer); ok {
		_ = closer.Close()
	}
	delete(m.profiles, name)
	if info, ok := m.profileInfo[name]; ok {
		delete(m.codeToName, info.Code)
	}
	delete(m.profileInfo, name)
	return nil
}

// UpdateSummary 更新 Profile 摘要信息
func (m *ProfileManager) UpdateSummary(name string, summary map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.profileInfo[name]; !ok {
		return fmt.Errorf("storage: profile '%s' not found", name)
	}
	info := m.profileInfo[name]
	info.Summary = summary
	m.profileInfo[name] = info
	return nil
}

// Replace 原子替换一个 Profile 实例（关闭旧实例、注册新实例，一步完成）
func (m *ProfileManager) Replace(name, code, backend string, newFS FileStorage, summary map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 关闭旧实例
	if old, ok := m.profiles[name]; ok {
		if closer, ok := old.(io.Closer); ok {
			_ = closer.Close()
		}
		// 清理旧 code 映射
		if oldInfo, ok := m.profileInfo[name]; ok && oldInfo.Code != code {
			delete(m.codeToName, oldInfo.Code)
		}
	}

	// 注册新实例
	m.profiles[name] = newFS
	m.profileInfo[name] = ProfileInfo{
		Name:    name,
		Code:    code,
		Backend: backend,
		Enabled: true,
		Summary: summary,
	}
	if code != "" {
		m.codeToName[code] = name
	}
	return nil
}

// Shutdown 关闭所有 Profile 实例，释放资源
func (m *ProfileManager) Shutdown() error {
	m.StopHealthCheck()
	m.mu.Lock()
	defer m.mu.Unlock()
	var lastErr error
	for name, fs := range m.profiles {
		if closer, ok := fs.(io.Closer); ok {
			if err := closer.Close(); err != nil {
				lastErr = err
			}
		}
		delete(m.profiles, name)
		if info, ok := m.profileInfo[name]; ok {
			delete(m.codeToName, info.Code)
		}
	}
	m.profiles = make(map[string]FileStorage)
	m.profileInfo = make(map[string]ProfileInfo)
		m.codeToName = make(map[string]string)
	m.defaultName = ""
	return lastErr
}
