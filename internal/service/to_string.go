package service

import "fmt"

// toString safely converts an interface{} value to string.
// Handles []byte (from SQLite BLOB/TEXT columns) by converting to string.
func toString(v interface{}) string {
	if v == nil {
		return "<nil>"
	}
	switch val := v.(type) {
	case []byte:
		return string(val)
	case string:
		return val
	default:
		return fmt.Sprintf("%v", v)
	}
}

func mapStr(row map[string]interface{}, key string, target *string) {
	if v, ok := row[key]; ok && v != nil {
		if b, ok := v.([]byte); ok {
			*target = string(b)
		} else {
			*target = toString(v)
		}
	}
}
