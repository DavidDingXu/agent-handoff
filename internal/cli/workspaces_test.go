package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceCatalogCombinesSavedRootsAndTaskDirectories(t *testing.T) {
	base := t.TempDir()
	saved := filepath.Join(base, "saved")
	task := filepath.Join(base, "from-task")
	for _, path := range []string{saved, task} {
		if err := os.Mkdir(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	have := newWorkspaceCatalog()
	have.add(saved, "Saved name")
	have.add(saved, "Duplicate label")
	have.add(task, "")
	have.add(filepath.Join(base, "missing"), "Unavailable")
	have.add("relative", "Relative")
	if len(have.items) != 2 || have.items[0].Name != "Saved name" || have.items[1].Name != "from-task" {
		t.Fatalf("workspace catalog = %+v", have.items)
	}
}

func TestSavedCodexWorkspacesKeepDesktopLabels(t *testing.T) {
	profile := t.TempDir()
	path := filepath.Join(profile, "project")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{
		"electron-saved-workspace-roots": []string{path},
		"electron-workspace-root-labels": map[string]string{path: "Project label"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, ".codex-global-state.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	catalog := newWorkspaceCatalog()
	if err := catalog.addSavedCodexWorkspaces(profile); err != nil {
		t.Fatal(err)
	}
	if len(catalog.items) != 1 || catalog.items[0].Name != "Project label" {
		t.Fatalf("saved Codex workspaces = %+v", catalog.items)
	}
}
