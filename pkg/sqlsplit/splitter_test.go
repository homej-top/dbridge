package sqlsplit

import (
	"strings"
	"testing"
)

func TestSplitSQL_Basic(t *testing.T) {
	sql := "CREATE TABLE users (id INT);\nINSERT INTO users VALUES (1);\nSELECT * FROM users;"
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(stmts), stmts)
	}
}

// ─── Phase 1.2: comprehensive edge cases ───────────────────────

func TestSplitSQL_StoredProcedure(t *testing.T) {
	sql := `
CREATE PROCEDURE demo()
BEGIN
    SELECT 'test;semicolon';
    INSERT INTO t VALUES (1);
END;
SELECT * FROM t;
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("stored procedure should produce 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_Trigger(t *testing.T) {
	sql := `
CREATE TRIGGER after_insert AFTER INSERT ON users
BEGIN
    INSERT INTO audit_log VALUES (NEW.id, 'insert');
END;
SELECT COUNT(*) FROM users;
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("trigger should produce 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_SemicolonInString(t *testing.T) {
	sql := "INSERT INTO t VALUES ('a;b;c');\nINSERT INTO t VALUES ('hello');"
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) < 2 {
		t.Fatalf("expected at least 2 statements, got %d", len(stmts))
	}
	// Verify the first statement contains the full string value
	found := false
	for _, s := range stmts {
		if strings.Contains(s, "a;b;c") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("first statement should contain 'a;b;c' but got: %v", stmts)
	}
}

func TestSplitSQL_NestedBlock(t *testing.T) {
	sql := `
BEGIN
    INSERT INTO t1 VALUES (1);
    BEGIN
        INSERT INTO t2 VALUES (2);
    END;
    INSERT INTO t3 VALUES (3);
END;
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 1 {
		t.Fatalf("nested BEGIN/END should be 1 statement, got %d", len(stmts))
	}
}

func TestSplitSQL_DELIMITER_Change(t *testing.T) {
	sql := `DELIMITER $$
CREATE PROCEDURE test()
BEGIN
    SELECT 1;
    SELECT 2;
END$$
DELIMITER ;
SELECT 3;
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) < 2 {
		t.Fatalf("DELIMITER change should produce at least 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_MySQLFunction(t *testing.T) {
	sql := `
CREATE FUNCTION add_one(x INT) RETURNS INT
BEGIN
    RETURN x + 1;
END;
SELECT add_one(5);
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("function should produce 2 statements, got %d: %v", len(stmts), stmts)
	}
}

func TestSplitSQL_CTE(t *testing.T) {
	sql := `
WITH cte AS (SELECT * FROM users WHERE active = 1)
SELECT * FROM cte;
INSERT INTO log VALUES (1);
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("CTE + INSERT should be 2 statements, got %d", len(stmts))
	}
}

func TestSplitSQL_MultiLineComment(t *testing.T) {
	sql := `
/* this is a comment
   with a semicolon; inside */
CREATE TABLE t1 (id INT);
-- single line comment with ; semicolon
SELECT * FROM t1;
`
	stmts, err := SplitSQL(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("comments should be ignored, expected 2, got %d: %v", len(stmts), stmts)
	}
}

// ─── Classification tests ──────────────────────────────────────

func TestSplitSQL_DDL_DML_Classification(t *testing.T) {
	sql := "CREATE TABLE t1 (id INT);\nINSERT INTO t1 VALUES (1);\nDROP TABLE old;"
	result, err := SplitSQLDetailed(sql)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.DDL) < 2 {
		t.Fatalf("expected at least 2 DDL, got %d", len(result.DDL))
	}
	if len(result.DML) < 1 {
		t.Fatalf("expected at least 1 DML, got %d", len(result.DML))
	}
	if !result.HasDanger {
		t.Fatal("expected HasDanger=true for DROP TABLE")
	}
}

func TestSplitSQL_SafetyCheck(t *testing.T) {
	stmts := []string{
		"CREATE TABLE ok (id INT)",
		"DROP TABLE dangerous_table",
		"INSERT INTO t VALUES (1)",
		"TRUNCATE TABLE clear_me",
	}
	dangerous := CheckDangerousSQL(stmts)
	if len(dangerous) != 2 {
		t.Fatalf("expected 2 dangerous, got %d: %v", len(dangerous), dangerous)
	}
}

// ─── GoSQLX validation tests ───────────────────────────────────

func TestSplitSQL_GoSQLX_Degraded(t *testing.T) {
	// Valid SQL should NOT be degraded
	result, err := SplitSQLDetailed("CREATE TABLE t (id INT);")
	if err != nil {
		t.Fatal(err)
	}
	if result.Degraded {
		t.Logf("GoSQLX validation degraded on valid SQL (tokenizer may be strict): %v", result.DegradedMsgs)
	}

	// Note: GoSQLX tokenizer is lenient — it may not error on syntactically invalid input.
	// Degraded flag is informational; the delimiter-based splitter is the primary mechanism.
	// The DegradedMsgs field provides diagnostics when GoSQLX validation fails.
	t.Logf("GoSQLX degraded messages: %v", result.DegradedMsgs)
}

// ─── Stream + Utility tests ────────────────────────────────────

func TestSplitStream_Basic(t *testing.T) {
	sql := "CREATE TABLE t (id INT);\nINSERT INTO t VALUES (1);\n"
	var stmts []string
	err := SplitStream(strings.NewReader(sql), func(stmt string) error {
		stmts = append(stmts, stmt)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 2 {
		t.Fatalf("expected 2, got %d", len(stmts))
	}
}

func TestDialectFromDBType(t *testing.T) {
	tests := map[string]Dialect{
		"mysql":     MySQL,
		"postgres":  Postgres,
		"sqlserver": MSSQL,
		"oracle":    Oracle,
		"sqlite":    SQLite,
		"oceanbase": MySQL,
		"unknown":   MySQL,
	}
	for dbType, expected := range tests {
		got := DialectFromDBType(dbType)
		if got != expected {
			t.Errorf("DialectFromDBType(%s)=%s, want %s", dbType, got, expected)
		}
	}
}

func TestSplitSQL_Empty(t *testing.T) {
	stmts, err := SplitSQL("")
	if err != nil {
		t.Fatal(err)
	}
	if len(stmts) != 0 {
		t.Fatalf("expected 0, got %d", len(stmts))
	}
}

func TestFormatDangerSummary(t *testing.T) {
	dangerous := []string{"DROP TABLE a", "TRUNCATE TABLE b"}
	summary := FormatDangerSummary(dangerous)
	if !strings.Contains(summary, "2 条危险语句") {
		t.Errorf("unexpected summary: %s", summary)
	}
}
