package adapter

import (
	"context"
	"fmt"
	"sync"

	"github.com/homej-top/dbridge/internal/semantic/engine"
	"go.uber.org/zap"
)

// Registry 适配器注册表。
type Registry struct {
	mu             sync.RWMutex
	adapters       map[string]engine.SemanticQueryEngine
	defaultAdapter string
}

// NewRegistry 创建适配器注册表。
func NewRegistry() *Registry {
	return &Registry{
		adapters: make(map[string]engine.SemanticQueryEngine),
	}
}

// Register 注册适配器。
func (r *Registry) Register(name string, adapter engine.SemanticQueryEngine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.adapters[name] = adapter
}

// Get 获取适配器。
func (r *Registry) Get(name string) (engine.SemanticQueryEngine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	adapter, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("adapter not found: %s", name)
	}
	return adapter, nil
}

// GetDefault 获取默认适配器。
func (r *Registry) GetDefault() (engine.SemanticQueryEngine, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.defaultAdapter == "" {
		return nil, fmt.Errorf("no default adapter configured")
	}
	adapter, ok := r.adapters[r.defaultAdapter]
	if !ok {
		return nil, fmt.Errorf("default adapter not found: %s", r.defaultAdapter)
	}
	return adapter, nil
}

// SetDefault 设置默认适配器。
func (r *Registry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.adapters[name]; !ok {
		return fmt.Errorf("adapter not found: %s", name)
	}
	r.defaultAdapter = name
	return nil
}

// List 列出所有已注册的适配器名称。
func (r *Registry) List() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.adapters))
	for name := range r.adapters {
		names = append(names, name)
	}
	return names
}

// InitAll 初始化所有适配器。单个适配器失败时记录警告但继续初始化其他适配器。
func (r *Registry) InitAll(ctx context.Context, configs map[string]engine.AdapterConfig, deps *engine.AdapterDeps) error {
	var lastErr error
	successCount := 0
	var logger *zap.Logger
	if deps != nil {
		logger, _ = deps.Logger.(*zap.Logger)
	}
	for name, cfg := range configs {
		adapter, err := CreateAdapter(cfg.Type)
		if err != nil {
			lastErr = fmt.Errorf("create adapter %s failed: %w", name, err)
			if logger != nil {
				logger.Warn("skip adapter", zap.String("name", name), zap.Error(lastErr))
			}
			continue
		}

		// 先注入依赖，再初始化（Init 可能依赖注入的 DB/Logger 等）
		if injector, ok := adapter.(interface{ InjectDeps(*engine.AdapterDeps) }); ok {
			injector.InjectDeps(deps)
		}

		if initializer, ok := adapter.(engine.Initializer); ok {
			if err := initializer.Init(ctx, cfg); err != nil {
				lastErr = fmt.Errorf("init adapter %s failed: %w", name, err)
				if logger != nil {
					logger.Warn("skip adapter", zap.String("name", name), zap.Error(lastErr))
				}
				continue
			}
		}

		r.Register(name, adapter)
		successCount++
	}
	if successCount == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

// CloseAll 关闭所有适配器。
func (r *Registry) CloseAll() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, adapter := range r.adapters {
		if closer, ok := adapter.(interface{ Close() error }); ok {
			if err := closer.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateAdapter 根据类型创建适配器。
func CreateAdapter(adapterType string) (engine.SemanticQueryEngine, error) {
	switch adapterType {
	case "local":
		return NewLocalAdapter(), nil
	case "cubejs":
		return NewCubeJSAdapter(), nil
	default:
		return nil, fmt.Errorf("unknown adapter type: %s", adapterType)
	}
}
