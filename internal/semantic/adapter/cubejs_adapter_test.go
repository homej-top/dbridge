package adapter

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/homej-top/dbridge/internal/semantic/adapter/cubejs"
	"github.com/homej-top/dbridge/internal/semantic/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ─── 测试辅助 ────────────────────────────────────────────────────────────────

var testMetaResponse = cubejs.MetaResponse{
	Cubes: []cubejs.Cube{
		{
			Name:        "orders",
			Title:       "Orders",
			Description: "Order data",
			DataSource:  "default",
			Measures: []cubejs.Member{
				{Name: "orders.count", Title: "Orders Count", Description: "Total orders", Type: "number"},
				{Name: "orders.total", Title: "Total Amount", Description: "Sum of amount", Type: "number"},
			},
			Dimensions: []cubejs.Member{
				{Name: "orders.status", Title: "Status", Description: "Order status", Type: "string"},
				{Name: "orders.created_at", Title: "Created At", Description: "", Type: "time", PrimaryKey: true},
			},
			Joins: []cubejs.Join{
				{Name: "customers", Relationship: "belongsTo", JoinType: "left", SQL: "{customers}.id = {orders}.customer_id"},
			},
		},
		{
			Name:        "customers",
			Title:       "Customers",
			Description: "Customer data",
			DataSource:  "default",
			Measures: []cubejs.Member{
				{Name: "customers.count", Title: "Customers Count", Type: "number"},
			},
			Dimensions: []cubejs.Member{
				{Name: "customers.name", Title: "Name", Type: "string"},
			},
		},
	},
}

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/meta":
			json.NewEncoder(w).Encode(testMetaResponse)
		case "/v1/load":
			var query cubejs.Query
			json.NewDecoder(r.Body).Decode(&query)
			resp := cubejs.LoadResponse{
				Data: []map[string]interface{}{
					{"orders.status": "active", "orders.count": float64(10)},
					{"orders.status": "inactive", "orders.count": float64(5)},
				},
			}
			json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
}

func newTestAdapter(t *testing.T) (*CubeJSAdapter, *httptest.Server) {
	t.Helper()
	srv := newTestServer(t)
	adapter := &CubeJSAdapter{
		client: cubejs.NewClient(srv.URL, "test-token"),
		config: &engine.CubeJSConfig{APIURL: srv.URL, APIToken: "test-token"},
	}
	return adapter, srv
}

// ─── Init 测试 ───────────────────────────────────────────────────────────────

func TestCubeJSAdapter_Init(t *testing.T) {
	srv := newTestServer(t)
	defer srv.Close()

	a := NewCubeJSAdapter()
	err := a.Init(context.Background(), engine.AdapterConfig{
		Type:   "cubejs",
		CubeJS: &engine.CubeJSConfig{APIURL: srv.URL, APIToken: "token"},
	})
	assert.NoError(t, err)
}

func TestCubeJSAdapter_Init_MissingConfig(t *testing.T) {
	a := NewCubeJSAdapter()
	err := a.Init(context.Background(), engine.AdapterConfig{Type: "cubejs"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cubejs config is required")
}

func TestCubeJSAdapter_Init_MissingURL(t *testing.T) {
	a := NewCubeJSAdapter()
	err := a.Init(context.Background(), engine.AdapterConfig{
		Type:   "cubejs",
		CubeJS: &engine.CubeJSConfig{APIToken: "token"},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "api_url is required")
}

// ─── Capabilities 测试 ──────────────────────────────────────────────────────

func TestCubeJSAdapter_Capabilities(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	cap := a.Capabilities()
	assert.Equal(t, "cubejs", cap.Name)
	assert.True(t, cap.SupportsPreAgg)
	assert.True(t, cap.SupportsJoin)
	assert.True(t, cap.SupportsMultiSource)
	assert.True(t, cap.SupportsDynamicSQL)
	assert.False(t, cap.SupportsCreate)
	assert.False(t, cap.SupportsUpdate)
	assert.False(t, cap.SupportsDelete)
	assert.Contains(t, cap.SupportedAggTypes, "count")
	assert.Contains(t, cap.SupportedAggTypes, "running_total")
}

// ─── ListCubes 测试 ─────────────────────────────────────────────────────────

func TestCubeJSAdapter_ListCubes(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	cubes, err := a.ListCubes(context.Background(), nil)
	require.NoError(t, err)
	assert.Len(t, cubes, 2)

	assert.Equal(t, "orders", cubes[0].Name)
	assert.Equal(t, "Orders", cubes[0].DisplayName)
	assert.Equal(t, "Order data", cubes[0].Description)
	assert.Equal(t, []string{"orders.count", "orders.total"}, cubes[0].Measures)
	assert.Equal(t, []string{"orders.status", "orders.created_at"}, cubes[0].Dimensions)
	assert.Equal(t, "default", cubes[0].DataSource)
}

func TestCubeJSAdapter_ListCubes_Filter(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	cubes, err := a.ListCubes(context.Background(), &engine.CubeFilter{Search: "customer"})
	require.NoError(t, err)
	assert.Len(t, cubes, 1)
	assert.Equal(t, "customers", cubes[0].Name)
}

func TestCubeJSAdapter_ListCubes_FilterNoMatch(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	cubes, err := a.ListCubes(context.Background(), &engine.CubeFilter{Search: "nonexistent"})
	require.NoError(t, err)
	assert.Len(t, cubes, 0)
}

// ─── GetCube 测试 ────────────────────────────────────────────────────────────

func TestCubeJSAdapter_GetCube(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	detail, err := a.GetCube(context.Background(), "orders")
	require.NoError(t, err)
	assert.Equal(t, "orders", detail.Name)
	assert.Equal(t, "Orders", detail.DisplayName)
	assert.Len(t, detail.Measures, 2)
	assert.Len(t, detail.Dimensions, 2)
	assert.Len(t, detail.Joins, 1)
	assert.Equal(t, "customers", detail.Joins[0].JoinCubeName)
	assert.Equal(t, "belongsTo", detail.Joins[0].Relationship)
}

func TestCubeJSAdapter_GetCube_NotFound(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	_, err := a.GetCube(context.Background(), "nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cube not found")
}

// ─── ListMeasures / ListDimensions 测试 ──────────────────────────────────────

func TestCubeJSAdapter_ListMeasures(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	measures, err := a.ListMeasures(context.Background(), "orders")
	require.NoError(t, err)
	assert.Len(t, measures, 2)
	assert.Equal(t, "orders.count", measures[0].Name)
	assert.Equal(t, "Orders Count", measures[0].DisplayName)
	assert.Equal(t, "number", measures[0].Type)
}

func TestCubeJSAdapter_ListMeasures_NotFound(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	_, err := a.ListMeasures(context.Background(), "nonexistent")
	assert.Error(t, err)
}

func TestCubeJSAdapter_ListDimensions(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	dims, err := a.ListDimensions(context.Background(), "orders")
	require.NoError(t, err)
	assert.Len(t, dims, 2)
	assert.Equal(t, "orders.status", dims[0].Name)
	assert.Equal(t, "string", dims[0].Type)
}

func TestCubeJSAdapter_ListDimensions_NotFound(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	_, err := a.ListDimensions(context.Background(), "nonexistent")
	assert.Error(t, err)
}

// ─── Query 测试 ──────────────────────────────────────────────────────────────

func TestCubeJSAdapter_Query(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	req := &engine.QueryRequest{
		Measures:   []string{"orders.count"},
		Dimensions: []string{"orders.status"},
		Limit:      100,
	}

	result, err := a.Query(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "cubejs", result.Engine)
	assert.Equal(t, []string{"orders.status", "orders.count"}, result.Columns)
	assert.Len(t, result.Rows, 2)
	assert.Equal(t, int64(2), result.TotalRows)
}

func TestCubeJSAdapter_Query_WithFilters(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "equals", Values: []string{"active"}},
		},
	}

	result, err := a.Query(context.Background(), req)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// ─── translateQuery 单元测试 ─────────────────────────────────────────────────

func TestTranslateQuery(t *testing.T) {
	req := &engine.QueryRequest{
		Measures:   []string{"orders.count"},
		Dimensions: []string{"orders.status"},
		TimeDimensions: []engine.TimeDimensionQuery{
			{Dimension: "orders.created_at", DateRange: []string{"2024-01-01", "2024-12-31"}, Granularity: "month"},
		},
		Filters: []engine.Filter{
			{Member: "orders.status", Operator: "equals", Values: []string{"active"}},
		},
		Order: []engine.OrderItem{
			{Member: "orders.count", Direction: "desc"},
		},
		Limit:  100,
		Offset: 10,
	}

	q := translateQuery(req)
	assert.Equal(t, []string{"orders.count"}, q.Measures)
	assert.Equal(t, []string{"orders.status"}, q.Dimensions)
	assert.Len(t, q.TimeDimensions, 1)
	assert.Equal(t, "orders.created_at", q.TimeDimensions[0].Dimension)
	assert.Equal(t, []string{"2024-01-01", "2024-12-31"}, q.TimeDimensions[0].DateRange)
	assert.Equal(t, "month", q.TimeDimensions[0].Granularity)
	assert.Len(t, q.Filters, 1)
	assert.Equal(t, "orders.status", q.Filters[0].Member)
	assert.Equal(t, "equals", q.Filters[0].Operator)
	assert.Len(t, q.Order, 1)
	assert.Equal(t, []string{"orders.count", "desc"}, q.Order[0])
	assert.Equal(t, 100, q.Limit)
	assert.Equal(t, 10, q.Offset)
}

func TestTranslateQuery_Minimal(t *testing.T) {
	req := &engine.QueryRequest{
		Measures: []string{"orders.count"},
	}

	q := translateQuery(req)
	assert.Equal(t, []string{"orders.count"}, q.Measures)
	assert.Nil(t, q.Dimensions)
	assert.Nil(t, q.TimeDimensions)
	assert.Nil(t, q.Filters)
	assert.Nil(t, q.Order)
	assert.Equal(t, 0, q.Limit)
	assert.Equal(t, 0, q.Offset)
}

// ─── translateResult 单元测试 ────────────────────────────────────────────────

func TestTranslateResult(t *testing.T) {
	resp := &cubejs.LoadResponse{
		Data: []map[string]interface{}{
			{"orders.status": "active", "orders.count": float64(10)},
			{"orders.status": "inactive", "orders.count": float64(5)},
		},
	}
	req := &engine.QueryRequest{
		Measures:   []string{"orders.count"},
		Dimensions: []string{"orders.status"},
	}

	result, err := translateResult(resp, req)
	require.NoError(t, err)
	assert.Equal(t, []string{"orders.status", "orders.count"}, result.Columns)
	assert.Len(t, result.Rows, 2)
	assert.Equal(t, "active", result.Rows[0][0])
	assert.Equal(t, float64(10), result.Rows[0][1])
	assert.Equal(t, int64(2), result.TotalRows)
	assert.Equal(t, "cubejs", result.Engine)
}

func TestTranslateResult_Empty(t *testing.T) {
	resp := &cubejs.LoadResponse{Data: []map[string]interface{}{}}
	req := &engine.QueryRequest{Measures: []string{"orders.count"}}

	result, err := translateResult(resp, req)
	require.NoError(t, err)
	assert.Len(t, result.Rows, 0)
	assert.Equal(t, int64(0), result.TotalRows)
}

// ─── Ping 测试 ───────────────────────────────────────────────────────────────

func TestCubeJSAdapter_Ping(t *testing.T) {
	a, srv := newTestAdapter(t)
	defer srv.Close()

	err := a.Ping(context.Background())
	assert.NoError(t, err)
}

func TestCubeJSAdapter_Ping_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer srv.Close()

	a := &CubeJSAdapter{
		client: cubejs.NewClient(srv.URL, "token"),
		config: &engine.CubeJSConfig{APIURL: srv.URL},
	}
	err := a.Ping(context.Background())
	assert.Error(t, err)
}

// ─── CreateAdapter 测试 ──────────────────────────────────────────────────────

func TestCreateAdapter_CubeJS(t *testing.T) {
	adapter, err := CreateAdapter("cubejs")
	assert.NoError(t, err)
	assert.NotNil(t, adapter)
	_, ok := adapter.(*CubeJSAdapter)
	assert.True(t, ok)
}
