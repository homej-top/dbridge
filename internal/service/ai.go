package service

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/config"
	"github.com/dbridge/dbridge/internal/repository"
	cryptoPkg "github.com/dbridge/dbridge/pkg/crypto"
	"gorm.io/gorm"
)

var sqlBlockRe = regexp.MustCompile("(?s)```sql\\s*(.+?)\\s*```")

type AIService struct {
	db     *gorm.DB
	cfg    config.AIConfig
	client *http.Client
}

func NewAIService(db *gorm.DB, cfg config.AIConfig) *AIService {
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &AIService{
		db:  db,
		cfg: cfg,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	DataSourceID string    `json:"data_source_id"`
	Schema       string    `json:"schema"`
	Messages     []Message `json:"messages"`
}

type SQLRequest struct {
	DataSourceID string `json:"data_source_id"`
	Schema       string `json:"schema"`
	SQL          string `json:"sql"`
}

type FixRequest struct {
	DataSourceID string `json:"data_source_id"`
	Schema       string `json:"schema"`
	SQL          string `json:"sql"`
	Error        string `json:"error"`
}

type AIResponse struct {
	Content string `json:"content"`
	SQL     string `json:"sql,omitempty"`
}

func (s *AIService) IsConfigured() bool {
	apiKey, baseURL, _, _, _ := s.resolveConfig()
	return apiKey != "" || baseURL != ""
}

// GetConfig returns the resolved AI configuration as a map
func (s *AIService) GetConfig() map[string]string {
	apiKey, baseURL, model, _, _ := s.resolveConfig()
	return map[string]string{
		"ai_api_key":  apiKey,
		"ai_base_url": baseURL,
		"ai_model":    model,
	}
}

func (s *AIService) Chat(ctx context.Context, req ChatRequest) (*AIResponse, error) {
	if !s.IsConfigured() {
		return nil, fmt.Errorf("AI 服务未配置，请在设置中配置 LLM API Key")
	}
	if len(req.Messages) > 50 {
		return nil, fmt.Errorf("对话消息过多，最多支持 50 条")
	}
	for _, m := range req.Messages {
		if len(m.Content) > 10000 {
			return nil, fmt.Errorf("单条消息过长，最多 10000 字符")
		}
	}
	/*
		schemaCtx, err := s.buildSchemaContext(req.DataSourceID, req.Schema)
		if err != nil {
			schemaCtx = ""
		}

		// Determine database type for prompt customization
		dbType := "mysql"
		if req.DataSourceID != "" {
			var ds repository.DataSource
			if err := s.db.Where("id = ?", req.DataSourceID).First(&ds).Error; err == nil {
				dbType = ds.Type
			}
		}

		systemPrompt := s.buildText2SQLPrompt(schemaCtx, dbType)

		// Don't duplicate system prompt if caller already provided one
		messages := req.Messages
		if len(messages) == 0 || messages[0].Role != "system" {
			messages = append([]Message{{Role: "system", Content: systemPrompt}}, messages...)
		}
	*/
	content, err := s.callLLM(ctx, req.Messages)
	if err != nil {
		return nil, err
	}

	resp := &AIResponse{Content: content}
	if sqlStr := extractSQL(content); sqlStr != "" {
		resp.SQL = sqlStr
	}

	return resp, nil
}

func (s *AIService) ExplainSQL(ctx context.Context, req SQLRequest) (*AIResponse, error) {
	if !s.IsConfigured() {
		return nil, fmt.Errorf("AI 服务未配置，请在设置中配置 LLM API Key")
	}
	if len(req.SQL) > 50000 {
		return nil, fmt.Errorf("SQL 过长")
	}

	messages := []Message{
		{Role: "system", Content: "你是一个数据库专家。请用通俗易懂的中文解释以下 SQL 语句的功能、逻辑和可能的性能问题。"},
		{Role: "user", Content: fmt.Sprintf("请解释以下 SQL:\n```sql\n%s\n```", req.SQL)},
	}

	content, err := s.callLLM(ctx, messages)
	if err != nil {
		return nil, err
	}
	return &AIResponse{Content: content}, nil
}

func (s *AIService) OptimizeSQL(ctx context.Context, req SQLRequest) (*AIResponse, error) {
	if !s.IsConfigured() {
		return nil, fmt.Errorf("AI 服务未配置，请在设置中配置 LLM API Key")
	}
	if len(req.SQL) > 50000 {
		return nil, fmt.Errorf("SQL 过长")
	}

	explainResult := ""
	if req.DataSourceID != "" {
		explainResult, _ = s.getExplainResult(req.DataSourceID, req.Schema, req.SQL)
	}

	prompt := "你是一个数据库性能优化专家。请分析以下 SQL 语句，给出优化建议。\n\n要求：\n1. 指出当前 SQL 的性能问题\n2. 给出优化后的 SQL\n3. 解释优化的原因和预期效果\n"
	if explainResult != "" {
		prompt += fmt.Sprintf("\nEXPLAIN 结果:\n%s\n", explainResult)
	}

	messages := []Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: fmt.Sprintf("请优化以下 SQL:\n```sql\n%s\n```", req.SQL)},
	}

	content, err := s.callLLM(ctx, messages)
	if err != nil {
		return nil, err
	}

	resp := &AIResponse{Content: content}
	if sqlStr := extractSQL(content); sqlStr != "" {
		resp.SQL = sqlStr
	}
	return resp, nil
}

func (s *AIService) FixSQL(ctx context.Context, req FixRequest) (*AIResponse, error) {
	if !s.IsConfigured() {
		return nil, fmt.Errorf("AI 服务未配置，请在设置中配置 LLM API Key")
	}
	if len(req.SQL) > 50000 {
		return nil, fmt.Errorf("SQL 过长")
	}

	schemaCtx, _ := s.buildSchemaContext(req.DataSourceID, req.Schema)

	messages := []Message{
		{Role: "system", Content: fmt.Sprintf("你是一个数据库专家。请根据错误信息修复 SQL 语句。\n仅输出修正后的 SQL 和简要解释。\n\n%s", schemaCtx)},
		{Role: "user", Content: fmt.Sprintf("原始 SQL:\n```sql\n%s\n```\n\n错误信息:\n%s", req.SQL, req.Error)},
	}

	content, err := s.callLLM(ctx, messages)
	if err != nil {
		return nil, err
	}

	resp := &AIResponse{Content: content}
	if sqlStr := extractSQL(content); sqlStr != "" {
		resp.SQL = sqlStr
	}
	return resp, nil
}

func (s *AIService) buildSchemaContext(dataSourceID, schema string) (string, error) {
	if dataSourceID == "" {
		return "", nil
	}

	var ds repository.DataSource
	if err := s.db.Where("id = ?", dataSourceID).First(&ds).Error; err != nil {
		return "", err
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return "", err
	}

	conn, err := s.connectDS(ds, pwd)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if schema != "" && ds.Type == "mysql" && ds.Database == "" {
		if err := validateSchemaName(schema); err != nil {
			return "", fmt.Errorf("schema 名称验证失败: %w", err)
		}
		safeSchema := strings.ReplaceAll(schema, "`", "``")
		if _, err := conn.Exec("USE `" + safeSchema + "`"); err != nil {
			return "", fmt.Errorf("failed to use schema %s: %w", schema, err)
		}
	}

	var tables []string
	switch ds.Type {
	case "mysql":
		rows, err := conn.Query("SHOW TABLES")
		if err != nil {
			return "", err
		}
		defer rows.Close()
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				continue
			}
			tables = append(tables, t)
		}
		if rows.Err() != nil {
			return "", fmt.Errorf("row iteration failed: %w", rows.Err())
		}
	case "postgres":
		schemaName := "public"
		if schema != "" {
			schemaName = schema
		}
		rows, err := conn.Query("SELECT table_name FROM information_schema.tables WHERE table_schema = $1", schemaName)
		if err != nil {
			return "", err
		}
		defer rows.Close()
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				continue
			}
			tables = append(tables, t)
		}
		if rows.Err() != nil {
			return "", fmt.Errorf("row iteration failed: %w", rows.Err())
		}
	}

	if len(tables) == 0 {
		return "", nil
	}

	maxTables := 30
	if len(tables) > maxTables {
		tables = tables[:maxTables]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("数据库类型: %s\n", ds.Type))
	if schema != "" {
		sb.WriteString(fmt.Sprintf("Schema: %s\n", schema))
	} else if ds.Database != "" {
		sb.WriteString(fmt.Sprintf("Database: %s\n", ds.Database))
	}
	sb.WriteString("\n表结构:\n")

	for _, table := range tables {
		switch ds.Type {
		case "mysql":
			s.appendMySQLTableSchema(&sb, conn, table)
		case "postgres":
			s.appendPGTableSchema(&sb, conn, table, schema)
		}
	}

	return sb.String(), nil
}

func (s *AIService) appendMySQLTableSchema(sb *strings.Builder, conn *sql.DB, table string) {
	rows, err := conn.Query("SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, COLUMN_COMMENT FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION", table)
	if err != nil {
		return
	}
	defer rows.Close()

	safeTable := strings.ReplaceAll(table, "`", "``")
	sb.WriteString(fmt.Sprintf("\n-- %s\nCREATE TABLE `%s` (\n", safeTable, safeTable))
	first := true
	for rows.Next() {
		var name, colType, nullable, key, comment string
		if err := rows.Scan(&name, &colType, &nullable, &key, &comment); err != nil {
			continue
		}
		if !first {
			sb.WriteString(",\n")
		}
		first = false
		safeName := strings.ReplaceAll(name, "`", "``")
		line := fmt.Sprintf("  `%s` %s", safeName, colType)
		if nullable == "NO" {
			line += " NOT NULL"
		}
		if key == "PRI" {
			line += " PRIMARY KEY"
		}
		if comment != "" {
			safeComment := strings.ReplaceAll(comment, "'", "\\'")
			line += fmt.Sprintf(" COMMENT '%s'", safeComment)
		}
		sb.WriteString(line)
	}

	sb.WriteString("\n);\n")
	if rows.Err() != nil {
		logger.Error("row iteration failed for table")
	}
}

func (s *AIService) appendPGTableSchema(sb *strings.Builder, conn *sql.DB, table, schema string) {
	schemaName := "public"
	if schema != "" {
		schemaName = schema
	}
	rows, err := conn.Query("SELECT column_name, data_type, is_nullable, column_default FROM information_schema.columns WHERE table_schema = $1 AND table_name = $2 ORDER BY ordinal_position", schemaName, table)
	if err != nil {
		return
	}
	defer rows.Close()

	sb.WriteString(fmt.Sprintf("\n-- %s\nCREATE TABLE %s (\n", table, table))
	first := true
	for rows.Next() {

		var name, dataType, nullable string
		var defaultVal *string
		if err := rows.Scan(&name, &dataType, &nullable, &defaultVal); err != nil {
			continue
		}
		if !first {
			sb.WriteString(",\n")
		}
		first = false
		line := fmt.Sprintf("  %s %s", name, dataType)
		if nullable == "NO" {
			line += " NOT NULL"
		}
		if defaultVal != nil {
			line += fmt.Sprintf(" DEFAULT %s", *defaultVal)
		}
		sb.WriteString(line)
	}
	sb.WriteString("\n);\n")
	if rows.Err() != nil {
		logger.Error("row iteration failed for table")
		return
	}
}

func (s *AIService) resolveConfig() (apiKey, baseURL, model string, maxTokens int, temperature float64) {
	apiKey = s.cfg.APIKey
	baseURL = s.cfg.BaseURL
	model = s.cfg.Model
	maxTokens = s.cfg.MaxTokens
	temperature = s.cfg.Temperature

	if s.db != nil {
		var settings []struct {
			Key   string
			Value string
		}
		if err := s.db.Table("settings").Where("category = ?", "ai").Find(&settings).Error; err != nil {
			return
		}

		// Build a map for easy lookup
		settingsMap := make(map[string]string)
		for _, st := range settings {
			settingsMap[st.Key] = st.Value
		}

		// Check for active model first
		if activeModelID, ok := settingsMap["ai_active_model"]; ok && activeModelID != "" {
			prefix := "ai_model_" + activeModelID + "_"
			if v, ok := settingsMap[prefix+"api_key"]; ok && v != "" {
				apiKey = v
			}
			if v, ok := settingsMap[prefix+"base_url"]; ok && v != "" {
				baseURL = v
			}
			if v, ok := settingsMap[prefix+"model"]; ok && v != "" {
				model = v
			}
			// max_tokens and temperature are not stored per-model in settings
		}

		// Fall back to legacy settings if no active model or active model has no API key
		if apiKey == "" {
			if v, ok := settingsMap["ai_api_key"]; ok && v != "" {
				apiKey = v
			}
			if v, ok := settingsMap["ai_base_url"]; ok && v != "" {
				baseURL = v
			}
			if v, ok := settingsMap["ai_model"]; ok && v != "" {
				model = v
			}
		}
	}
	return
}

func (s *AIService) callLLM(ctx context.Context, messages []Message) (string, error) {
	apiKey, baseURL, model, maxTokens, temperature := s.resolveConfig()

	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	if temperature == 0 {
		temperature = 0.1
	}

	body := map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": temperature,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request failed: %w", err)
	}

	url := baseURL + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request failed: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("LLM 请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("LLM 服务返回错误 (状态码 %d)，请检查配置", resp.StatusCode)
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %w", err)
	}

	if len(result.Choices) == 0 {
		return "", fmt.Errorf("LLM 未返回结果")
	}

	return result.Choices[0].Message.Content, nil
}

func (s *AIService) connectDS(ds repository.DataSource, pwd string) (*sql.DB, error) {
	return openDBConn(ds, pwd)
}

func (s *AIService) getExplainResult(dataSourceID, schema, query string) (string, error) {
	trimmed := strings.TrimSpace(query)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return "", fmt.Errorf("only SELECT queries can be explained")
	}
	if strings.Contains(trimmed, ";") {
		return "", fmt.Errorf("multi-statement queries are not supported")
	}

	var ds repository.DataSource
	if err := s.db.Where("id = ?", dataSourceID).First(&ds).Error; err != nil {
		return "", err
	}

	pwd, err := cryptoPkg.Decrypt(ds.Password)
	if err != nil {
		return "", err
	}

	conn, err := s.connectDS(ds, pwd)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	if schema != "" && ds.Type == "mysql" && ds.Database == "" {
		if err := validateSchemaName(schema); err != nil {
			return "", fmt.Errorf("schema 名称验证失败: %w", err)
		}
		safeSchema := strings.ReplaceAll(schema, "`", "``")
		if _, err := conn.Exec("USE `" + safeSchema + "`"); err != nil {
			return "", fmt.Errorf("failed to use schema: %w", err)
		}
	}

	explainSQL := "EXPLAIN " + trimmed
	rows, err := conn.Query(explainSQL)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var result strings.Builder
	result.WriteString(strings.Join(cols, "\t") + "\n")

	for rows.Next() {
		values := make([]interface{}, len(cols))
		valuePtrs := make([]interface{}, len(cols))
		for i := range values {
			valuePtrs[i] = &values[i]
		}
		if err := rows.Scan(valuePtrs...); err != nil {
			continue
		}
		var parts []string
		for _, v := range values {
			if v == nil {
				parts = append(parts, "NULL")
			} else if b, ok := v.([]byte); ok {
				parts = append(parts, string(b))
			} else {
				parts = append(parts, fmt.Sprintf("%v", v))
			}
		}
		result.WriteString(strings.Join(parts, "\t") + "\n")
	}
	return result.String(), nil
}

func extractSQL(content string) string {
	matches := sqlBlockRe.FindStringSubmatch(content)
	if len(matches) >= 2 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// ChatCompletionWithAgent runs a simple completion using an agent's config
func (s *AIService) ChatCompletionWithAgent(agent repository.Agent, userPrompt string) (string, error) {
	model := agent.Model
	if model == "" {
		model = s.cfg.Model
	}
	temp := agent.Temperature
	if temp == 0 {
		temp = 0.2
	}
	maxTokens := agent.MaxTokens
	if maxTokens == 0 {
		maxTokens = 4096
	}

	sysPrompt := agent.SystemPrompt
	if sysPrompt == "" {
		sysPrompt = "You are a helpful assistant. Respond with valid JSON only."
	}

	messages := []Message{
		{Role: "system", Content: sysPrompt},
		{Role: "user", Content: userPrompt},
	}

	apiKey, baseURL, _, _, _ := s.resolveConfig()
	baseURL = strings.TrimRight(baseURL, "/")

	body := map[string]interface{}{
		"model":       model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": temp,
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := baseURL + "/chat/completions"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("llm error %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}
	return result.Choices[0].Message.Content, nil
}
