package service

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

var memberPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*\.[a-zA-Z_][a-zA-Z0-9_]*$`)

var validOperators = map[string]bool{
	"equals":      true,
	"notEquals":   true,
	"contains":    true,
	"gt":          true,
	"gte":         true,
	"lt":          true,
	"lte":         true,
	"in":          true,
	"set":         true,
	"notSet":      true,
}

const (
	maxLimit     = 10000
	maxPageSize  = 10000
)

// ValidateQueryRequest 校验语义查询请求。
func ValidateQueryRequest(req *engine.QueryRequest) error {
	if req == nil {
		return fmt.Errorf("query request is nil")
	}

	for _, m := range req.Measures {
		if !memberPattern.MatchString(m) {
			return fmt.Errorf("invalid measure format: %q, expected cube_name.member_name", m)
		}
	}

	for _, d := range req.Dimensions {
		if !memberPattern.MatchString(d) {
			return fmt.Errorf("invalid dimension format: %q, expected cube_name.member_name", d)
		}
	}

	for _, td := range req.TimeDimensions {
		if !memberPattern.MatchString(td.Dimension) {
			return fmt.Errorf("invalid time dimension format: %q, expected cube_name.member_name", td.Dimension)
		}
	}

	for _, f := range req.Filters {
		if !memberPattern.MatchString(f.Member) {
			return fmt.Errorf("invalid filter member format: %q, expected cube_name.member_name", f.Member)
		}
		if !validOperators[f.Operator] {
			return fmt.Errorf("unsupported filter operator: %q", f.Operator)
		}
		if requiresValues(f.Operator) && len(f.Values) == 0 {
			return fmt.Errorf("operator %q requires at least one value", f.Operator)
		}
	}

	for _, o := range req.Order {
		if !memberPattern.MatchString(o.Member) {
			return fmt.Errorf("invalid order member format: %q, expected cube_name.member_name", o.Member)
		}
		dir := strings.ToLower(o.Direction)
		if dir != "asc" && dir != "desc" {
			return fmt.Errorf("invalid order direction: %q, expected asc or desc", o.Direction)
		}
	}

	if req.Limit < 0 {
		return fmt.Errorf("limit must be >= 0, got %d", req.Limit)
	}
	if req.Limit > maxLimit {
		return fmt.Errorf("limit %d exceeds maximum %d", req.Limit, maxLimit)
	}
	if req.Offset < 0 {
		return fmt.Errorf("offset must be >= 0, got %d", req.Offset)
	}

	return nil
}

func requiresValues(op string) bool {
	switch op {
	case "equals", "notEquals", "contains", "gt", "gte", "lt", "lte", "in":
		return true
	case "set", "notSet":
		return false
	}
	return false
}
