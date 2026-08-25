package service

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/dbridge/dbridge/internal/service/drivers"
)

// ─── Column Helpers ────────────────────────────────────────────────────────

func buildColumnList(dbType string, cols []string) string {
	quoted := make([]string, len(cols))
	for i, c := range cols {
		quoted[i] = quoteColName(dbType, c)
	}
	return strings.Join(quoted, ", ")
}

func quoteColName(dbType, col string) string {
	switch dbType {
	case "mysql":
		return "`" + col + "`"
	case "postgres", "oracle":
		return `"` + col + `"`
	case "sqlserver":
		return "[" + col + "]"
	default:
		return col
	}
}

func quoteTableName(dbType, schema, table string) string {
	switch dbType {
	case "mysql":
		if schema != "" {
			return fmt.Sprintf("`%s`.`%s`", schema, table)
		}
		return fmt.Sprintf("`%s`", table)
	case "postgres", "oracle":
		if schema != "" {
			return fmt.Sprintf(`"%s"."%s"`, schema, table)
		}
		return fmt.Sprintf(`"%s"`, table)
	case "sqlserver":
		if schema != "" {
			return fmt.Sprintf("[%s].[%s]", schema, table)
		}
		return fmt.Sprintf("[%s]", table)
	default:
		if schema != "" {
			return schema + "." + table
		}
		return table
	}
}

// ─── Batch Insert ──────────────────────────────────────────────────────────

func batchInsert(ctx context.Context, db *sql.DB, dbType, table string, cols []string, rows [][]interface{}) (*DataSyncResult, error) {
	if len(rows) == 0 {
		return &DataSyncResult{Success: true, Errors: []string{}}, nil
	}

	batchSize := 5000
	if dbType == "postgres" && len(cols) > 0 {
		maxCols := 65535 / len(cols)
		if maxCols < 1 {
			maxCols = 1
		}
		if batchSize > maxCols {
			batchSize = maxCols
		}
	}
	synced := 0
	var errors []string
	colList := buildColumnList(dbType, cols)
	placeholders := buildPlaceholders(dbType, len(cols))

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction failed: %w", err)
	}

	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		batch := rows[i:end]

		var valueParts []string
		var args []interface{}
		for j, row := range batch {
			ph := placeholders
			if dbType == "postgres" {
				ph = buildPGPlaceholders(len(cols), i+j)
			} else if dbType == "sqlserver" {
				ph = buildMSSQLPlaceholders(len(cols), i+j)
			}
			valueParts = append(valueParts, ph)
			args = append(args, row...)
		}

		var insertSQL string
		switch dbType {
		case "mysql":
			insertSQL = fmt.Sprintf("INSERT IGNORE INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		case "postgres":
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s ON CONFLICT DO NOTHING", table, colList, strings.Join(valueParts, ", "))
		case "oracle":
			insertSQL, args = buildOracleInsertAll(table, colList, batch, cols)
			if _, err := tx.ExecContext(ctx, insertSQL, args...); err != nil {
				_ = tx.Rollback()
				return &DataSyncResult{Success: false, SyncedRows: synced, Errors: []string{fmt.Sprintf("batch insert: %v", err)}}, nil
			}
			synced += len(batch)
			continue
		case "sqlserver":
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		default:
			insertSQL = fmt.Sprintf("INSERT INTO %s (%s) VALUES %s", table, colList, strings.Join(valueParts, ", "))
		}

		if _, err := tx.ExecContext(ctx, insertSQL, args...); err != nil {
			_ = tx.Rollback()
			errors = append(errors, fmt.Sprintf("batch insert: %v", err))
			return &DataSyncResult{Success: false, SyncedRows: synced, Errors: errors}, nil
		}
		synced += len(batch)
	}

	if err := tx.Commit(); err != nil {
		return &DataSyncResult{Success: false, SyncedRows: synced, Errors: []string{fmt.Sprintf("commit: %v", err)}}, nil
	}

	return &DataSyncResult{Success: true, SyncedRows: synced, TotalRows: synced}, nil
}

func buildPlaceholders(dbType string, nCols int) string {
	switch dbType {
	case "postgres":
		return "(" + strings.TrimSuffix(strings.Repeat("$? ,", nCols), " ,") + ")"
	case "oracle":
		return "(" + strings.TrimSuffix(strings.Repeat(":v?,", nCols), ",") + ")"
	case "sqlserver":
		return "(" + strings.TrimSuffix(strings.Repeat("@p?,", nCols), ",") + ")"
	default:
		return "(" + strings.TrimSuffix(strings.Repeat("?,", nCols), ",") + ")"
	}
}

func buildPGPlaceholders(nCols, offset int) string {
	parts := make([]string, nCols)
	for i := 0; i < nCols; i++ {
		parts[i] = fmt.Sprintf("$%d", offset*nCols+i+1)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

func buildMSSQLPlaceholders(nCols, offset int) string {
	parts := make([]string, nCols)
	for i := 0; i < nCols; i++ {
		parts[i] = fmt.Sprintf("@p%d", offset*nCols+i+1)
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// buildOracleInsertAll builds an INSERT ALL statement for Oracle multi-row insert.
// Oracle doesn't support multi-row VALUES, so we use:
//   INSERT ALL INTO t (c1,c2) VALUES (:1,:2) INTO t (c1,c2) VALUES (:3,:4) SELECT 1 FROM DUAL
func buildOracleInsertAll(table, colList string, batch [][]interface{}, allCols []string) (string, []interface{}) {
	var parts []string
	var args []interface{}
	for i, row := range batch {
		offset := i * len(allCols)
		placeholders := make([]string, len(allCols))
		for j := 0; j < len(allCols); j++ {
			placeholders[j] = fmt.Sprintf(":%d", offset+j+1)
		}
		parts = append(parts, fmt.Sprintf("INTO %s (%s) VALUES (%s)", table, colList, strings.Join(placeholders, ", ")))
		args = append(args, row...)
	}
	return fmt.Sprintf("INSERT ALL %s SELECT 1 FROM DUAL", strings.Join(parts, " ")), args
}

// ─── Batch Update ──────────────────────────────────────────────────────────

func batchUpdate(ctx context.Context, db *sql.DB, dbType, table string,
	cols []string, colIdx map[string]int, checkFields []string,
	toUpdate []diffUpdate, updateCols []string) (int, []string) {

	var errors []string
	synced := 0

	for _, batch := range ChunkRows(toUpdate, DefaultUpdateBatchSize) {
		for _, upd := range batch {
			// Build WHERE clause from check fields
			var whereParts []string
			var whereVals []interface{}
			for _, cf := range checkFields {
				idx := colIdx[cf]
				whereParts = append(whereParts, fmt.Sprintf("%s = ?", quoteColName(dbType, cf)))
				whereVals = append(whereVals, normalizeVal(upd.tgtRow[idx]))
			}

			// Build SET clause only for changed columns
			var setParts []string
			var setVals []interface{}
			for _, col := range updateCols {
				idx := colIdx[col]
				if !valuesEqual(upd.srcRow[idx], upd.tgtRow[idx]) {
					setParts = append(setParts, fmt.Sprintf("%s = ?", quoteColName(dbType, col)))
					setVals = append(setVals, normalizeVal(upd.srcRow[idx]))
				}
			}

			if len(setParts) == 0 {
				continue
			}

			allVals := append(setVals, whereVals...)
			sql := fmt.Sprintf("UPDATE %s SET %s WHERE %s",
				table, strings.Join(setParts, ", "), strings.Join(whereParts, " AND "))

			if _, err := db.ExecContext(ctx, sql, allVals...); err != nil {
				errors = append(errors, fmt.Sprintf("update: %v", err))
			} else {
				synced++
			}
		}
	}
	return synced, errors
}

func normalizeVal(v interface{}) interface{} {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}

// ─── Common ────────────────────────────────────────────────────────────────

func scanQueryResult(rows *sql.Rows) ([]string, [][]interface{}, error) {
	return drivers.ScanQueryResult(rows)
}
