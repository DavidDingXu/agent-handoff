package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/mattn/go-isatty"
)

type switchRuntime struct {
	input       io.Reader
	prompt      io.Writer
	interactive bool
	executable  string
}

// switch deliberately requires an exact identity. The adapters' historical
// "current" resolution is useful for share, but is unsafe for a local switch
// action that may run outside the source agent's process environment.
func cmdSwitch(args []string) error {
	interactive := isatty.IsTerminal(os.Stdin.Fd()) || isatty.IsCygwinTerminal(os.Stdin.Fd())
	return runSwitch(args, switchRuntime{input: os.Stdin, prompt: os.Stderr, interactive: interactive})
}

func runSwitch(args []string, runtime switchRuntime) error {
	fs := flag.NewFlagSet("switch", flag.ContinueOnError)
	source := sourceFlag(fs)
	target := fs.String("target", "", "required target agent: codex or claude")
	thread := fs.String("thread", "", "exact source task/session ID")
	home := fs.String("home", "", "source agent home")
	targetHome := fs.String("target-home", "", "target agent home")
	cwd := fs.String("cwd", "", "target workspace (defaults to source workspace); also filters local task selection")
	open := fs.Bool("open", false, "open target CLI with the new history ID, without submitting a prompt")
	includeCode := fs.Bool("include-code", false, "include eligible tracked text changes from the source workspace")
	briefNote := fs.String("brief-note", "", "optional handoff note (plain text; no model required)")
	var codeFiles pathFlags
	fs.Var(&codeFiles, "code-file", "explicit changed/new file relative to the source repository (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: agent-handoff switch --target codex|claude [--source codex|claude] [--thread ID]")
	}
	if *target != bundle.AgentCodex && *target != bundle.AgentClaude {
		return fmt.Errorf("--target is required (expected codex|claude)")
	}
	launchPath := ""
	if *open {
		path, err := exec.LookPath(*target)
		if err != nil {
			_ = printJSON(map[string]any{
				"status": "target_unavailable", "error_code": "target_cli_unavailable", "target_agent": *target,
				"message": "Install the target CLI and put it on PATH, or omit --open to create the native task and receive its resume command.",
			})
			return fmt.Errorf("target CLI %s is unavailable; no task was created", *target)
		}
		launchPath, err = filepath.Abs(path)
		if err != nil {
			return err
		}
	}
	if strings.TrimSpace(*source) == "" &&
		(strings.TrimSpace(os.Getenv("CODEX_THREAD_ID")) != "" || strings.TrimSpace(os.Getenv("CODEX_SESSION_ID")) != "") &&
		(strings.TrimSpace(os.Getenv("CLAUDE_SESSION_ID")) != "" || strings.TrimSpace(os.Getenv("CLAUDE_CODE_SESSION_ID")) != "") {
		_ = printJSON(map[string]any{
			"status": "selection_required", "error_code": "source_agent_required",
			"message": "Both Codex and Claude source identities are present. Choose --source codex or --source claude; no task was selected or changed.",
		})
		return fmt.Errorf("source agent is ambiguous; pass --source codex|claude")
	}
	agent, err := detectSource(*source)
	if err != nil {
		return err
	}
	if *cwd != "" {
		*cwd, err = filepath.Abs(*cwd)
		if err != nil {
			return err
		}
	}
	id := strings.TrimSpace(*thread)
	if id == "current" {
		id = ""
	}
	if id == "" {
		keys := []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"}
		if agent == bundle.AgentClaude {
			keys = []string{"CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"}
		}
		for _, key := range keys {
			if value := strings.TrimSpace(os.Getenv(key)); value != "" {
				id = value
				break
			}
		}
	}
	if id == "" {
		resolvedHome, tasks, err := listLocalTasks(agent, *home, *cwd)
		if err != nil {
			return err
		}
		if runtime.interactive && len(tasks) > 0 {
			fmt.Fprintln(runtime.prompt, "Choose the exact source task to hand off:")
			for i, task := range tasks {
				fmt.Fprintf(runtime.prompt, "  %d. %q  ID=%q  cwd=%q  updated=%s\n", i+1, task.Title, task.ID, task.CWD, task.UpdatedAt)
			}
			fmt.Fprint(runtime.prompt, "Task number (Enter to cancel): ")
			scanner := bufio.NewScanner(runtime.input)
			scanner.Buffer(make([]byte, 1024), 4096)
			if scanner.Scan() {
				choice, err := strconv.Atoi(strings.TrimSpace(scanner.Text()))
				if err == nil && choice > 0 && choice <= len(tasks) {
					id = tasks[choice-1].ID
				}
			}
		}
		if id == "" {
			_ = printJSON(map[string]any{
				"status": "selection_required", "error_code": "source_task_required",
				"source_agent": agent, "source_home": resolvedHome, "tasks": tasks,
				"message": "Choose the source task ID, then re-run switch with --thread ID. Listing tasks never changes the selected source task.",
			})
			return fmt.Errorf("source task ID required; run agent-handoff tasks --source %s or pass --thread ID", agent)
		}
	}
	executable := runtime.executable
	if executable == "" {
		executable, err = os.Executable()
		if err != nil {
			return err
		}
	}
	tempDir, err := os.MkdirTemp("", "agent-handoff-switch-")
	if err != nil {
		return err
	}
	preserveZip := false
	defer func() {
		if !preserveZip {
			_ = os.RemoveAll(tempDir)
		}
	}()
	zipPath := filepath.Join(tempDir, "task.agent-handoff.zip")
	shareArgs := []string{"share", "--source", agent, "--thread", id, "--format", "zip", "--out", zipPath}
	if *home != "" {
		shareArgs = append(shareArgs, "--home", *home)
	}
	if *includeCode {
		shareArgs = append(shareArgs, "--include-code")
	}
	for _, path := range codeFiles {
		shareArgs = append(shareArgs, "--code-file", path)
	}
	if *briefNote != "" {
		shareArgs = append(shareArgs, "--brief-note", *briefNote)
	}
	shared, err := runLocalCore(executable, shareArgs)
	if err != nil || shared["status"] != "ok" {
		if shared == nil {
			shared = map[string]any{"status": "error"}
		}
		shared["error_code"] = "export_failed"
		_ = printJSON(shared)
		return fmt.Errorf("local source export failed: %w", nonNilSwitchError(err))
	}
	// A launcher can live outside the chosen task's workspace. Use the exact
	// local export response rather than letting import inherit the launch cwd.
	sourceCWD, _ := shared["source_cwd"].(string)
	targetCWD := *cwd
	if targetCWD == "" {
		targetCWD = sourceCWD
	}
	cwdAvailable := filepath.IsAbs(targetCWD)
	if cwdAvailable {
		info, err := os.Stat(targetCWD)
		cwdAvailable = err == nil && info.IsDir()
	}
	if !cwdAvailable {
		_ = printJSON(map[string]any{
			"status": "selection_required", "error_code": "target_cwd_required",
			"source_agent": agent, "source_thread_id": id, "source_cwd": sourceCWD,
			"target_agent": *target, "target_cwd": targetCWD,
			"message": "Choose an existing local workspace with --cwd DIR; no target task was created.",
		})
		return fmt.Errorf("target workspace is unavailable; re-run switch with --cwd DIR pointing to an existing local directory")
	}
	preserveZip = true
	importArgs := []string{"import", zipPath, "--target", *target, "--execute", "--allow-duplicate", "--cwd", targetCWD}
	if *targetHome != "" {
		importArgs = append(importArgs, "--home", *targetHome)
	}
	imported, err := runLocalCore(executable, importArgs)
	if imported == nil {
		imported = map[string]any{}
	}
	imported["source_agent"], imported["source_thread_id"], imported["target_agent"] = agent, id, *target
	if err != nil || imported["status"] != "ok" {
		imported["status"], imported["error_code"], imported["path"] = "import_failed", "import_failed", zipPath
		imported["message"] = "The local zip is ready. Import failed; keep this zip and retry import after fixing the target."
		_ = printJSON(imported)
		return fmt.Errorf("local import failed; usable zip preserved at %s: %w", zipPath, nonNilSwitchError(err))
	}
	newID, _ := imported["thread_id"].(string)
	if newID == "" {
		newID, _ = imported["session_id"].(string)
	}
	if newID == "" || newID == id {
		imported["status"], imported["error_code"], imported["path"] = "import_failed", "new_task_identity_required", zipPath
		_ = printJSON(imported)
		return fmt.Errorf("import did not report a fresh task identity; usable zip preserved at %s", zipPath)
	}
	imported["new_task"], imported["target_thread_id"] = true, newID
	resumeArgs := []string{"resume", newID}
	if *target == bundle.AgentClaude {
		resumeArgs = []string{"--resume", newID}
	}
	if fallback, _ := imported["fallback_command"].(string); fallback == "" {
		imported["fallback_command"] = *target + " " + strings.Join(resumeArgs, " ")
	}
	homeKey := "CODEX_HOME"
	if *target == bundle.AgentClaude {
		homeKey = "CLAUDE_CONFIG_DIR"
	}
	resolvedTargetHome, _ := imported["target_home"].(string)
	imported["resume_command"] = map[string]any{"executable": *target, "args": resumeArgs, "cwd": imported["target_cwd"], "env": map[string]string{homeKey: resolvedTargetHome}}
	preserveZip = false
	if err := os.RemoveAll(tempDir); err != nil {
		imported["cleanup_warning"], imported["path"] = "The task was created, but the temporary local zip could not be removed.", zipPath
	}
	if *open {
		imported["open_requested"] = true
	}
	if err := printJSON(imported); err != nil {
		return err
	}
	if *open {
		cmd := exec.Command(launchPath, resumeArgs...)
		cmd.Dir, _ = imported["target_cwd"].(string)
		cmd.Env = switchResumeEnv(homeKey, resolvedTargetHome)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = runtime.input, os.Stdout, runtime.prompt
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("new task %s was created; opening %s failed: %w; resume with %s", newID, *target, err, imported["fallback_command"])
		}
	}
	return nil
}

func switchResumeEnv(homeKey, home string) []string {
	var env []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		keep := true
		for _, inherited := range []string{homeKey, "CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID", "CLAUDECODE"} {
			if strings.EqualFold(key, inherited) {
				keep = false
				break
			}
		}
		if keep {
			env = append(env, entry)
		}
	}
	return append(env, homeKey+"="+home)
}

// runLocalCore uses the exact running binary and argv; no shell, model, or
// network service participates in the local zip export/import path.
func runLocalCore(executable string, args []string) (map[string]any, error) {
	cmd := exec.Command(executable, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var result map[string]any
	if stdout.Len() > 0 {
		if decodeErr := json.Unmarshal(stdout.Bytes(), &result); decodeErr != nil {
			return nil, fmt.Errorf("local core returned an invalid JSON response")
		}
	}
	if err != nil {
		return result, fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return result, nil
}

func nonNilSwitchError(err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("operation did not succeed")
}
