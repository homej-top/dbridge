package engine

import "strings"

// RowLevelFilter 行级权限过滤定义。
type RowLevelFilter struct {
	CubeName string   `json:"cube_name,omitempty"`
	Roles    []string `json:"roles,omitempty"`
	Filters  []Filter `json:"filters"`
}

// ResolveRowLevelFilters 根据 Cube 名称和用户角色解析适用的行级过滤规则，
// 并替换 Filter 中 Values 的 ${user.xxx} 变量。
func ResolveRowLevelFilters(rlFilters []RowLevelFilter, req *QueryRequest, cubeName string, userRoles []string) []Filter {
	roleSet := make(map[string]struct{}, len(userRoles))
	for _, r := range userRoles {
		roleSet[r] = struct{}{}
	}

	var resolved []Filter
	for _, rlf := range rlFilters {
		if rlf.CubeName != "" && rlf.CubeName != cubeName {
			continue
		}
		if !rolesMatch(rlf.Roles, roleSet) {
			continue
		}
		for _, f := range rlf.Filters {
			resolved = append(resolved, Filter{
				Member:   f.Member,
				Operator: f.Operator,
				Values:   replaceVariables(f.Values, req.UserContext),
			})
		}
	}
	return resolved
}

func rolesMatch(required []string, userRoles map[string]struct{}) bool {
	if len(required) == 0 {
		return true
	}
	for _, r := range required {
		if _, ok := userRoles[r]; ok {
			return true
		}
	}
	return false
}

// replaceVariables 将 "${user.xxx}" 替换为 UserContext 中对应的值。
func replaceVariables(values []string, userCtx map[string]string) []string {
	if len(userCtx) == 0 {
		return values
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = v
		for k, val := range userCtx {
			placeholder := "${user." + k + "}"
			out[i] = strings.ReplaceAll(out[i], placeholder, val)
		}
	}
	return out
}
