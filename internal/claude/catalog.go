package claude

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// CatalogEntry identifies a local session without choosing it for the caller.
type CatalogEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	UpdatedAt string `json:"updated_at"`
}

// ListTasks reads the project indexes and direct session files. Subagent
// transcripts in nested directories are not independent user tasks.
func ListTasks(home, cwd string) ([]CatalogEntry, error) {
	root := filepath.Join(home, ProjectsDir)
	projects, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return []CatalogEntry{}, nil
	}
	if err != nil {
		return nil, err
	}
	byID := map[string]CatalogEntry{}
	for _, project := range projects {
		if !project.IsDir() {
			continue
		}
		dir := filepath.Join(root, project.Name())
		idx, err := ReadIndex(filepath.Join(dir, IndexFile))
		if err != nil {
			return nil, err
		}
		sidechains := map[string]bool{}
		for _, e := range idx.Entries {
			if e.IsSidechain {
				sidechains[e.SessionID] = true
				continue
			}
			updated := e.Modified
			if updated == "" && e.FileMtime > 0 {
				updated = time.UnixMilli(e.FileMtime).UTC().Format(time.RFC3339Nano)
			}
			byID[e.SessionID] = CatalogEntry{ID: e.SessionID, Title: session.Truncate(e.FirstPrompt, 200), CWD: e.ProjectPath, UpdatedAt: updated}
		}
		files, err := os.ReadDir(dir)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".jsonl") {
				continue
			}
			id := strings.TrimSuffix(file.Name(), ".jsonl")
			if sidechains[id] {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				return nil, err
			}
			summary := SummarizeSession(data)
			entry := byID[id]
			entry.ID = id
			if summary.CWD != "" {
				entry.CWD = summary.CWD
			}
			if summary.Title != "" {
				entry.Title = summary.Title
			} else if entry.Title == "" {
				entry.Title = session.Truncate(summary.FirstUserText, 200)
			}
			if summary.ModifiedAt != "" {
				entry.UpdatedAt = summary.ModifiedAt
			}
			byID[id] = entry
		}
	}
	out := make([]CatalogEntry, 0, len(byID))
	for _, entry := range byID {
		if entry.ID == "" || (cwd != "" && filepath.Clean(entry.CWD) != filepath.Clean(cwd)) {
			continue
		}
		if entry.Title == "" {
			entry.Title = entry.ID
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		iTime, _ := time.Parse(time.RFC3339Nano, out[i].UpdatedAt)
		jTime, _ := time.Parse(time.RFC3339Nano, out[j].UpdatedAt)
		if iTime.Equal(jTime) {
			return out[i].ID < out[j].ID
		}
		return iTime.After(jTime)
	})
	return out, nil
}
