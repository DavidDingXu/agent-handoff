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
		"agent-handoff/config.json",
		"--config <file>",
		"Do not ask the user which link provider",
		"Never ask the user to paste a provider token",
		"If no native question tool is available",
		"ask one concise plain-text question with exactly these two labeled choices and wait",
		"导出文件（推荐）",
		"Do not choose a share format on the user's behalf",
		"imitate a native form with a numbered menu",
		"do not run the share command before the user chooses",
		"Never narrate reading `SKILL.md`",
		"locating the plugin cache",
		"分享链接：",
		"```text",
		"<share_url>",
		"有效期：<expires_at>",
		"The fenced block must contain the complete `share_url` and nothing else",
		"Never put `share_url` in an ordinary paragraph",
		"wrap it in a Markdown link",
	}
	for _, want := range required {
		if !strings.Contains(skill, want) {
			t.Errorf("interactive skill contract missing %q", want)
		}
	}
}
