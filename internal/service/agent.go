package service

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AgentService struct {
	db *gorm.DB
}

func NewAgentService(db *gorm.DB) *AgentService {
	return &AgentService{db: db}
}

// ─── Agent CRUD ───────────────────────────────────────────────────────────

type CreateAgentInput struct {
	Name         string              `json:"name"`
	Description  string              `json:"description"`
	SystemPrompt string              `json:"system_prompt"`
	Model        string              `json:"model"`
	Temperature  float64             `json:"temperature"`
	MaxTokens    int                 `json:"max_tokens"`
	OutputFormat string              `json:"output_format"`
	ResultFormat string              `json:"result_format"`
	Scope        string              `json:"scope"`
	Tools        []string            `json:"tools"`
	Skills       []string            `json:"skills"`
	SubAgents    []string            `json:"sub_agents"`
	Datasources  []AgentDSInput      `json:"datasources"`
}

type AgentDSInput struct {
	DSID       string `json:"ds_id"`
	Database   string `json:"database"`
	Schema     string `json:"schema"`
	Permission string `json:"permission"`
}

func (s *AgentService) Create(userID, tenantID string, input CreateAgentInput) (*repository.Agent, error) {
	toolsJSON, _ := json.Marshal(input.Tools)
	skillsJSON, _ := json.Marshal(input.Skills)
	subJSON, _ := json.Marshal(input.SubAgents)

	if input.Scope == "" {
		input.Scope = "user"
	}
	if input.Model == "" {
		input.Model = "deepseek-v4-flash"
	}

	agent := &repository.Agent{
		ID:           "agent_" + uuid.New().String()[:12],
		Name:         input.Name,
		Description:  input.Description,
		SystemPrompt: input.SystemPrompt,
		Model:        input.Model,
		Temperature:  input.Temperature,
		MaxTokens:    input.MaxTokens,
		OutputFormat: input.OutputFormat,
		ResultFormat: input.ResultFormat,
		Scope:        input.Scope,
		Tools:        string(toolsJSON),
		Skills:       string(skillsJSON),
		SubAgents:    string(subJSON),
		UserID:       userID,
		TenantID:     tenantID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.db.Create(agent).Error; err != nil {
		return nil, err
	}

	// Create datasource associations
	for _, ds := range input.Datasources {
		ads := &repository.AgentDataSource{
			ID:         "ads_" + uuid.New().String()[:12],
			AgentID:    agent.ID,
			DSID:       ds.DSID,
			Database:   ds.Database,
			SchemaName: ds.Schema,
			Permission: ds.Permission,
		}
		s.db.Create(ads)
	}

	return agent, nil
}

func (s *AgentService) List(userID, tenantID string) ([]repository.Agent, error) {
	var agents []repository.Agent
	q := s.db.Where("deleted_at IS NULL")
	if userID != "" {
		q = q.Where("scope = 'global' OR user_id = ?", userID)
	}
	if tenantID != "" {
		q = q.Where("tenant_id = ? OR tenant_id = ''", tenantID)
	}
	err := q.Order("created_at DESC").Find(&agents).Error
	if agents == nil {
		agents = make([]repository.Agent, 0)
	}
	return agents, err
}

func (s *AgentService) Get(id string) (*repository.Agent, error) {
	var agent repository.Agent
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&agent).Error; err != nil {
		return nil, err
	}
	return &agent, nil
}

func (s *AgentService) Update(id string, input CreateAgentInput) (*repository.Agent, error) {
	var agent repository.Agent
	if err := s.db.Where("id = ? AND deleted_at IS NULL", id).First(&agent).Error; err != nil {
		return nil, err
	}

	toolsJSON, _ := json.Marshal(input.Tools)
	skillsJSON, _ := json.Marshal(input.Skills)
	subJSON, _ := json.Marshal(input.SubAgents)

	fmt.Printf("[agent service] updating agent=%s tools=%v tools_json=%s\n", id, input.Tools, string(toolsJSON))

	agent.Name = input.Name
	agent.Description = input.Description
	agent.SystemPrompt = input.SystemPrompt
	agent.Model = input.Model
	agent.Temperature = input.Temperature
	agent.MaxTokens = input.MaxTokens
	agent.OutputFormat = input.OutputFormat
	agent.ResultFormat = input.ResultFormat
	agent.Scope = input.Scope
	agent.Tools = string(toolsJSON)
	agent.Skills = string(skillsJSON)
	agent.SubAgents = string(subJSON)
	agent.UpdatedAt = time.Now()

	if err := s.db.Save(&agent).Error; err != nil {
		return nil, err
	}

	fmt.Printf("[agent service] updated agent=%s saved_tools=%s\n", id, agent.Tools)

	// Replace datasources
	s.db.Where("agent_id = ?", id).Delete(&repository.AgentDataSource{})
	for _, ds := range input.Datasources {
		ads := &repository.AgentDataSource{
			ID:         "ads_" + uuid.New().String()[:12],
			AgentID:    agent.ID,
			DSID:       ds.DSID,
			Database:   ds.Database,
			SchemaName: ds.Schema,
			Permission: ds.Permission,
		}
		s.db.Create(ads)
	}

	return &agent, nil
}

func (s *AgentService) Delete(id string) error {
	return s.db.Model(&repository.Agent{}).Where("id = ?", id).Update("deleted_at", time.Now()).Error
}

// ─── Datasources ──────────────────────────────────────────────────────────

func (s *AgentService) GetDatasources(agentID string) ([]repository.AgentDataSource, error) {
	var items []repository.AgentDataSource
	err := s.db.Where("agent_id = ?", agentID).Find(&items).Error
	if items == nil {
		items = make([]repository.AgentDataSource, 0)
	}
	return items, err
}

// ─── Permission Check ─────────────────────────────────────────────────────

func (s *AgentService) CheckPermission(agentID, dsID, database, schema string) (string, error) {
	var ads repository.AgentDataSource
	err := s.db.Where("agent_id = ? AND ds_id = ?", agentID, dsID).First(&ads).Error
	if err != nil {
		return "", fmt.Errorf("access denied: datasource not authorized")
	}
	return ads.Permission, nil
}
