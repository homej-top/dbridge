package service

import (
	"context"
	"testing"

	"github.com/dbridge/dbridge/internal/semantic/engine"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

func TestQueryService_ValidationFailure(t *testing.T) {
	svc := &SemanticQueryService{
		auditLogger: NewAuditLogger(&gorm.DB{}, zap.NewNop()),
		logger:      zap.NewNop(),
	}

	req := &engine.QueryRequest{
		Measures: []string{"invalid format!"},
	}

	_, err := svc.Query(context.Background(), req, nil, "user1", "tenant1", "admin", "127.0.0.1", "test")
	if err == nil {
		t.Fatal("expected validation error, got nil")
	}
}

func TestResolveRowLevelFilters_AppendsFilters(t *testing.T) {
	rlFilters := []engine.RowLevelFilter{
		{
			CubeName: "orders",
			Roles:    []string{"analyst"},
			Filters: []engine.Filter{
				{Member: "orders.region", Operator: "equals", Values: []string{"${user.region}"}},
			},
		},
	}

	req := &engine.QueryRequest{
		Measures:    []string{"orders.count"},
		UserContext: map[string]string{"region": "US-East"},
	}

	resolved := engine.ResolveRowLevelFilters(rlFilters, req, "orders", []string{"analyst"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 resolved filter, got %d", len(resolved))
	}
	if resolved[0].Values[0] != "US-East" {
		t.Fatalf("expected variable replacement to 'US-East', got %q", resolved[0].Values[0])
	}
}

func TestResolveRowLevelFilters_NoMatchByCube(t *testing.T) {
	rlFilters := []engine.RowLevelFilter{
		{
			CubeName: "revenue",
			Filters: []engine.Filter{
				{Member: "revenue.dept", Operator: "equals", Values: []string{"sales"}},
			},
		},
	}

	req := &engine.QueryRequest{Measures: []string{"orders.count"}}
	resolved := engine.ResolveRowLevelFilters(rlFilters, req, "orders", []string{"analyst"})
	if len(resolved) != 0 {
		t.Fatalf("expected 0 filters for non-matching cube, got %d", len(resolved))
	}
}

func TestResolveRowLevelFilters_NoMatchByRole(t *testing.T) {
	rlFilters := []engine.RowLevelFilter{
		{
			CubeName: "orders",
			Roles:    []string{"admin"},
			Filters: []engine.Filter{
				{Member: "orders.region", Operator: "equals", Values: []string{"US"}},
			},
		},
	}

	req := &engine.QueryRequest{Measures: []string{"orders.count"}}
	resolved := engine.ResolveRowLevelFilters(rlFilters, req, "orders", []string{"analyst"})
	if len(resolved) != 0 {
		t.Fatalf("expected 0 filters for non-matching role, got %d", len(resolved))
	}
}

func TestResolveRowLevelFilters_EmptyRolesMatchAll(t *testing.T) {
	rlFilters := []engine.RowLevelFilter{
		{
			CubeName: "orders",
			Roles:    nil,
			Filters: []engine.Filter{
				{Member: "orders.active", Operator: "equals", Values: []string{"true"}},
			},
		},
	}

	req := &engine.QueryRequest{Measures: []string{"orders.count"}}
	resolved := engine.ResolveRowLevelFilters(rlFilters, req, "orders", []string{"any_role"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 filter when roles is empty (match all), got %d", len(resolved))
	}
}

func TestResolveRowLevelFilters_VariableReplacement(t *testing.T) {
	rlFilters := []engine.RowLevelFilter{
		{
			Filters: []engine.Filter{
				{
					Member:   "orders.owner",
					Operator: "equals",
					Values:   []string{"${user.id}"},
				},
			},
		},
	}

	req := &engine.QueryRequest{
		Measures:    []string{"orders.count"},
		UserContext: map[string]string{"id": "user-123"},
	}

	resolved := engine.ResolveRowLevelFilters(rlFilters, req, "orders", []string{"analyst"})
	if len(resolved) != 1 {
		t.Fatalf("expected 1 filter, got %d", len(resolved))
	}
	if resolved[0].Values[0] != "user-123" {
		t.Fatalf("expected 'user-123', got %q", resolved[0].Values[0])
	}
}
