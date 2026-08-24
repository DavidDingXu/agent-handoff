// Package claude reads and writes Claude Code session state (~/.claude):
// projects/<dir-slug>/<session>.jsonl transcripts and sessions-index.json.
package claude

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ProjectsDir is the session tree inside a Claude home.
const ProjectsDir = "projects"

// IndexFile is the per-project session index file name.
const IndexFile = "sessions-index.json"

// DefaultHome returns ~/.claude.
func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// ResolveHome resolves the Claude home: explicit flag > CLAUDE_CONFIG_DIR >
// ~/.claude.
func ResolveHome(flagHome string) (string, error) {
	if flagHome != "" {
		return filepath.Abs(flagHome)
	}
	if env := os.Getenv("CLAUDE_CONFIG_DIR"); env != "" {
		return filepath.Abs(env)
	}
	return DefaultHome()
}

// ProjectDirName converts a cwd into Claude's project directory name
// (every path separator becomes '-': /Users/foo/bar -> -Users-foo-bar).
func ProjectDirName(cwd string) string {
	return strings.ReplaceAll(filepath.Clean(cwd), string(filepath.Separator), "-")
}

// ResolveSession maps "current" (or empty) to a real session id. Priority:
// CLAUDE_SESSION_ID/CLAUDE_CODE_SESSION_ID env, the newest session in the
// current cwd's project index, then the newest session anywhere.
func ResolveSession(home, requested string) (string, error) {
	req := strings.TrimSpace(requested)
	if req != "" && req != "current" {
		return req, nil
	}
	for _, key := range []string{"CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v, nil
		}
	}
	if wd, _ := os.Getwd(); wd != "" {
		if id, err := newestIndexSession(home, ProjectDirName(wd)); err == nil && id != "" {
			return id, nil
		}
	}
	if id, err := newestSessionAnywhere(home); err == nil && id != "" {
		return id, nil
	}
	return "", errors.New("no Claude sessions found")
}

func newestIndexSession(home, projectDir string) (string, error) {
	idx, err := ReadIndex(filepath.Join(home, ProjectsDir, projectDir, IndexFile))
	if err != nil || len(idx.Entries) == 0 {
		return "", os.ErrNotExist
	}
	best := ""
	var bestAt int64
	for _, e := range idx.Entries {
		if e.SessionID == "" || e.IsSidechain {
			continue
		}
		at := parseTime(e.Modified)
		if at == 0 {
			at = e.FileMtime
		}
		if best == "" || at > bestAt {
			best, bestAt = e.SessionID, at
		}
	}
	return best, nil
}

func newestSessionAnywhere(home string) (string, error) {
	root := filepath.Join(home, ProjectsDir)
	type candidate struct {
		id string
		at time.Time
	}
	var found []candidate
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		found = append(found, candidate{
			id: strings.TrimSuffix(d.Name(), ".jsonl"),
			at: info.ModTime(),
		})
		return nil
	})
	if len(found) == 0 {
		return "", os.ErrNotExist
	}
	sort.Slice(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	return found[0].id, nil
}

// ---- index file ----

// IndexEntry is one entry in sessions-index.json.
type IndexEntry struct {
	SessionID    string `json:"sessionId"`
	FullPath     string `json:"fullPath"`
	FileMtime    int64  `json:"fileMtime"`
	FirstPrompt  string `json:"firstPrompt"`
	MessageCount int    `json:"messageCount"`
	Created      string `json:"created"`
	Modified     string `json:"modified"`
	GitBranch    string `json:"gitBranch"`
	ProjectPath  string `json:"projectPath"`
	IsSidechain  bool   `json:"isSidechain"`
}

// Index is the sessions-index.json document.
type Index struct {
	Version      int          `json:"version"`
	Entries      []IndexEntry `json:"entries"`
	OriginalPath string       `json:"originalPath,omitempty"`
}

// ReadIndex reads and parses a sessions-index.json.
func ReadIndex(path string) (*Index, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return &Index{Version: 1}, nil
	}
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	if idx.Version == 0 {
		idx.Version = 1
	}
	return &idx, nil
}

// UpsertEntry replaces or prepends an entry keyed by sessionId.
func (idx *Index) UpsertEntry(e IndexEntry) {
	for i := range idx.Entries {
		if idx.Entries[i].SessionID == e.SessionID {
			idx.Entries[i] = e
			return
		}
	}
	idx.Entries = append([]IndexEntry{e}, idx.Entries...)
}

// WriteIndex writes the index back to disk.
func (idx *Index) WriteIndex(path string) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func parseTime(s string) int64 {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return 0
	}
	return t.UnixMilli()
}

func fmtTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

// ---- session discovery ----

// findSessionPath walks the projects tree for <sessionID>.jsonl.
func findSessionPath(home, sessionID string) (string, error) {
	var found string
	root := filepath.Join(home, ProjectsDir)
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() || d.Name() != sessionID+".jsonl" {
			return nil
		}
		found = path
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("session file for %s not found", sessionID)
	}
	return found, nil
}

// findIndexEntry scans every project index for a session id.
func findIndexEntry(home, sessionID string) (*IndexEntry, string, error) {
	root := filepath.Join(home, ProjectsDir)
	var entry *IndexEntry
	var idxPath string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != IndexFile || entry != nil {
			return nil
		}
		idx, err := ReadIndex(path)
		if err != nil {
			return nil
		}
		for i := range idx.Entries {
			if idx.Entries[i].SessionID == sessionID {
				entry = &idx.Entries[i]
				idxPath = path
				return nil
			}
		}
		return nil
	})
	if entry == nil {
		return nil, "", os.ErrNotExist
	}
	return entry, idxPath, nil
}
