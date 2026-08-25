package service

import (
	"context"
	"fmt"
)

// DiffSyncStrategy handles row-level diff synchronization.
// Compares source and target rows by checkFields, then INSERT new / UPDATE changed rows.
type DiffSyncStrategy struct{}

func (s *DiffSyncStrategy) Name() string { return "diff" }

func (s *DiffSyncStrategy) Validate(req SyncDataRequest) error {
	opts := req.Options
	if len(opts.CheckFields) == 0 {
		return fmt.Errorf("diff sync requires at least one check field")
	}
	return nil
}

func (s *DiffSyncStrategy) Execute(ctx context.Context, conn *SyncConnection, req SyncDataRequest) (*DataSyncResult, error) {
	opts := req.Options
	cols := conn.SyncColumns

	// Validate check fields exist in sync columns
	colIdx := make(map[string]int, len(cols))
	for i, c := range cols {
		colIdx[c] = i
	}
	for _, cf := range opts.CheckFields {
		if _, ok := colIdx[cf]; !ok {
			return &DataSyncResult{Success: false, Errors: []string{
				fmt.Sprintf("check field %q not in sync columns", cf),
			}}, nil
		}
	}

	// ─── Read source data ─────────────────────────────────────────────
	colList := buildColumnList(conn.SourceDS.Type, cols)
	sourceRows, err := conn.SourceDB.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s", colList, conn.SourceTable))
	if err != nil {
		return nil, fmt.Errorf("read source: %w", err)
	}
	_, srcData, err := scanQueryResult(sourceRows)
	sourceRows.Close()
	if err != nil {
		return nil, fmt.Errorf("scan source: %w", err)
	}

	// ─── Read target data ─────────────────────────────────────────────
	targetRows, err := conn.TargetDB.QueryContext(ctx,
		fmt.Sprintf("SELECT %s FROM %s", buildColumnList(conn.TargetDS.Type, cols), conn.TargetTable))
	if err != nil {
		return nil, fmt.Errorf("read target: %w", err)
	}
	_, tgtData, err := scanQueryResult(targetRows)
	targetRows.Close()
	if err != nil {
		return nil, fmt.Errorf("scan target: %w", err)
	}

	// ─── Build target index ───────────────────────────────────────────
	targetMap := make(map[string][]interface{}, len(tgtData))
	for _, row := range tgtData {
		targetMap[buildDiffKey(row, colIdx, opts.CheckFields)] = row
	}

	// ─── Diff: classify rows ──────────────────────────────────────────
	var toInsert [][]interface{}
	var toUpdate []diffUpdate
	skipped := 0

	for _, srcRow := range srcData {
		key := buildDiffKey(srcRow, colIdx, opts.CheckFields)
		tgtRow, exists := targetMap[key]
		if !exists {
			toInsert = append(toInsert, srcRow)
		} else if hasValueDiff(srcRow, tgtRow, cols, opts.CheckFields) {
			toUpdate = append(toUpdate, diffUpdate{srcRow: srcRow, tgtRow: tgtRow})
		} else {
			skipped++
		}
	}

	totalRows := len(srcData)
	syncedRows := 0
	var errors []string

	// ─── Apply changes (chunked) ──────────────────────────────────────
	if len(toInsert) > 0 {
		result, err := batchInsert(ctx, conn.TargetDB, conn.TargetDS.Type, conn.TargetTable, cols, toInsert)
		if err != nil {
			errors = append(errors, fmt.Sprintf("insert: %s", err))
		} else {
			syncedRows += result.SyncedRows
			errors = append(errors, result.Errors...)
		}
	}

	if len(toUpdate) > 0 {
		updateCols := resolveUpdateCols(cols, opts)
		updated, updErrs := batchUpdate(ctx, conn.TargetDB, conn.TargetDS.Type, conn.TargetTable,
			cols, colIdx, opts.CheckFields, toUpdate, updateCols)
		syncedRows += updated
		errors = append(errors, updErrs...)
	}

	return &DataSyncResult{
		Success:     len(errors) == 0,
		TotalRows:   totalRows,
		SyncedRows:  syncedRows,
		SkippedRows: skipped,
		Errors:      errors,
	}, nil
}

// ─── Helpers ───────────────────────────────────────────────────────────────

// buildDiffKey creates a collision-safe key from check field values using JSON.
func buildDiffKey(row []interface{}, colIdx map[string]int, checkFields []string) string {
	vals := make([]interface{}, len(checkFields))
	for i, cf := range checkFields {
		idx := colIdx[cf]
		if b, ok := row[idx].([]byte); ok {
			vals[i] = string(b)
		} else {
			vals[i] = row[idx]
		}
	}
	return BuildRowKey(vals)
}

// hasValueDiff checks if any non-check-field value differs between source and target.
func hasValueDiff(srcRow, tgtRow []interface{}, cols []string, checkFields []string) bool {
	checkSet := make(map[string]bool, len(checkFields))
	for _, cf := range checkFields {
		checkSet[cf] = true
	}
	for _, col := range cols {
		if checkSet[col] {
			continue
		}
		idx := colIdxOf(cols, col)
		if !valuesEqual(srcRow[idx], tgtRow[idx]) {
			return true
		}
	}
	return false
}

func colIdxOf(cols []string, col string) int {
	for i, c := range cols {
		if c == col {
			return i
		}
	}
	return -1
}

func valuesEqual(a, b interface{}) bool {
	// []byte fields (often from SQL scans) need special handling
	if ba, ok := a.([]byte); ok {
		a = string(ba)
	}
	if bb, ok := b.([]byte); ok {
		b = string(bb)
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func resolveUpdateCols(syncCols []string, opts DataSyncOptions) []string {
	if len(opts.SyncColumns) > 0 {
		return opts.SyncColumns
	}
	return syncCols
}
