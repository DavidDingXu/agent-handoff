package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Selecting the newest task in the same directory would silently move the
// wrong conversation when the host has not supplied an exact session ID.
func TestSwitchRequiresExplicitTaskSelectionOutsideAgent(t *testing.T) {
	clearSwitchAgentEnv(t)
	home, cwd, targetHome := t.TempDir(), t.TempDir(), t.TempDir()
	writeSwitchCodexTask(t, home, "wanted-task", "Wanted task", cwd, 10)
	writeSwitchCodexTask(t, home, "newer-task", "Newer task", cwd, 20)
	result, err := captureLocalCommand(t, []string{"switch", "--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome, "--cwd", cwd})
	if err == nil || result["status"] != "selection_required" || result["error_code"] != "source_task_required" {
		t.Fatalf("switch result = %#v, error = %v", result, err)
	}
	tasks, _ := result["tasks"].([]any)
	if len(tasks) != 2 || tasks[0].(map[string]any)["id"] != "newer-task" {
		t.Fatalf("selection candidates = %#v", tasks)
	}
	if entries, err := os.ReadDir(targetHome); err != nil || len(entries) != 0 {
		t.Fatalf("selection wrote target state: entries=%v, error=%v", entries, err)
	}
}

// A switch must copy the explicitly chosen task and create a fresh receiver
// identity on every invocation, including a second copy of the same source.
func TestSwitchCopiesExactTaskWithoutInvokingAgentModels(t *testing.T) {
	clearSwitchAgentEnv(t)
	binary := buildSwitchCore(t)
	for _, source := range []string{"codex", "claude"} {
		for _, target := range []string{"codex", "claude"} {
			t.Run(source+"-to-"+target, func(t *testing.T) {
				home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
				var sourcePath string
				if source == "codex" {
					sourcePath = writeSwitchCodexTask(t, home, "exact-source", "Chosen task", cwd, 10)
					writeSwitchCodexTask(t, home, "wrong-newest", "Wrong task", cwd, 20)
				} else {
					sourcePath = writeSwitchClaudeTask(t, home, "exact-source", "Chosen task", cwd)
					writeSwitchClaudeTask(t, home, "wrong-newest", "Wrong task", cwd)
				}
				before, err := os.ReadFile(sourcePath)
				if err != nil {
					t.Fatal(err)
				}
				args := []string{"switch", "--source", source, "--target", target, "--thread", "exact-source", "--home", home, "--target-home", targetHome, "--cwd", cwd}
				// No agent CLI or shell is available in this PATH. The exact core
				// executable still completes local export/import without a model.
				env := []string{"PATH=" + t.TempDir(), "CODEX_THREAD_ID=wrong-newest", "CLAUDE_SESSION_ID=wrong-newest"}
				first, err := runSwitchBinary(t, binary, args, env)
				if err != nil {
					t.Fatalf("first switch: %v; result=%#v", err, first)
				}
				second, err := runSwitchBinary(t, binary, args, env)
				if err != nil {
					t.Fatalf("second switch: %v; result=%#v", err, second)
				}
				firstID, _ := first["target_thread_id"].(string)
				secondID, _ := second["target_thread_id"].(string)
				if first["status"] != "ok" || first["source_thread_id"] != "exact-source" || first["target_agent"] != target || firstID == "" || firstID == "exact-source" || secondID == firstID || first["new_task"] != true {
					t.Fatalf("first=%#v, second=%#v", first, second)
				}
				if !strings.Contains(first["fallback_command"].(string), firstID) {
					t.Fatalf("missing exact resume fallback: %#v", first)
				}
				catalog, err := runSwitchBinary(t, binary, []string{"tasks", "--source", target, "--home", targetHome}, env)
				if err != nil || len(catalog["tasks"].([]any)) != 2 {
					t.Fatalf("target catalog=%#v, error=%v", catalog, err)
				}
				after, err := os.ReadFile(sourcePath)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("source session changed: %v", err)
				}
			})
		}
	}
}

func TestSwitchFailureAndLocalSelection(t *testing.T) {
	clearSwitchAgentEnv(t)
	binary := buildSwitchCore(t)
	t.Run("trusted source env selects exact task", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "env-task", "Environment task", cwd, 10)
		writeSwitchCodexTask(t, home, "newer-task", "Wrong task", cwd, 20)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome, "--cwd", cwd}, []string{"CODEX_THREAD_ID=env-task"})
		if err != nil || result["source_thread_id"] != "env-task" || result["status"] != "ok" {
			t.Fatalf("result=%#v, error=%v", result, err)
		}
	})
	t.Run("conflicting agent environments require choosing the source", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "codex-source", "Codex source", cwd, 10)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--target", "claude", "--home", home, "--target-home", targetHome, "--cwd", cwd}, []string{"CODEX_THREAD_ID=codex-source", "CLAUDE_SESSION_ID=claude-source"})
		if err == nil || result["status"] != "selection_required" || result["error_code"] != "source_agent_required" {
			t.Fatalf("ambiguous source result=%#v, error=%v", result, err)
		}
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 {
			t.Fatal("ambiguous source created a target task")
		}
	})
	t.Run("missing environment task never falls back to newest", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "newest-task", "Wrong task", cwd, 20)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome, "--cwd", cwd}, []string{"CODEX_THREAD_ID=missing-task"})
		if err == nil || result["error_code"] != "export_failed" {
			t.Fatalf("missing exact source result=%#v, error=%v", result, err)
		}
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 {
			t.Fatal("missing exact source created a target task")
		}
	})
	t.Run("prefix ID cannot select a different rollout", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "source-other", "Wrong task", cwd, 20)
		if err := os.Remove(filepath.Join(home, "state_5.sqlite")); err != nil {
			t.Fatal(err)
		}
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--thread", "source", "--home", home, "--target-home", targetHome, "--cwd", cwd}, nil)
		if err == nil || result["error_code"] != "export_failed" {
			t.Fatalf("prefix source result=%#v, error=%v", result, err)
		}
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 {
			t.Fatal("prefix ID exported a different native session")
		}
	})
	t.Run("interactive selection needs an explicit numbered choice", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "chosen-task", "Chosen task", cwd, 10)
		writeSwitchCodexTask(t, home, "newest-task", "Newer task", cwd, 20)
		var prompt bytes.Buffer
		result, err := captureLocalOperation(t, func() error {
			return runSwitch([]string{"--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome, "--cwd", cwd}, switchRuntime{input: strings.NewReader("2\n"), prompt: &prompt, interactive: true, executable: binary})
		})
		if err != nil || result["source_thread_id"] != "chosen-task" || !strings.Contains(prompt.String(), "chosen-task") {
			t.Fatalf("result=%#v, error=%v, prompt=%q", result, err, prompt.String())
		}
	})
	t.Run("interactive dismissal never selects the newest task", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "candidate", "Candidate task", cwd, 10)
		var prompt bytes.Buffer
		result, err := captureLocalOperation(t, func() error {
			return runSwitch([]string{"--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome}, switchRuntime{input: strings.NewReader("\n"), prompt: &prompt, interactive: true, executable: binary})
		})
		if err == nil || result["status"] != "selection_required" {
			t.Fatalf("dismissal result=%#v, error=%v", result, err)
		}
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 {
			t.Fatalf("dismissal wrote target state: %v", entries)
		}
	})
	t.Run("import failure preserves a usable local zip", func(t *testing.T) {
		home, cwd := t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "source", "Source task", cwd, 10)
		blockedTarget := filepath.Join(t.TempDir(), "file-not-home")
		if err := os.WriteFile(blockedTarget, []byte("do not change"), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--thread", "source", "--home", home, "--target-home", blockedTarget, "--cwd", cwd}, nil)
		path, _ := result["path"].(string)
		if err == nil || result["status"] != "import_failed" || path == "" {
			t.Fatalf("failure result=%#v, error=%v", result, err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(path)) })
		preview, err := runSwitchBinary(t, binary, []string{"preview", path}, nil)
		if err != nil || preview["source_thread"] != "source" {
			t.Fatalf("preserved zip preview=%#v, error=%v", preview, err)
		}
		data, _ := os.ReadFile(blockedTarget)
		if string(data) != "do not change" {
			t.Fatal("failed import changed target file")
		}
	})
	t.Run("secret scan cannot be silently overridden", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		path := writeSwitchClaudeTask(t, home, "secret-source", "synthetic sk-proj-abcdefghijklmnopqrstuvwxyz123456", cwd)
		before, _ := os.ReadFile(path)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "claude", "--target", "codex", "--thread", "secret-source", "--home", home, "--target-home", targetHome, "--cwd", cwd}, nil)
		if err == nil || result["status"] != "blocked" || result["path"] != nil {
			t.Fatalf("secret result=%#v, error=%v", result, err)
		}
		after, _ := os.ReadFile(path)
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 || !bytes.Equal(before, after) {
			t.Fatal("blocked switch changed native session state")
		}
	})
}

func TestSwitchForwardsSelectedContextAndOpensOnlyExactResume(t *testing.T) {
	clearSwitchAgentEnv(t)
	binary := buildSwitchCore(t)
	t.Run("selected code and note arrive as inert evidence", func(t *testing.T) {
		home, targetHome := t.TempDir(), t.TempDir()
		cwd := workflowRepo(t)
		if err := os.WriteFile(filepath.Join(cwd, "extra.txt"), []byte("selected new file\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		writeSwitchCodexTask(t, home, "source-with-code", "Code task", cwd, 10)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--thread", "source-with-code", "--home", home, "--target-home", targetHome, "--cwd", cwd, "--include-code", "--code-file", "extra.txt", "--brief-note", "Review the changed and selected new files."}, nil)
		if err != nil || result["code"] == nil || result["code_applied"] != false {
			t.Fatalf("context result=%#v, error=%v", result, err)
		}
		briefPath, _ := result["brief_path"].(string)
		brief, err := os.ReadFile(briefPath)
		if err != nil || !strings.Contains(string(brief), "Review the changed and selected new files.") {
			t.Fatalf("brief evidence=%q, error=%v", brief, err)
		}
		patch, err := os.ReadFile(filepath.Join(filepath.Dir(briefPath), "code", "changes.patch"))
		if err != nil || !strings.Contains(string(patch), "hello.txt") {
			t.Fatalf("code evidence=%q, error=%v", patch, err)
		}
		newFile, err := os.ReadFile(filepath.Join(filepath.Dir(briefPath), "code", "files", "extra.txt"))
		if err != nil || string(newFile) != "selected new file\n" {
			t.Fatalf("selected new file evidence=%q, error=%v", newFile, err)
		}
	})
	t.Run("missing target CLI fails before creating task", func(t *testing.T) {
		home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "source", "Source", cwd, 10)
		result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--thread", "source", "--home", home, "--target-home", targetHome, "--cwd", cwd, "--open"}, []string{"PATH=" + t.TempDir()})
		if err == nil || result["error_code"] != "target_cli_unavailable" {
			t.Fatalf("open failure=%#v, error=%v", result, err)
		}
		if entries, _ := os.ReadDir(targetHome); len(entries) != 0 {
			t.Fatal("unavailable target caused a native import")
		}
	})
	for _, target := range []string{"codex", "claude"} {
		t.Run("opens "+target+" with no prompt", func(t *testing.T) {
			home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
			writeSwitchCodexTask(t, home, "source", "Source", cwd, 10)
			targetDir := t.TempDir()
			buildSwitchTargetRecorder(t, targetDir, target)
			record := filepath.Join(t.TempDir(), "open.json")
			result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", target, "--thread", "source", "--home", home, "--target-home", targetHome, "--cwd", cwd, "--open"}, []string{"PATH=" + targetDir, "SWITCH_OPEN_RECORD=" + record, "CLAUDECODE=1", "CODEX_THREAD_ID=wrong-old-id", "CLAUDE_SESSION_ID=wrong-old-id"})
			if err != nil || result["status"] != "ok" || result["open_requested"] != true {
				t.Fatalf("open result=%#v, error=%v", result, err)
			}
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			var opened struct {
				Args     []string `json:"args"`
				CWD      string   `json:"cwd"`
				Home     string   `json:"home"`
				SourceID string   `json:"source_id"`
				Nested   string   `json:"nested"`
			}
			if err := json.Unmarshal(data, &opened); err != nil {
				t.Fatal(err)
			}
			resume := "resume"
			if target == "claude" {
				resume = "--resume"
			}
			expectedCWD, err := os.Stat(cwd)
			if err != nil {
				t.Fatal(err)
			}
			actualCWD, err := os.Stat(opened.CWD)
			if err != nil {
				t.Fatal(err)
			}
			if len(opened.Args) != 2 || opened.Args[0] != resume || opened.Args[1] != result["target_thread_id"] || !os.SameFile(expectedCWD, actualCWD) || opened.Home != targetHome || opened.SourceID != "" || opened.Nested != "" {
				t.Fatalf("opened target=%#v; result=%#v", opened, result)
			}
		})
	}
}

// A toolbar or picker may run in an unrelated launcher directory. The saved
// source workspace must remain the default unless the user overrides it.
func TestSwitchUsesSourceWorkspaceInsteadOfLauncherDirectory(t *testing.T) {
	clearSwitchAgentEnv(t)
	binary := buildSwitchCore(t)
	for _, source := range []string{"codex", "claude"} {
		for _, target := range []string{"codex", "claude"} {
			t.Run(source+"-to-"+target, func(t *testing.T) {
				home, targetHome, sourceCWD, launcherCWD := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
				if source == "codex" {
					writeSwitchCodexTask(t, home, "selected-source", "Selected source", sourceCWD, 10)
				} else {
					writeSwitchClaudeTask(t, home, "selected-source", "Selected source", sourceCWD)
				}
				result, err := runSwitchBinaryFrom(t, binary, []string{"switch", "--source", source, "--target", target, "--thread", "selected-source", "--home", home, "--target-home", targetHome}, nil, launcherCWD)
				if err != nil || result["target_cwd"] != sourceCWD {
					t.Fatalf("source cwd=%q, launcher cwd=%q, result=%#v, error=%v", sourceCWD, launcherCWD, result, err)
				}
				resume, _ := result["resume_command"].(map[string]any)
				if resume["cwd"] != sourceCWD {
					t.Fatalf("resume workspace=%#v", resume)
				}
			})
		}
	}
	t.Run("picker selection keeps the selected workspace", func(t *testing.T) {
		home, targetHome, sourceCWD, launcherCWD := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
		writeSwitchCodexTask(t, home, "picked-source", "Picked source", sourceCWD, 10)
		t.Chdir(launcherCWD)
		var prompt bytes.Buffer
		result, err := captureLocalOperation(t, func() error {
			return runSwitch([]string{"--source", "codex", "--target", "claude", "--home", home, "--target-home", targetHome}, switchRuntime{input: strings.NewReader("1\n"), prompt: &prompt, interactive: true, executable: binary})
		})
		if err != nil || result["source_thread_id"] != "picked-source" || result["target_cwd"] != sourceCWD {
			t.Fatalf("picker result=%#v, error=%v", result, err)
		}
	})
	for _, sourceAvailable := range []bool{true, false} {
		t.Run("explicit workspace overrides saved workspace "+map[bool]string{true: "present", false: "missing"}[sourceAvailable], func(t *testing.T) {
			home, targetHome, sourceCWD, launcherCWD, targetCWD := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
			if !sourceAvailable {
				sourceCWD = ""
			}
			writeSwitchCodexTask(t, home, "source", "Source", sourceCWD, 10)
			result, err := runSwitchBinaryFrom(t, binary, []string{"switch", "--source", "codex", "--target", "claude", "--thread", "source", "--home", home, "--target-home", targetHome, "--cwd", targetCWD}, nil, launcherCWD)
			if err != nil || result["target_cwd"] != targetCWD {
				t.Fatalf("explicit workspace result=%#v, error=%v", result, err)
			}
		})
	}
	t.Run("unavailable workspace requires an explicit local directory before import", func(t *testing.T) {
		for _, kind := range []string{"missing source", "removed source", "relative source", "source is a file", "explicit unavailable"} {
			t.Run(kind, func(t *testing.T) {
				home, targetHome, launcherCWD := t.TempDir(), t.TempDir(), t.TempDir()
				sourceCWD := ""
				var override []string
				switch kind {
				case "removed source":
					sourceCWD = filepath.Join(t.TempDir(), "removed-workspace")
				case "relative source":
					sourceCWD = "relative-workspace"
					if err := os.Mkdir(filepath.Join(launcherCWD, sourceCWD), 0o700); err != nil {
						t.Fatal(err)
					}
				case "source is a file":
					sourceCWD = filepath.Join(t.TempDir(), "file-not-directory")
					if err := os.WriteFile(sourceCWD, []byte("keep"), 0o600); err != nil {
						t.Fatal(err)
					}
				case "explicit unavailable":
					sourceCWD = t.TempDir()
					override = []string{"--cwd", filepath.Join(t.TempDir(), "missing-target")}
				}
				writeSwitchCodexTask(t, home, "source", "Source", sourceCWD, 10)
				args := append([]string{"switch", "--source", "codex", "--target", "claude", "--thread", "source", "--home", home, "--target-home", targetHome}, override...)
				result, err := runSwitchBinaryFrom(t, binary, args, nil, launcherCWD)
				if err == nil || result["status"] != "selection_required" || result["error_code"] != "target_cwd_required" || !strings.Contains(result["message"].(string), "--cwd") {
					t.Fatalf("workspace decision result=%#v, error=%v", result, err)
				}
				if entries, err := os.ReadDir(targetHome); err != nil || len(entries) != 0 {
					t.Fatalf("invalid workspace wrote native target state: entries=%v, error=%v", entries, err)
				}
			})
		}
	})
}

func buildSwitchTargetRecorder(t *testing.T, dir, target string) {
	t.Helper()
	source := filepath.Join(t.TempDir(), "main.go")
	code := `package main
import ("encoding/json"; "os")
func main() {
 cwd, _ := os.Getwd()
 home := os.Getenv("CODEX_HOME")
 if "TARGET" == "claude" { home = os.Getenv("CLAUDE_CONFIG_DIR") }
 data, _ := json.Marshal(map[string]any{"args": os.Args[1:], "cwd": cwd, "home": home, "source_id": os.Getenv("CODEX_THREAD_ID") + os.Getenv("CLAUDE_SESSION_ID"), "nested": os.Getenv("CLAUDECODE")})
 _ = os.WriteFile(os.Getenv("SWITCH_OPEN_RECORD"), data, 0600)
}`
	// Embed the target choice in this test-only executable for Windows .exe.
	code = strings.Replace(code, `"TARGET"`, `"`+target+`"`, 1)
	if err := os.WriteFile(source, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	name := target
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), source)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build target recorder: %v\n%s", err, output)
	}
}

func buildSwitchCore(t *testing.T) string {
	t.Helper()
	name := "agent-handoff"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	path := filepath.Join(t.TempDir(), name)
	cmd := exec.Command("go", "build", "-o", path, ".")
	cmd.Dir = filepath.Join("..", "..")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build local core: %v\n%s", err, output)
	}
	return path
}

func runSwitchBinary(t *testing.T, binary string, args, env []string) (map[string]any, error) {
	t.Helper()
	return runSwitchBinaryFrom(t, binary, args, env, "")
}

func runSwitchBinaryFrom(t *testing.T, binary string, args, env []string, cwd string) (map[string]any, error) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var result map[string]any
	if stdout.Len() > 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			t.Fatalf("invalid core JSON %q: %v; stderr=%s", stdout.String(), decodeErr, stderr.String())
		}
	}
	if err != nil {
		t.Logf("core stderr: %s", stderr.String())
	}
	return result, err
}

func clearSwitchAgentEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAUDECODE"} {
		t.Setenv(key, "")
	}
}

func TestWindowsShimKeepsPathAndArgumentsLiteral(t *testing.T) {
	cmd := windowsShimCommand(`C:\Program Files\A&B's CLI\‘quoted’\codex.cmd`, []string{"resume", "fresh-id"})
	want := `& 'C:\Program Files\A&B''s CLI\‘‘quoted’’\codex.cmd' 'resume' 'fresh-id'; exit $LASTEXITCODE`
	if got := cmd.Args[len(cmd.Args)-1]; got != want {
		t.Fatalf("launcher command = %q, want %q", got, want)
	}
}

func TestSwitchOpensWindowsNpmShims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires Windows batch process execution")
	}
	clearSwitchAgentEnv(t)
	binary := buildSwitchCore(t)
	for _, target := range []string{"codex", "claude"} {
		t.Run(target, func(t *testing.T) {
			home, targetHome, cwd := t.TempDir(), t.TempDir(), t.TempDir()
			writeSwitchCodexTask(t, home, "source", "Synthetic source", cwd, 10)
			dir := filepath.Join(t.TempDir(), "npm shim & spaces")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			buildSwitchTargetRecorder(t, dir, target)
			if err := os.Rename(filepath.Join(dir, target+".exe"), filepath.Join(dir, "recorder.exe")); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, target+".cmd"), []byte("@\"%~dp0recorder.exe\" %*\r\n"), 0600); err != nil {
				t.Fatal(err)
			}
			record := filepath.Join(t.TempDir(), "open.json")
			result, err := runSwitchBinary(t, binary, []string{"switch", "--source", "codex", "--target", target, "--thread", "source", "--home", home, "--target-home", targetHome, "--cwd", cwd, "--open"}, []string{"PATH=" + dir, "SWITCH_OPEN_RECORD=" + record})
			if err != nil || result["status"] != "ok" {
				t.Fatalf("batch launcher result=%#v error=%v", result, err)
			}
			var opened struct {
				Args []string `json:"args"`
				Home string   `json:"home"`
			}
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &opened); err != nil {
				t.Fatal(err)
			}
			resume := "resume"
			if target == "claude" {
				resume = "--resume"
			}
			if len(opened.Args) != 2 || opened.Args[0] != resume || opened.Args[1] != result["target_thread_id"] || opened.Home != targetHome {
				t.Fatalf("batch launcher changed target identity: %#v", opened)
			}
		})
	}
}
