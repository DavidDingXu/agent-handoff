package qoder

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	_ "modernc.org/sqlite"
)

const readFixtureID = "86739315-cb6c-4e78-824c-2f19ae099ec2"

func TestReadTaskCatalog(t *testing.T) {
	path := readFixtureCatalog(t)
	entries, err := List(path, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].ID != "451246e8-4fb9-4005-958b-ec24f7c0b620" || entries[1].ID != readFixtureID {
		t.Fatalf("unexpected catalog order or scope: %+v", entries)
	}
	matches, err := List(path, "SOURCE")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].Title != "Source task" || matches[0].CWD != "/fixture" || matches[0].UpdatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("unexpected catalog filter: %+v", matches)
	}
}

func TestLoadKeepsVisibleActiveQoderHistory(t *testing.T) {
	path := readFixtureCatalog(t)
	home := t.TempDir()
	project := filepath.Join(home, "projects", "encoded-path")
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	history := strings.ReplaceAll(`{"type":"runtime-config","sessionId":"SESSION","timestamp":1767225600000,"permissionMode":"bypassPermissions","settings":"PRIVATE"}
{"type":"user","uuid":"1","parentUuid":null,"timestamp":"2026-01-01T00:00:00Z","sessionId":"SESSION","permissionMode":"PRIVATE","message":{"role":"user","content":[{"type":"text","text":"<in-app-browser-context active=\"yes\">PRIVATE BROWSER</in-app-browser-context>\nFind the <in-app-browser-context>PRIVATE PAGE</in-app-browser-context>fixture"},{"type":"text","text":"<external_browser_tabs>PRIVATE TABS</external_browser_tabs>"}]}}
{"type":"assistant","uuid":"discarded","parentUuid":"1","sessionId":"SESSION","message":{"role":"assistant","content":[{"type":"text","text":"OLD BRANCH"}]}}
{"type":"system","uuid":"2","parentUuid":"1","sessionId":"SESSION","content":"PRIVATE SYSTEM"}
{"type":"assistant","uuid":"3","parentUuid":"2","sessionId":"SESSION","message":{"role":"assistant","usage":{"other":"PRIVATE"},"content":[{"type":"thinking","thinking":"PRIVATE THEORY"},{"type":"tool_use","id":"call","name":"Read","input":{"path":"fixture.txt"},"authority":"PRIVATE"}]}}
{"type":"user","uuid":"4","parentUuid":"3","sessionId":"SESSION","toolUseResult":{"environment":"PRIVATE"},"message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"orphan","content":"PRIVATE ORPHAN"},{"type":"tool_result","tool_use_id":"call","is_error":true,"content":[{"type":"text","text":"read failed","authority":"PRIVATE"}]}]}}
{"type":"user","uuid":"5","parentUuid":"4","sessionId":"SESSION","isMeta":true,"message":{"role":"user","content":"PRIVATE META"}}
{"type":"assistant","uuid":"6","parentUuid":"5","sessionId":"SESSION","message":{"role":"assistant","content":[{"type":"text","text":"The fixture was unavailable."}]}}
{"type":"active-leaf","leafUuid":"6","sessionId":"SESSION","timestamp":1767225600000,"explicit":false}
{"type":"assistant","uuid":"side","parentUuid":"6","sessionId":"SESSION","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"PRIVATE SIDECAR"}]}}
`, "SESSION", readFixtureID)
	if err := os.WriteFile(filepath.Join(project, readFixtureID+".jsonl"), []byte(history), 0600); err != nil {
		t.Fatal(err)
	}
	task, data, transcript, err := Load(path, home, readFixtureID)
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != readFixtureID || transcript.Title != "Source task" || transcript.SourceAgent != "qoder" || transcript.CreatedAt != "2025-12-31T00:00:00Z" {
		t.Fatalf("wrong task metadata: %+v %+v", task, transcript)
	}
	for _, unsafe := range []string{"PRIVATE", "OLD BRANCH", "active-leaf", "\"parentUuid\":\"2\"", "\"parentUuid\":\"5\""} {
		if strings.Contains(string(data), unsafe) {
			t.Errorf("unexpected %q in passive history: %s", unsafe, data)
		}
	}
	entries := transcript.Entries
	if len(entries) != 3 {
		t.Fatalf("unexpected transcript: %+v", transcript)
	}
	if entries[0].Role != "user" || entries[0].Text != "Find the fixture" ||
		entries[1].Kind != neutral.KindTool || entries[1].Tool != "Read" || entries[1].Input != `{"path":"fixture.txt"}` || entries[1].Output != "read failed" || entries[1].Status != "failed" ||
		entries[2].Role != "assistant" || entries[2].Text != "The fixture was unavailable." {
		t.Fatalf("unexpected entries: %+v", entries)
	}
	roundTrip, err := ToNeutral(task, data)
	if err != nil || len(roundTrip.Entries) != len(entries) {
		t.Fatalf("could not interpret passive history: %+v, %v", roundTrip, err)
	}
}

func TestLoadRejectsMismatchedOrAmbiguousNativeHistory(t *testing.T) {
	path := readFixtureCatalog(t)
	home := t.TempDir()
	first := filepath.Join(home, "projects", "one")
	second := filepath.Join(home, "projects", "two")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	foreignHistory := []byte(`{"type":"user","uuid":"m1","sessionId":"foreign-session","message":{"role":"user","content":"Unrelated data"}}`)
	if err := os.WriteFile(filepath.Join(first, readFixtureID+".jsonl"), foreignHistory, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(path, home, readFixtureID); err == nil {
		t.Fatal("history with a foreign session ID was accepted")
	}
	if err := os.WriteFile(filepath.Join(second, readFixtureID+".jsonl"), foreignHistory, 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := Load(path, home, readFixtureID); err == nil || !strings.Contains(err.Error(), "multiple") {
		t.Fatalf("ambiguous history was accepted: %v", err)
	}
}

func readFixtureCatalog(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Qoder # 测试.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE chat_sessions (
		session_id TEXT PRIMARY KEY, title TEXT, cwd TEXT, created_at TIMESTAMP, updated_at TIMESTAMP,
		archived INTEGER, deleted_at TIMESTAMP, session_kind TEXT
	);
	INSERT INTO chat_sessions VALUES
		('86739315-cb6c-4e78-824c-2f19ae099ec2', 'Source task', '/fixture', 1767139200000, 1767225600000, 0, NULL, 'standard'),
		('451246e8-4fb9-4005-958b-ec24f7c0b620', 'Archived', '/archive', 1767225600000, 1767312000000, 1, NULL, 'standard'),
		('0632823a-87ab-426f-8e1f-d9f21cb79a30', 'Side chat', '/fixture', 1767225600000, 1767312000000, 0, NULL, 'sideChat'),
		('0e8cbbeb-4121-4c7b-8ba1-830a42acf4cc', 'Deleted', '/fixture', 1767225600000, 1767312000000, 0, 1767312000000, 'standard');`)
	if err != nil {
		t.Fatal(err)
	}
	return path
}
