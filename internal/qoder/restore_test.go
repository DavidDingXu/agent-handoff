package qoder

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const restoreTestSchema = `
CREATE TABLE workspaces (
  workspace_id TEXT PRIMARY KEY, name TEXT NOT NULL, workspace_source TEXT NOT NULL,
  root_paths_json TEXT NOT NULL, product_mode TEXT NOT NULL, extra_json TEXT NOT NULL DEFAULT '{}',
  created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL, archived INTEGER NOT NULL DEFAULT 0,
  deleted_at INTEGER
);
CREATE TABLE chat_sessions (
  session_id TEXT PRIMARY KEY, session_kind TEXT NOT NULL, conversation_mode TEXT NOT NULL,
  product_mode TEXT NOT NULL, title TEXT NOT NULL, cwd TEXT NOT NULL, execution_kind TEXT,
  workspace_id TEXT, model TEXT, permission_mode TEXT, additional_direcotries TEXT NOT NULL DEFAULT '{}',
  extra_json TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  execution_target_json TEXT NOT NULL, message_envelope_version INTEGER, archived INTEGER NOT NULL DEFAULT 0, deleted_at INTEGER
);
CREATE TABLE chat_session_messages (
  session_id TEXT NOT NULL REFERENCES chat_sessions(session_id), message_id TEXT NOT NULL,
  turn_id TEXT, sequence INTEGER NOT NULL, payload_json TEXT NOT NULL, status TEXT NOT NULL,
  source TEXT NOT NULL, created_at INTEGER NOT NULL, updated_at INTEGER NOT NULL,
  role TEXT, turn_started_at INTEGER, is_compaction_boundary INTEGER, has_visible_content INTEGER,
  payload_bytes INTEGER, payload_hash TEXT, payload_schema_version INTEGER,
  PRIMARY KEY(session_id,message_id), UNIQUE(session_id,sequence)
);
`

func restoreFixture(t *testing.T) (*sql.DB, string, RestoreOptions) {
	t.Helper()
	base := t.TempDir()
	opts := RestoreOptions{DatabasePath: filepath.Join(base, "main.sqlite"),
		RuntimeHome: filepath.Join(base, "runtime"), Fingerprint: "test-share"}
	db, err := sql.Open("sqlite", opts.DatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(restoreTestSchema); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(base, "target's 指定目录")
	if err := os.Mkdir(cwd, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		t.Fatal(err)
	}
	return db, cwd, opts
}

func restoreTestTranscript() neutral.Transcript {
	return neutral.Transcript{
		Schema: neutral.Schema, SourceAgent: "codex", SourceID: "source-history",
		Entries: []neutral.Entry{
			{Kind: neutral.KindMessage, Role: "user", Text: "Validate this synthetic task."},
			{Kind: neutral.KindTool, Tool: "shell", Status: "completed", Input: "echo test", Output: "test"},
			{Kind: neutral.KindMessage, Role: "assistant", Text: "Validation completed."},
		},
	}
}

func TestRestoreCatalogRuntimeAndDuplicate(t *testing.T) {
	db, cwd, opts := restoreFixture(t)
	transcript := restoreTestTranscript()
	res, err := Restore(transcript, "Synthetic title", cwd, opts)
	if err != nil {
		t.Fatal(err)
	}
	if res.Duplicate || res.SessionID == "" || res.SessionID == transcript.SourceID || res.WorkspaceID == "" {
		t.Fatalf("unexpected restore result %+v", res)
	}
	if res.Title != "[Handoff] Synthetic title" || res.CWD != cwd {
		t.Fatalf("incorrect identity %+v", res)
	}

	var sessions, messages, workspaces int
	var model, permission sql.NullString
	err = db.QueryRow(`SELECT
	 (SELECT count(*) FROM chat_sessions WHERE session_id=?),
	 (SELECT count(*) FROM chat_session_messages WHERE session_id=?),
	 (SELECT count(*) FROM workspaces),
	 (SELECT model FROM chat_sessions WHERE session_id=?),
	 (SELECT permission_mode FROM chat_sessions WHERE session_id=?)`,
		res.SessionID, res.SessionID, res.SessionID, res.SessionID).Scan(&sessions, &messages, &workspaces, &model, &permission)
	if err != nil || sessions != 1 || messages != 2 || workspaces != 1 || model.String != "auto" || permission.String != "default" {
		t.Fatalf("unexpected desktop state %d/%d/%d/%v/%v, err=%v", sessions, messages, workspaces, model, permission, err)
	}

	receipt, err := os.ReadFile(filepath.Join(res.BackupDir, "restore.json"))
	if err != nil {
		t.Fatal(err)
	}
	var recovery struct {
		SessionID        string `json:"session_id"`
		RuntimePath      string `json:"runtime_path"`
		WorkspaceCreated bool   `json:"workspace_created"`
	}
	if err := json.Unmarshal(receipt, &recovery); err != nil || recovery.SessionID != res.SessionID ||
		recovery.RuntimePath != res.RuntimePath || !recovery.WorkspaceCreated {
		t.Fatalf("unexpected recovery information %+v, err=%v", recovery, err)
	}
	content, err := os.ReadFile(res.RuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{res.SessionID, "Validate this synthetic task.", "echo test", "Validation completed.", "active-leaf"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("runtime history omitted %q", want)
		}
	}
	for _, invalid := range []string{transcript.SourceID, "tool_use", "permissionMode", "runtime-config"} {
		if strings.Contains(string(content), invalid) {
			t.Fatalf("unexpected runtime state %q", invalid)
		}
	}

	duplicate, err := Restore(transcript, "Other title", cwd, opts)
	if err != nil || !duplicate.Duplicate || duplicate.SessionID != res.SessionID || duplicate.RuntimePath != res.RuntimePath || duplicate.BackupDir != "" {
		t.Fatalf("unexpected duplicate result %+v, err=%v", duplicate, err)
	}
	opts.AllowDuplicate = true
	copyResult, err := Restore(transcript, "Other title", cwd, opts)
	if err != nil || copyResult.Duplicate || copyResult.SessionID == res.SessionID || copyResult.WorkspaceID != res.WorkspaceID {
		t.Fatalf("unexpected forced restore result %+v, err=%v", copyResult, err)
	}
}

func TestRestoreRollsBackOnDesktopFailure(t *testing.T) {
	db, cwd, opts := restoreFixture(t)
	if _, err := db.Exec(`CREATE TRIGGER fail_message BEFORE INSERT ON chat_session_messages
	  BEGIN SELECT RAISE(ABORT, 'synthetic failure'); END;`); err != nil {
		t.Fatal(err)
	}
	_, err := Restore(restoreTestTranscript(), "Title", cwd, opts)
	if err == nil || !strings.Contains(err.Error(), "synthetic failure") {
		t.Fatalf("expected the synthetic failure, got %v", err)
	}
	var rows int
	if err := db.QueryRow("SELECT (SELECT count(*) FROM chat_sessions)+(SELECT count(*) FROM workspaces)").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("failed restore exposed %d rows, err=%v", rows, err)
	}
	files, err := filepath.Glob(filepath.Join(opts.RuntimeHome, "projects", "*", "*.jsonl"))
	if err != nil || len(files) != 0 {
		t.Fatalf("failed restore left runtime history: %v, err=%v", files, err)
	}
}

func TestRestoreConcurrentDuplicate(t *testing.T) {
	_, cwd, opts := restoreFixture(t)
	var wg sync.WaitGroup
	var results [2]RestoreResult
	var errors [2]error
	for i := range results {
		wg.Go(func() { results[i], errors[i] = Restore(restoreTestTranscript(), "same share", cwd, opts) })
	}
	wg.Wait()
	if errors[0] != nil || errors[1] != nil || results[0].SessionID != results[1].SessionID || results[0].Duplicate == results[1].Duplicate {
		t.Fatalf("concurrent restore results %+v, errors %v", results, errors)
	}
}

func TestRestoreNativeQoderConversation(t *testing.T) {
	db, cwd, opts := restoreFixture(t)
	transcript := neutral.Transcript{SourceAgent: "qoder", SourceID: "source-id"}
	opts.NativeSession = []byte(`{"type":"user","uuid":"source-user","parentUuid":null,"sessionId":"source-id","timestamp":"2026-04-17T12:01:04Z","message":{"role":"user","content":[{"type":"text","text":"First part."},{"type":"text","text":"Second part."}]},"permissionMode":"yolo"}
{"type":"assistant","uuid":"source-assistant","parentUuid":"source-user","sessionId":"source-id","message":{"role":"assistant","content":[{"type":"tool_use","id":"source-call","name":"Bash","input":{"command":"echo synthetic"}},{"type":"thinking","text":"omit all reasoning"}]}}
{"type":"user","uuid":"source-result","parentUuid":"source-assistant","sessionId":"source-id","message":{"role":"user","content":[{"type":"text","text":"Follow-up part one."},{"type":"tool_result","tool_use_id":"source-call","content":"synthetic output"},{"type":"text","text":"Follow-up part two."}]}}
{"type":"assistant","uuid":"source-final","parentUuid":"source-result","sessionId":"source-id","message":{"role":"assistant","content":[{"type":"text","text":"Finished."},{"type":"tool_use","id":"source-call-final","name":"Bash","input":{}}]}}
{"type":"user","uuid":"source-result-final","parentUuid":"source-final","sessionId":"source-id","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"source-call-final","content":"trailing output","is_error":true}]}}
`)
	res, err := Restore(transcript, "Native", cwd, opts)
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(res.RuntimePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"First part.", "Second part.", "2026-04-17T12:01:04.000Z",
		"[Historical tool call] Bash", "echo synthetic", "[Historical tool result] Bash", "synthetic output", "Finished.",
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("native history omitted %q", want)
		}
	}
	for _, bad := range []string{"source-id", "source-user", "source-call", "yolo", "omit all reasoning", "tool_use", "tool_result"} {
		if strings.Contains(string(content), bad) {
			t.Fatalf("native history carried source state %q", bad)
		}
	}
	_, _, reopened, err := Load(res.DatabasePath, opts.RuntimeHome, res.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	var userText []string
	var toolOutputs int
	for _, entry := range reopened.Entries {
		if entry.Role == "user" {
			userText = append(userText, entry.Text)
		}
		if strings.Contains(entry.Text, "Output: ") {
			toolOutputs++
			if entry.Role != "assistant" {
				t.Fatalf("historical tool output attributed to %s: %q", entry.Role, entry.Text)
			}
		}
	}
	if strings.Join(userText, " ") != "First part. Second part. Follow-up part one. Follow-up part two." || toolOutputs != 2 {
		t.Fatalf("unexpected reopened user messages %v and tool output count %d", userText, toolOutputs)
	}

	var firstTurn, mixedToolTurn, followUpTurn, trailingToolTurn string
	for _, item := range []struct {
		text string
		turn *string
	}{
		{"First part.", &firstTurn}, {"synthetic output", &mixedToolTurn},
		{"Follow-up part one.", &followUpTurn}, {"trailing output", &trailingToolTurn},
	} {
		if err := db.QueryRow(`SELECT turn_id FROM chat_session_messages
			WHERE session_id = ? AND instr(payload_json, ?) > 0`, res.SessionID, item.text).Scan(item.turn); err != nil {
			t.Fatal(err)
		}
	}
	if firstTurn != mixedToolTurn || followUpTurn != trailingToolTurn || firstTurn == followUpTurn {
		t.Fatalf("unexpected turn assignment %q, %q, %q, %q", firstTurn, mixedToolTurn, followUpTurn, trailingToolTurn)
	}
	if !strings.Contains(string(content), `"lastPrompt":"Follow-up part one.\nFollow-up part two."`) {
		t.Fatal("last-prompt did not preserve the genuine user text")
	}
}
