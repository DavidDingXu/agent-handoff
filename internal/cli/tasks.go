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

type localTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	UpdatedAt string `json:"updated_at"`
}

func cmdTasks(args []string) error {
	fs := flag.NewFlagSet("tasks", flag.ContinueOnError)
	source := sourceFlag(fs)
	database := fs.String("database", "", "Qoder desktop main.sqlite")
	home := fs.String("home", "", "source agent home")
	cwd := fs.String("cwd", "", "only list tasks whose workspace matches this directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("usage: agent-handoff tasks [--source codex|claude|qoder] [--home DIR] [--cwd DIR]")
	}
	agent, err := detectSource(*source)
	if err != nil {
		return err
	}
	filter := *cwd
	if filter != "" {
		filter, err = filepath.Abs(filter)
		if err != nil {
			return err
		}
	}
	if agent == bundle.AgentQoder {
		path, runtimeHome, err := qoderPaths(*database, *home)
		if err != nil {
			return err
		}
		tasks, err := qoder.List(path, "")
		if err != nil {
			return err
		}
		filtered := []qoder.Task{}
		for _, task := range tasks {
			if filter == "" || filepath.Clean(task.CWD) == filepath.Clean(filter) {
				filtered = append(filtered, task)
			}
		}
		return printJSON(map[string]any{"status": "ok", "source_agent": agent, "source_home": runtimeHome, "tasks": filtered})
	}
	resolvedHome, tasks, err := listLocalTasks(agent, *home, filter)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"status": "ok", "source_agent": agent, "source_home": resolvedHome, "tasks": tasks})
}

func listLocalTasks(agent, home, cwd string) (string, []localTask, error) {
	out := []localTask{}
	switch agent {
	case bundle.AgentCodex:
		resolvedHome, err := codex.ResolveHome(home)
		if err != nil {
			return "", nil, err
		}
		tasks, err := codex.ListTasks(resolvedHome, cwd)
		if err != nil {
			return "", nil, err
		}
		for _, task := range tasks {
			out = append(out, localTask(task))
		}
		return resolvedHome, out, nil
	case bundle.AgentClaude:
		resolvedHome, err := claude.ResolveHome(home)
		if err != nil {
			return "", nil, err
		}
		tasks, err := claude.ListTasks(resolvedHome, cwd)
		if err != nil {
			return "", nil, err
		}
		for _, task := range tasks {
			out = append(out, localTask(task))
		}
		return resolvedHome, out, nil
	default:
		return "", nil, fmt.Errorf("unsupported task catalog source %q", agent)
	}
}
