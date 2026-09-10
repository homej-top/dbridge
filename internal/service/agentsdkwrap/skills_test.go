package agentsdkwrap

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/dbridge/dbridge/internal/repository"
	"github.com/opentoys/agentsdk/vfs"
)

func TestBuildSkillMarkdown_UsesKebabCaseAllowedTools(t *testing.T) {
	sk := repository.AISkill{
		ID: "1", Slug: "sql-opt", Name: "SQL 优化", Description: "分析慢 SQL",
		PromptTemplate: "分析 {{sql}}", SystemPrompt: "你是专家",
		Tools: `["execute_query","list_tables"]`, Version: 2, IsBuiltin: true,
	}
	md, err := BuildSkillMarkdown(sk)
	if err != nil {
		t.Fatalf("BuildSkillMarkdown error: %v", err)
	}
	if strings.Contains(md, "allowedTools") {
		t.Fatalf("must not contain camelCase allowedTools:\n%s", md)
	}
	if !strings.Contains(md, "allowed-tools:") {
		t.Fatalf("missing allowed-tools:\n%s", md)
	}
	if !strings.Contains(md, "execute_query") || !strings.Contains(md, "list_tables") {
		t.Fatalf("missing tools:\n%s", md)
	}
	if !strings.Contains(md, "## System Instructions") {
		t.Fatalf("missing system instructions:\n%s", md)
	}
}

func TestBuildSkillMarkdown_SpecialCharsCollapsed(t *testing.T) {
	sk := repository.AISkill{
		ID: "1", Slug: "x", Name: "a: b\nnext", Description: "#comment\nline2",
		PromptTemplate: "body", Tools: "[]",
	}
	md, err := BuildSkillMarkdown(sk)
	if err != nil {
		t.Fatalf("BuildSkillMarkdown error: %v", err)
	}
	if !strings.Contains(md, "name: a: b next") {
		t.Fatalf("name should be single-line and colon-safe:\n%s", md)
	}
	if !strings.Contains(md, "description: #comment line2") {
		t.Fatalf("description should be single-line:\n%s", md)
	}
}

func TestWriteSkillFiles(t *testing.T) {
	mem := vfs.NewMem()
	files := `[{"path":"scripts/run.py","content":"print(1)"},{"path":"../evil.txt","content":"bad"},{"path":"references/guide.md","content":"# guide"}]`
	if err := writeSkillFiles(mem, "skill", files); err != nil {
		t.Fatalf("writeSkillFiles error: %v", err)
	}
	if _, err := fs.Stat(mem, "skill/scripts/run.py"); err != nil {
		t.Fatalf("scripts/run.py missing: %v", err)
	}
	if _, err := fs.Stat(mem, "evil.txt"); err == nil {
		t.Fatalf("path traversal should be blocked")
	}
	if _, err := fs.Stat(mem, "skill/references/guide.md"); err != nil {
		t.Fatalf("references/guide.md missing: %v", err)
	}
}

func TestParseSkillIDs(t *testing.T) {
	if got := parseSkillIDs(""); got != nil {
		t.Fatalf("empty -> nil, got %v", got)
	}
	if got := parseSkillIDs("[]"); got != nil {
		t.Fatalf("[] -> nil, got %v", got)
	}
	if got := parseSkillIDs(`["a","b"]`); len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if got := parseSkillIDs("bad"); got != nil {
		t.Fatalf("bad -> nil, got %v", got)
	}
}
