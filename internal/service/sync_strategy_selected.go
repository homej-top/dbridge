package service

import (
	"context"
	"fmt"
)

// SelectedSyncStrategy syncs only explicitly selected rows to the target table.
type SelectedSyncStrategy struct{}

func (s *SelectedSyncStrategy) Name() string { return "selected" }

func (s *SelectedSyncStrategy) Validate(req SyncDataRequest) error {
	if len(req.Options.SelectedRows) == 0 {
		return fmt.Errorf("selected sync requires at least one selected row")
	}
	return nil
}

func (s *SelectedSyncStrategy) Execute(ctx context.Context, conn *SyncConnection, req SyncDataRequest) (*DataSyncResult, error) {
	opts := req.Options
	cols := conn.SyncColumns

	// Convert SelectedRows ([]map[string]any) to [][]interface{} in column order
	var allRows [][]interface{}
	for _, rowMap := range opts.SelectedRows {
		row := make([]interface{}, len(cols))
		for i, col := range cols {
			row[i] = rowMap[col]
		}
		allRows = append(allRows, row)
	}

	if len(allRows) == 0 {
		return &DataSyncResult{Success: true, Errors: []string{}}, nil
	}

	return batchInsert(ctx, conn.TargetDB, conn.TargetDS.Type, conn.TargetTable, cols, allRows)
}
