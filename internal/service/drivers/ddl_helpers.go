package drivers

import (
	"strconv"
	"strings"
)

// ─── Quoting helpers ──────────────────────────────────────────────────────

func quoteMySQL(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "``") + "`"
}

func quotePG(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func quoteMSSQL(name string) string {
	return "[" + strings.ReplaceAll(name, "]", "]]") + "]"
}

func quoteOracle(name string) string {
	// Oracle stores unquoted identifiers as UPPERCASE by default
	return `"` + strings.ToUpper(strings.ReplaceAll(name, `"`, `""`)) + `"`
}

// ─── Value formatting ─────────────────────────────────────────────────────

func mysqlString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func pgString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func formatDefault(v string) string {
	if v == "" {
		return "''"
	}
	lower := strings.ToLower(v)
	switch lower {
	case "null", "current_timestamp", "now()", "true", "false", "current_date", "current_time":
		return v
	}
	if isNumeric(v) {
		return v
	}
	if (strings.HasPrefix(v, "'") && strings.HasSuffix(v, "'")) || (strings.HasPrefix(v, `"`) && strings.HasSuffix(v, `"`)) {
		return v
	}
	if strings.Contains(v, "(") {
		return v
	}
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, r := range s {
		if r == '-' && i == 0 {
			continue
		}
		if r == '.' && !dot {
			dot = true
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func toInt(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// ─── Column type helpers ──────────────────────────────────────────────────

func mysqlColumnType(colType, length string) string {
	t := strings.ToLower(colType)
	if length != "" && needsLength(t) {
		return t + "(" + length + ")"
	}
	return t
}

func pgColumnTypeFromParts(colType, length string) string {
	t := strings.ToLower(colType)
	switch t {
	case "character varying":
		t = "varchar"
	case "character":
		t = "char"
	}
	if length != "" && needsLength(t) {
		return t + "(" + length + ")"
	}
	return t
}

func needsLength(t string) bool {
	switch strings.ToLower(t) {
	case "varchar", "char", "nvarchar", "nchar", "varchar2", "nvarchar2",
		"decimal", "numeric", "number", "varbinary", "binary":
		return true
	}
	return false
}
