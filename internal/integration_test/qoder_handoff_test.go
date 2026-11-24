package integration_test

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
)

func TestQoderHandoffCLI(t *testing.T) {
	binary := buildCLIBinary(t)
	profile := t.TempDir()
	database := filepath.Join(profile, "main.sqlite")
	ddl, err := os.ReadFile("testdata/qoder-desktop.sql")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", database)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	cwd, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	qoderArgs := []string{"--target", "qoder", "--database", database, "--home", profile, "--cwd", cwd}
	makeShare := func(source, home, id string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "task.agent-handoff.zip")
		args := []string{"share", "--source", source, "--home", home, "--thread", id, "--out", path}
		if source == "qoder" {
			args = append(args, "--database", database)
		}
		if result := runCLI(t, binary, args...); result["status"] != "ok" {
			t.Fatalf("share %s: %#v", source, result)
		}
		return path
	}

	for _, source := range []string{"codex", "claude", "dsh"} {
		t.Run(source+"_qoder", func(t *testing.T) {
			var path string
			if source == "dsh" {
				path = filepath.Join(t.TempDir(), "dsh.agent-handoff.zip")
				runCLI(t, binary, "bridge", "share", "--input", "testdata/dsh-bridge-share.json", "--out", path)
			} else {
				home, id := buildCLIHome(t, source)
				path = makeShare(source, home, id)
			}
			preview := runCLI(t, binary, "preview", path)
			if preview["source_agent"] != source || preview["checksum_status"] != "ok" {
				t.Fatalf("preview: %#v", preview)
			}
			args := append([]string{"import", path}, qoderArgs...)
			changed := exec.Command(binary, append(args, "--expected-fingerprint", strings.Repeat("0", 64), "--execute")...)
			if out, err := changed.CombinedOutput(); err == nil || !strings.Contains(string(out), "changed since preview") {
				t.Fatalf("changed preview was accepted: %v, %s", err, out)
			}
			result := runCLI(t, binary, append(args, "--expected-fingerprint", preview["fingerprint"].(string), "--execute")...)
			id, _ := result["session_id"].(string)
			if result["status"] != "ok" || result["target_cwd"] != cwd || id == "" {
				t.Fatalf("import: %#v", result)
			}
			if dupe := runCLI(t, binary, append(args, "--execute")...); dupe["status"] != "duplicate" || dupe["session_id"] != id {
				t.Fatalf("duplicate: %#v", dupe)
			}
			if result := runCLI(t, binary, "verify", "--source", "qoder", "--database", database,
				"--home", profile, "--thread", id, "--cwd", cwd); result["status"] != "ok" {
				t.Fatalf("verify: %#v", result)
			}
			catalog := runCLI(t, binary, "qoder", "list", "--database", database, "--query", id)
			tasks := catalog["tasks"].([]any)
			if len(tasks) != 1 || tasks[0].(map[string]any)["id"] != id {
				t.Fatalf("catalog did not find %q", id)
			}
			shared := makeShare("qoder", profile, id)
			b := loadBundle(t, shared)
			if b.Manifest.SourceAgent != bundle.AgentQoder || len(b.Neutral.Entries) == 0 {
				t.Fatalf("invalid Qoder bundle for %s", source)
			}
			if source != "codex" {
				return
			}

			for _, target := range []string{"codex", "claude", "qoder"} {
				t.Run("qoder_"+target, func(t *testing.T) {
					var args []string
					if target == "qoder" {
						args = qoderArgs
					} else {
						args = []string{"--target", target, "--cwd", cwd, "--home", t.TempDir()}
					}
					out := runCLI(t, binary, append(append([]string{"import", shared}, args...), "--execute")...)
					if out["status"] != "ok" {
						t.Fatalf("Qoder to %s: %#v", target, out)
					}
				})
			}
			if out := runCLI(t, binary, "bridge", "read", shared); out["status"] != "ok" {
				t.Fatalf("DSH bridge reader: %#v", out)
			}
		})
	}
}
