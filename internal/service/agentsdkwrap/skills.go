package agentsdkwrap

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/dbridge/dbridge/internal/service"
	"github.com/opentoys/agentsdk/skill"
	"github.com/opentoys/agentsdk/vfs"
)

// SkillRef is the minimal skill metadata exposed to the handler for
// observability (SSE events / audit).
type SkillRef struct {
	ID      string `json:"id"`
	Slug    string `json:"slug"`
	Name    string `json:"name"`
	Version int    `json:"version"`
}

// parseSkillIDs parses the Agent.Skills JSON string into a string slice
func parseSkillIDs(skillsJSON string) []string {
	if skillsJSON == "" || skillsJSON == "[]" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(skillsJSON), &ids); err != nil {
		return nil
	}
	return ids
}

// buildSkillsFS builds an in-memory filesystem containing all skills
// associated with an agent. It resolves each agent skill ID to its slug
// (falling back to ID) so runtime directory names are stable and readable.
// The returned refs are used for observability.
func buildSkillsFS(agentCfg *repository.Agent) (fs.FS, []SkillRef, error) {
	skillIDs := parseSkillIDs(agentCfg.Skills)
	if len(skillIDs) == 0 {
		return nil, nil, nil
	}

	mem := vfs.NewMem()
	db := repository.GetDB()
	refs := make([]SkillRef, 0, len(skillIDs))

	for _, id := range skillIDs {
		var sk repository.AISkill
		if err := db.Where("id = ? AND is_active = ?", id, true).First(&sk).Error; err != nil {
			continue
		}
		// Tenant scope: built-in/system skills are visible to all tenants,
		// otherwise the skill must belong to the agent's tenant.
		if !sk.IsBuiltin && sk.TenantID != "" && sk.TenantID != agentCfg.TenantID && sk.TenantID != service.SystemTenantID {
			continue
		}
		content, err := BuildSkillMarkdown(sk)
		if err != nil {
			continue
		}
		dir := sk.Slug
		if dir == "" {
			dir = sk.ID
		}
		if err := mem.WriteFile(dir+"/SKILL.md", []byte(content)); err != nil {
			continue
		}
		if err := writeSkillFiles(mem, dir, sk.ScriptFiles); err != nil {
			continue
		}
		if err := writeSkillFiles(mem, dir, sk.Resources); err != nil {
			continue
		}
		refs = append(refs, SkillRef{ID: sk.ID, Slug: sk.Slug, Name: sk.Name, Version: sk.Version})
	}

	return mem, refs, nil
}

// BuildSkillMarkdown converts an AISkill DB record to agentsdk SKILL.md format.
// It returns an error when the generated content fails to parse, so callers
// never hand a structurally broken skill package to the runtime.
func BuildSkillMarkdown(sk repository.AISkill) (string, error) {
	var sb strings.Builder

	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("name: %s\n", singleLine(sk.Name)))
	sb.WriteString(fmt.Sprintf("description: %s\n", singleLine(sk.Description)))
	if sk.Version > 0 {
		sb.WriteString(fmt.Sprintf("version: \"%d\"\n", sk.Version))
	}
	if tools := parseStringArray(sk.Tools); len(tools) > 0 {
		// NOTE: agentsdk's YAML parser expects kebab-case `allowed-tools`.
		sb.WriteString("allowed-tools:\n")
		for _, t := range tools {
			sb.WriteString(fmt.Sprintf("  - %s\n", t))
		}
	}
	sb.WriteString("---\n\n")

	if sk.PromptTemplate != "" {
		sb.WriteString(sk.PromptTemplate)
	}
	if sk.SystemPrompt != "" {
		sb.WriteString("\n\n## System Instructions\n")
		sb.WriteString(sk.SystemPrompt)
	}

	content := sb.String()

	// Self-validation: feed the generated SKILL.md back into the parser so a
	// broken frontmatter/body never reaches the runtime.
	mem := vfs.NewMem()
	if err := mem.WriteFile("skill/SKILL.md", []byte(content)); err != nil {
		return "", fmt.Errorf("write SKILL.md: %w", err)
	}
	if _, err := skill.ParseSkillPackage(mem, "skill"); err != nil {
		return "", fmt.Errorf("generated SKILL.md failed validation: %w", err)
	}

	return content, nil
}

// singleLine normalizes frontmatter values to a single line. The bundled
// YAML parser does not unquote strings, so multi-line values would corrupt the
// frontmatter; collapsing them to one line keeps the document parseable.
func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

// writeSkillFiles writes the script/resource files declared in a skill's
// script_files / resources JSON ([]{"path","content"}) into the in-memory
// skill package. Paths are cleaned and constrained to the skill directory.
func writeSkillFiles(mem *vfs.MemFS, dir, filesJSON string) error {
	if strings.TrimSpace(filesJSON) == "" || filesJSON == "[]" {
		return nil
	}
	var files []struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(filesJSON), &files); err != nil {
		return err
	}
	for _, f := range files {
		p := strings.TrimSpace(f.Path)
		if p == "" {
			continue
		}
		cleaned := filepath.ToSlash(filepath.Clean(p))
		if cleaned == ".." || strings.HasPrefix(cleaned, "../") || filepath.IsAbs(cleaned) {
			continue
		}
		if err := mem.WriteFile(dir+"/"+cleaned, []byte(f.Content)); err != nil {
			return err
		}
	}
	return nil
}

func parseStringArray(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var arr []string
	if err := json.Unmarshal([]byte(raw), &arr); err != nil {
		return nil
	}
	return arr
}
