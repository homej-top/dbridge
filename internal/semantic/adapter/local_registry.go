package adapter

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/homej-top/dbridge/internal/repository"
	"github.com/homej-top/dbridge/internal/semantic/engine"
	"gorm.io/gorm"
)

// LocalRegistry 本地 Cube 注册表，从 DB 加载定义到内存。
type LocalRegistry struct {
	mu    sync.RWMutex
	cubes map[string]*engine.CubeDefinition
	db    *gorm.DB
}

// NewLocalRegistry 创建本地注册表。
func NewLocalRegistry(db *gorm.DB) *LocalRegistry {
	return &LocalRegistry{
		cubes: make(map[string]*engine.CubeDefinition),
		db:    db,
	}
}

// Load 从数据库加载所有 Cube 定义。
func (r *LocalRegistry) Load() error {
	var cubes []repository.SemanticCube
	if err := r.db.Find(&cubes).Error; err != nil {
		return fmt.Errorf("load semantic cubes: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.cubes = make(map[string]*engine.CubeDefinition, len(cubes))
	for i := range cubes {
		def, err := r.toDefinition(&cubes[i])
		if err != nil {
			return fmt.Errorf("parse cube %q: %w", cubes[i].Name, err)
		}
		r.cubes[def.Name] = def
	}
	return nil
}

// GetCube 获取 Cube 定义。
func (r *LocalRegistry) GetCube(name string) (*engine.CubeDefinition, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.cubes[name]
	return def, ok
}

// ListCubes 列出 Cube 元数据摘要。
func (r *LocalRegistry) ListCubes(filter *engine.CubeFilter) []*engine.CubeMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*engine.CubeMeta
	for _, def := range r.cubes {
		if filter != nil {
			if filter.Search != "" && !strings.Contains(strings.ToLower(def.Name), strings.ToLower(filter.Search)) &&
				!strings.Contains(strings.ToLower(def.DisplayName), strings.ToLower(filter.Search)) {
				continue
			}
			if filter.DataSourceID != "" && def.DataSourceID != filter.DataSourceID {
				continue
			}
		}

		measureNames := make([]string, len(def.Measures))
		for i, m := range def.Measures {
			measureNames[i] = m.Name
		}
		dimNames := make([]string, len(def.Dimensions))
		for i, d := range def.Dimensions {
			dimNames[i] = d.Name
		}

		result = append(result, &engine.CubeMeta{
			Name:        def.Name,
			DisplayName: def.DisplayName,
			Description: def.Description,
			Measures:    measureNames,
			Dimensions:  dimNames,
			DataSource:  def.DataSourceID,
		})
	}
	return result
}

// GetCubeDetail 获取 Cube 完整详情。
func (r *LocalRegistry) GetCubeDetail(name string) (*engine.CubeDetail, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.cubes[name]
	if !ok {
		return nil, false
	}

	measureNames := make([]string, len(def.Measures))
	for i, m := range def.Measures {
		measureNames[i] = m.Name
	}
	dimNames := make([]string, len(def.Dimensions))
	for i, d := range def.Dimensions {
		dimNames[i] = d.Name
	}

	return &engine.CubeDetail{
		CubeMeta: &engine.CubeMeta{
			Name:        def.Name,
			DisplayName: def.DisplayName,
			Description: def.Description,
			Measures:    measureNames,
			Dimensions:  dimNames,
			DataSource:  def.DataSourceID,
		},
		SQLTable:        def.SQLTable,
		SQLQuery:        def.SQLQuery,
		Measures:        def.Measures,
		Dimensions:      def.Dimensions,
		Joins:           def.Joins,
		PreAggregations: def.PreAggregations,
	}, true
}

// ListMeasures 列出指定 Cube 的度量摘要。
func (r *LocalRegistry) ListMeasures(cubeName string) ([]*engine.MeasureMeta, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.cubes[cubeName]
	if !ok {
		return nil, false
	}

	result := make([]*engine.MeasureMeta, len(def.Measures))
	for i, m := range def.Measures {
		result[i] = m.MeasureMeta
	}
	return result, true
}

// ListDimensions 列出指定 Cube 的维度摘要。
func (r *LocalRegistry) ListDimensions(cubeName string) ([]*engine.DimensionMeta, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.cubes[cubeName]
	if !ok {
		return nil, false
	}

	result := make([]*engine.DimensionMeta, len(def.Dimensions))
	for i, d := range def.Dimensions {
		result[i] = d.DimensionMeta
	}
	return result, true
}

// Save 保存 Cube 定义到数据库（UPSERT）。
func (r *LocalRegistry) Save(def *engine.CubeDefinition) error {
	model, err := r.fromDefinition(def)
	if err != nil {
		return err
	}

	var existing repository.SemanticCube
	result := r.db.Where("name = ?", def.Name).First(&existing)
	if result.Error == gorm.ErrRecordNotFound {
		if err := r.db.Create(model).Error; err != nil {
			return err
		}
	} else if result.Error != nil {
		return result.Error
	} else {
		model.ID = existing.ID
		if err := r.db.Save(model).Error; err != nil {
			return err
		}
	}

	return r.Load()
}

// Delete 从数据库删除 Cube。
func (r *LocalRegistry) Delete(name string) error {
	if err := r.db.Where("name = ?", name).Delete(&repository.SemanticCube{}).Error; err != nil {
		return err
	}
	return r.Load()
}

func (r *LocalRegistry) toDefinition(m *repository.SemanticCube) (*engine.CubeDefinition, error) {
	def := &engine.CubeDefinition{
		Name:         m.Name,
		DisplayName:  m.DisplayName,
		Description:  m.Description,
		DataSourceID: m.DataSourceID,
		Database:     m.Database,
		Schema:       m.SchemaName,
		SQLTable:     m.SQLTable,
		SQLQuery:     m.SQLQuery,
		Version:      m.Version,
	}

	if m.MeasuresJSON != "" {
		if err := json.Unmarshal([]byte(m.MeasuresJSON), &def.Measures); err != nil {
			return nil, fmt.Errorf("unmarshal measures: %w", err)
		}
	}
	if m.DimensionsJSON != "" {
		if err := json.Unmarshal([]byte(m.DimensionsJSON), &def.Dimensions); err != nil {
			return nil, fmt.Errorf("unmarshal dimensions: %w", err)
		}
	}
	if m.JoinsJSON != "" {
		if err := json.Unmarshal([]byte(m.JoinsJSON), &def.Joins); err != nil {
			return nil, fmt.Errorf("unmarshal joins: %w", err)
		}
	}
	if m.PreAggsJSON != "" {
		if err := json.Unmarshal([]byte(m.PreAggsJSON), &def.PreAggregations); err != nil {
			return nil, fmt.Errorf("unmarshal pre_aggregations: %w", err)
		}
	}
	if m.UpstreamTables != "" {
		if err := json.Unmarshal([]byte(m.UpstreamTables), &def.UpstreamTables); err != nil {
			return nil, fmt.Errorf("unmarshal upstream_tables: %w", err)
		}
	}

	return def, nil
}

func (r *LocalRegistry) fromDefinition(def *engine.CubeDefinition) (*repository.SemanticCube, error) {
	m := &repository.SemanticCube{
		Name:         def.Name,
		DisplayName:  def.DisplayName,
		Description:  def.Description,
		DataSourceID: def.DataSourceID,
		Database:     def.Database,
		SchemaName:   def.Schema,
		SQLTable:     def.SQLTable,
		SQLQuery:     def.SQLQuery,
		Version:      def.Version,
	}

	if len(def.Measures) > 0 {
		data, err := json.Marshal(def.Measures)
		if err != nil {
			return nil, fmt.Errorf("marshal measures: %w", err)
		}
		m.MeasuresJSON = string(data)
	}
	if len(def.Dimensions) > 0 {
		data, err := json.Marshal(def.Dimensions)
		if err != nil {
			return nil, fmt.Errorf("marshal dimensions: %w", err)
		}
		m.DimensionsJSON = string(data)
	}
	if len(def.Joins) > 0 {
		data, err := json.Marshal(def.Joins)
		if err != nil {
			return nil, fmt.Errorf("marshal joins: %w", err)
		}
		m.JoinsJSON = string(data)
	}
	if len(def.PreAggregations) > 0 {
		data, err := json.Marshal(def.PreAggregations)
		if err != nil {
			return nil, fmt.Errorf("marshal pre_aggregations: %w", err)
		}
		m.PreAggsJSON = string(data)
	}
	if len(def.UpstreamTables) > 0 {
		data, err := json.Marshal(def.UpstreamTables)
		if err != nil {
			return nil, fmt.Errorf("marshal upstream_tables: %w", err)
		}
		m.UpstreamTables = string(data)
	}

	return m, nil
}
