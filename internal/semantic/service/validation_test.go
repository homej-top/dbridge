package service

import (
	"testing"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

func TestValidateQueryRequest_ValidMember(t *testing.T) {
	req := &engine.QueryRequest{
		Measures:   []string{"orders.count"},
		Dimensions: []string{"orders.status"},
	}
	if err := ValidateQueryRequest(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateQueryRequest_InvalidMeasureFormat(t *testing.T) {
	cases := []string{"noDot", "a.b.c", "1cube.field", "cube.1field", ".field", "cube."}
	for _, m := range cases {
		req := &engine.QueryRequest{Measures: []string{m}}
		if err := ValidateQueryRequest(req); err == nil {
			t.Errorf("expected error for measure %q, got nil", m)
		}
	}
}

func TestValidateQueryRequest_InvalidDimensionFormat(t *testing.T) {
	req := &engine.QueryRequest{
		Measures:   []string{"orders.count"},
		Dimensions: []string{"bad format!"},
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for invalid dimension, got nil")
	}
}

func TestValidateQueryRequest_InvalidOperator(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "like", Values: []string{"active"}},
		},
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for unsupported operator, got nil")
	}
}

func TestValidateQueryRequest_OperatorRequiresValues(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "equals", Values: nil},
		},
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for operator requiring values with empty values, got nil")
	}
}

func TestValidateQueryRequest_SetOperatorNoValues(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "set"},
		},
	}
	if err := ValidateQueryRequest(req); err != nil {
		t.Fatalf("set/notSet should not require values, got %v", err)
	}
}

func TestValidateQueryRequest_LimitExceedsMax(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Limit:    20000,
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for limit > 10000, got nil")
	}
}

func TestValidateQueryRequest_NegativeOffset(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Offset:   -1,
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for negative offset, got nil")
	}
}

func TestValidateQueryRequest_NilRequest(t *testing.T) {
	if err := ValidateQueryRequest(nil); err == nil {
		t.Fatal("expected error for nil request, got nil")
	}
}

func TestValidateQueryRequest_InvalidOrderDirection(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Order:    []engine.OrderItem{{Member: "orders.count", Direction: "sideways"}},
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for invalid order direction, got nil")
	}
}

func TestValidateQueryRequest_ValidOrderDirection(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Order:    []engine.OrderItem{{Member: "orders.count", Direction: "ASC"}},
	}
	if err := ValidateQueryRequest(req); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateQueryRequest_TimeDimensionFormat(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		TimeDimensions: []engine.TimeDimensionQuery{
			{Dimension: "bad format", DateRange: []string{"2024-01-01", "2024-12-31"}},
		},
	}
	if err := ValidateQueryRequest(req); err == nil {
		t.Fatal("expected error for invalid time dimension format, got nil")
	}
}
