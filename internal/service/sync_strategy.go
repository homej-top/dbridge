package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/dbridge/dbridge/internal/repository"
)

// ─── Strategy Interface ────────────────────────────────────────────────────

// DataSyncStrategy defines the interface for data synchronization strategies.
// Each strategy handles a specific synchronization mode (full, diff, selected, etc.)
type DataSyncStrategy interface {
	// Name returns the strategy identifier (e.g., "full", "diff", "selected")
	Name() string

	// Validate checks that the request has all required fields for this strategy
	Validate(req SyncDataRequest) error

	// Execute performs the synchronization and returns the result
	Execute(ctx context.Context, conn *SyncConnection, req SyncDataRequest) (*DataSyncResult, error)
}

// ─── Sync Connection ───────────────────────────────────────────────────────

// SyncConnection bundles source and target database connections with metadata.
type SyncConnection struct {
	SourceDB    *sql.DB
	SourceDS    repository.DataSource
	SourceTable string // quoted table name

	TargetDB    *sql.DB
	TargetDS    repository.DataSource
	TargetTable string // quoted table name

	SyncColumns []string // common columns to sync
	PKColumn    string   // detected primary key column (if any)
}

// ─── RowKey ─────────────────────────────────────────────────────────────────

// BuildRowKey constructs a collision-safe key for diff comparison.
// Uses JSON encoding to avoid delimiter collisions (e.g., "|" in field values).
func BuildRowKey(values []interface{}) string {
	b, _ := json.Marshal(values)
	return string(b)
}

// ─── Batch Helpers ─────────────────────────────────────────────────────────

const (
	DefaultInsertBatchSize = 500
	DefaultUpdateBatchSize = 500
	DefaultDeleteBatchSize = 1000
	MaxBatchSize           = 2000
)

// ChunkRows splits a slice into batches of the given size.
func ChunkRows[T any](rows []T, size int) [][]T {
	if size <= 0 {
		size = DefaultInsertBatchSize
	}
	if size > MaxBatchSize {
		size = MaxBatchSize
	}
	var batches [][]T
	for i := 0; i < len(rows); i += size {
		end := i + size
		if end > len(rows) {
			end = len(rows)
		}
		batches = append(batches, rows[i:end])
	}
	return batches
}

// ─── Strategy Registry ─────────────────────────────────────────────────────

// SyncStrategyRegistry manages registered sync strategies.
type SyncStrategyRegistry struct {
	strategies map[string]DataSyncStrategy
}

// NewSyncStrategyRegistry creates a registry with all built-in strategies pre-registered.
func NewSyncStrategyRegistry() *SyncStrategyRegistry {
	r := &SyncStrategyRegistry{strategies: make(map[string]DataSyncStrategy)}
	r.Register(&FullSyncStrategy{})
	r.Register(&DiffSyncStrategy{})
	r.Register(&SelectedSyncStrategy{})
	return r
}

// Register adds a strategy to the registry.
func (r *SyncStrategyRegistry) Register(s DataSyncStrategy) {
	r.strategies[s.Name()] = s
}

// Get returns the named strategy, or an error if not found.
func (r *SyncStrategyRegistry) Get(name string) (DataSyncStrategy, error) {
	s, ok := r.strategies[name]
	if !ok {
		return nil, fmt.Errorf("unsupported sync mode: %s", name)
	}
	return s, nil
}

// Default returns the default strategy name.
func (r *SyncStrategyRegistry) Default() string {
	return "full"
}
