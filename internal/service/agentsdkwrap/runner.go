package agentsdkwrap

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/semantic/engine"
	semantic_service "github.com/dbridge/dbridge/internal/semantic/service"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/dbridge/dbridge/internal/service/ai_security"
	agentsdk "github.com/opentoys/agentsdk"
	"github.com/opentoys/agentsdk/modules/aichat"
	"github.com/opentoys/agentsdk/types"
)

// PendingApproval stores the last approval request from tool execution.
var PendingApproval *ai_security.ApprovalCard

type Runner struct {
	agent  *agentsdk.Agent
	skills []SkillRef
}

// Skills returns the skills loaded for this runner (for observability).
func (r *Runner) Skills() []SkillRef { return r.skills }

type RunnerPool struct {
	mu sync.RWMutex
	m  map[string]*Runner
}

func NewRunnerPool() *RunnerPool              { return &RunnerPool{m: make(map[string]*Runner)} }
func (p *RunnerPool) Get(k string) *Runner    { p.mu.RLock(); defer p.mu.RUnlock(); return p.m[k] }
func (p *RunnerPool) Set(k string, r *Runner) { p.mu.Lock(); p.m[k] = r; p.mu.Unlock() }
func (p *RunnerPool) Delete(k string)         { p.mu.Lock(); delete(p.m, k); p.mu.Unlock() }

// NewRunner builds an agentsdk runner for the given agent. onToolCall is an
// optional observer invoked before each tool execution (for SSE/audit).
func NewRunner(ctx context.Context, agentCfg *repository.Agent,
	dsSvc *service.DataSourceService, querySvc *service.QueryService,
	tblSvc *service.TableManagerService, cmpSvc *service.CompareService,
	semanticSvc *semantic_service.SemanticQueryService,
	onToolCall func(name, args string)) (*Runner, error) {

	ss := service.NewSettingsService(repository.GetDB())
	sc, _ := ss.GetAIConfig()
	key, base := sc["ai_api_key"], sc["ai_base_url"]
	// Fallback to config.yaml if DB settings are empty
	if key == "" {
		cfg := service.NewAIService(repository.GetDB(), config.AIConfig{}).GetConfig()
		if cfg != nil {
			key = cfg["ai_api_key"]
			if base == "" {
				base = cfg["ai_base_url"]
			}
		}
	}
	if base == "" || base == "https://api.deepseek.com" {
		base = "https://api.deepseek.com/v1"
	}

	cc := aichat.New(aichat.WithKey(key), aichat.WithBase(base), aichat.WithModel(agentCfg.Model))
	fmt.Printf("[agent runner] agent=%s model=%s base=%s key_len=%d sys_prompt_len=%d tools=%d skills=%d\n",
		agentCfg.Name, agentCfg.Model, base, len(key), len(agentCfg.SystemPrompt),
		len(agentCfg.Tools), len(agentCfg.Skills))
	tools := buildTools(dsSvc, querySvc, tblSvc, cmpSvc, semanticSvc, agentCfg, onToolCall)

	skillsFS, refs, err := buildSkillsFS(agentCfg)
	if err != nil {
		return nil, err
	}

	cfg := types.Config{ChatClient: cc, Tools: tools, SkillsFS: skillsFS, SubAgents: buildSubAgents(agentCfg)}
	if agentCfg.SystemPrompt != "" {
		cfg.History = []types.ChatCompletionMessage{{Role: "system", Content: agentCfg.SystemPrompt}}
	}
	return &Runner{agent: agentsdk.New(cfg), skills: refs}, nil
}

// buildSubAgents converts the agent's SubAgents JSON array (agent IDs) into
// agentsdk sub-agent configs. Each referenced agent is exposed as a tool the
// parent agent can call. Sub-agents reuse the parent's tools/skills by default.
func buildSubAgents(agentCfg *repository.Agent) []types.SubAgentConfig {
	ids := parseStringArray(agentCfg.SubAgents)
	if len(ids) == 0 {
		return nil
	}
	db := repository.GetDB()
	out := make([]types.SubAgentConfig, 0, len(ids))
	for _, id := range ids {
		if id == "" || id == agentCfg.ID {
			continue
		}
		var ref repository.Agent
		if err := db.Where("id = ? AND deleted_at IS NULL", id).First(&ref).Error; err != nil {
			continue
		}
		out = append(out, types.SubAgentConfig{
			Name:         ref.Name,
			Description:  ref.Description,
			SystemPrompt: ref.SystemPrompt,
		})
	}
	return out
}

func (r *Runner) Run(ctx context.Context, p string) (string, error) { return r.agent.Run(ctx, p) }

// defaultReadTools is applied when the agent does not declare an explicit tool set.
var defaultReadTools = []string{"list_tables", "execute_query", "get_table_structure", "list_datasources", "semantic_list_engines", "semantic_list_cubes"}

func buildTools(dsSvc *service.DataSourceService, querySvc *service.QueryService,
	tblSvc *service.TableManagerService, cmpSvc *service.CompareService,
	semanticSvc *semantic_service.SemanticQueryService,
	agentCfg *repository.Agent, onToolCall func(name, args string)) []types.Tool {

	all := []types.Tool{
		{Type: "function", Function: &types.FunctionDefinition{Name: "list_tables",
			Description: "List tables in a schema. Args: {\"data_source_id\":\"...\", \"schema\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "list_tables", in)
				args := parseArgs(in)
				dsID := argVal(args, "data_source_id", "ds_id")
				schema := argVal(args, "schema", "s")
				if dsID == "" {
					return "Error: data_source_id required", nil
				}
				items, err := dsSvc.TableList(dsID, schema, argVal(args, "database", "db"))
				if err != nil {
					return "", err
				}
				out := fmt.Sprintf("%d tables:\n", len(items))
				for _, t := range items {
					out += fmt.Sprintf("- %s (%s)\n", t.Name, t.Type)
				}
				return out, nil
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "execute_query",
			Description: "Execute a read-only SQL query (SELECT/SHOW/DESCRIBE/EXPLAIN). Args: {\"data_source_id\":\"...\", \"sql\":\"...\", \"schema\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "execute_query", in)
				return executeSQL(querySvc, "query", in)
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "execute_dml",
			Description: "Execute a DML statement (INSERT/UPDATE/DELETE). Requires approval for high-risk operations. Args: {\"data_source_id\":\"...\", \"sql\":\"...\", \"schema\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "execute_dml", in)
				return executeSQL(querySvc, "dml", in)
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "execute_ddl",
			Description: "Execute a DDL statement (CREATE/ALTER/DROP/TRUNCATE). Requires approval for high-risk operations. Args: {\"data_source_id\":\"...\", \"sql\":\"...\", \"schema\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "execute_ddl", in)
				return executeSQL(querySvc, "ddl", in)
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "get_table_structure",
			Description: "Get full table structure (columns, types, indexes). Args: {\"data_source_id\":\"...\", \"schema\":\"...\", \"table\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "get_table_structure", in)
				args := parseArgs(in)
				dsID := argVal(args, "data_source_id", "ds_id")
				table := argVal(args, "table", "t")
				if dsID == "" || table == "" {
					return "Error: data_source_id and table required", nil
				}
				st, err := tblSvc.GetFullStructure(dsID, argVal(args, "schema", "s"), table, argVal(args, "database", "db"))
				if err != nil {
					return "", err
				}
				b, _ := json.Marshal(st)
				return string(b), nil
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "get_view_definition",
			Description: "Get a view's definition SQL. Args: {\"data_source_id\":\"...\", \"schema\":\"...\", \"view\":\"...\", \"database\":\"...\"}"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "get_view_definition", in)
				args := parseArgs(in)
				dsID := argVal(args, "data_source_id", "ds_id")
				view := argVal(args, "view", "table", "t")
				if dsID == "" || view == "" {
					return "Error: data_source_id and view required", nil
				}
				def, err := tblSvc.GetViewDefinition(dsID, argVal(args, "schema", "s"), view, argVal(args, "database", "db"))
				if err != nil {
					return "", err
				}
				return def, nil
			}},
		{Type: "function", Function: &types.FunctionDefinition{Name: "list_datasources",
			Description: "List authorized data sources"},
			Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "list_datasources", in)
				list, _ := dsSvc.List("default")
				out := ""
				for _, ds := range list {
					out += fmt.Sprintf("- %s (id=%s, type=%s)\n", ds.Name, ds.ID, ds.Type)
				}
				return out, nil
			}},
	}

	// ─── Semantic layer tools (only when semantic service is available) ──
	if semanticSvc != nil {
		all = append(all,
			types.Tool{Type: "function", Function: &types.FunctionDefinition{
				Name:        "semantic_list_engines",
				Description: "列出所有已注册的语义层引擎及其能力信息。无参数。",
			}, Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "semantic_list_engines", in)
				registry := semanticSvc.Registry()
				names := registry.List()
				out := ""
				for _, name := range names {
					eng, err := registry.Get(name)
					if err != nil {
						continue
					}
					cap := eng.Capabilities()
					out += fmt.Sprintf("- %s (version=%s, agg_types=%v)\n", cap.Name, cap.Version, cap.SupportedAggTypes)
				}
				if out == "" {
					return "No semantic engines registered", nil
				}
				return out, nil
			}},
			types.Tool{Type: "function", Function: &types.FunctionDefinition{
				Name:        "semantic_list_cubes",
				Description: "列出语义层中所有可用的 Cube（数据模型）。每个 Cube 包含 Measures（度量）和 Dimensions（维度）。Args: {\"engine\":\"引擎名（可选，不填则使用默认引擎）\"}",
			}, Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "semantic_list_cubes", in)
				var input struct {
					Engine string `json:"engine"`
				}
				json.Unmarshal([]byte(in), &input)
				registry := semanticSvc.Registry()
				var eng engine.SemanticQueryEngine
				var err error
				if input.Engine != "" {
					eng, err = registry.Get(input.Engine)
				} else {
					eng, err = registry.GetDefault()
				}
				if err != nil {
					return "Error: " + err.Error(), nil
				}
				cubes, err := eng.ListCubes(ctx, nil)
				if err != nil {
					return "Error: " + err.Error(), nil
				}
				out := fmt.Sprintf("%d cubes:\n", len(cubes))
				for _, c := range cubes {
					out += fmt.Sprintf("- %s (%s): %d measures, %d dimensions\n", c.Name, c.DisplayName, len(c.Measures), len(c.Dimensions))
				}
				return out, nil
			}},
			types.Tool{Type: "function", Function: &types.FunctionDefinition{
				Name: "semantic_query",
				Description: "执行语义层查询。通过指定 Measures（度量）和 Dimensions（维度）进行聚合查询，无需编写 SQL。" +
					"Args: {\"engine\":\"引擎名（可选）\", \"data_source_id\":\"数据源ID\", \"measures\":[\"度量名\"], \"dimensions\":[\"维度名\"], \"filters\":[{\"field\":\"字段\",\"operator\":\"=\",\"value\":\"值\"}], \"order\":[{\"field\":\"字段\",\"direction\":\"asc\"}], \"limit\":100, \"offset\":0}",
			}, Exec: func(ctx context.Context, in string) (string, error) {
				onToolCallSafe(onToolCall, "semantic_query", in)
				var input struct {
					Engine       string           `json:"engine"`
					DataSourceID string           `json:"data_source_id"`
					Measures     []string         `json:"measures"`
					Dimensions   []string         `json:"dimensions"`
					Filters      []engine.Filter  `json:"filters"`
					Order        []engine.OrderItem `json:"order"`
					Limit        int              `json:"limit"`
					Offset       int              `json:"offset"`
				}
				if err := json.Unmarshal([]byte(in), &input); err != nil {
					return "Error: invalid JSON input: " + err.Error(), nil
				}
				if len(input.Measures) == 0 {
					return "Error: measures required", nil
				}
				queryReq := &engine.QueryRequest{
					Engine:       input.Engine,
					DataSourceID: input.DataSourceID,
					Measures:     input.Measures,
					Dimensions:   input.Dimensions,
					Filters:      input.Filters,
					Order:        input.Order,
					Limit:        input.Limit,
					Offset:       input.Offset,
				}
				result, err := semanticSvc.Query(ctx, queryReq, nil, "", "", "", "", "agent")
				if err != nil {
					return "Error: " + err.Error(), nil
				}
				out := fmt.Sprintf("SQL: %s\n%d rows:\n", result.SQL, len(result.Rows))
				out += fmt.Sprintf("Columns: %v\n", result.Columns)
				for _, row := range result.Rows {
					out += fmt.Sprintf("%v\n", row)
				}
				return out, nil
			}},
		)
	}

	allowed := parseStringArray(agentCfg.Tools)
	fmt.Printf("[agent runner] agent=%s raw_tools=%q parsed_tools=%v\n", agentCfg.Name, agentCfg.Tools, allowed)
	if len(allowed) == 0 {
		allowed = defaultReadTools
		fmt.Printf("[agent runner] agent=%s using default tools: %v\n", agentCfg.Name, allowed)
	}
	allowedSet := make(map[string]bool, len(allowed))
	for _, name := range allowed {
		allowedSet[name] = true
	}
	filtered := make([]types.Tool, 0, len(all))
	for _, t := range all {
		if allowedSet[t.Function.Name] {
			filtered = append(filtered, t)
		}
	}
	fmt.Printf("[agent runner] agent=%s filtered %d tools from %d total\n", agentCfg.Name, len(filtered), len(all))
	return filtered
}

func executeSQL(querySvc *service.QueryService, kind, in string) (string, error) {
	args := parseArgs(in)
	dsID := argVal(args, "data_source_id", "ds_id")
	sql := argVal(args, "sql", "query")
	database := argVal(args, "database", "db")
	if dsID == "" || sql == "" {
		return "Error: data_source_id and sql required", nil
	}

	if ai_security.IsEnabled(ai_security.KeyRiskControl) {
		level, reason := ai_security.ClassifyRiskWithPolicy(dsID, sql)
		fmt.Printf("[AI-SEC] Risk check: kind=%s sql=%s level=%s reason=%s\n", kind, sql[:min(len(sql), 80)], level, reason)
		if level == ai_security.RiskL3 {
			PendingApproval = ai_security.BuildApprovalCard(sql, level, reason)
			return fmt.Sprintf("SECURITY_NOTICE: %s operation requires approval. SQL: %s. Reason: %s. Please inform the user that this operation needs to be approved, and the system will show an approval dialog.", kind, sql, reason), nil
		}
		if level == ai_security.RiskL2 {
			fmt.Printf("[AI-SEC] L2 risk for sql=%s reason=%s (proceeding silently)\n", sql[:min(len(sql), 80)], reason)
		}
	}

	r, err := querySvc.Execute(service.QueryInput{DataSourceID: dsID, SQL: sql, Schema: argVal(args, "schema", "s"), Database: database, PageSize: 20})
	if err != nil {
		return "", err
	}

	if ai_security.IsEnabled(ai_security.KeyDataMask) {
		r.Rows = ai_security.ApplyMaskRules(r.Columns, r.Rows)
	}

	out := fmt.Sprintf("%d rows:\n", len(r.Rows))
	for _, row := range r.Rows {
		out += fmt.Sprintf("%v\n", row)
	}
	return out, nil
}

func onToolCallSafe(cb func(name, args string), name, args string) {
	if cb != nil {
		cb(name, args)
	}
}

func parseArgs(in string) map[string]string {
	m := make(map[string]string)
	json.Unmarshal([]byte(in), &m)
	return m
}

func argVal(m map[string]string, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok && v != "" {
			return v
		}
	}
	return ""
}
