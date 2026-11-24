package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/qoder"
)

type handoffWorkspace struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

type workspaceCatalog struct {
	seen  map[string]bool
	items []handoffWorkspace
}

func newWorkspaceCatalog() *workspaceCatalog {
	return &workspaceCatalog{seen: make(map[string]bool), items: []handoffWorkspace{}}
}

func (c *workspaceCatalog) add(path, name string) {
	if !filepath.IsAbs(path) || len(path) > 4096 || len(c.items) >= 500 {
		return
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return
	}
	key := resolved
	if runtime.GOOS == "windows" {
		key = strings.ToLower(key)
	}
	if c.seen[key] {
		return
	}
	if info, err := os.Stat(resolved); err != nil || !info.IsDir() {
		return
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(resolved)
	}
	c.seen[key] = true
	c.items = append(c.items, handoffWorkspace{resolved, name})
}

func cmdWorkspaces(args []string) error {
	fs := flag.NewFlagSet("workspaces", flag.ContinueOnError)
	source := fs.String("source", "", "desktop host: codex or qoder")
	home := fs.String("home", "", "agent profile")
	database := fs.String("database", "", "Qoder desktop main.sqlite")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument to workspaces")
	}
	result := newWorkspaceCatalog()
	switch *source {
	case "qoder":
		database, _, err := qoderPaths(*database, *home)
		if err != nil {
			return err
		}
		roots, err := qoder.ListWorkspaces(database)
		if err != nil {
			return err
		}
		for _, workspace := range roots {
			result.add(workspace.Path, workspace.Name)
		}
		tasks, err := qoder.List(database, "")
		if err != nil {
			return err
		}
		for _, task := range tasks {
			result.add(task.CWD, "")
		}
	case "codex":
		profile, err := codex.ResolveHome(*home)
		if err != nil {
			return err
		}
		if err := result.addSavedCodexWorkspaces(profile); err != nil {
			return err
		}
		tasks, err := codex.ListTasks(profile, "")
		if err != nil {
			return err
		}
		for _, task := range tasks {
			result.add(task.CWD, "")
		}
	default:
		return fmt.Errorf("select --source codex or --source qoder")
	}
	return printJSON(map[string]any{"workspaces": result.items})
}

func (c *workspaceCatalog) addSavedCodexWorkspaces(profile string) error {
	path := filepath.Join(profile, ".codex-global-state.json")
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	const maxGlobalState = 16 << 20
	if !info.Mode().IsRegular() || info.Size() > maxGlobalState {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var state struct {
		Roots  []string          `json:"electron-saved-workspace-roots"`
		Labels map[string]string `json:"electron-workspace-root-labels"`
	}
	// Desktop may rewrite this file while the panel opens. Task directories
	// remain usable when the optional saved-root snapshot cannot be decoded.
	if json.NewDecoder(io.LimitReader(f, maxGlobalState)).Decode(&state) != nil {
		return nil
	}
	for _, root := range state.Roots {
		c.add(root, state.Labels[root])
	}
	return nil
}
