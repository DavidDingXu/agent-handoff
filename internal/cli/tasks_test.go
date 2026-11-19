package cli

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Losing the source-home or cwd scope would expose unrelated task candidates.
func TestTasksCatalogUsesExplicitHomeAndExactCWD(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(t.TempDir(), "project")
	writeSwitchCodexTask(t, home, "source-one", "First task", cwd, 10)
	writeSwitchCodexTask(t, home, "source-two", "Second task", cwd, 20)
	writeSwitchCodexTask(t, home, "other-project", "Other task", cwd+"-other", 30)
	before, err := os.ReadFile(filepath.Join(home, "session_index.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	result, err := captureLocalCommand(t, []string{"tasks", "--source", "codex", "--home", home, "--cwd", cwd})
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	tasks, _ := result["tasks"].([]any)
	if result["status"] != "ok" || result["source_agent"] != "codex" || len(tasks) != 2 {
		t.Fatalf("catalog = %#v", result)
	}
	first, _ := tasks[0].(map[string]any)
	second, _ := tasks[1].(map[string]any)
	if first["id"] != "source-two" || first["title"] != "Second task" || first["cwd"] != cwd || first["updated_at"] != "1970-01-01T00:00:20Z" || second["id"] != "source-one" {
		t.Fatalf("tasks = %#v", tasks)
	}
	after, err := os.ReadFile(filepath.Join(home, "session_index.jsonl"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("catalog modified source index: %v", err)
	}
}

// Recent Claude sessions may have no legacy sessions-index.json entry.
func TestTasksCatalogFindsClaudeSessionsWithoutIndex(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	writeSwitchClaudeTask(t, home, "claude-source", "Fix the widget", cwd)
	result, err := captureLocalCommand(t, []string{"tasks", "--source", "claude", "--home", home, "--cwd", cwd})
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	tasks, _ := result["tasks"].([]any)
	if len(tasks) != 1 {
		t.Fatalf("catalog = %#v", result)
	}
	first, _ := tasks[0].(map[string]any)
	if first["id"] != "claude-source" || first["title"] != "Fix the widget" || first["cwd"] != cwd || first["updated_at"] != "2026-11-19T10:00:01Z" {
		t.Fatalf("task = %#v", first)
	}
}

// A Codex CLI home may contain an index and rollout without desktop SQLite.
func TestTasksCatalogFindsCodexWorkspaceWithoutDatabase(t *testing.T) {
	home, cwd := t.TempDir(), t.TempDir()
	writeSwitchCodexTask(t, home, "cli-only", "CLI task", cwd, 20)
	if err := os.Remove(filepath.Join(home, "state_5.sqlite")); err != nil {
		t.Fatal(err)
	}
	result, err := captureLocalCommand(t, []string{"tasks", "--source", "codex", "--home", home, "--cwd", cwd})
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	tasks, _ := result["tasks"].([]any)
	if len(tasks) != 1 || tasks[0].(map[string]any)["id"] != "cli-only" {
		t.Fatalf("catalog = %#v", result)
	}
}

func captureLocalCommand(t *testing.T, args []string) (map[string]any, error) {
	t.Helper()
	return captureLocalOperation(t, func() error { return Run(args) })
}

func captureLocalOperation(t *testing.T, operation func() error) (map[string]any, error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = out
	defer func() { os.Stdout = original; _ = out.Close() }()
	commandErr := operation()
	data, err := os.ReadFile(out.Name())
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if len(data) > 0 {
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatalf("invalid command JSON %q: %v", data, err)
		}
	}
	return result, commandErr
}

func writeSwitchCodexTask(t *testing.T, home, id, title, cwd string, updated int64) string {
	t.Helper()
	sessionPath := filepath.Join(home, "sessions", "2026", "11", "19", "rollout-"+id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o700); err != nil {
		t.Fatal(err)
	}
	meta, _ := json.Marshal(map[string]any{"type": "session_meta", "timestamp": "2026-11-19T10:00:00Z", "payload": map[string]any{"id": id, "cwd": cwd}})
	user := []byte(`{"type":"response_item","timestamp":"2026-11-19T10:00:01Z","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the synthetic bug"}]}}`)
	data := append(append(meta, '\n'), append(user, '\n')...)
	if err := os.WriteFile(sessionPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	index, _ := json.Marshal(map[string]any{"id": id, "thread_name": title})
	indexFile, err := os.OpenFile(filepath.Join(home, "session_index.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := indexFile.Write(append(index, '\n')); err != nil {
		t.Fatal(err)
	}
	_ = indexFile.Close()
	db, err := sql.Open("sqlite", filepath.Join(home, "state_5.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS threads (id TEXT PRIMARY KEY, title TEXT, cwd TEXT, rollout_path TEXT, updated_at INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads (id, title, cwd, rollout_path, updated_at) VALUES (?, ?, ?, ?, ?)`, id, title, cwd, sessionPath, updated); err != nil {
		t.Fatal(err)
	}
	return sessionPath
}

func writeSwitchClaudeTask(t *testing.T, home, id, title, cwd string) string {
	t.Helper()
	path := filepath.Join(home, "projects", "synthetic-project", id+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(map[string]any{
		"type": "user", "uuid": "user-one", "sessionId": id, "cwd": cwd, "timestamp": "2026-11-19T10:00:01Z",
		"message": map[string]any{"role": "user", "content": title},
	})
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
