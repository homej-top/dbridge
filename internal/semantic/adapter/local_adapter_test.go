package adapter

import (
	"context"
	"testing"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/semantic/engine"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&repository.SemanticCube{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestLocalAdapter_InjectDeps(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()

	deps := &engine.AdapterDeps{DB: db}
	a.InjectDeps(deps)

	if a.db != db {
		t.Error("expected DB to be injected")
	}
}

func TestLocalAdapter_Init(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})

	err := a.Init(context.Background(), engine.AdapterConfig{Type: "local"})
	if err != nil {
		t.Fatalf("init failed: %v", err)
	}

	if a.registry == nil {
		t.Error("expected registry to be initialized")
	}
	if a.compiler == nil {
		t.Error("expected compiler to be initialized")
	}
}

func TestLocalAdapter_CreateAndListCube(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})
	if err := a.Init(context.Background(), engine.AdapterConfig{Type: "local"}); err != nil {
		t.Fatalf("init: %v", err)
	}

	ctx := context.Background()
	def := &engine.CubeDefinition{
		Name:         "test_cube",
		DisplayName:  "Test Cube",
		Description:  "A test cube",
		DataSourceID: "ds-1",
		SQLTable:     "test_table",
		Measures: []*engine.MeasureDetail{
			{
				MeasureMeta: &engine.MeasureMeta{Name: "cnt", DisplayName: "Count", Type: "count"},
				SQL:         "{id}",
			},
		},
		Dimensions: []*engine.DimensionDetail{
			{
				DimensionMeta: &engine.DimensionMeta{Name: "name", DisplayName: "Name", Type: "string"},
				SQL:           "{name}",
			},
		},
	}

	if err := a.CreateCube(ctx, def); err != nil {
		t.Fatalf("create cube: %v", err)
	}

	cubes, err := a.ListCubes(ctx, nil)
	if err != nil {
		t.Fatalf("list cubes: %v", err)
	}
	if len(cubes) != 1 {
		t.Fatalf("expected 1 cube, got %d", len(cubes))
	}
	if cubes[0].Name != "test_cube" {
		t.Errorf("expected cube name 'test_cube', got %q", cubes[0].Name)
	}
}

func TestLocalAdapter_GetCube(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})
	a.Init(context.Background(), engine.AdapterConfig{Type: "local"})

	ctx := context.Background()
	def := &engine.CubeDefinition{
		Name:         "detail_cube",
		DisplayName:  "Detail Cube",
		DataSourceID: "ds-1",
		SQLTable:     "detail_table",
		Measures: []*engine.MeasureDetail{
			{MeasureMeta: &engine.MeasureMeta{Name: "total", Type: "sum"}, SQL: "{amount}"},
		},
		Dimensions: []*engine.DimensionDetail{
			{DimensionMeta: &engine.DimensionMeta{Name: "category", Type: "string"}, SQL: "{category}"},
		},
	}
	a.CreateCube(ctx, def)

	detail, err := a.GetCube(ctx, "detail_cube")
	if err != nil {
		t.Fatalf("get cube: %v", err)
	}
	if detail.SQLTable != "detail_table" {
		t.Errorf("expected sql_table 'detail_table', got %q", detail.SQLTable)
	}
	if len(detail.Measures) != 1 {
		t.Errorf("expected 1 measure, got %d", len(detail.Measures))
	}
}

func TestLocalAdapter_DeleteCube(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})
	a.Init(context.Background(), engine.AdapterConfig{Type: "local"})

	ctx := context.Background()
	a.CreateCube(ctx, &engine.CubeDefinition{
		Name: "to_delete", DisplayName: "Delete Me", DataSourceID: "ds-1", SQLTable: "t",
	})

	if err := a.DeleteCube(ctx, "to_delete"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	cubes, _ := a.ListCubes(ctx, nil)
	if len(cubes) != 0 {
		t.Errorf("expected 0 cubes after delete, got %d", len(cubes))
	}
}

func TestLocalAdapter_UpdateCube(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})
	a.Init(context.Background(), engine.AdapterConfig{Type: "local"})

	ctx := context.Background()
	a.CreateCube(ctx, &engine.CubeDefinition{
		Name: "to_update", DisplayName: "V1", DataSourceID: "ds-1", SQLTable: "t",
	})

	err := a.UpdateCube(ctx, "to_update", &engine.CubeDefinition{
		DisplayName: "V2", DataSourceID: "ds-1", SQLTable: "t2",
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	detail, _ := a.GetCube(ctx, "to_update")
	if detail.DisplayName != "V2" {
		t.Errorf("expected display_name 'V2', got %q", detail.DisplayName)
	}
}

func TestLocalAdapter_Capabilities(t *testing.T) {
	a := NewLocalAdapter()
	caps := a.Capabilities()

	if caps.Name != "local" {
		t.Errorf("expected name 'local', got %q", caps.Name)
	}
	if !caps.SupportsCreate {
		t.Error("expected SupportsCreate to be true")
	}
	if caps.SupportsPreAgg {
		t.Error("expected SupportsPreAgg to be false")
	}
	if caps.SupportsDerivedMetrics {
		t.Error("expected SupportsDerivedMetrics to be false")
	}
}

func TestLocalAdapter_QueryRejectsDerivedMetrics(t *testing.T) {
	db := setupTestDB(t)
	a := NewLocalAdapter()
	a.InjectDeps(&engine.AdapterDeps{DB: db})
	a.Init(context.Background(), engine.AdapterConfig{Type: "local"})

	_, err := a.Query(context.Background(), &engine.QueryRequest{
		Measures: []string{"orders.total"},
		DerivedMetrics: []engine.DerivedMetric{
			{Name: "ratio", Type: engine.MetricTypeRatio},
		},
	})
	if err == nil {
		t.Fatal("expected error for derived metrics")
	}
}

func TestRegistry_CreateAdapter(t *testing.T) {
	adapter, err := CreateAdapter("local")
	if err != nil {
		t.Fatalf("create adapter: %v", err)
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestRegistry_CreateAdapterUnknown(t *testing.T) {
	_, err := CreateAdapter("unknown")
	if err == nil {
		t.Fatal("expected error for unknown adapter type")
	}
}

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()
	a := NewLocalAdapter()

	r.Register("local", a)

	got, err := r.Get("local")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != a {
		t.Error("expected same adapter instance")
	}
}

func TestRegistry_GetDefault(t *testing.T) {
	r := NewRegistry()
	a := NewLocalAdapter()
	r.Register("local", a)
	r.SetDefault("local")

	got, err := r.GetDefault()
	if err != nil {
		t.Fatalf("get default: %v", err)
	}
	if got != a {
		t.Error("expected default adapter")
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	r := NewRegistry()
	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent adapter")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry()
	r.Register("local", NewLocalAdapter())

	names := r.List()
	if len(names) != 1 || names[0] != "local" {
		t.Errorf("expected [local], got %v", names)
	}
}
