package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/adapter"
	"github.com/dbridge/dbridge/internal/semantic/engine"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// SemanticQueryService 语义查询编排服务。
type SemanticQueryService struct {
	registry        *adapter.Registry
	auditLogger     *AuditLogger
	rowLevelFilters []engine.RowLevelFilter
	logger          *zap.Logger
}

// NewSemanticQueryService 创建语义查询编排服务。
func NewSemanticQueryService(registry *adapter.Registry, db *gorm.DB, logger *zap.Logger) *SemanticQueryService {
	return &SemanticQueryService{
		registry:    registry,
		auditLogger: NewAuditLogger(db, logger),
		logger:      logger,
	}
}

// SetRowLevelFilters 配置行级过滤规则。
func (s *SemanticQueryService) SetRowLevelFilters(filters []engine.RowLevelFilter) {
	s.rowLevelFilters = filters
}

// Query 编排完整查询流程：校验 → 路由 → 行级过滤 → 执行 → 审计。
func (s *SemanticQueryService) Query(ctx context.Context, req *engine.QueryRequest, userRoles []string, userID, tenantID, username, ip, ua string) (*engine.QueryResult, error) {
	start := time.Now()

	if err := ValidateQueryRequest(req); err != nil {
		return nil, fmt.Errorf("validation failed: %w", err)
	}

	eng, err := s.resolveEngine(req)
	if err != nil {
		return nil, err
	}

	if len(s.rowLevelFilters) > 0 && len(req.Measures) > 0 {
		cubeName := extractCubeName(req.Measures[0])
		extraFilters := engine.ResolveRowLevelFilters(s.rowLevelFilters, req, cubeName, userRoles)
		req.Filters = append(req.Filters, extraFilters...)
	}

	result, err := eng.Query(ctx, req)

	durationMs := time.Since(start).Milliseconds()

	status := "success"
	var errMsg string
	if err != nil {
		status = "failure"
		errMsg = err.Error()
	}

	var rowCount int64
	var generatedSQL string
	if result != nil {
		rowCount = result.TotalRows
		generatedSQL = result.SQL
	}

	filtersJSON, _ := json.Marshal(req.Filters)

	s.auditLogger.Log(ctx, AuditEntry{
		UserID:       userID,
		TenantID:     tenantID,
		Username:     username,
		Engine:       req.Engine,
		DataSourceID: req.DataSourceID,
		Measures:     req.Measures,
		Dimensions:   req.Dimensions,
		Filters:      filtersJSON,
		GeneratedSQL: generatedSQL,
		DurationMs:   durationMs,
		RowCount:     rowCount,
		Status:       status,
		ErrorMessage: errMsg,
		IP:           ip,
		UserAgent:    ua,
	})

	if err != nil {
		return nil, err
	}
	return result, nil
}

// Admin 获取管理引擎（类型断言为 SemanticAdminEngine）。
func (s *SemanticQueryService) Admin(engineName string) (engine.SemanticAdminEngine, error) {
	eng, err := s.registry.Get(engineName)
	if err != nil {
		return nil, err
	}
	admin, ok := eng.(engine.SemanticAdminEngine)
	if !ok {
		return nil, fmt.Errorf("engine %q does not support admin operations", engineName)
	}
	return admin, nil
}

func (s *SemanticQueryService) resolveEngine(req *engine.QueryRequest) (engine.SemanticQueryEngine, error) {
	if override, ok := req.EngineOverrides["engine"]; ok && override != "" {
		return s.registry.Get(override)
	}
	if req.Engine != "" {
		return s.registry.Get(req.Engine)
	}
	return s.registry.GetDefault()
}

// Registry 暴露适配器注册表供 Handler 使用。
func (s *SemanticQueryService) Registry() *adapter.Registry {
	return s.registry
}

// Engine 获取指定名称的查询引擎；name 为空时返回默认引擎。
func (s *SemanticQueryService) Engine(name string) (engine.SemanticQueryEngine, error) {
	if name == "" {
		return s.registry.GetDefault()
	}
	return s.registry.Get(name)
}

func extractCubeName(member string) string {
	parts := strings.SplitN(member, ".", 2)
	return parts[0]
}
