package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/workspace"
)

func TestCodeImportCreatesSeparateWorktreeAndKeepsOriginal(t *testing.T) {
	binary := buildCLIBinary(t)
	repo := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("git %v: %v %s", args, e, out)
		}
	}
	git("init")
	git("config", "core.autocrlf", "false")
	git("config", "user.name", "Synthetic")
	git("config", "user.email", "synthetic@example.test")
	if e := os.WriteFile(filepath.Join(repo, "fix.txt"), []byte("base\n"), 0600); e != nil {
		t.Fatal(e)
	}
	git("add", "fix.txt")
	git("commit", "-m", "synthetic base")
	if e := os.WriteFile(filepath.Join(repo, "fix.txt"), []byte("changed\n"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("explicit new file\n"), 0600); e != nil {
		t.Fatal(e)
	}
	code, e := workspace.Capture(repo, []string{"fix.txt", "new.txt"})
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "task.zip")
	manifest := &bundle.Manifest{FormatVersion: bundle.Version2, ArtifactType: bundle.ArtifactType, SourceAgent: bundle.AgentDSH, SourceThreadID: "synthetic-code", Title: "Synthetic code task", MessageCount: 2}
	transcript := neutral.Transcript{Schema: neutral.Schema, SourceAgent: bundle.AgentDSH, SourceID: "synthetic-code", Entries: []neutral.Entry{{Kind: neutral.KindMessage, Role: "user", Text: "Continue the synthetic code change"}, {Kind: neutral.KindMessage, Role: "assistant", Text: "Review before continuing"}}}
	native := []byte(`{"schema":"agent-handoff.dsh.native.v1","runtime_version":"0.2.2-rc.5","session_format_version":4,"header":{"id":"synthetic-code"},"inherited_event_count":0,"events":[{"type":"synthetic"}]}`)
	if e = bundle.WriteZip(p, bundle.WriterInput{Manifest: manifest, Session: native, Neutral: transcript, Code: code}); e != nil {
		t.Fatal(e)
	}
	for _, target := range []string{bundle.AgentCodex, bundle.AgentClaude} {
		t.Run(target, func(t *testing.T) {
			home, _ := buildCLIHome(t, target)
			destination := filepath.Join(t.TempDir(), "separate-worktree")
			conversation := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", repo, "--execute")
			if conversation["status"] != "ok" || conversation["code_applied"] != false {
				t.Fatalf("conversation-only import: %#v", conversation)
			}
			plan := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", repo, "--apply-code", "--code-directory", destination)
			if plan["code_directory"] != destination || plan["status"] != "planned" {
				t.Fatal(plan)
			}
			if _, e := os.Stat(destination); !os.IsNotExist(e) {
				t.Fatal("dry-run wrote a worktree")
			}
			result := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", repo, "--apply-code", "--code-directory", destination, "--execute")
			if result["target_cwd"] != destination || result["code_applied"] != true || result["brief_path"] == nil {
				t.Fatal(result)
			}
			idField := "thread_id"
			if target == bundle.AgentClaude {
				idField = "session_id"
			}
			if result[idField] == conversation[idField] {
				t.Fatal("code application reused the conversation-only task")
			}
			duplicate := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", repo, "--apply-code", "--code-directory", destination, "--execute")
			if duplicate["status"] != "duplicate" || duplicate["existing_thread_id"] != result[idField] {
				t.Fatalf("code application was not idempotent: %#v", duplicate)
			}
			original := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", repo, "--execute")
			if original["status"] != "duplicate" || original["existing_thread_id"] != conversation[idField] {
				t.Fatalf("conversation-only identity changed: %#v", original)
			}
			for name, want := range map[string]string{"fix.txt": "changed\n", "new.txt": "explicit new file\n"} {
				got, e := os.ReadFile(filepath.Join(destination, name))
				if e != nil || string(got) != want {
					t.Fatalf("applied %s: %q %v", name, got, e)
				}
			}
			got, _ := os.ReadFile(filepath.Join(repo, "fix.txt"))
			if !bytes.Equal(got, []byte("changed\n")) {
				t.Fatal("original modified")
			}
			briefData, e := os.ReadFile(result["brief_path"].(string))
			if e != nil || !bytes.Contains(briefData, []byte("Continue the synthetic code change")) {
				t.Fatalf("brief sidecar missing: %v", e)
			}
			encoded, _ := json.Marshal(result)
			if bytes.Contains(encoded, []byte("--prompt")) {
				t.Fatal("import must not start a model turn")
			}
		})
	}
}
