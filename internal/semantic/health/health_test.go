package health

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dbridge/dbridge/internal/semantic/engine"
	"github.com/stretchr/testify/assert"
)

type mockEngine struct {
	pingErr error
	delay   time.Duration
}

func (m *mockEngine) Query(ctx context.Context, req *engine.QueryRequest) (*engine.QueryResult, error) {
	return nil, nil
}
func (m *mockEngine) ListCubes(ctx context.Context, filter *engine.CubeFilter) ([]*engine.CubeMeta, error) {
	return nil, nil
}
func (m *mockEngine) GetCube(ctx context.Context, name string) (*engine.CubeDetail, error) { return nil, nil }
func (m *mockEngine) ListMeasures(ctx context.Context, cubeName string) ([]*engine.MeasureMeta, error) {
	return nil, nil
}
func (m *mockEngine) ListDimensions(ctx context.Context, cubeName string) ([]*engine.DimensionMeta, error) {
	return nil, nil
}
func (m *mockEngine) Capabilities() *engine.EngineCapabilities { return &engine.EngineCapabilities{} }
func (m *mockEngine) Ping(ctx context.Context) error {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.pingErr
}

func TestHealthChecker_AllHealthy(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"local": &mockEngine{},
	}
	checker := NewHealthChecker(engines)

	statuses := checker.CheckAll(context.Background())

	assert.Len(t, statuses, 1)
	assert.Equal(t, "healthy", statuses["local"].Status)
	assert.Equal(t, int64(0), statuses["local"].ErrorCount)
}

func TestHealthChecker_OneUnhealthy(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"local":   &mockEngine{},
		"cubejs":  &mockEngine{pingErr: fmt.Errorf("connection refused")},
	}
	checker := NewHealthChecker(engines)

	statuses := checker.CheckAll(context.Background())

	assert.Equal(t, "healthy", statuses["local"].Status)
	assert.Equal(t, "unhealthy", statuses["cubejs"].Status)
	assert.Equal(t, "connection refused", statuses["cubejs"].Message)
	assert.Equal(t, int64(1), statuses["cubejs"].ErrorCount)
}

func TestHealthChecker_ErrorCountAccumulates(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"failing": &mockEngine{pingErr: fmt.Errorf("down")},
	}
	checker := NewHealthChecker(engines)

	checker.CheckAll(context.Background())
	checker.CheckAll(context.Background())
	statuses := checker.CheckAll(context.Background())

	assert.Equal(t, int64(3), statuses["failing"].ErrorCount)
}

func TestHealthChecker_OverallStatus_Healthy(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"local": &mockEngine{},
	}
	checker := NewHealthChecker(engines)
	checker.CheckAll(context.Background())

	assert.Equal(t, "healthy", checker.OverallStatus())
}

func TestHealthChecker_OverallStatus_Unhealthy(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"local":  &mockEngine{},
		"cubejs": &mockEngine{pingErr: fmt.Errorf("down")},
	}
	checker := NewHealthChecker(engines)
	checker.CheckAll(context.Background())

	assert.Equal(t, "unhealthy", checker.OverallStatus())
}

func TestHealthChecker_IsHealthy(t *testing.T) {
	engines := map[string]engine.SemanticQueryEngine{
		"local": &mockEngine{},
	}
	checker := NewHealthChecker(engines)

	assert.False(t, checker.IsHealthy("local")) // not checked yet
	checker.CheckAll(context.Background())
	assert.True(t, checker.IsHealthy("local"))
	assert.False(t, checker.IsHealthy("nonexistent"))
}

func TestHealthChecker_Empty(t *testing.T) {
	checker := NewHealthChecker(map[string]engine.SemanticQueryEngine{})
	assert.Equal(t, "unknown", checker.OverallStatus())
}
