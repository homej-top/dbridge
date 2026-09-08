package handler

import "testing"

func TestStripSQLComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"line comment", "SELECT 1 -- comment", "SELECT 1 "},
		{"block comment", "SELECT /* comment */ 1", "SELECT   1"},
		{"multi-line block", "SELECT /* line1\nline2 */ 1", "SELECT   1"},
		{"no comments", "SELECT 1", "SELECT 1"},
		{"only line comment", "-- just a comment", ""},
		{"mixed", "/* block */ SELECT -- line\n1", "  SELECT \n1"},
		{"DELIMITER line", "DELIMITER $$\nCREATE PROCEDURE foo() BEGIN END$$\nDELIMITER ;", "CREATE PROCEDURE foo() BEGIN END;"},
		{"DELIMITER with spaces", "  DELIMITER  //\nSELECT 1\n  DELIMITER ;", "SELECT 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripSQLComments(tt.input)
			if got != tt.want {
				t.Errorf("stripSQLComments(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripStringLiterals(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"simple string", "SELECT 'hello'", "SELECT ''"},
		{"escaped quote", "SELECT 'it''s'", "SELECT ''"},
		{"backslash escape", "SELECT 'it\\'s'", "SELECT ''"},
		{"no strings", "SELECT 1", "SELECT 1"},
		{"multiple strings", "SELECT 'a', 'b'", "SELECT '', ''"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripStringLiterals(tt.input)
			if got != tt.want {
				t.Errorf("stripStringLiterals(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestValidateDDLType(t *testing.T) {
	tests := []struct {
		name     string
		ddl      string
		prefixes []string
		wantErr  bool
	}{
		{"CREATE TABLE", "CREATE TABLE foo (id INT)", []string{"CREATE"}, false},
		{"CREATE PROCEDURE", "CREATE PROCEDURE foo() BEGIN END", []string{"CREATE"}, false},
		{"DROP allowed", "DROP TABLE foo", []string{"DROP"}, false},
		{"ALTER not allowed", "ALTER TABLE foo ADD col INT", []string{"CREATE"}, true},
		{"comment before CREATE", "/* comment */ CREATE TABLE foo", []string{"CREATE"}, false},
		{"line comment before CREATE", "-- comment\nCREATE TABLE foo", []string{"CREATE"}, false},
		{"INSERT not allowed", "INSERT INTO foo VALUES(1)", []string{"CREATE"}, true},
		{"string masked", "CREATE TABLE 'DROP TABLE' (id INT)", []string{"CREATE"}, false},
		{"DELIMITER before CREATE PROCEDURE", "DELIMITER $$\nCREATE PROCEDURE foo() BEGIN SELECT 1; END$$\nDELIMITER ;", []string{"CREATE"}, false},
		{"DELIMITER before CREATE FUNCTION", "DELIMITER $$\nCREATE FUNCTION foo() RETURNS INT BEGIN RETURN 1; END$$\nDELIMITER ;", []string{"CREATE"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDDLType(tt.ddl, tt.prefixes)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateDDLType() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateNoDangerousSql(t *testing.T) {
	tests := []struct {
		name    string
		ddl     string
		wantErr bool
	}{
		{"safe CREATE", "CREATE TABLE foo (id INT)", false},
		{"safe DROP", "DROP PROCEDURE IF EXISTS foo", false},
		{"safe ALTER", "ALTER PROCEDURE foo COMPILE", false},
		{"GRANT blocked", "GRANT ALL ON foo TO bar", true},
		{"REVOKE blocked", "REVOKE ALL ON foo FROM bar", true},
		{"TRUNCATE blocked", "TRUNCATE TABLE foo", true},
		{"DELETE blocked", "DELETE FROM foo", true},
		{"UPDATE blocked", "UPDATE foo SET bar=1", true},
		{"INSERT blocked", "INSERT INTO foo VALUES(1)", true},
		{"ALTER USER blocked", "ALTER USER foo IDENTIFIED BY bar", true},
		{"DROP USER blocked", "DROP USER foo", true},
		{"CREATE USER blocked", "CREATE USER foo", true},
		{"string masked GRANT", "CREATE TABLE foo (name VARCHAR(100) DEFAULT 'GRANT')", false},
		{"comment masked", "/* GRANT */ CREATE TABLE foo", false},
		{"multi-statement safe", "CREATE TABLE foo (id INT);\nCREATE TABLE bar (id INT)", false},
		{"multi-statement dangerous", "CREATE TABLE foo (id INT); DROP USER bar", true},
		{"procedure with INSERT body", "CREATE PROCEDURE foo() BEGIN INSERT INTO log VALUES(1); END", false},
		{"procedure with DELETE body", "CREATE PROCEDURE foo() BEGIN DELETE FROM old_data; END", false},
		{"procedure with UPDATE body", "CREATE PROCEDURE foo() BEGIN UPDATE counter SET val=val+1; END", false},
		{"dangerous after procedure", "CREATE PROCEDURE foo() BEGIN SELECT 1; END; DROP USER bar", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoDangerousSql(tt.ddl)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateNoDangerousSql(%q) error = %v, wantErr %v", tt.ddl, err, tt.wantErr)
			}
		})
	}
}
