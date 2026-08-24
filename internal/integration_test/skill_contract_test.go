package integration_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSkillInteractiveContract(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "skills", "agent-handoff", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	skill := string(data)

	required := []string{
		"request_user_input",
		"AskUserQuestion",
		"multiSelect\":false",
		"share_format",
		"proceed_with_secrets",
		"confirm_import",
		"allow_duplicate",
		"导出文件 (Recommended)",
		"生成链接",
		"Do not ask again",
		"not plain text",
	}
	for _, want := range required {
		if !strings.Contains(skill, want) {
			t.Errorf("interactive skill contract missing %q", want)
		}
	}
}
