package adapter

import (
	"strings"
	"testing"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

func newTestRegistry() *LocalRegistry {
	r := &LocalRegistry{
		cubes: make(map[string]*engine.CubeDefinition),
	}
	r.cubes["orders"] = &engine.CubeDefinition{
		Name:         "orders",
		DisplayName:  "Orders",
		DataSourceID: "ds-1",
		SQLTable:     "orders",
		Schema:       "public",
		Measures: []*engine.MeasureDetail{
			{
				MeasureMeta: &engine.MeasureMeta{Name: "total_amount", DisplayName: "Total Amount", Type: "sum"},
				SQL:         "{amount}",
			},
			{
				MeasureMeta: &engine.MeasureMeta{Name: "order_count", DisplayName: "Order Count", Type: "count"},
				SQL:         "{id}",
			},
		},
		Dimensions: []*engine.DimensionDetail{
			{
				DimensionMeta: &engine.DimensionMeta{Name: "status", DisplayName: "Status", Type: "string"},
				SQL:           "{status}",
			},
			{
				DimensionMeta: &engine.DimensionMeta{Name: "created_at", DisplayName: "Created At", Type: "time"},
				SQL:           "{created_at}",
			},
		},
	}
	return r
}

func TestCompile_SimpleMeasure(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"orders.total_amount"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "SUM(orders.amount)") {
		t.Errorf("expected SUM(orders.amount), got: %s", sql)
	}
	if !strings.Contains(sql, "FROM public.orders AS orders") {
		t.Errorf("expected FROM public.orders AS orders, got: %s", sql)
	}
}

func TestCompile_MeasureAndDimension(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures:   []string{"orders.total_amount"},
		Dimensions: []string{"orders.status"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "orders.status AS `status`") {
		t.Errorf("expected dimension column, got: %s", sql)
	}
	if !strings.Contains(sql, "GROUP BY") {
		t.Errorf("expected GROUP BY, got: %s", sql)
	}
}

func TestCompile_Filter(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"orders.total_amount"},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "equals", Values: []string{"completed"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "WHERE") {
		t.Errorf("expected WHERE clause, got: %s", sql)
	}
	if !strings.Contains(sql, "orders.status = ?") {
		t.Errorf("expected filter expression, got: %s", sql)
	}
	if len(result.Args) != 1 || result.Args[0] != "completed" {
		t.Errorf("expected arg 'completed', got: %v", result.Args)
	}
}

func TestCompile_TimeDimension(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"orders.total_amount"},
		TimeDimensions: []engine.TimeDimensionQuery{
			{Dimension: "orders.created_at", Granularity: "month", DateRange: []string{"2026-01-01", "2026-12-31"}},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "DATE_FORMAT") {
		t.Errorf("expected DATE_FORMAT for month granularity, got: %s", sql)
	}
	if !strings.Contains(sql, ">= ?") {
		t.Errorf("expected date range filter, got: %s", sql)
	}
}

func TestCompile_MultipleMeasures(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"orders.total_amount", "orders.order_count"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "SUM(orders.amount)") {
		t.Errorf("expected SUM, got: %s", sql)
	}
	if !strings.Contains(sql, "COUNT(orders.id)") {
		t.Errorf("expected COUNT, got: %s", sql)
	}
}

func TestCompile_OrderLimit(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	result, err := c.Compile(&engine.QueryRequest{
		Measures:   []string{"orders.total_amount"},
		Dimensions: []string{"orders.status"},
		Order:      []engine.OrderItem{{Member: "orders.total_amount", Direction: "desc"}},
		Limit:      10,
		Offset:     20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sql := result.SQL
	if !strings.Contains(sql, "ORDER BY") {
		t.Errorf("expected ORDER BY, got: %s", sql)
	}
	if !strings.Contains(sql, "DESC") {
		t.Errorf("expected DESC, got: %s", sql)
	}
	if !strings.Contains(sql, "LIMIT 10") {
		t.Errorf("expected LIMIT 10, got: %s", sql)
	}
	if !strings.Contains(sql, "OFFSET 20") {
		t.Errorf("expected OFFSET 20, got: %s", sql)
	}
}

func TestCompile_UnknownMeasure(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	_, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"orders.nonexistent"},
	})
	if err == nil {
		t.Fatal("expected error for unknown measure")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error should mention the measure name, got: %v", err)
	}
}

func TestCompile_UnknownCube(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	_, err := c.Compile(&engine.QueryRequest{
		Measures: []string{"nonexistent.some_measure"},
	})
	if err == nil {
		t.Fatal("expected error for unknown cube")
	}
	if !strings.Contains(err.Error(), "cube not found") {
		t.Errorf("error should mention cube not found, got: %v", err)
	}
}

func TestCompile_NoMeasures(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	_, err := c.Compile(&engine.QueryRequest{})
	if err == nil {
		t.Fatal("expected error for empty measures")
	}
}

func TestCompile_FilterOperators(t *testing.T) {
	r := newTestRegistry()
	c := NewLocalCompiler(r)

	tests := []struct {
		operator string
		values   []string
		contains string
	}{
		{"equals", []string{"x"}, "= ?"},
		{"notEquals", []string{"x"}, "!= ?"},
		{"contains", []string{"x"}, "LIKE ?"},
		{"gt", []string{"100"}, "> ?"},
		{"lt", []string{"100"}, "< ?"},
		{"in", []string{"a", "b"}, "IN (?, ?)"},
		{"set", nil, "IS NOT NULL"},
		{"notSet", nil, "IS NULL"},
	}

	for _, tt := range tests {
		t.Run(tt.operator, func(t *testing.T) {
			result, err := c.Compile(&engine.QueryRequest{
				Measures: []string{"orders.total_amount"},
				Filters:  []engine.Filter{{Member: "orders.status", Operator: tt.operator, Values: tt.values}},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !strings.Contains(result.SQL, tt.contains) {
				t.Errorf("operator %s: expected %q in SQL, got: %s", tt.operator, tt.contains, result.SQL)
			}
		})
	}
}
