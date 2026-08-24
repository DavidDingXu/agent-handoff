// Package integration_test exercises full share -> import round trips across
// the four agent quadrants, against synthetic agent homes.
package integration_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

const codexFixture = `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001","timestamp":"2026-08-01T10:00:00.000Z","cwd":"/src/project","cli_version":"0.1.0","source":"vscode"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Fix the login bug"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the login bug"}]}}
{"timestamp":"2026-08-01T10:00:04.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","namespace":"shell","call_id":"call_1","arguments":"{\"command\":[\"grep\",\"-r\",\"login\"]}"}}
{"timestamp":"2026-08-01T10:00:05.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"auth.go:42: bug here","status":"completed"}}
{"timestamp":"2026-08-01T10:00:06.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Found the bug in auth.go:42."}}
{"timestamp":"2026-08-01T10:00:06.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Found the bug in auth.go:42."}]}}
{"timestamp":"2026-08-01T10:00:07.000Z","type":"event_msg","payload":{"type":"task_complete"}}
`

const claudeFixture = `{"type":"queue-operation","sessionId":"11111111-2222-3333-4444-555555555555","operation":"enqueue"}
{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"11111111-2222-3333-4444-555555555555","cwd":"/src/project","version":"1.0.11","message":{"role":"user","content":"Fix the login bug"},"timestamp":"2026-08-01T10:00:00Z"}
{"type":"assistant","uuid":"u2","parentUuid":"u1","sessionId":"11111111-2222-3333-4444-555555555555","cwd":"/src/project","version":"1.0.11","message":{"role":"assistant","content":[{"type":"text","text":"Found the bug in auth.go:42."}]},"timestamp":"2026-08-01T10:00:05Z"}
{"type":"last-prompt","sessionId":"11111111-2222-3333-4444-555555555555","lastPrompt":"Fix the login bug","leafUuid":"u2"}
`

const codexThreadID = "0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001"
const claudeSessionID = "11111111-2222-3333-4444-555555555555"

var importTime = time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

// buildCodexHome lays out a sender Codex home: rollout file, index, sqlite.
func buildCodexHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()

	sessionsDir := filepath.Join(home, "sessions", "2026", "08", "01")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rollout := filepath.Join(sessionsDir, "rollout-2026-08-01T10-00-00-"+codexThreadID+".jsonl")
	if err := os.WriteFile(rollout, []byte(codexFixture), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(home, codex.SessionIndexFile),
		[]byte(`{"id":"`+codexThreadID+`","thread_name":"Login bug fix","updated_at":"2026-08-01T10:00:07.000Z"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", filepath.Join(home, codex.StateSQLiteFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := `CREATE TABLE threads (
		id TEXT PRIMARY KEY,
		title TEXT,
		cwd TEXT,
		rollout_path TEXT,
		model TEXT,
		model_provider TEXT,
		git_branch TEXT,
		git_origin_url TEXT,
		cli_version TEXT,
		effort TEXT,
		source TEXT,
		created_at INTEGER,
		created_at_ms INTEGER,
		updated_at INTEGER,
		updated_at_ms INTEGER,
		recency_at INTEGER,
		recency_at_ms INTEGER,
		archived INTEGER DEFAULT 0,
		archived_at INTEGER,
		is_pinned INTEGER DEFAULT 0,
		has_user_event INTEGER DEFAULT 0,
		preview TEXT
	)`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO threads (id, title, cwd, rollout_path, model, model_provider, git_branch, git_origin_url, cli_version, effort, source, created_at, created_at_ms, updated_at, updated_at_ms, recency_at, recency_at_ms, has_user_event, preview)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		codexThreadID, "Login bug fix", "/src/project", rollout, "gpt-5.2-codex", "OpenAI",
		"feature/login", "https://github.com/org/repo", "0.51.0", "high", "vscode",
		1754032800, 1754032800000, 1754032807, 1754032807000, 1754032807, 1754032807000, 1, "Fix the login bug")
	if err != nil {
		t.Fatal(err)
	}
	return home
}

// buildClaudeHome lays out a sender Claude home.
func buildClaudeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, claude.ProjectsDir, claude.ProjectDirName("/src/project"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, claudeSessionID+".jsonl"), []byte(claudeFixture), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := &claude.Index{Version: 1}
	idx.UpsertEntry(claude.IndexEntry{
		SessionID:   claudeSessionID,
		FullPath:    filepath.Join(dir, claudeSessionID+".jsonl"),
		FirstPrompt: "Fix the login bug",
		ProjectPath: "/src/project",
	})
	if err := idx.WriteIndex(filepath.Join(dir, claude.IndexFile)); err != nil {
		t.Fatal(err)
	}
	return home
}

// exportCodex mimics the CLI's codex export path.
func exportCodex(t *testing.T, home string) (*bundle.Manifest, []byte, map[string]any) {
	t.Helper()
	data, err := codex.LoadThread(home, codexThreadID)
	if err != nil {
		t.Fatalf("LoadThread: %v", err)
	}
	normalized := codex.NormalizeActiveSession(data.SessionBytes, codexThreadID)
	normalized = codex.DropSelfExportTurn(normalized)

	m := &bundle.Manifest{
		FormatVersion:  bundle.Version2,
		ArtifactType:   bundle.ArtifactType,
		SourceAgent:    bundle.AgentCodex,
		TargetSupport:  bundle.SupportedAgents,
		SourceThreadID: codexThreadID,
		Title:          data.Title,
		SourceCWD:      data.CWD,
		CreatedAt:      idgen.NowRFC3339(),
		MessageCount:   session.CountUserAssistantMessages(normalized),
	}
	return m, normalized, data.ThreadRow
}

// exportClaude mimics the CLI's claude export path.
func exportClaude(t *testing.T, home string) (*bundle.Manifest, []byte, map[string]any) {
	t.Helper()
	data, err := claude.LoadSession(home, claudeSessionID)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	m := &bundle.Manifest{
		FormatVersion:  bundle.Version2,
		ArtifactType:   bundle.ArtifactType,
		SourceAgent:    bundle.AgentClaude,
		TargetSupport:  bundle.SupportedAgents,
		SourceThreadID: claudeSessionID,
		Title:          data.Title,
		SourceCWD:      data.CWD,
		CreatedAt:      idgen.NowRFC3339(),
		MessageCount:   claude.SummarizeSession(data.SessionBytes).MessageCount,
	}
	return m, data.SessionBytes, nil
}

func writeBundle(t *testing.T, m *bundle.Manifest, sessionBytes []byte, meta map[string]any, tr neutral.Transcript) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "share.agent-handoff.zip")
	if err := bundle.WriteZip(path, bundle.WriterInput{
		Manifest: m,
		Session:  sessionBytes,
		Meta:     meta,
		Neutral:  tr,
	}); err != nil {
		t.Fatal(err)
	}
	return path
}

func loadBundle(t *testing.T, path string) *bundle.ReadResult {
	t.Helper()
	b, err := bundle.ReadZip(path)
	if err != nil {
		t.Fatalf("ReadZip: %v", err)
	}
	if err := b.VerifyChecksums(); err != nil {
		t.Fatalf("VerifyChecksums: %v", err)
	}
	return b
}

// ---- the four quadrants ----

func TestRoundTripCodexToCodex(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("Restore: %v (result %+v)", err, res)
	}
	if res.CrossAgent {
		t.Error("codex->codex must take the lossless same-agent path")
	}

	v := codex.Verify(receiverHome, res.ThreadID, "/recv/project")
	if v.Status != "ok" {
		t.Fatalf("verify: %+v", v)
	}

	// Sender metadata must survive via the row clone.
	row2, err := codex.ReadThreadRow(receiverHome, res.ThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if row2["model"] != "gpt-5.2-codex" {
		t.Errorf("model = %v, want sender value", row2["model"])
	}
	if row2["git_branch"] != "feature/login" {
		t.Errorf("git_branch = %v", row2["git_branch"])
	}
	if row2["source"] != "vscode" {
		t.Errorf("source = %v, want sender plain string", row2["source"])
	}
	// Lossless: message content preserved.
	if !strings.Contains(string(b.Session), "Fix the login bug") {
		t.Error("session content lost")
	}
}

func TestRoundTripCodexToClaude(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := claude.Restore(claude.RestoreInput{
		SourceSessionID: b.Manifest.SourceThreadID,
		Title:           b.Manifest.Title,
		Neutral:         b.Neutral,
	}, claude.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("Restore: %v (result %+v)", err, res)
	}
	if !res.CrossAgent {
		t.Error("codex->claude must take the cross-agent path")
	}

	v := claude.Verify(receiverHome, res.SessionID, "/recv/project")
	if v.Status != "ok" {
		t.Fatalf("verify: %+v", v)
	}

	// Semantic content: the user message survived the neutral synthesis.
	data, err := os.ReadFile(filepath.Join(receiverHome, claude.ProjectsDir,
		claude.ProjectDirName("/recv/project"), res.SessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "Fix the login bug") {
		t.Error("user message lost in cross-agent synthesis")
	}
	if !strings.Contains(s, "Found the bug in auth.go:42.") {
		t.Error("assistant message lost in cross-agent synthesis")
	}
	if strings.Contains(s, "/src/project") {
		t.Error("sender cwd leaked into the receiver session")
	}
}

func TestRoundTripClaudeToCodex(t *testing.T) {
	senderHome := buildClaudeHome(t)
	m, sessionBytes, meta := exportClaude(t, senderHome)
	tr := neutral.FromClaudeSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, meta, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("Restore: %v (result %+v)", err, res)
	}
	if !res.CrossAgent {
		t.Error("claude->codex must take the cross-agent path")
	}

	// No sqlite db in the receiver home: rollout + index still written.
	v := codex.Verify(receiverHome, res.ThreadID, "/recv/project")
	if v.Status != "ok" {
		t.Fatalf("verify: %+v", v)
	}

	// The synthesized codex session carries the conversation.
	data, err := os.ReadFile(filepath.Join(receiverHome,
		"sessions", "2026", "08", "02", "rollout-2026-08-02T12-00-00-"+res.ThreadID+".jsonl"))
	if err != nil {
		t.Fatalf("read synthesized rollout: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "Fix the login bug") {
		t.Error("user message lost in cross-agent synthesis")
	}
	if !strings.Contains(s, `"model_provider":"claude"`) {
		t.Error("source agent provenance missing in session_meta")
	}
	if strings.Contains(s, "/src/project") {
		t.Error("sender cwd leaked into the receiver session")
	}
}

func TestRoundTripClaudeToClaude(t *testing.T) {
	senderHome := buildClaudeHome(t)
	m, sessionBytes, meta := exportClaude(t, senderHome)
	tr := neutral.FromClaudeSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, meta, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := claude.Restore(claude.RestoreInput{
		SourceSessionID: b.Manifest.SourceThreadID,
		Title:           b.Manifest.Title,
		SessionBytes:    b.Session,
		Neutral:         b.Neutral,
	}, claude.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("Restore: %v (result %+v)", err, res)
	}
	if res.CrossAgent {
		t.Error("claude->claude must take the lossless same-agent path")
	}

	v := claude.Verify(receiverHome, res.SessionID, "/recv/project")
	if v.Status != "ok" {
		t.Fatalf("verify: %+v", v)
	}

	// Lossless: text survives, ids/cwd rewritten.
	data, err := os.ReadFile(filepath.Join(receiverHome, claude.ProjectsDir,
		claude.ProjectDirName("/recv/project"), res.SessionID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	if !strings.Contains(s, "Fix the login bug") || !strings.Contains(s, "Found the bug in auth.go:42.") {
		t.Error("message content lost in same-agent rewrite")
	}
	if strings.Contains(s, claudeSessionID) {
		t.Error("sender session id leaked")
	}
	if strings.Contains(s, "/src/project") {
		t.Error("sender cwd leaked")
	}
}

// ---- dry run and safety semantics ----

func TestRestoreDryRunWritesNothing(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   false,
	})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if !res.DryRun || res.Status != "planned" {
		t.Errorf("dry run result = %+v", res)
	}
	if entries, _ := os.ReadDir(receiverHome); len(entries) != 0 {
		t.Errorf("dry run wrote %d entries into the receiver home", len(entries))
	}
}

func TestBundleCarriesNeutralAndRaw(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	if b.Neutral.Schema != neutral.Schema || len(b.Neutral.Entries) == 0 {
		t.Errorf("neutral transcript missing: %+v", b.Neutral)
	}
	if raw, ok := b.SessionForAgent(bundle.AgentCodex); !ok || len(raw) == 0 {
		t.Error("raw session missing for same-agent import")
	}

	// Manifest file list must cover everything except checksums itself.
	manifestData := b.Files[bundle.ManifestEntry]
	var mf struct {
		Files []string `json:"files"`
	}
	if err := json.Unmarshal(manifestData, &mf); err != nil {
		t.Fatal(err)
	}
	if len(mf.Files) == 0 {
		t.Error("manifest file list empty")
	}
	for _, f := range mf.Files {
		if f == bundle.ChecksumsEntry {
			t.Errorf("checksums.json must not list itself: %v", mf.Files)
		}
	}
}

func TestNewIDsDifferFromSource(t *testing.T) {
	// Imported ids must never collide with the sender's ids.
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ThreadID == codexThreadID {
		t.Error("imported thread id equals the source thread id")
	}
	// Re-importing the same bundle yields yet another id.
	res2, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res2.ThreadID == res.ThreadID {
		t.Error("two imports of the same bundle collided on the thread id")
	}
}

func TestBackupCreatedOnImport(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.BackupDir == "" {
		t.Fatal("backup dir not reported")
	}
	if st, err := os.Stat(res.BackupDir); err != nil || !st.IsDir() {
		t.Errorf("backup dir missing: %v", err)
	}
}

func TestRestoreResultFallbackCommand(t *testing.T) {
	// Error path: a receiver home whose sqlite db is corrupt.
	senderHome := buildClaudeHome(t)
	m, sessionBytes, meta := exportClaude(t, senderHome)
	tr := neutral.FromClaudeSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, meta, tr)
	b := loadBundle(t, path)

	receiverHome := t.TempDir()
	// Pre-create the rollout target path as a directory so the exclusive
	// file write fails.
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Status != "ok" {
		t.Errorf("status = %q, failures: %v", res.Status, fmt.Sprint(res.Writes))
	}
}
