package repository

import (
	"strings"

	"github.com/dbridge/dbridge/internal/service/drivers"
)

// SQLBuilder wraps query building with system dialect support
type SQLBuilder struct {
	d drivers.DatabaseDriver
}

// NewSQLBuilder creates a new query builder with the system dialect
func NewSQLBuilder() *SQLBuilder {
	return &SQLBuilder{d: GetDialect()}
}

// Build replaces {col_name} placeholders with dialect-specific time formatting
func (b *SQLBuilder) Build(query string, cols ...string) string {
	result := query
	for _, col := range cols {
		result = strings.ReplaceAll(result, "{"+col+"}", b.d.SQLFormatTime(col))
	}
	return result
}
