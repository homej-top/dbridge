package adapter

import (
	"fmt"
	"strings"

	"github.com/dbridge/dbridge/internal/semantic/engine"
)

// LocalCompiler 将语义查询编译为 SQL。
type LocalCompiler struct {
	registry *LocalRegistry
}

// NewLocalCompiler 创建编译器。
func NewLocalCompiler(registry *LocalRegistry) *LocalCompiler {
	return &LocalCompiler{registry: registry}
}

type compiledQuery struct {
	SQL  string
	Args []interface{}
}

// Compile 将 QueryRequest 编译为可执行 SQL。
func (c *LocalCompiler) Compile(req *engine.QueryRequest) (*compiledQuery, error) {
	if len(req.Measures) == 0 {
		return nil, fmt.Errorf("at least one measure is required")
	}

	cubeName := extractCubeName(req.Measures[0])
	cube, ok := c.registry.GetCube(cubeName)
	if !ok {
		return nil, fmt.Errorf("cube not found: %s", cubeName)
	}

	alias := cubeName
	var args []interface{}

	// SELECT
	selectCols, err := c.buildSelect(req, cube, alias)
	if err != nil {
		return nil, err
	}

	// FROM
	from := c.buildFrom(cube, alias)

	// JOIN
	joinClause, joinArgs := c.buildJoins(req, cube, alias)
	args = append(args, joinArgs...)

	// WHERE
	whereClause, whereArgs := c.buildWhere(req, cube, alias)
	args = append(args, whereArgs...)

	// GROUP BY
	groupBy := c.buildGroupBy(req, cube, alias)

	// ORDER BY
	orderBy := c.buildOrderBy(req, cube, alias)

	// Assemble
	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(strings.Join(selectCols, ", "))
	sb.WriteString(" ")
	sb.WriteString(from)
	if joinClause != "" {
		sb.WriteString(" ")
		sb.WriteString(joinClause)
	}
	if whereClause != "" {
		sb.WriteString(" WHERE ")
		sb.WriteString(whereClause)
	}
	if groupBy != "" {
		sb.WriteString(" GROUP BY ")
		sb.WriteString(groupBy)
	}
	if orderBy != "" {
		sb.WriteString(" ORDER BY ")
		sb.WriteString(orderBy)
	}
	if req.Limit > 0 {
		sb.WriteString(fmt.Sprintf(" LIMIT %d", req.Limit))
	}
	if req.Offset > 0 {
		sb.WriteString(fmt.Sprintf(" OFFSET %d", req.Offset))
	}

	return &compiledQuery{SQL: sb.String(), Args: args}, nil
}

func (c *LocalCompiler) buildSelect(req *engine.QueryRequest, cube *engine.CubeDefinition, alias string) ([]string, error) {
	var cols []string

	for _, m := range req.Measures {
		_, measureName := splitMember(m)
		measure, err := findMeasure(cube, measureName)
		if err != nil {
			return nil, err
		}
		expr := replacePlaceholder(measure.SQL, alias)
		agg := wrapAggregate(measure.Type, expr)
		cols = append(cols, fmt.Sprintf("%s AS %s", agg, quoteIdentifier(measureName)))
	}

	for _, d := range req.Dimensions {
		_, dimName := splitMember(d)
		dim, err := findDimension(cube, dimName)
		if err != nil {
			return nil, err
		}
		expr := replacePlaceholder(dim.SQL, alias)
		cols = append(cols, fmt.Sprintf("%s AS %s", expr, quoteIdentifier(dimName)))
	}

	for _, td := range req.TimeDimensions {
		_, dimName := splitMember(td.Dimension)
		dim, err := findDimension(cube, dimName)
		if err != nil {
			return nil, err
		}
		expr := replacePlaceholder(dim.SQL, alias)
		truncated := applyGranularity(expr, td.Granularity)
		cols = append(cols, fmt.Sprintf("%s AS %s", truncated, quoteIdentifier(dimName)))
	}

	return cols, nil
}

func (c *LocalCompiler) buildFrom(cube *engine.CubeDefinition, alias string) string {
	table := cube.SQLTable
	if cube.Schema != "" {
		table = cube.Schema + "." + table
	}
	return fmt.Sprintf("FROM %s AS %s", table, alias)
}

func (c *LocalCompiler) buildJoins(req *engine.QueryRequest, cube *engine.CubeDefinition, alias string) (string, []interface{}) {
	if len(cube.Joins) == 0 {
		return "", nil
	}

	neededCubes := c.collectNeededCubes(req, cube)
	if len(neededCubes) == 0 {
		return "", nil
	}

	var parts []string
	for _, j := range cube.Joins {
		if !neededCubes[j.JoinCubeName] {
			continue
		}
		joinType := strings.ToUpper(j.JoinType)
		if joinType == "" {
			joinType = "LEFT"
		} else {
			joinType = strings.ReplaceAll(joinType, "_", " ")
		}
		cond := replaceJoinPlaceholders(j.SQL)
		parts = append(parts, fmt.Sprintf("%s JOIN %s ON %s", joinType, j.JoinCubeName, cond))
	}

	return strings.Join(parts, " "), nil
}

func (c *LocalCompiler) collectNeededCubes(req *engine.QueryRequest, cube *engine.CubeDefinition) map[string]bool {
	needed := make(map[string]bool)
	for _, d := range req.Dimensions {
		cubeRef, _ := splitMember(d)
		if cubeRef != "" && cubeRef != cube.Name {
			needed[cubeRef] = true
		}
	}
	for _, f := range req.Filters {
		cubeRef, _ := splitMember(f.Member)
		if cubeRef != "" && cubeRef != cube.Name {
			needed[cubeRef] = true
		}
	}
	return needed
}

func (c *LocalCompiler) buildWhere(req *engine.QueryRequest, cube *engine.CubeDefinition, alias string) (string, []interface{}) {
	var conditions []string
	var args []interface{}

	for _, f := range req.Filters {
		cond, condArgs := c.buildFilter(f, cube, alias)
		if cond != "" {
			conditions = append(conditions, cond)
			args = append(args, condArgs...)
		}
	}

	for _, td := range req.TimeDimensions {
		if len(td.DateRange) == 2 {
			_, dimName := splitMember(td.Dimension)
			dim, err := findDimension(cube, dimName)
			if err != nil {
				continue
			}
			expr := replacePlaceholder(dim.SQL, alias)
			conditions = append(conditions, fmt.Sprintf("%s >= ? AND %s <= ?", expr, expr))
			args = append(args, td.DateRange[0], td.DateRange[1])
		}
	}

	return strings.Join(conditions, " AND "), args
}

func (c *LocalCompiler) buildFilter(f engine.Filter, cube *engine.CubeDefinition, alias string) (string, []interface{}) {
	cubeRef, fieldName := splitMember(f.Member)
	if cubeRef == "" {
		cubeRef = alias
	}

	dim, err := findDimension(cube, fieldName)
	if err != nil {
		measure, err2 := findMeasure(cube, fieldName)
		if err2 != nil {
			return "", nil
		}
		return c.buildFilterExpr(wrapAggregate(measure.Type, replacePlaceholder(measure.SQL, alias)), f.Operator, f.Values)
	}

	expr := replacePlaceholder(dim.SQL, alias)
	if cubeRef != alias {
		expr = cubeRef + "." + fieldName
	}
	return c.buildFilterExpr(expr, f.Operator, f.Values)
}

func (c *LocalCompiler) buildFilterExpr(expr string, operator string, values []string) (string, []interface{}) {
	switch operator {
	case "equals":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s = ?", expr), []interface{}{values[0]}
	case "notEquals":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s != ?", expr), []interface{}{values[0]}
	case "contains":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s LIKE ?", expr), []interface{}{"%" + values[0] + "%"}
	case "gt":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s > ?", expr), []interface{}{values[0]}
	case "gte":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s >= ?", expr), []interface{}{values[0]}
	case "lt":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s < ?", expr), []interface{}{values[0]}
	case "lte":
		if len(values) == 0 {
			return "", nil
		}
		return fmt.Sprintf("%s <= ?", expr), []interface{}{values[0]}
	case "in":
		if len(values) == 0 {
			return "", nil
		}
		placeholders := make([]string, len(values))
		args := make([]interface{}, len(values))
		for i, v := range values {
			placeholders[i] = "?"
			args[i] = v
		}
		return fmt.Sprintf("%s IN (%s)", expr, strings.Join(placeholders, ", ")), args
	case "set":
		return fmt.Sprintf("%s IS NOT NULL", expr), nil
	case "notSet":
		return fmt.Sprintf("%s IS NULL", expr), nil
	default:
		return "", nil
	}
}

func (c *LocalCompiler) buildGroupBy(req *engine.QueryRequest, cube *engine.CubeDefinition, alias string) string {
	var dims []string

	for _, d := range req.Dimensions {
		_, dimName := splitMember(d)
		dim, err := findDimension(cube, dimName)
		if err != nil {
			continue
		}
		expr := replacePlaceholder(dim.SQL, alias)
		dims = append(dims, expr)
	}

	for _, td := range req.TimeDimensions {
		_, dimName := splitMember(td.Dimension)
		dim, err := findDimension(cube, dimName)
		if err != nil {
			continue
		}
		expr := replacePlaceholder(dim.SQL, alias)
		dims = append(dims, applyGranularity(expr, td.Granularity))
	}

	return strings.Join(dims, ", ")
}

func (c *LocalCompiler) buildOrderBy(req *engine.QueryRequest, cube *engine.CubeDefinition, alias string) string {
	if len(req.Order) == 0 {
		return ""
	}

	var parts []string
	for _, o := range req.Order {
		cubeRef, fieldName := splitMember(o.Member)
		dir := strings.ToUpper(o.Direction)
		if dir != "ASC" && dir != "DESC" {
			dir = "ASC"
		}

		if dim, err := findDimension(cube, fieldName); err == nil {
			expr := replacePlaceholder(dim.SQL, alias)
			if cubeRef != "" && cubeRef != alias {
				expr = cubeRef + "." + fieldName
			}
			parts = append(parts, fmt.Sprintf("%s %s", expr, dir))
		} else if measure, err := findMeasure(cube, fieldName); err == nil {
			parts = append(parts, fmt.Sprintf("%s %s", quoteIdentifier(fieldName), dir))
			_ = measure
		}
	}

	return strings.Join(parts, ", ")
}

// ─── helpers ────────────────────────────────────────────────────────────────

func extractCubeName(member string) string {
	parts := strings.SplitN(member, ".", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return ""
}

func splitMember(member string) (string, string) {
	parts := strings.SplitN(member, ".", 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", member
}

func findMeasure(cube *engine.CubeDefinition, name string) (*engine.MeasureDetail, error) {
	for _, m := range cube.Measures {
		if m.Name == name {
			return m, nil
		}
	}
	return nil, fmt.Errorf("measure %q not found in cube %q", name, cube.Name)
}

func findDimension(cube *engine.CubeDefinition, name string) (*engine.DimensionDetail, error) {
	for _, d := range cube.Dimensions {
		if d.Name == name {
			return d, nil
		}
	}
	return nil, fmt.Errorf("dimension %q not found in cube %q", name, cube.Name)
}

func replacePlaceholder(sql string, alias string) string {
	var result strings.Builder
	i := 0
	for i < len(sql) {
		if sql[i] == '{' {
			end := strings.IndexByte(sql[i:], '}')
			if end != -1 {
				field := sql[i+1 : i+end]
				result.WriteString(alias)
				result.WriteByte('.')
				result.WriteString(field)
				i += end + 1
				continue
			}
		}
		result.WriteByte(sql[i])
		i++
	}
	return result.String()
}

func replaceJoinPlaceholders(sql string) string {
	result := sql
	for {
		start := strings.Index(result, "{")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		end += start
		cubeName := result[start+1 : end]
		result = result[:start] + cubeName + result[end+1:]
	}
	return result
}

func wrapAggregate(aggType string, expr string) string {
	switch strings.ToLower(aggType) {
	case "sum":
		return fmt.Sprintf("SUM(%s)", expr)
	case "avg":
		return fmt.Sprintf("AVG(%s)", expr)
	case "min":
		return fmt.Sprintf("MIN(%s)", expr)
	case "max":
		return fmt.Sprintf("MAX(%s)", expr)
	case "count":
		return fmt.Sprintf("COUNT(%s)", expr)
	case "count_distinct":
		return fmt.Sprintf("COUNT(DISTINCT %s)", expr)
	case "number":
		return expr
	default:
		return fmt.Sprintf("SUM(%s)", expr)
	}
}

func applyGranularity(expr string, granularity string) string {
	switch strings.ToLower(granularity) {
	case "day":
		return fmt.Sprintf("DATE(%s)", expr)
	case "week":
		return fmt.Sprintf("DATE(%s)", expr)
	case "month":
		return fmt.Sprintf("DATE_FORMAT(%s, '%%%%Y-%%%%m-01')", expr)
	case "quarter":
		return fmt.Sprintf("CONCAT(YEAR(%s), '-Q', QUARTER(%s))", expr, expr)
	case "year":
		return fmt.Sprintf("DATE_FORMAT(%s, '%%%%Y-01-01')", expr)
	default:
		return expr
	}
}

func quoteIdentifier(name string) string {
	return "`" + name + "`"
}
