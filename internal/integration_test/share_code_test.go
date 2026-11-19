package integration_test

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

// Ignoring share's optional flags, reading the launcher's repository instead of
// the source task's cwd, or collecting every new file must fail this test.
func TestCLIShareSelectedCodeFromBothNativeSources(t *testing.T) {
	binary := buildCLIBinary(t)
	isolatedHome := t.TempDir()
	t.Setenv("HOME", isolatedHome)
	t.Setenv("USERPROFILE", isolatedHome)
	t.Setenv("CODEX_HOME", filepath.Join(isolatedHome, "unused-codex"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(isolatedHome, "unused-claude"))
	for _, source := range []string{bundle.AgentCodex, bundle.AgentClaude} {
		t.Run(source, func(t *testing.T) {
			repo, base := shareCodeRepository(t, "source change\n")
			launcher, _ := shareCodeRepository(t, "unrelated launcher change\n")
			selectedNew := []byte("explicitly selected new file\n")
			if err := os.WriteFile(filepath.Join(repo, "selected-new.txt"), selectedNew, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(repo, "unselected-new.txt"), []byte("must stay local\n"), 0600); err != nil {
				t.Fatal(err)
			}
			home, id, sourcePath := nativeCodeSource(t, source, repo)
			original, err := os.ReadFile(sourcePath)
			if err != nil {
				t.Fatal(err)
			}
			zipPath := filepath.Join(t.TempDir(), "selected-code.zip")
			cmd := exec.Command(binary, "share", "--source", source, "--thread", id, "--home", home, "--format", "zip", "--out", zipPath, "--include-code", "--code-file", "selected-new.txt", "--brief-note", "Review the selected code before continuing.")
			cmd.Dir = launcher
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("share %s: %v\n%s", source, err, output)
			}
			var shared map[string]any
			if err := json.Unmarshal(output, &shared); err != nil || shared["status"] != "ok" {
				t.Fatalf("share result: %s (%v)", output, err)
			}
			exported, err := bundle.ReadZip(zipPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := exported.VerifyChecksums(); err != nil {
				t.Fatal(err)
			}
			if exported.Manifest.SourceAgent != source || exported.Manifest.SourceThreadID != id || exported.Manifest.SourceCWD != repo {
				t.Fatalf("wrong source identity/workspace: %+v", exported.Manifest)
			}
			if exported.Code == nil || exported.Code.Manifest.BaseCommit != base {
				t.Fatalf("missing source Git payload: %+v", exported.Code)
			}
			paths := map[string]string{}
			for _, file := range exported.Code.Manifest.Files {
				paths[file.Path] = file.Status
			}
			if len(paths) != 2 || paths["tracked.txt"] != "modified" || paths["selected-new.txt"] != "untracked" {
				t.Fatalf("incorrect code selection: %+v", paths)
			}
			if !bytes.Contains(exported.Code.Patch, []byte("+source change")) || bytes.Contains(exported.Code.Patch, []byte("launcher")) {
				t.Fatalf("patch used wrong repository: %s", exported.Code.Patch)
			}
			if len(exported.Code.Files) != 1 || !bytes.Equal(exported.Code.Files["selected-new.txt"], selectedNew) {
				t.Fatalf("untracked selection leaked or changed: %+v", exported.Code.Files)
			}
			if exported.Brief == nil || exported.Brief.Notes != "Review the selected code before continuing." || exported.Brief.Objective == nil || exported.Brief.Objective.Text != "Continue the selected work" {
				t.Fatalf("missing recorded brief/notes: %+v", exported.Brief)
			}
			for _, target := range []string{bundle.AgentCodex, bundle.AgentClaude} {
				t.Run("to_"+target, func(t *testing.T) {
					receiverHome, receiverCWD := t.TempDir(), t.TempDir()
					result := runCLI(t, binary, "import", zipPath, "--target", target, "--home", receiverHome, "--cwd", receiverCWD, "--execute")
					if result["status"] != "ok" || result["code_applied"] != false || result["code"] == nil {
						t.Fatalf("optional code not retained: %+v", result)
					}
					manifest, ok := result["code"].(map[string]any)
					if !ok || manifest["base_commit"] != base {
						t.Fatalf("code base did not survive import: %+v", result["code"])
					}
					briefPath, ok := result["brief_path"].(string)
					if !ok {
						t.Fatalf("missing sidecar: %+v", result)
					}
					briefData, err := os.ReadFile(briefPath)
					if err != nil || !bytes.Contains(briefData, []byte("Review the selected code before continuing.")) {
						t.Fatalf("notes not retained: %q %v", briefData, err)
					}
					sidecar := filepath.Dir(briefPath)
					for path, want := range map[string][]byte{"code/changes.patch": exported.Code.Patch, "code/files/selected-new.txt": selectedNew} {
						got, err := os.ReadFile(filepath.Join(sidecar, filepath.FromSlash(path)))
						if err != nil || !bytes.Equal(got, want) {
							t.Fatalf("attachment %s changed: %q %v", path, got, err)
						}
					}
					if _, err := os.Stat(filepath.Join(sidecar, "code", "files", "unselected-new.txt")); !os.IsNotExist(err) {
						t.Fatal("unselected file reached receiver")
					}
					if _, err := os.Stat(filepath.Join(receiverCWD, "selected-new.txt")); !os.IsNotExist(err) {
						t.Fatal("plain import applied code")
					}
				})
			}
			after, err := os.ReadFile(sourcePath)
			if err != nil || !bytes.Equal(after, original) {
				t.Fatalf("source native history changed: %v", err)
			}
			if data, err := os.ReadFile(filepath.Join(repo, "tracked.txt")); err != nil || string(data) != "source change\n" {
				t.Fatal("source code changed during export/import")
			}
		})
	}
}

func shareCodeRepository(t *testing.T, change string) (string, string) {
	t.Helper()
	root := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "core.hooksPath=" + os.DevNull}, args...)...)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(bytes.TrimSpace(out))
	}
	git("init", "-q")
	git("config", "user.name", "Synthetic")
	git("config", "user.email", "synthetic@example.invalid")
	git("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte("base\n"), 0600); err != nil {
		t.Fatal(err)
	}
	git("add", "tracked.txt")
	git("commit", "-qm", "synthetic base")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "tracked.txt"), []byte(change), 0600); err != nil {
		t.Fatal(err)
	}
	return root, base
}

func nativeCodeSource(t *testing.T, source, repo string) (home, id, path string) {
	t.Helper()
	home = t.TempDir()
	transcript := neutral.Transcript{Schema: neutral.Schema, SourceAgent: source, SourceID: "synthetic-code-source", Entries: []neutral.Entry{
		{Kind: neutral.KindMessage, Role: "user", Text: "Continue the selected work"},
		{Kind: neutral.KindMessage, Role: "assistant", Text: "The current change remains in local files."},
	}}
	if source == bundle.AgentCodex {
		result, err := codex.Restore(codex.RestoreInput{SourceThreadID: transcript.SourceID, Title: "Selected code source", Neutral: transcript}, codex.RestoreOptions{Home: home, TargetCWD: repo, Execute: true})
		if err != nil {
			t.Fatal(err)
		}
		data, err := codex.LoadThread(home, result.ThreadID)
		if err != nil {
			t.Fatal(err)
		}
		return home, result.ThreadID, data.SessionPath
	}
	result, err := claude.Restore(claude.RestoreInput{SourceSessionID: transcript.SourceID, Title: "Selected code source", Neutral: transcript}, claude.RestoreOptions{Home: home, TargetCWD: repo, Execute: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := claude.LoadSession(home, result.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	return home, result.SessionID, data.SessionPath
}
