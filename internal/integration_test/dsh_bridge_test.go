package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

// Exercise the shipped CLI boundary and real existing native homes. Unlike a
// fake DSH filesystem adapter, export comes only from the plugin JSON snapshot.
func TestCLIDSHBridgeNativeDestinations(t *testing.T) {
	binary := buildCLIBinary(t)
	sentinel := filepath.Join(t.TempDir(), "never-execute")
	input := map[string]any{"schema": "agent-handoff.bridge.share.v1", "source_thread_id": "synthetic-dsh-source", "title": "DSH bridge task", "source_cwd": "/sender/never-select", "source_cli": "dsh@0.2.2-rc.5", "native": map[string]any{"schema": bundle.DSHNativeSchema, "runtime_version": "0.2.2-rc.5", "session_format_version": 4, "header": map[string]any{"id": "synthetic-dsh-source"}, "inherited_event_count": 0, "events": []any{map[string]any{"type": "synthetic", "text": "inert"}}}, "neutral": neutral.Transcript{Schema: neutral.Schema, SourceAgent: bundle.AgentDSH, SourceID: "synthetic-dsh-source", Entries: []neutral.Entry{{Kind: neutral.KindMessage, Role: "user", Text: "Synthetic DSH question"}, {Kind: neutral.KindTool, Tool: "shell", Status: "completed", Input: "touch " + filepath.ToSlash(sentinel), Output: "synthetic tool evidence"}, {Kind: neutral.KindMessage, Role: "assistant", Text: "Synthetic DSH answer"}}}}
	raw, e := json.Marshal(input)
	if e != nil {
		t.Fatal(e)
	}
	p := filepath.Join(t.TempDir(), "bridge.zip")
	cmd := exec.Command(binary, "bridge", "share", "--input", "-", "--out", p)
	cmd.Stdin = bytes.NewReader(raw)
	out, e := cmd.CombinedOutput()
	if e != nil {
		t.Fatalf("bridge stdin: %v: %s", e, out)
	}
	var shared map[string]any
	if e = json.Unmarshal(out, &shared); e != nil {
		t.Fatal(e)
	}
	if shared["schema"] != "agent-handoff.bridge.share-result.v1" {
		t.Fatal(shared)
	}
	b, e := bundle.ReadZip(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, target := range []string{bundle.AgentCodex, bundle.AgentClaude} {
		t.Run(target, func(t *testing.T) {
			home, oldID := buildCLIHome(t, target)
			cwd := t.TempDir()
			var oldNative, oldMeta []byte
			if target == bundle.AgentCodex {
				d, e := codex.LoadThread(home, oldID)
				if e != nil {
					t.Fatal(e)
				}
				oldNative = d.SessionBytes
				oldMeta, _ = json.Marshal(d.ThreadRow)
			} else {
				d, e := claude.LoadSession(home, oldID)
				if e != nil {
					t.Fatal(e)
				}
				oldNative = d.SessionBytes
				oldMeta, _ = json.Marshal(d.IndexEntry)
			}
			result := runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", cwd)
			if result["status"] != "planned" {
				t.Fatal(result)
			}
			result = runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", cwd, "--execute")
			key := "thread_id"
			if target == bundle.AgentClaude {
				key = "session_id"
			}
			id, _ := result[key].(string)
			if id == "" || id == oldID || id == "synthetic-dsh-source" || result["cross_agent"] != true {
				t.Fatal(result)
			}
			runCLI(t, binary, "verify", "--source", target, "--home", home, "--cwd", cwd, "--thread", id)
			audit := filepath.Join(home, "agent-handoff-assets", id, "source-session.jsonl")
			data, e := os.ReadFile(audit)
			if e != nil || !bytes.Equal(data, b.Session) {
				t.Fatalf("raw audit changed: %v", e)
			}
			st, e := os.Stat(audit)
			if e != nil {
				t.Fatal(e)
			}
			if runtime.GOOS != "windows" && st.Mode().Perm() != 0600 {
				t.Fatal("audit is not private")
			}
			var gotNative, gotMeta []byte
			if target == bundle.AgentCodex {
				d, e := codex.LoadThread(home, oldID)
				if e != nil {
					t.Fatal(e)
				}
				gotNative = d.SessionBytes
				gotMeta, _ = json.Marshal(d.ThreadRow)
				fresh, e := codex.LoadThread(home, id)
				if e != nil {
					t.Fatal(e)
				}
				if fresh.CWD != cwd || !bytes.Contains(fresh.SessionBytes, []byte("synthetic tool evidence")) {
					t.Fatal("lost cwd or evidence")
				}
			} else {
				d, e := claude.LoadSession(home, oldID)
				if e != nil {
					t.Fatal(e)
				}
				gotNative = d.SessionBytes
				gotMeta, _ = json.Marshal(d.IndexEntry)
				fresh, e := claude.LoadSession(home, id)
				if e != nil {
					t.Fatal(e)
				}
				if fresh.CWD != cwd || !bytes.Contains(fresh.SessionBytes, []byte("synthetic tool evidence")) {
					t.Fatal("lost cwd or evidence")
				}
			}
			if !bytes.Equal(oldNative, gotNative) || !bytes.Equal(oldMeta, gotMeta) {
				t.Fatal("existing native task was changed")
			}
			result = runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", cwd, "--execute")
			if result["status"] != "duplicate" || result["existing_thread_id"] != id {
				t.Fatal(result)
			}
			result = runCLI(t, binary, "import", p, "--target", target, "--home", home, "--cwd", cwd, "--execute", "--allow-duplicate")
			if result["status"] != "ok" || result[key] == id {
				t.Fatal("duplicate override did not create new identity", result)
			}
		})
	}
	if _, e := os.Stat(sentinel); !os.IsNotExist(e) {
		t.Fatal("imported shell evidence was executed")
	}
}
