package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SystemTenantID is the reserved tenant identifier for built-in skills.
const SystemTenantID = "__system__"

// AISkill represents an AI skill/prompt template (service-layer view)
type AISkill struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Name           string `json:"name"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	Category       string `json:"category"`
	PromptTemplate string `json:"prompt_template"`
	SystemPrompt   string `json:"system_prompt"`
	Tools          string `json:"tools"`
	ScriptFiles    string `json:"script_files"`
	Resources      string `json:"resources"`
	InputVars      string `json:"input_vars"`
	OutputFormat   string `json:"output_format"`
	ScriptSandbox  string `json:"script_sandbox"`
	IsBuiltin      bool   `json:"is_builtin"`
	IsActive       bool   `json:"is_active"`
	Version        int    `json:"version"`
	AuthorID       string `json:"author_id"`
	TenantID       string `json:"tenant_id"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type AISkillService struct {
	db *gorm.DB
}

func NewAISkillService(db *gorm.DB) *AISkillService {
	return &AISkillService{db: db}
}

type CreateAISkillInput struct {
	Slug           string `json:"slug"`
	Name           string `json:"name" binding:"required"`
	Description    string `json:"description"`
	Icon           string `json:"icon"`
	Category       string `json:"category"`
	PromptTemplate string `json:"prompt_template" binding:"required"`
	SystemPrompt   string `json:"system_prompt"`
	Tools          string `json:"tools"`
	ScriptFiles    string `json:"script_files"`
	Resources      string `json:"resources"`
	InputVars      string `json:"input_vars"`
	OutputFormat   string `json:"output_format"`
	ScriptSandbox  string `json:"script_sandbox"`
}

type UpdateAISkillInput struct {
	Slug           *string `json:"slug"`
	Name           *string `json:"name"`
	Description    *string `json:"description"`
	Icon           *string `json:"icon"`
	Category       *string `json:"category"`
	PromptTemplate *string `json:"prompt_template"`
	SystemPrompt   *string `json:"system_prompt"`
	Tools          *string `json:"tools"`
	ScriptFiles    *string `json:"script_files"`
	Resources      *string `json:"resources"`
	InputVars      *string `json:"input_vars"`
	OutputFormat   *string `json:"output_format"`
	ScriptSandbox  *string `json:"script_sandbox"`
}

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{2,63}$`)
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)
var injectionTokenRe = regexp.MustCompile(`<\|[a-zA-Z0-9_:.-]+\|>`)

var allowedSkillTools = map[string]bool{
	"list_tables":         true,
	"execute_query":       true,
	"execute_dml":         true,
	"execute_ddl":         true,
	"get_table_structure": true,
	"get_view_definition": true,
	"list_datasources":    true,
}

// ToRepository converts the service-layer view to the repository model.
func (s *AISkill) ToRepository() repository.AISkill {
	return repository.AISkill{
		ID: s.ID, Slug: s.Slug, Name: s.Name, Description: s.Description,
		Icon: s.Icon, Category: s.Category, PromptTemplate: s.PromptTemplate,
		SystemPrompt: s.SystemPrompt, Tools: s.Tools, ScriptFiles: s.ScriptFiles,
		Resources: s.Resources, InputVars: s.InputVars, OutputFormat: s.OutputFormat,
		ScriptSandbox: s.ScriptSandbox, IsBuiltin: s.IsBuiltin, IsActive: s.IsActive,
		Version: s.Version, AuthorID: s.AuthorID, TenantID: s.TenantID,
	}
}

func (s *AISkillService) mapSkill(row map[string]interface{}) *AISkill {
	skill := &AISkill{}
	mapStr(row, "id", &skill.ID)
	mapStr(row, "slug", &skill.Slug)
	mapStr(row, "name", &skill.Name)
	mapStr(row, "description", &skill.Description)
	mapStr(row, "icon", &skill.Icon)
	mapStr(row, "category", &skill.Category)
	mapStr(row, "prompt_template", &skill.PromptTemplate)
	mapStr(row, "system_prompt", &skill.SystemPrompt)
	mapStr(row, "tools", &skill.Tools)
	mapStr(row, "script_files", &skill.ScriptFiles)
	mapStr(row, "resources", &skill.Resources)
	mapStr(row, "input_vars", &skill.InputVars)
	mapStr(row, "output_format", &skill.OutputFormat)
	mapStr(row, "script_sandbox", &skill.ScriptSandbox)
	if v, ok := row["is_builtin"]; ok && v != nil {
		if n, ok2 := v.(int64); ok2 {
			skill.IsBuiltin = n == 1
		}
	}
	if v, ok := row["is_active"]; ok && v != nil {
		if n, ok2 := v.(int64); ok2 {
			skill.IsActive = n == 1
		}
	}
	if v, ok := row["version"]; ok && v != nil {
		if n, ok2 := v.(int64); ok2 {
			skill.Version = int(n)
		}
	}
	mapStr(row, "author_id", &skill.AuthorID)
	mapStr(row, "tenant_id", &skill.TenantID)
	mapStr(row, "created_at", &skill.CreatedAt)
	mapStr(row, "updated_at", &skill.UpdatedAt)
	return skill
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

func (s *AISkillService) querySkills(query string, args ...interface{}) ([]*AISkill, error) {
	rows, err := s.db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []*AISkill
	for rows.Next() {
		vals := make([]interface{}, len(cols))
		ptrs := make([]interface{}, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			continue
		}
		row := make(map[string]interface{})
		for i, c := range cols {
			row[c] = vals[i]
		}
		results = append(results, s.mapSkill(row))
	}
	if results == nil {
		results = make([]*AISkill, 0)
	}
	return results, nil
}

const skillSelectCols = `id, slug, name, description, icon, category, prompt_template, system_prompt, tools,
	script_files, resources, input_vars, output_format, script_sandbox,
	CAST(is_builtin AS INTEGER) as is_builtin, CAST(is_active AS INTEGER) as is_active, version, author_id, tenant_id,
	created_at, updated_at`

// List returns AI skills, optionally filtered by category
func (s *AISkillService) List(tenantID, category string) ([]*AISkill, error) {
	query := `SELECT ` + skillSelectCols + `
		FROM ai_skills WHERE (tenant_id = ? OR tenant_id = ? OR CAST(is_builtin AS INTEGER) = 1)`
	args := []interface{}{tenantID, SystemTenantID}
	if category != "" {
		query += " AND category = ?"
		args = append(args, category)
	}
	query += " ORDER BY is_builtin DESC, updated_at DESC"
	return s.querySkills(query, args...)
}

// Get returns a single AI skill by ID
func (s *AISkillService) Get(id string) (*AISkill, error) {
	skills, err := s.querySkills(`SELECT `+skillSelectCols+` FROM ai_skills WHERE id = ?`, id)
	if err != nil || len(skills) == 0 {
		return nil, fmt.Errorf("ai skill not found: %s", id)
	}
	return skills[0], nil
}

// Create creates a new AI skill
func (s *AISkillService) Create(input CreateAISkillInput, authorID, tenantID string) (*AISkill, error) {
	if err := s.validateInput(&input); err != nil {
		return nil, err
	}
	if tenantID == "" {
		tenantID = SystemTenantID
	}
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")

	slug := strings.TrimSpace(input.Slug)
	if slug == "" {
		slug = slugify(input.Name)
	}
	if slug == "" {
		slug = "skill-" + uuid.New().String()[:8]
	}
	resolved, err := s.resolveSlug(slug, tenantID, "")
	if err != nil {
		return nil, err
	}

	icon := orDefault(input.Icon, "thunderbolt")
	category := orDefault(input.Category, "general")
	outputFormat := orDefault(input.OutputFormat, "markdown")
	tools := orDefault(input.Tools, "[]")
	scriptFiles := orDefault(input.ScriptFiles, "[]")
	resources := orDefault(input.Resources, "[]")
	inputVars := orDefault(input.InputVars, "[]")
	scriptSandbox := orDefault(input.ScriptSandbox, `{"allow_network":false,"max_memory_mb":128,"timeout_seconds":30}`)

	err = s.db.Exec(`INSERT INTO ai_skills (id, slug, name, description, icon, category, prompt_template, system_prompt, tools, script_files, resources, input_vars, output_format, script_sandbox, is_builtin, is_active, version, author_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 1, 1, ?, ?, ?, ?)`,
		id, resolved, strings.TrimSpace(input.Name), sanitizeSkillContent(input.Description), icon, category,
		sanitizeSkillContent(input.PromptTemplate), sanitizeSkillContent(input.SystemPrompt), tools,
		scriptFiles, resources, inputVars, outputFormat, scriptSandbox,
		authorID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Update updates an AI skill
func (s *AISkillService) Update(id string, input UpdateAISkillInput) (*AISkill, error) {
	skill, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	original := *skill

	applyStr := func(dst *string, src *string) {
		if src != nil {
			*dst = *src
		}
	}
	applyStr(&skill.Name, input.Name)
	applyStr(&skill.Description, input.Description)
	applyStr(&skill.Icon, input.Icon)
	applyStr(&skill.Category, input.Category)
	applyStr(&skill.PromptTemplate, input.PromptTemplate)
	applyStr(&skill.SystemPrompt, input.SystemPrompt)
	applyStr(&skill.Tools, input.Tools)
	applyStr(&skill.ScriptFiles, input.ScriptFiles)
	applyStr(&skill.Resources, input.Resources)
	applyStr(&skill.InputVars, input.InputVars)
	applyStr(&skill.OutputFormat, input.OutputFormat)
	applyStr(&skill.ScriptSandbox, input.ScriptSandbox)
	if input.Slug != nil {
		skill.Slug = strings.TrimSpace(*input.Slug)
	}

	// Validate
	if strings.TrimSpace(skill.Name) == "" {
		return nil, fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(skill.PromptTemplate) == "" {
		return nil, fmt.Errorf("prompt_template 不能为空")
	}
	if skill.Slug != "" && !slugRe.MatchString(skill.Slug) {
		return nil, fmt.Errorf("slug 仅允许小写字母/数字/连字符，长度 3~64")
	}
	if err := validateTools(skill.Tools); err != nil {
		return nil, err
	}

	tenant := skill.TenantID
	if tenant == "" {
		tenant = SystemTenantID
	}
	if skill.Slug == "" {
		skill.Slug = slugify(skill.Name)
		if skill.Slug == "" {
			skill.Slug = "skill-" + uuid.New().String()[:8]
		}
	}
	resolved, err := s.resolveSlug(skill.Slug, tenant, id)
	if err != nil {
		return nil, err
	}
	skill.Slug = resolved
	if skill.OutputFormat == "" {
		skill.OutputFormat = "markdown"
	}
	if skill.Tools == "" {
		skill.Tools = "[]"
	}

	if err := s.snapshotVersion(&original); err != nil {
		return nil, err
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	err = s.db.Exec(`UPDATE ai_skills SET slug=?, name=?, description=?, icon=?, category=?, prompt_template=?, system_prompt=?, tools=?, script_files=?, resources=?, input_vars=?, output_format=?, script_sandbox=?, version=version+1, updated_at=? WHERE id=?`,
		skill.Slug, strings.TrimSpace(skill.Name), sanitizeSkillContent(skill.Description), skill.Icon, skill.Category,
		sanitizeSkillContent(skill.PromptTemplate), sanitizeSkillContent(skill.SystemPrompt), skill.Tools,
		skill.ScriptFiles, skill.Resources, skill.InputVars, skill.OutputFormat, skill.ScriptSandbox, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete deletes an AI skill (built-in skills are protected at handler level)
func (s *AISkillService) Delete(id string) error {
	return s.db.Exec(`DELETE FROM ai_skills WHERE id = ? AND is_builtin = 0`, id).Error
}

// ToggleActive toggles the active status of an AI skill
func (s *AISkillService) ToggleActive(id string) (*AISkill, error) {
	skill, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	newActive := !skill.IsActive
	now := time.Now().Format("2006-01-02 15:04:05")
	err = s.db.Exec(`UPDATE ai_skills SET is_active = ?, updated_at = ? WHERE id = ?`,
		newActive, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// ─── Version history & references ─────────────────────────────────────────

func (s *AISkillService) snapshotVersion(skill *AISkill) error {
	v := repository.AISkillVersion{
		ID:             uuid.New().String(),
		SkillID:        skill.ID,
		Version:        skill.Version,
		Name:           skill.Name,
		Description:    skill.Description,
		PromptTemplate: skill.PromptTemplate,
		SystemPrompt:   skill.SystemPrompt,
		Tools:          skill.Tools,
		ScriptFiles:    skill.ScriptFiles,
		Resources:      skill.Resources,
		AuthorID:       skill.AuthorID,
		CreatedAt:      time.Now(),
	}
	return s.db.Create(&v).Error
}

// GetVersions returns the version history for a skill (newest first).
func (s *AISkillService) GetVersions(id string) ([]repository.AISkillVersion, error) {
	var versions []repository.AISkillVersion
	if err := s.db.Where("skill_id = ?", id).Order("version DESC").Find(&versions).Error; err != nil {
		return nil, err
	}
	if versions == nil {
		versions = make([]repository.AISkillVersion, 0)
	}
	return versions, nil
}

// Rollback restores a skill to a historical version. The restored content is
// persisted as a new version (the current state is snapshotted first).
func (s *AISkillService) Rollback(id string, version int) (*AISkill, error) {
	var v repository.AISkillVersion
	if err := s.db.Where("skill_id = ? AND version = ?", id, version).First(&v).Error; err != nil {
		return nil, fmt.Errorf("version not found: %d", version)
	}

	name := v.Name
	desc := v.Description
	prompt := v.PromptTemplate
	sys := v.SystemPrompt
	tools := v.Tools
	scripts := v.ScriptFiles
	resources := v.Resources
	return s.Update(id, UpdateAISkillInput{
		Name:           &name,
		Description:    &desc,
		PromptTemplate: &prompt,
		SystemPrompt:   &sys,
		Tools:          &tools,
		ScriptFiles:    &scripts,
		Resources:      &resources,
	})
}

// CountAgentRefs returns how many agents reference the given skill ID.
func (s *AISkillService) CountAgentRefs(skillID string) (int, error) {
	var agents []repository.Agent
	if err := s.db.Where("deleted_at IS NULL").Find(&agents).Error; err != nil {
		return 0, err
	}
	count := 0
	for _, a := range agents {
		var ids []string
		if err := json.Unmarshal([]byte(a.Skills), &ids); err != nil {
			continue
		}
		for _, id := range ids {
			if id == skillID {
				count++
				break
			}
		}
	}
	return count, nil
}

// ─── Validation / Slug helpers ────────────────────────────────────────────

func (s *AISkillService) validateInput(input *CreateAISkillInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("name 不能为空")
	}
	if strings.TrimSpace(input.PromptTemplate) == "" {
		return fmt.Errorf("prompt_template 不能为空")
	}
	if input.Slug != "" && !slugRe.MatchString(input.Slug) {
		return fmt.Errorf("slug 仅允许小写字母/数字/连字符，长度 3~64")
	}
	if err := validateTools(input.Tools); err != nil {
		return err
	}
	return nil
}

func validateTools(toolsJSON string) error {
	if strings.TrimSpace(toolsJSON) == "" || toolsJSON == "[]" {
		return nil
	}
	var tools []string
	if err := json.Unmarshal([]byte(toolsJSON), &tools); err != nil {
		return fmt.Errorf("tools 必须是 JSON 字符串数组: %w", err)
	}
	for _, t := range tools {
		if !allowedSkillTools[t] {
			return fmt.Errorf("不允许的工具: %s", t)
		}
	}
	return nil
}

// resolveSlug returns the first available slug for the given tenant scope.
func (s *AISkillService) resolveSlug(base, tenantID, excludeID string) (string, error) {
	base = strings.Trim(base, "-")
	if base == "" {
		base = "skill"
	}
	for attempt := 0; attempt < 100; attempt++ {
		candidate := base
		if attempt > 0 {
			candidate = fmt.Sprintf("%s-%d", base, attempt+1)
		}
		var count int64
		var err error
		if excludeID == "" {
			err = s.db.Raw(`SELECT COUNT(*) FROM ai_skills WHERE slug = ? AND tenant_id = ?`, candidate, tenantID).Scan(&count).Error
		} else {
			err = s.db.Raw(`SELECT COUNT(*) FROM ai_skills WHERE slug = ? AND tenant_id = ? AND id <> ?`, candidate, tenantID, excludeID).Scan(&count).Error
		}
		if err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("无法生成唯一 slug")
}

func slugify(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == ' ' || r == '-' || r == '_':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func sanitizeSkillContent(s string) string {
	if s == "" {
		return s
	}
	s = strings.TrimSpace(s)
	s = htmlTagRe.ReplaceAllString(s, "")
	s = injectionTokenRe.ReplaceAllString(s, "")
	if len(s) > 50000 {
		s = s[:50000]
	}
	return strings.TrimSpace(s)
}

// BackfillSkillSlugs fills empty slugs for existing skills (idempotent).
// It must run after AutoMigrate and before any unique slug constraint is created.
func (s *AISkillService) BackfillSkillSlugs() error {
	rows, err := s.db.Raw(`SELECT id, name, slug, tenant_id, is_builtin FROM ai_skills`).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()

	now := time.Now().Format("2006-01-02 15:04:05")
	type rec struct {
		ID        string
		Name      string
		Slug      string
		TenantID  string
		IsBuiltin bool
	}
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.ID, &r.Name, &r.Slug, &r.TenantID, &r.IsBuiltin); err != nil {
			continue
		}
		if r.Slug != "" {
			continue
		}
		if r.IsBuiltin {
			// Built-in slugs are managed by SeedBuiltinSkills.
			continue
		}
		base := slugify(r.Name)
		if base == "" {
			base = "skill-" + uuid.New().String()[:8]
		}
		tenant := r.TenantID
		if tenant == "" {
			tenant = SystemTenantID
		}
		resolved, err := s.resolveSlug(base, tenant, r.ID)
		if err != nil {
			return err
		}
		if err := s.db.Exec(`UPDATE ai_skills SET slug = ?, updated_at = ? WHERE id = ?`, resolved, now, r.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

// ─── Built-in skills ──────────────────────────────────────────────────────

type builtinSkill struct {
	ID, Slug, Name, Desc, Icon, Category, Prompt, System string
	Tools                                                []string
}

// SeedBuiltinSkills upserts built-in skills by stable ID, keeping slug and
// tenant_id normalized so they are globally unique and never tenant-owned.
func (s *AISkillService) SeedBuiltinSkills() error {
	now := time.Now().Format("2006-01-02 15:04:05")
	skills := []builtinSkill{
		{
			"builtin-data-analysis", "builtin-data-analysis", "Data Analysis",
			"Analyze database tables and generate insights", "bar-chart", "analysis",
			"Analyze the data in {{table}} and provide insights about data distribution, null rates, and patterns.",
			"You are a data analyst specializing in database analysis.", []string{"execute_query", "list_tables"},
		},
		{
			"builtin-performance", "builtin-performance", "Performance Optimization",
			"Optimize SQL queries and indexes", "thunderbolt", "optimization",
			"Analyze the following SQL query and suggest performance optimizations including index recommendations: {{sql}}",
			"You are a database performance expert.", []string{"execute_query", "get_table_structure"},
		},
		{
			"builtin-migration", "builtin-migration", "Migration Assistant",
			"Assist with database migration planning", "swap", "migration",
			"Help plan the migration from {{source_db}} to {{target_db}} for table {{table}}. Identify potential issues and generate migration SQL.",
			"You are a database migration expert.", []string{"list_tables", "get_table_structure"},
		},
		{
			"builtin-table-design", "builtin-table-design", "Table Design",
			"Design and review database table schemas", "table", "design",
			"Review and suggest improvements for the table design: {{table}}. Consider normalization, indexing, data types, and naming conventions.",
			"You are a database schema design expert.", []string{"get_table_structure"},
		},
		{
			"mask_rule_gen", "mask-rule-gen", "脱敏规则生成器",
			"分析表结构，自动识别敏感字段并生成脱敏规则配置", "tool", "general",
			"根据给定的表结构信息，识别包含敏感信息（手机号、身份证号、邮箱、银行卡号等）的字段，并为每个字段生成合适的脱敏规则。",
			"你是数据安全与脱敏规则专家，只输出结构化 JSON，不臆造字段。", []string{"get_table_structure"},
		},
		{
			"builtin-export-assistant", "builtin-export-assistant", "数据导出助手",
			"根据用户需求生成数据导出 SQL 与说明", "rocket", "migration",
			"根据表 {{table}} 的导出需求，生成 SELECT 导出 SQL，并说明导出字段、过滤条件与注意事项。",
			"你是数据导出助手，只生成可执行的只读 SQL。", []string{"list_tables", "get_table_structure", "execute_query"},
		},
		{
			"builtin-sql-transpile", "builtin-sql-transpile", "SQL 方言转换",
			"在不同数据库方言之间转换 SQL", "swap", "migration",
			"将以下 SQL 从 {{source_db}} 转换为 {{target_db}} 方言，并说明转换要点：{{sql}}",
			"你是数据库方言专家，准确转换类型、函数与分页语法。", []string{},
		},
		{
			"builtin-data-audit", "builtin-data-audit", "数据质量审计",
			"审计表数据质量，识别空值、重复与异常值", "bulb", "analysis",
			"分析表 {{table}} 的数据质量，检查空值率、重复值与异常分布，并给出改进建议。",
			"你是数据质量专家，结论基于真实查询结果。", []string{"get_table_structure", "execute_query"},
		},
	}

	for _, sk := range skills {
		toolsJSON, _ := json.Marshal(sk.Tools)
		var exists int64
		s.db.Raw(`SELECT COUNT(*) FROM ai_skills WHERE id = ?`, sk.ID).Scan(&exists)
		if exists > 0 {
			err := s.db.Exec(`UPDATE ai_skills SET slug=?, name=?, description=?, icon=?, category=?, prompt_template=?, system_prompt=?, tools=?, is_builtin=1, is_active=1, tenant_id=?, updated_at=? WHERE id=?`,
				sk.Slug, sk.Name, sk.Desc, sk.Icon, sk.Category, sk.Prompt, sk.System, string(toolsJSON),
				SystemTenantID, now, sk.ID).Error
			if err != nil {
				return err
			}
			continue
		}
		err := s.db.Exec(`INSERT INTO ai_skills (id, slug, name, description, icon, category, prompt_template, system_prompt, tools, script_files, resources, input_vars, output_format, script_sandbox, is_builtin, is_active, version, author_id, tenant_id, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '[]', '[]', '[]', 'markdown', '{"allow_network":false,"max_memory_mb":128,"timeout_seconds":30}', 1, 1, 1, 'system', ?, ?, ?)`,
			sk.ID, sk.Slug, sk.Name, sk.Desc, sk.Icon, sk.Category, sk.Prompt, sk.System, string(toolsJSON),
			SystemTenantID, now, now).Error
		if err != nil {
			return err
		}
	}
	return nil
}
