package service

import (
	"fmt"

	"github.com/dbridge/dbridge/internal/repository"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReportCategory represents a report category tree node
type ReportCategory struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	ParentID  *string `json:"parent_id"`
	SortOrder int     `json:"sort_order"`
	UserID    string  `json:"user_id"`
	TenantID  string  `json:"tenant_id"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`
}

// ReportCategoryTreeNode represents a node in the category tree
type ReportCategoryTreeNode struct {
	*ReportCategory
	Children []*ReportCategoryTreeNode `json:"children,omitempty"`
}

type ReportCategoryService struct {
	db *gorm.DB
}

func NewReportCategoryService(db *gorm.DB) *ReportCategoryService {
	return &ReportCategoryService{db: db}
}

type CreateCategoryInput struct {
	Name     string  `json:"name" binding:"required"`
	ParentID *string `json:"parent_id"`
}

type UpdateCategoryInput struct {
	Name *string `json:"name"`
}

type MoveCategoryInput struct {
	ParentID *string `json:"parent_id"`
}

func mapCategory(row map[string]interface{}) *ReportCategory {
	c := &ReportCategory{}
	mapStr(row, "id", &c.ID)
	mapStr(row, "name", &c.Name)
	if v, ok := row["parent_id"]; ok && v != nil {
		s := toString(v)
		c.ParentID = &s
	}
	if v, ok := row["sort_order"]; ok && v != nil {
		fmt.Sscanf(fmt.Sprintf("%v", v), "%d", &c.SortOrder)
	}
	mapStr(row, "user_id", &c.UserID)
	mapStr(row, "tenant_id", &c.TenantID)
	mapStr(row, "created_at", &c.CreatedAt)
	mapStr(row, "updated_at", &c.UpdatedAt)
	return c
}

func (s *ReportCategoryService) queryCategories(query string, args ...interface{}) ([]*ReportCategory, error) {
	builder := repository.NewSQLBuilder()
	query = builder.Build(query, "created_at", "updated_at")
	rows, err := s.db.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	var results []*ReportCategory
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
		results = append(results, mapCategory(row))
	}
	if results == nil {
		results = make([]*ReportCategory, 0)
	}
	return results, nil
}

// List returns categories as a tree structure
func (s *ReportCategoryService) List(userID, tenantID string) ([]*ReportCategoryTreeNode, error) {
	categories, err := s.queryCategories(`SELECT id, name, parent_id, sort_order, user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM report_categories WHERE user_id = ? AND tenant_id = ?
		ORDER BY sort_order ASC, created_at ASC`, userID, tenantID)
	if err != nil {
		return nil, err
	}

	// Build tree
	nodeMap := make(map[string]*ReportCategoryTreeNode)
	var roots []*ReportCategoryTreeNode

	for _, cat := range categories {
		node := &ReportCategoryTreeNode{ReportCategory: cat}
		nodeMap[cat.ID] = node
		if cat.ParentID == nil || *cat.ParentID == "" {
			roots = append(roots, node)
		}
	}

	for _, cat := range categories {
		if cat.ParentID != nil && *cat.ParentID != "" {
			if parent, ok := nodeMap[*cat.ParentID]; ok {
				parent.Children = append(parent.Children, nodeMap[cat.ID])
			}
		}
	}

	if roots == nil {
		roots = make([]*ReportCategoryTreeNode, 0)
	}
	return roots, nil
}

// Get returns a single category by ID
func (s *ReportCategoryService) Get(id string) (*ReportCategory, error) {
	categories, err := s.queryCategories(`SELECT id, name, parent_id, sort_order, user_id, tenant_id,
		{created_at} as created_at,
		{updated_at} as updated_at
		FROM report_categories WHERE id = ?`, id)
	if err != nil || len(categories) == 0 {
		return nil, fmt.Errorf("category not found: %s", id)
	}
	return categories[0], nil
}

// Create creates a new category
func (s *ReportCategoryService) Create(input CreateCategoryInput, userID, tenantID string) (*ReportCategory, error) {
	id := uuid.New().String()
	now := time.Now().Format("2006-01-02 15:04:05")

	var maxSort int
	s.db.Raw(`SELECT COALESCE(MAX(sort_order), 0) FROM report_categories WHERE parent_id IS ? AND user_id = ?`,
		input.ParentID, userID).Scan(&maxSort)

	err := s.db.Exec(`INSERT INTO report_categories (id, name, parent_id, sort_order, user_id, tenant_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, input.Name, input.ParentID, maxSort+1, userID, tenantID, now, now).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Update updates a category name
func (s *ReportCategoryService) Update(id string, input UpdateCategoryInput) (*ReportCategory, error) {
	now := time.Now().Format("2006-01-02 15:04:05")
	err := s.db.Exec(`UPDATE report_categories SET name=?, updated_at=? WHERE id=?`,
		*input.Name, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}

// Delete deletes a category (and moves children to parent)
func (s *ReportCategoryService) Delete(id string) error {
	cat, err := s.Get(id)
	if err != nil {
		return err
	}
	// Move children to this category's parent
	s.db.Exec(`UPDATE report_categories SET parent_id = ? WHERE parent_id = ?`, cat.ParentID, id)
	// Unlink reports from this category
	s.db.Exec(`UPDATE reports SET category_id = NULL WHERE category_id = ?`, id)
	return s.db.Exec(`DELETE FROM report_categories WHERE id = ?`, id).Error
}

// Move moves a category to a new parent
func (s *ReportCategoryService) Move(id string, input MoveCategoryInput) (*ReportCategory, error) {
	now := time.Now().Format("2006-01-02 15:04:05")

	var maxSort int
	s.db.Raw(`SELECT COALESCE(MAX(sort_order), 0) FROM report_categories WHERE parent_id IS ?`, input.ParentID).Scan(&maxSort)

	err := s.db.Exec(`UPDATE report_categories SET parent_id=?, sort_order=?, updated_at=? WHERE id=?`,
		input.ParentID, maxSort+1, now, id).Error
	if err != nil {
		return nil, err
	}
	return s.Get(id)
}
