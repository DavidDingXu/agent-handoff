package cli

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/qoder"
)

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	source := sourceFlag(fs)
	database := fs.String("database", "", "Qoder desktop main.sqlite")
	home := fs.String("home", "", "agent home")
	thread := fs.String("thread", "", "thread/session id to verify")
	cwd := fs.String("cwd", "", "expected cwd")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *thread == "" {
		return fmt.Errorf("missing --thread")
	}
	sourceAgent, err := detectSource(*source)
	if err != nil {
		return err
	}

	switch sourceAgent {
	case bundle.AgentQoder:
		path, runtimeHome, err := qoderPaths(*database, *home)
		if err != nil {
			return err
		}
		task, _, transcript, err := qoder.Load(path, runtimeHome, *thread)
		if err != nil {
			return err
		}
		if *cwd != "" {
			expected, err := filepath.EvalSymlinks(absolutePath(*cwd))
			if err != nil {
				return err
			}
			if filepath.Clean(task.CWD) != filepath.Clean(expected) {
				return fmt.Errorf("the Qoder task workspace mismatch")
			}
		}
		return printJSON(map[string]any{"status": "ok", "session_id": task.ID, "cwd": task.CWD, "history_entries": len(transcript.Entries)})
	case bundle.AgentClaude:
		claudeHome, err := claude.ResolveHome(*home)
		if err != nil {
			return err
		}
		return printJSON(claude.Verify(claudeHome, *thread, *cwd))
	default:
		codexHome, err := codex.ResolveHome(*home)
		if err != nil {
			return err
		}
		return printJSON(codex.Verify(codexHome, *thread, *cwd))
	}
}
