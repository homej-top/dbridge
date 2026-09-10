package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/pkg/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Script represents a SQL script or folder node
type Script struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	ParentID    *string `json:"parent_id"`
	NodeType    string  `json:"node_type"` // "script" or "folder"
	DBType      string  `json:"db_type"`
	SortOrder   int     `json:"sort_order"`
	UserID      string  `json:"user_id"`
	TenantID    string  `json:"tenant_id"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   string  `json:"updated_at"`
}

// ScriptTreeNode represents a node in the script tree (with children)
type ScriptTreeNode struct {
	*Script
	Children []*ScriptTreeNode `json:"children,omitempty"`
}

type ScriptService struct {
	db *gorm.DB
}

func NewScriptService(db *gorm.DB) *ScriptService {
	return &ScriptService{db: db}
}

func (s *ScriptService) scriptContentPath(scriptID string) string {
	b := storage.ResolveModule(storage.ModuleScript)
	base := b.BasePath
	if base == "" {
		base = "scripts"
	}
	return filepath.Join(base, scriptID+".sql")
}

func (s *ScriptService) getScriptStorage() storage.FileStorage {
	b := storage.ResolveModule(storage.ModuleScript)
	if b.Storage != nil {
		return b.Storage
	}
	return storage.Get()
}

func (s *ScriptService) saveContent(scriptID, content string) error {
	st := s.getScriptStorage()
	if st == nil {
		return fmt.Errorf("no storage available for script content")
	}
	p := s.scriptContentPath(scriptID)
	reader := strings.NewReader(content)
	_, err := st.Save(context.Background(), p, reader, "text/plain")
	return err
}

func (s *ScriptService) loadContent(scriptID string) (string, error) {
	st := s.getScriptStorage()
	if st == nil {
		return "", fmt.Errorf("no storage available for script content")
	}
	p := s.scriptContentPath(scriptID)
	data, err := st.Read(context.Background(), p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *ScriptService) deleteContent(scriptID string) {
	st := s.getScriptStorage()
	if st == nil {
		return
	}
	p := s.scriptContentPath(scriptID)
	st.Delete(context.Background(), p)
}

// MigrateScriptContent migrates script content from DB content column to file storage.
// Called once at startup. Idempotent: skips already-migrated scripts.
func (s *ScriptService) MigrateScriptContent() {
	var scripts []struct {
		ID      string
		Content string
	}
	s.db.Raw(`SELECT id, content FROM scripts WHERE content != '' AND node_type = 'script'`).Scan(&scripts)
	if len(scripts) == 0 {
		return
	}

	fmt.Printf("script: migrating %d script contents to file storage\n", len(scripts))
	for _, sc := range scripts {
		if err := s.saveContent(sc.ID, sc.Content); err != nil {
			fmt.Printf("script: failed to migrate content for %s: %v\n", sc.ID, err)
			continue
		}
		s.db.Exec(`UPDATE scripts SET content = '' WHERE id = ?`, sc.ID)
	}
	fmt.Printf("script: migration completed for %d scripts\n", len(scripts))
}

// CreateScriptInput is the input for creating a script
type CreateScriptInput struct {
	Name        string  `json:"name" binding:"required"`
	Description string  `json:"description"`
	Content     string  `json:"content"`
	Category    string  `json:"category"`
	ParentID    *string `json:"parent_id"`
	DBType      string  `json:"db_type"`
}

// CreateFolderInput is the input for creating a folder
type CreateFolderInput struct {
	Name     string  `json:"name" binding:"required"`
	ParentID *string `json:"parent_id"`
}

// UpdateScriptInput is the input for updating a script
type UpdateScriptInput struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Content     *string `json:"content"`
	Category    *string `json:"category"`
	DBType      *string `json:"db_type"`
}

// MoveNodeInput moves a node to a new parent
type MoveNodeInput struct {
	ParentID *string `json:"parent_id"`
}

func esc(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func scanScript(row map[string]interface{}) *Script {
	s := &Script{}
	if v, ok := row["id"]; ok {
		s.ID = toString(v)
	}
	if v, ok := row["name"]; ok {
		s.Name = toString(v)
	}
	if v, ok := row["description"]; ok && v != nil {
		s.Description = toString(v)
	}
	if v, ok := row["content"]; ok && v != nil {
		s.Content = toString(v)
	}
	if v, ok := row["category"]; ok && v != nil {
		s.Category = toString(v)
	}
	if v, ok := row["parent_id"]; ok && v != nil {
		sid := toString(v)
		s.ParentID = &sid
	}
	if v, ok := row["node_type"]; ok && v != nil {
		s.NodeType = toString(v)
	} else {
		s.NodeType = "script"
	}
	if v, ok := row["db_type"]; ok && v != nil {
		s.DBType = toString(v)
	} else {
		s.DBType = "mysql"
	}
	if v, ok := row["sort_order"]; ok && v != nil {
		fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &s.SortOrder)
	}
	if v, ok := row["user_id"]; ok && v != nil {
		s.UserID = toString(v)
	}
	if v, ok := row["tenant_id"]; ok && v != nil {
		s.TenantID = toString(v)
	}
	if v, ok := row["created_at"]; ok && v != nil {
		s.CreatedAt = toString(v)
	}
	if v, ok := row["updated_at"]; ok && v != nil {
		s.UpdatedAt = toString(v)
	}
	return s
}

// List returns scripts matching keyword (searches name and content)
func (s *ScriptService) List(userID, tenantID, keyword string) ([]*Script, error) {
	builder := repository.NewSQLBuilder()
	sql := builder.Build(`SELECT id, name, description, content, category, parent_id, node_type, db_type, sort_order,
		user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM scripts WHERE user_id = ?`, "created_at", "updated_at")
	args := []interface{}{userID}
	if tenantID != "" {
		sql += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	if keyword != "" {
		sql += " AND (name LIKE ? OR content LIKE ?)"
		kw := "%" + keyword + "%"
		args = append(args, kw, kw)
	}
	sql += " ORDER BY sort_order ASC, updated_at DESC"

	var results []*Script
	rows, err := s.db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
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
		results = append(results, scanScript(row))
	}
	if results == nil {
		results = make([]*Script, 0)
	}
	return results, nil
}

// Tree returns scripts as a tree structure (without content for performance)
func (s *ScriptService) Tree(userID, tenantID string) ([]*ScriptTreeNode, error) {
	// Lightweight query - no content column for tree display
	sql := `SELECT id, name, description, '' as content, category, parent_id, node_type, db_type, sort_order,
		user_id, tenant_id, created_at, updated_at
		FROM scripts WHERE user_id = ?`
	args := []interface{}{userID}
	if tenantID != "" {
		sql += " AND tenant_id = ?"
		args = append(args, tenantID)
	}
	sql += " ORDER BY sort_order ASC, node_type ASC, updated_at DESC"

	rows, err := s.db.Raw(sql, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var scripts []*Script
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
		scripts = append(scripts, scanScript(row))
	}

	// Build lookup map
	nodeMap := make(map[string]*ScriptTreeNode)
	var roots []*ScriptTreeNode

	for _, sc := range scripts {
		node := &ScriptTreeNode{Script: sc}
		nodeMap[sc.ID] = node
		if sc.ParentID == nil || *sc.ParentID == "" {
			roots = append(roots, node)
		}
	}

	// Build tree
	for _, sc := range scripts {
		if sc.ParentID != nil && *sc.ParentID != "" {
			if parent, ok := nodeMap[*sc.ParentID]; ok {
				parent.Children = append(parent.Children, nodeMap[sc.ID])
			}
		}
	}

	if roots == nil {
		roots = make([]*ScriptTreeNode, 0)
	}
	return roots, nil
}

// Get returns a single script by ID
func (s *ScriptService) Get(id string) (*Script, error) {
	var result Script
	builder := repository.NewSQLBuilder()
	row := s.db.Raw(builder.Build(`SELECT id, name, description, content, category, parent_id, node_type, db_type, sort_order,
		user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM scripts WHERE id = ?`, "created_at", "updated_at"), id).Row()
	err := row.Scan(&result.ID, &result.Name, &result.Description, &result.Content,
		&result.Category, &result.ParentID, &result.NodeType, &result.DBType,
		&result.SortOrder, &result.UserID, &result.TenantID,
		&result.CreatedAt, &result.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("script not found: %s", id)
	}

	if result.NodeType == "script" {
		if result.Content != "" {
			if saveErr := s.saveContent(id, result.Content); saveErr == nil {
				s.db.Exec(`UPDATE scripts SET content = '' WHERE id = ?`, id)
			}
		} else {
			if fileContent, loadErr := s.loadContent(id); loadErr == nil {
				result.Content = fileContent
			}
		}
	}
	return &result, nil
}

// Create creates a new script
func (s *ScriptService) Create(input CreateScriptInput, userID, tenantID string) (*Script, error) {
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")
	dbType := input.DBType
	if dbType == "" {
		dbType = "mysql"
	}

	// Get max sort_order for siblings
	var maxSort int
	s.db.Raw(`SELECT COALESCE(MAX(sort_order), 0) FROM scripts WHERE parent_id IS ? AND user_id = ?`,
		input.ParentID, userID).Scan(&maxSort)

	dbContent := ""
	if input.Content != "" {
		if err := s.saveContent(id, input.Content); err != nil {
			return nil, fmt.Errorf("failed to save script content: %w", err)
		}
	}

	err := s.db.Exec(`INSERT INTO scripts (id, name, description, content, category, parent_id, node_type, db_type, sort_order, user_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'script', ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.Description, dbContent, input.Category,
		input.ParentID, dbType, maxSort+1, userID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// CreateFolder creates a new folder node
func (s *ScriptService) CreateFolder(input CreateFolderInput, userID, tenantID string) (*Script, error) {
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")

	var maxSort int
	s.db.Raw(`SELECT COALESCE(MAX(sort_order), 0) FROM scripts WHERE parent_id IS ? AND user_id = ?`,
		input.ParentID, userID).Scan(&maxSort)

	err := s.db.Exec(`INSERT INTO scripts (id, name, description, content, category, parent_id, node_type, db_type, sort_order, user_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, '', '', '', ?, 'folder', 'mysql', ?, ?, ?, ?, ?)`,
		id, input.Name, input.ParentID, maxSort+1, userID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Update updates a script
func (s *ScriptService) Update(id string, input UpdateScriptInput) (*Script, error) {
	script, err := s.Get(id)
	if err != nil {
		return nil, err
	}

	if input.Name != nil {
		script.Name = *input.Name
	}
	if input.Description != nil {
		script.Description = *input.Description
	}
	if input.Content != nil {
		if *input.Content != "" {
			if err := s.saveContent(id, *input.Content); err != nil {
				return nil, fmt.Errorf("failed to save script content: %w", err)
			}
		}
		script.Content = ""
	}
	if input.Category != nil {
		script.Category = *input.Category
	}
	if input.DBType != nil {
		script.DBType = *input.DBType
	}

	now := time.Now().Format("2006-01-02 15:04:05")
	err = s.db.Exec(`UPDATE scripts SET name=?, description=?, content=?, category=?, db_type=?, updated_at=? WHERE id=?`,
		script.Name, script.Description, script.Content, script.Category, script.DBType, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete deletes a script (and its children)
func (s *ScriptService) Delete(id string) error {
	var childIDs []string
	s.db.Raw(`SELECT id FROM scripts WHERE parent_id = ? AND node_type = 'script'`, id).Scan(&childIDs)
	for _, cid := range childIDs {
		s.deleteContent(cid)
	}
	s.deleteContent(id)

	s.db.Exec(`DELETE FROM scripts WHERE parent_id = ?`, id)
	return s.db.Exec(`DELETE FROM scripts WHERE id = ?`, id).Error
}

// Move moves a node to a new parent
func (s *ScriptService) Move(id string, input MoveNodeInput) (*Script, error) {
	now := time.Now().Format("2006-01-02 15:04:05")

	// Get max sort_order in new parent
	var maxSort int
	s.db.Raw(`SELECT COALESCE(MAX(sort_order), 0) FROM scripts WHERE parent_id IS ?`, input.ParentID).Scan(&maxSort)

	err := s.db.Exec(`UPDATE scripts SET parent_id=?, sort_order=?, updated_at=? WHERE id=?`,
		input.ParentID, maxSort+1, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
