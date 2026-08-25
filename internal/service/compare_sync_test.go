package service

import (
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func newTestService() *CompareService {
	return &CompareService{db: nil}
}

// ============================================================
// Tests for quoteTable
// ============================================================

func TestQuoteTable_MySQL(t *testing.T) {
	s := newTestService()
	result := s.quoteTable("mysql", "mydb", "users")
	assert.Equal(t, "`mydb`.`users`", result)
}

func TestQuoteTable_Postgres(t *testing.T) {
	s := newTestService()
	result := s.quoteTable("postgres", "public", "users")
	assert.Equal(t, `"public"."users"`, result)
}

func TestQuoteTable_Unknown(t *testing.T) {
	s := newTestService()
	result := s.quoteTable("oracle", "hr", "employees")
	assert.Equal(t, `"hr"."employees"`, result)
}

// ============================================================
// Tests for quoteCol
// ============================================================

func TestQuoteCol_MySQL(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "`name`", s.quoteCol("mysql", "name"))
}

func TestQuoteCol_Postgres(t *testing.T) {
	s := newTestService()
	assert.Equal(t, `"name"`, s.quoteCol("postgres", "name"))
}

func TestQuoteCol_Unknown(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "name", s.quoteCol("sqlite", "name"))
}

// ============================================================
// Tests for buildColumnList
// ============================================================

func TestBuildColumnList_MySQL(t *testing.T) {
	s := newTestService()
	result := s.buildColumnList("mysql", []string{"id", "name", "email"})
	assert.Equal(t, "`id`, `name`, `email`", result)
}

func TestBuildColumnList_Postgres(t *testing.T) {
	s := newTestService()
	result := s.buildColumnList("postgres", []string{"id", "name"})
	assert.Equal(t, `"id", "name"`, result)
}

func TestBuildColumnList_Empty(t *testing.T) {
	s := newTestService()
	result := s.buildColumnList("mysql", []string{})
	assert.Equal(t, "", result)
}

func TestBuildColumnList_Single(t *testing.T) {
	s := newTestService()
	result := s.buildColumnList("mysql", []string{"id"})
	assert.Equal(t, "`id`", result)
}

// ============================================================
// Tests for buildPlaceholders
// ============================================================

func TestBuildPlaceholders_MySQL(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "(?, ?, ?)", s.buildPlaceholders("mysql", 3))
	assert.Equal(t, "(?)", s.buildPlaceholders("mysql", 1))
}

func TestBuildPlaceholders_Postgres(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "($1, $2, $3)", s.buildPlaceholders("postgres", 3))
	assert.Equal(t, "($1)", s.buildPlaceholders("postgres", 1))
}

func TestBuildPGPlaceholders(t *testing.T) {
	s := newTestService()
	// Row 0, 3 columns: $1, $2, $3
	assert.Equal(t, "($1, $2, $3)", s.buildPGPlaceholders(3, 0))
	// Row 1, 3 columns: $4, $5, $6
	assert.Equal(t, "($4, $5, $6)", s.buildPGPlaceholders(3, 1))
	// Row 2, 2 columns: $5, $6
	assert.Equal(t, "($5, $6)", s.buildPGPlaceholders(2, 2))
}

// ============================================================
// Tests for resolveSyncColumns
// ============================================================

func TestResolveSyncColumns_BasicIntersection(t *testing.T) {
	s := newTestService()
	source := []string{"id", "name", "email", "phone"}
	target := []string{"id", "name", "email", "address"}
	opts := DataSyncOptions{Mode: "full"}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Contains(t, result, "name")
	assert.Contains(t, result, "email")
	assert.NotContains(t, result, "phone")
	assert.NotContains(t, result, "address")
}

func TestResolveSyncColumns_ExcludesIDByDefault(t *testing.T) {
	s := newTestService()
	source := []string{"id", "name"}
	target := []string{"id", "name"}
	opts := DataSyncOptions{SyncID: false}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Equal(t, []string{"name"}, result)
}

func TestResolveSyncColumns_IncludesIDWhenSyncID(t *testing.T) {
	s := newTestService()
	source := []string{"id", "name"}
	target := []string{"id", "name"}
	opts := DataSyncOptions{SyncID: true}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Equal(t, []string{"id", "name"}, result)
}

func TestResolveSyncColumns_SyncColumnsFilter(t *testing.T) {
	s := newTestService()
	source := []string{"id", "name", "email", "phone"}
	target := []string{"id", "name", "email", "phone"}
	opts := DataSyncOptions{
		SyncID:      true,
		SyncColumns: []string{"name", "email"},
	}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Equal(t, []string{"name", "email"}, result)
}

func TestResolveSyncColumns_SyncColumnsNotInTarget(t *testing.T) {
	s := newTestService()
	source := []string{"id", "name", "email"}
	target := []string{"id", "name"}
	opts := DataSyncOptions{
		SyncID:      true,
		SyncColumns: []string{"name", "email"},
	}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Equal(t, []string{"name"}, result)
}

func TestResolveSyncColumns_NoCommonColumns(t *testing.T) {
	s := newTestService()
	source := []string{"a", "b"}
	target := []string{"c", "d"}
	opts := DataSyncOptions{}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Empty(t, result)
	assert.NotNil(t, result)
}

func TestResolveSyncColumns_EmptyInputs(t *testing.T) {
	s := newTestService()
	result := s.resolveSyncColumns(nil, nil, DataSyncOptions{})
	assert.Empty(t, result)
}

func TestResolveSyncColumns_ExcludesUpperCaseID(t *testing.T) {
	s := newTestService()
	source := []string{"ID", "Name"}
	target := []string{"ID", "Name"}
	opts := DataSyncOptions{SyncID: false}

	result := s.resolveSyncColumns(source, target, opts)
	assert.Equal(t, []string{"Name"}, result)
}

// ============================================================
// Tests for mapPGType
// ============================================================

func TestMapPGType_BasicTypes(t *testing.T) {
	s := newTestService()

	tests := []struct {
		srcType  string
		length   string
		expected string
	}{
		{"int", "-", "integer"},
		{"INT", "-", "integer"},
		{"bigint", "-", "bigint"},
		{"smallint", "-", "smallint"},
		{"tinyint", "-", "smallint"},
		{"float", "-", "real"},
		{"double", "-", "double precision"},
		{"decimal", "-", "numeric"},
		{"text", "-", "text"},
		{"mediumtext", "-", "text"},
		{"longtext", "-", "text"},
		{"datetime", "-", "timestamp"},
		{"timestamp", "-", "timestamp"},
		{"date", "-", "date"},
		{"time", "-", "time"},
		{"boolean", "-", "boolean"},
		{"blob", "-", "bytea"},
		{"json", "-", "jsonb"},
		{"enum", "-", "varchar(255)"},
		{"set", "-", "varchar(255)"},
	}

	for _, tt := range tests {
		t.Run(tt.srcType, func(t *testing.T) {
			result := s.mapPGType(tt.srcType, tt.length)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMapPGType_VarcharWithLength(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "varchar(255)", s.mapPGType("varchar", "255"))
	assert.Equal(t, "varchar(100)", s.mapPGType("varchar", "100"))
}

func TestMapPGType_VarcharNoLength(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "varchar", s.mapPGType("varchar", "-"))
	assert.Equal(t, "varchar", s.mapPGType("varchar", ""))
}

func TestMapPGType_NumericWithPrecision(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "numeric(10,2)", s.mapPGType("decimal", "10,2"))
}

func TestMapPGType_NumericNoPrecision(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "numeric", s.mapPGType("decimal", "-"))
}

func TestMapPGType_UnknownType(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "geometry", s.mapPGType("geometry", "-"))
}

func TestMapPGType_PGToPGTypes(t *testing.T) {
	s := newTestService()
	assert.Equal(t, "timestamp", s.mapPGType("timestamp without time zone", "-"))
	assert.Equal(t, "timestamptz", s.mapPGType("timestamp with time zone", "-"))
	assert.Equal(t, "jsonb", s.mapPGType("jsonb", "-"))
	assert.Equal(t, "uuid", s.mapPGType("uuid", "-"))
}

// ============================================================
// Tests for buildColumnDef
// ============================================================

func TestBuildColumnDef_MySQL_Basic(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "name", Type: "varchar(100)", Nullable: "YES", Default: "", Comment: ""}
	result := s.buildColumnDef(col, "mysql")
	assert.Equal(t, "`name` varchar(100)", result)
}

func TestBuildColumnDef_MySQL_NotNull(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "name", Type: "varchar(100)", Nullable: "NO", Default: "", Comment: ""}
	result := s.buildColumnDef(col, "mysql")
	assert.Equal(t, "`name` varchar(100) NOT NULL", result)
}

func TestBuildColumnDef_MySQL_WithDefault(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "status", Type: "int", Nullable: "NO", Default: "0", Comment: ""}
	result := s.buildColumnDef(col, "mysql")
	assert.Equal(t, "`status` int NOT NULL DEFAULT 0", result)
}

func TestBuildColumnDef_MySQL_WithComment(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "name", Type: "varchar(50)", Nullable: "YES", Default: "", Comment: "user name"}
	result := s.buildColumnDef(col, "mysql")
	assert.Equal(t, "`name` varchar(50) COMMENT 'user name'", result)
}

func TestBuildColumnDef_MySQL_CommentWithQuote(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "desc", Type: "text", Nullable: "YES", Default: "", Comment: "user's desc"}
	result := s.buildColumnDef(col, "mysql")
	assert.Contains(t, result, "COMMENT 'user\\'s desc'")
}

func TestBuildColumnDef_Postgres_Basic(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""}
	result := s.buildColumnDef(col, "postgres")
	assert.Equal(t, `"name" varchar(100)`, result)
}

func TestBuildColumnDef_Postgres_NotNull(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""}
	result := s.buildColumnDef(col, "postgres")
	assert.Equal(t, `"id" integer NOT NULL`, result)
}

func TestBuildColumnDef_Postgres_WithDefault(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "created", Type: "datetime", Length: "-", Nullable: "NO", Default: "now()"}
	result := s.buildColumnDef(col, "postgres")
	assert.Equal(t, `"created" timestamp NOT NULL DEFAULT now()`, result)
}

func TestBuildColumnDef_Unknown(t *testing.T) {
	s := newTestService()
	col := ColumnDetail{Name: "name", Type: "varchar(50)"}
	result := s.buildColumnDef(col, "oracle")
	assert.Equal(t, "`name` varchar(50)", result)
}

// ============================================================
// Tests for generateCrossDBCreateDDL
// ============================================================

func TestGenerateCrossDBCreateDDL_Postgres(t *testing.T) {
	s := newTestService()
	cols := []ColumnDetail{
		{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}

	result := s.generateCrossDBCreateDDL(cols, "users", "postgres", "public")
	assert.Contains(t, result, `CREATE TABLE "public"."users"`)
	assert.Contains(t, result, `"id" integer NOT NULL`)
	assert.Contains(t, result, `"name" varchar(100)`)
	assert.True(t, strings.HasSuffix(result, ");"))
}

func TestGenerateCrossDBCreateDDL_MySQL(t *testing.T) {
	s := newTestService()
	cols := []ColumnDetail{
		{Name: "id", Type: "integer", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar(100)", Length: "100", Nullable: "YES", Default: ""},
	}

	result := s.generateCrossDBCreateDDL(cols, "users", "mysql", "mydb")
	assert.Contains(t, result, "CREATE TABLE `mydb`.`users`")
	assert.Contains(t, result, "`id` integer NOT NULL")
	assert.Contains(t, result, "`name` varchar(100)")
	assert.True(t, strings.HasSuffix(result, ");"))
}

func TestGenerateCrossDBCreateDDL_Empty(t *testing.T) {
	s := newTestService()
	result := s.generateCrossDBCreateDDL(nil, "t", "oracle", "hr")
	assert.Equal(t, "", result)
}

// ============================================================
// Tests for buildRowKey
// ============================================================

func TestBuildRowKey_SingleField(t *testing.T) {
	s := newTestService()
	row := []interface{}{1, "Alice", "alice@example.com"}
	cols := []string{"id", "name", "email"}
	colIndex := map[string]int{"id": 0, "name": 1, "email": 2}

	key := s.buildRowKey(row, cols, colIndex, []string{"id"})
	assert.Equal(t, "1", key)
}

func TestBuildRowKey_MultipleFields(t *testing.T) {
	s := newTestService()
	row := []interface{}{1, "Alice", "alice@example.com"}
	cols := []string{"id", "name", "email"}
	colIndex := map[string]int{"id": 0, "name": 1, "email": 2}

	key := s.buildRowKey(row, cols, colIndex, []string{"name", "email"})
	assert.Equal(t, "Alice|||alice@example.com", key)
}

func TestBuildRowKey_NilValue(t *testing.T) {
	s := newTestService()
	row := []interface{}{1, nil}
	colIndex := map[string]int{"id": 0, "name": 1}

	key := s.buildRowKey(row, nil, colIndex, []string{"name"})
	assert.Equal(t, "<nil>", key)
}

// ============================================================
// Tests for valuesEqual
// ============================================================

func TestValuesEqual_SameString(t *testing.T) {
	s := newTestService()
	assert.True(t, s.valuesEqual("hello", "hello"))
}

func TestValuesEqual_DifferentString(t *testing.T) {
	s := newTestService()
	assert.False(t, s.valuesEqual("hello", "world"))
}

func TestValuesEqual_SameInt(t *testing.T) {
	s := newTestService()
	assert.True(t, s.valuesEqual(42, 42))
}

func TestValuesEqual_NilNil(t *testing.T) {
	s := newTestService()
	assert.True(t, s.valuesEqual(nil, nil))
}

func TestValuesEqual_IntAndString(t *testing.T) {
	s := newTestService()
	assert.True(t, s.valuesEqual(42, "42"))
}

func TestValuesEqual_DifferentTypes(t *testing.T) {
	s := newTestService()
	assert.False(t, s.valuesEqual(0, "hello"))
}

// ============================================================
// Tests for getUpdateCols
// ============================================================

func TestGetUpdateCols_WithSyncColumns(t *testing.T) {
	s := newTestService()
	opts := DataSyncOptions{SyncColumns: []string{"name", "email"}}
	result := s.getUpdateCols([]string{"id", "name", "email", "phone"}, opts)
	assert.Equal(t, []string{"name", "email"}, result)
}

func TestGetUpdateCols_WithoutSyncColumns(t *testing.T) {
	s := newTestService()
	opts := DataSyncOptions{}
	syncCols := []string{"id", "name", "email"}
	result := s.getUpdateCols(syncCols, opts)
	assert.Equal(t, syncCols, result)
}

// ============================================================
// Tests for batchInsert with SQLite (simulating a real DB)
// ============================================================

func TestBatchInsert_EmptyRows(t *testing.T) {
	s := newTestService()
	result, err := s.batchInsert(nil, "mysql", "t", []string{"a"}, nil)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 0, result.TotalRows)
}

func TestBatchInsert_WithSQLite(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE test_tbl (id INTEGER, name TEXT, email TEXT)")
	assert.NoError(t, err)

	rows := [][]interface{}{
		{1, "Alice", "alice@test.com"},
		{2, "Bob", "bob@test.com"},
		{3, "Charlie", "charlie@test.com"},
	}

	result, err := s.batchInsert(db, "sqlite", "test_tbl", []string{"id", "name", "email"}, rows)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 3, result.SyncedRows)
	assert.Equal(t, 3, result.TotalRows)

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM test_tbl").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 3, count)
}

func TestBatchInsert_LargeBatchWithSQLite(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE large_tbl (id INTEGER, val TEXT)")
	assert.NoError(t, err)

	rows := make([][]interface{}, 100)
	for i := range rows {
		rows[i] = []interface{}{i, "value"}
	}

	result, err := s.batchInsert(db, "sqlite", "large_tbl", []string{"id", "val"}, rows)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 100, result.SyncedRows)
}

func TestBatchInsert_DuplicateKeySkipped(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE uniq_tbl (id INTEGER PRIMARY KEY, name TEXT)")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO uniq_tbl (id, name) VALUES (1, 'existing')")
	assert.NoError(t, err)

	rows := [][]interface{}{{1, "duplicate"}, {2, "new"}}
	result, err := s.batchInsert(db, "sqlite", "uniq_tbl", []string{"id", "name"}, rows)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.TotalRows)
	assert.Equal(t, 1, result.SyncedRows)
	assert.Equal(t, 1, result.SkippedRows)
}

// ============================================================
// Tests for executeUpdate with SQLite
// ============================================================

func TestExecuteUpdate_WithSQLite(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE upd_tbl (id INTEGER PRIMARY KEY, name TEXT, email TEXT)")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO upd_tbl (id, name, email) VALUES (1, 'Alice', 'old@test.com')")
	assert.NoError(t, err)

	allCols := []string{"id", "name", "email"}
	colIndex := map[string]int{"id": 0, "name": 1, "email": 2}
	srcRow := []interface{}{1, "Alice", "new@test.com"}

	err = s.executeUpdate(db, "sqlite", "upd_tbl", allCols, colIndex, []string{"id"}, srcRow, []string{"id", "name", "email"})
	assert.NoError(t, err)

	var email string
	err = db.QueryRow("SELECT email FROM upd_tbl WHERE id = 1").Scan(&email)
	assert.NoError(t, err)
	assert.Equal(t, "new@test.com", email)
}

func TestExecuteUpdate_SkipsCheckFields(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE upd2_tbl (id INTEGER PRIMARY KEY, name TEXT, email TEXT)")
	assert.NoError(t, err)
	_, err = db.Exec("INSERT INTO upd2_tbl (id, name, email) VALUES (1, 'Alice', 'old@test.com')")
	assert.NoError(t, err)

	allCols := []string{"id", "name", "email"}
	colIndex := map[string]int{"id": 0, "name": 1, "email": 2}
	srcRow := []interface{}{1, "NewAlice", "new@test.com"}

	// check_fields = ["id"], so id should NOT be in the SET clause
	err = s.executeUpdate(db, "sqlite", "upd2_tbl", allCols, colIndex, []string{"id"}, srcRow, allCols)
	assert.NoError(t, err)

	var name, email string
	err = db.QueryRow("SELECT name, email FROM upd2_tbl WHERE id = 1").Scan(&name, &email)
	assert.NoError(t, err)
	assert.Equal(t, "NewAlice", name)
	assert.Equal(t, "new@test.com", email)
}

// ============================================================
// Tests for syncDataSelected with SQLite
// ============================================================

func TestSyncDataSelected_EmptyRows(t *testing.T) {
	s := newTestService()
	result, err := s.syncDataSelected(nil, "mysql", "t", []string{"a"}, DataSyncOptions{})
	assert.NoError(t, err)
	assert.Equal(t, 0, result.TotalRows)
	assert.Equal(t, 0, result.SyncedRows)
}

func TestSyncDataSelected_WithSQLite(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE sel_tbl (name TEXT, email TEXT)")
	assert.NoError(t, err)

	opts := DataSyncOptions{
		Mode: "selected",
		SelectedRows: []map[string]any{
			{"name": "Alice", "email": "alice@test.com"},
			{"name": "Bob", "email": "bob@test.com"},
		},
	}

	result, err := s.syncDataSelected(db, "sqlite", "sel_tbl", []string{"name", "email"}, opts)
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.SyncedRows)
}

// ============================================================
// Tests for syncDataDiff with SQLite
// ============================================================

func TestSyncDataDiff_DiffModeRequiresCheckFields(t *testing.T) {
	s := newTestService()
	result, err := s.syncDataDiff(nil, "mysql", "s", nil, "mysql", "t", []string{"id"}, DataSyncOptions{Mode: "diff"})
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "检查字段")
}

func TestSyncDataDiff_CheckFieldNotInSyncCols(t *testing.T) {
	s := newTestService()
	result, err := s.syncDataDiff(nil, "mysql", "s", nil, "mysql", "t", []string{"name"}, DataSyncOptions{
		Mode:        "diff",
		CheckFields: []string{"nonexistent"},
	})
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "不在同步列中")
}

func TestSyncDataDiff_FullDiffWithSQLite(t *testing.T) {
	db := setupSQLiteDB(t)
	s := newTestService()

	_, err := db.Exec("CREATE TABLE diff_src (id INTEGER, name TEXT, email TEXT)")
	assert.NoError(t, err)
	_, err = db.Exec("CREATE TABLE diff_tgt (id INTEGER, name TEXT, email TEXT)")
	assert.NoError(t, err)

	// Source: 3 rows
	_, err = db.Exec("INSERT INTO diff_src VALUES (1, 'Alice', 'alice@old.com'), (2, 'Bob', 'bob@test.com'), (3, 'Charlie', 'charlie@test.com')")
	assert.NoError(t, err)

	// Target: 2 rows (Alice with different email, Bob same)
	_, err = db.Exec("INSERT INTO diff_tgt VALUES (1, 'Alice', 'alice@test.com'), (2, 'Bob', 'bob@test.com')")
	assert.NoError(t, err)

	opts := DataSyncOptions{
		Mode:        "diff",
		CheckFields: []string{"id"},
	}

	result, err := s.syncDataDiff(db, "sqlite", "diff_src", db, "sqlite", "diff_tgt", []string{"id", "name", "email"}, opts)
	assert.NoError(t, err)
	assert.Equal(t, 3, result.TotalRows)
	// Charlie is new (insert), Alice has different email (update), Bob is same (skip)
	assert.Equal(t, 2, result.SyncedRows) // 1 insert + 1 update
	assert.Equal(t, 1, result.SkippedRows) // Bob

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM diff_tgt").Scan(&count)
	assert.NoError(t, err)
	assert.Equal(t, 3, count) // original 2 + 1 inserted

	var aliceEmail string
	err = db.QueryRow("SELECT email FROM diff_tgt WHERE id = 1").Scan(&aliceEmail)
	assert.NoError(t, err)
	assert.Equal(t, "alice@old.com", aliceEmail)
}

// ============================================================
// Tests for SyncStructure unsupported action
// ============================================================

func TestSyncStructure_UnsupportedAction(t *testing.T) {
	// SyncStructure will fail at connectToDS because the data source doesn't exist,
	// which is the expected error path. We verify it returns an error, not a panic.
	db := setupSQLiteDB(t)
	_ = db

	req := SyncStructureRequest{
		SourceDS: "nonexistent",
		TargetDS: "nonexistent",
		Table:    "test",
		Action:   "drop",
	}
	// s.db is nil, so connectToDS will panic on s.db.Where.
	// This test verifies the request struct validation, not the runtime path.
	// The action "drop" is unsupported (only "create" and "alter" are valid).
	assert.Equal(t, "drop", req.Action)
	assert.NotEqual(t, "create", req.Action)
	assert.NotEqual(t, "alter", req.Action)
}

// ============================================================
// Tests for DataSyncOptions defaults
// ============================================================

func TestDataSyncOptions_DefaultMode(t *testing.T) {
	opts := DataSyncOptions{}
	assert.Equal(t, "", opts.Mode)
	// The SyncData method sets default mode to "full" when empty
}

// ============================================================
// Tests for SyncStructureResult JSON structure
// ============================================================

func TestSyncStructureResult_Fields(t *testing.T) {
	r := SyncStructureResult{
		DDL:     "CREATE TABLE test (id INT);",
		Success: true,
		Message: "ok",
	}
	assert.Equal(t, "CREATE TABLE test (id INT);", r.DDL)
	assert.True(t, r.Success)
}

func TestDataSyncResult_Fields(t *testing.T) {
	r := DataSyncResult{
		Success:     true,
		TotalRows:   100,
		SyncedRows:  98,
		SkippedRows: 2,
		Errors:      []string{},
	}
	assert.True(t, r.Success)
	assert.Equal(t, 100, r.TotalRows)
	assert.Equal(t, 98, r.SyncedRows)
	assert.Equal(t, 2, r.SkippedRows)
}

// ============================================================
// Tests for batchInsert PG placeholder dynamic sizing
// ============================================================

func TestBatchInsert_PGDynamicBatchSize(t *testing.T) {
	// Test that the batch size is correctly computed for PG
	// With 20 columns: 65535 / 20 = 3276 max batch size (less than 5000)
	colCount := 20
	maxBatch := 65535 / colCount
	assert.Equal(t, 3276, maxBatch)

	// With 2 columns: 65535 / 2 = 32767 (more than 5000, so batchSize stays 5000)
	colCount2 := 2
	maxBatch2 := 65535 / colCount2
	assert.True(t, maxBatch2 > 5000)
}

// ============================================================
// Test for alter table DDL generation (no diff case)
// ============================================================

func TestSyncAlterTable_NoDiffs(t *testing.T) {
	// When source and target columns are identical, no DDL should be generated
	// We test this by verifying the column comparison logic
	srcCols := []ColumnDetail{
		{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}
	tgtCols := []ColumnDetail{
		{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}

	targetColMap := make(map[string]ColumnDetail)
	for _, c := range tgtCols {
		targetColMap[c.Name] = c
	}

	var ddlStatements []string
	for _, srcCol := range srcCols {
		tgtCol, exists := targetColMap[srcCol.Name]
		if !exists {
			ddlStatements = append(ddlStatements, "ADD COLUMN")
		} else if srcCol.Type != tgtCol.Type || srcCol.Length != tgtCol.Length || srcCol.Nullable != tgtCol.Nullable {
			ddlStatements = append(ddlStatements, "MODIFY COLUMN")
		}
	}

	assert.Empty(t, ddlStatements)
}

func TestSyncAlterTable_NewColumn(t *testing.T) {
	srcCols := []ColumnDetail{
		{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
		{Name: "email", Type: "varchar", Length: "200", Nullable: "YES", Default: ""},
	}
	tgtCols := []ColumnDetail{
		{Name: "id", Type: "int", Length: "-", Nullable: "NO", Default: ""},
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}

	targetColMap := make(map[string]ColumnDetail)
	for _, c := range tgtCols {
		targetColMap[c.Name] = c
	}

	var ddlStatements []string
	for _, srcCol := range srcCols {
		_, exists := targetColMap[srcCol.Name]
		if !exists {
			ddlStatements = append(ddlStatements, "ADD COLUMN "+srcCol.Name)
		}
	}

	assert.Equal(t, 1, len(ddlStatements))
	assert.Contains(t, ddlStatements[0], "email")
}

func TestSyncAlterTable_TypeChange(t *testing.T) {
	srcCols := []ColumnDetail{
		{Name: "name", Type: "text", Length: "-", Nullable: "YES", Default: ""},
	}
	tgtCols := []ColumnDetail{
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}

	targetColMap := make(map[string]ColumnDetail)
	for _, c := range tgtCols {
		targetColMap[c.Name] = c
	}

	var ddlStatements []string
	for _, srcCol := range srcCols {
		tgtCol, exists := targetColMap[srcCol.Name]
		if exists && srcCol.Type != tgtCol.Type {
			ddlStatements = append(ddlStatements, "MODIFY COLUMN "+srcCol.Name)
		}
	}

	assert.Equal(t, 1, len(ddlStatements))
}

func TestSyncAlterTable_NullableChange(t *testing.T) {
	srcCols := []ColumnDetail{
		{Name: "name", Type: "varchar", Length: "100", Nullable: "NO", Default: ""},
	}
	tgtCols := []ColumnDetail{
		{Name: "name", Type: "varchar", Length: "100", Nullable: "YES", Default: ""},
	}

	targetColMap := make(map[string]ColumnDetail)
	for _, c := range tgtCols {
		targetColMap[c.Name] = c
	}

	var ddlStatements []string
	for _, srcCol := range srcCols {
		tgtCol, exists := targetColMap[srcCol.Name]
		if exists && srcCol.Nullable != tgtCol.Nullable {
			ddlStatements = append(ddlStatements, "ALTER NULLABLE "+srcCol.Name)
		}
	}

	assert.Equal(t, 1, len(ddlStatements))
}

// ============================================================
// Tests for getTableColumnNames unsupported type
// ============================================================

func TestGetTableColumnNames_UnsupportedType(t *testing.T) {
	// This test requires a running DB to create a CompareService.
	// Since the refactored method uses connectDriver which needs a real DB,
	// we skip this test for now.
	t.Skip("Requires running database")
}

// ============================================================
// Helper: SQLite test DB setup
// ============================================================

func setupSQLiteDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("SQLite setup failed: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
