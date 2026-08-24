package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCLIShareImportVerifyFourQuadrants(t *testing.T) {
	binary := buildCLIBinary(t)
	tests := []struct {
		name   string
		source string
		target string
	}{
		{name: "codex_to_codex", source: "codex", target: "codex"},
		{name: "codex_to_claude", source: "codex", target: "claude"},
		{name: "claude_to_codex", source: "claude", target: "codex"},
		{name: "claude_to_claude", source: "claude", target: "claude"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			senderHome, sourceID := buildCLIHome(t, tc.source)
			receiverHome := t.TempDir()
			targetCWD := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(targetCWD, 0o755); err != nil {
				t.Fatal(err)
			}
			bundlePath := filepath.Join(t.TempDir(), tc.name+".agent-handoff.zip")

			shared := runCLI(t, binary,
				"share", "--source", tc.source, "--thread", sourceID,
				"--home", senderHome, "--format", "zip", "--out", bundlePath)
			if shared["status"] != "ok" {
				t.Fatalf("share result = %#v", shared)
			}

			imported := runCLI(t, binary,
				"import", bundlePath, "--target", tc.target, "--home", receiverHome,
				"--cwd", targetCWD, "--execute")
			if imported["status"] != "ok" {
				t.Fatalf("import result = %#v", imported)
			}
			wantCrossAgent := tc.source != tc.target
			if got, _ := imported["cross_agent"].(bool); got != wantCrossAgent {
				t.Fatalf("cross_agent = %v, want %v (result %#v)", got, wantCrossAgent, imported)
			}
			idField := "thread_id"
			if tc.target == "claude" {
				idField = "session_id"
			}
			threadID, _ := imported[idField].(string)
			if threadID == "" {
				t.Fatalf("import result missing %s: %#v", idField, imported)
			}

			verified := runCLI(t, binary,
				"verify", "--source", tc.target, "--home", receiverHome,
				"--thread", threadID, "--cwd", targetCWD)
			if verified["status"] != "ok" {
				t.Fatalf("verify result = %#v", verified)
			}
		})
	}
}

func buildCLIBinary(t *testing.T) string {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	name := "agent-handoff"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-trimpath", "-buildvcs=false", "-o", binary, ".")
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}
	return binary
}

func buildCLIHome(t *testing.T, source string) (string, string) {
	t.Helper()
	switch source {
	case "codex":
		return buildCodexHome(t), codexThreadID
	case "claude":
		return buildClaudeHome(t), claudeSessionID
	default:
		t.Fatalf("unsupported source %q", source)
		return "", ""
	}
}

func runCLI(t *testing.T, binary string, args ...string) map[string]any {
	t.Helper()
	cmd := exec.Command(binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("agent-handoff %v: %v\n%s", args, err, out)
	}
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatalf("decode agent-handoff %v output: %v\n%s", args, err, out)
	}
	if result == nil {
		t.Fatal(fmt.Errorf("agent-handoff %v returned empty JSON", args))
	}
	return result
}
