package middleware

import (
	"context"
	"time"

	"github.com/homej-top/dbridge/internal/semantic/engine"
	"go.uber.org/zap"
)

// LoggingQueryEngine 结构化日志中间件。
type LoggingQueryEngine struct {
	inner         engine.SemanticQueryEngine
	logger        *zap.Logger
	slowThreshold time.Duration
}

// NewLoggingQueryEngine 创建日志中间件。
func NewLoggingQueryEngine(inner engine.SemanticQueryEngine, logger *zap.Logger) *LoggingQueryEngine {
	return &LoggingQueryEngine{inner: inner, logger: logger, slowThreshold: 5 * time.Second}
}

// Query 执行查询并记录日志。
func (s *LoggingQueryEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	start := time.Now()

	result, err := s.inner.Query(ctx, req)
	duration := time.Since(start)

	fields := []zap.Field{
		zap.String("engine", req.Engine),
		zap.Strings("measures", req.Measures),
		zap.Strings("dimensions", req.Dimensions),
		zap.Duration("duration", duration),
	}
	if result != nil {
		fields = append(fields,
			zap.Int64("rows", result.TotalRows),
			zap.Bool("cache_hit", result.CacheHit),
		)
	}

	if err != nil {
		fields = append(fields, zap.Error(err))
		s.logger.Error("semantic query failed", fields...)
	} else if duration > s.slowThreshold {
		s.logger.Warn("slow semantic query", fields...)
	} else {
		s.logger.Info("semantic query completed", fields...)
	}

	return result, err
}

func (s *LoggingQueryEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return s.inner.ListCubes(ctx, filter)
}

func (s *LoggingQueryEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) {
	return s.inner.GetCube(ctx, name)
}

func (s *LoggingQueryEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return s.inner.ListMeasures(ctx, cubeName)
}

func (s *LoggingQueryEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return s.inner.ListDimensions(ctx, cubeName)
}

func (s *LoggingQueryEngine) Capabilities() *engine.EngineCapabilities {
	return s.inner.Capabilities()
}

func (s *LoggingQueryEngine) Ping(ctx context.Context) error {
	return s.inner.Ping(ctx)
}
