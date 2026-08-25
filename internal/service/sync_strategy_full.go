package service

import (
	"context"
	"fmt"
)

// FullSyncStrategy handles full-table data synchronization.
// Reads all rows from the source table and inserts them into the target table.
type FullSyncStrategy struct{}

func (s *FullSyncStrategy) Name() string { return "full" }

func (s *FullSyncStrategy) Validate(req SyncDataRequest) error {
	return nil
}

func (s *FullSyncStrategy) Execute(ctx context.Context, conn *SyncConnection, req SyncDataRequest) (*DataSyncResult, error) {
	colList := buildColumnList(conn.SourceDS.Type, conn.SyncColumns)
	selectSQL := fmt.Sprintf("SELECT %s FROM %s", colList, conn.SourceTable)

	rows, err := conn.SourceDB.QueryContext(ctx, selectSQL)
	if err != nil {
		return nil, fmt.Errorf("read source data failed: %w", err)
	}
	defer rows.Close()

	_, srcData, err := scanQueryResult(rows)
	if err != nil {
		return nil, fmt.Errorf("scan source rows failed: %w", err)
	}

	if len(srcData) == 0 {
		return &DataSyncResult{Success: true, TotalRows: 0, SyncedRows: 0, SkippedRows: 0, Errors: []string{}}, nil
	}

	return batchInsert(ctx, conn.TargetDB, conn.TargetDS.Type, conn.TargetTable, conn.SyncColumns, srcData)
}
