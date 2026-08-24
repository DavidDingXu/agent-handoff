package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const claudeSrc = `{"type":"queue-operation","sessionId":"old-id","operation":"enqueue"}
{"type":"user","uuid":"u1","parentUuid":null,"sessionId":"old-id","cwd":"/old/dir","version":"1.0.0","message":{"role":"user","content":"Fix the login bug"},"timestamp":"2026-08-01T10:00:00Z"}
{"type":"assistant","uuid":"u2","parentUuid":"u1","sessionId":"old-id","cwd":"/old/dir","version":"1.0.0","message":{"role":"assistant","content":[{"type":"text","text":"On it."}]},"timestamp":"2026-08-01T10:00:01Z"}
{"type":"user","uuid":"u3","parentUuid":"u2","sessionId":"old-id","cwd":"/old/dir","version":"1.0.0","message":{"role":"user","content":[{"type":"tool_result","content":"auth.go:42"}]},"timestamp":"2026-08-01T10:00:02Z"}
{"type":"last-prompt","sessionId":"old-id","lastPrompt":"Fix the login bug","leafUuid":"u3"}
`

func TestRewriteSession(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	out := RewriteSession([]byte(claudeSrc), "new-id", "/new/dir", now)
	s := string(out)

	if strings.Contains(s, "old-id") {
		t.Error("old session id leaked")
	}
	if !strings.Contains(s, `"sessionId":"new-id"`) {
		t.Error("new session id missing")
	}
	if strings.Contains(s, "/old/dir") {
		t.Error("old cwd leaked")
	}
	if !strings.Contains(s, "/new/dir") {
		t.Error("new cwd missing")
	}
	if !strings.Contains(s, `"version":"agent-handoff"`) {
		t.Error("version marker missing")
	}
	// Text content must survive.
	if !strings.Contains(s, "Fix the login bug") || !strings.Contains(s, "On it.") {
		t.Error("message content lost in rewrite")
	}

	// UUID chain: every old uuid replaced, parent links still consistent.
	var uuids, parents []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if v, ok := obj["uuid"].(string); ok {
			uuids = append(uuids, v)
		}
		if v, ok := obj["parentUuid"].(string); ok && v != "" {
			parents = append(parents, v)
		}
	}
	if len(uuids) == 0 {
		t.Fatal("no uuids found in rewrite output")
	}
	seen := map[string]bool{}
	for _, u := range uuids {
		if u == "u1" || u == "u2" || u == "u3" {
			t.Errorf("old uuid %q survived", u)
		}
		seen[u] = true
	}
	for _, p := range parents {
		if !seen[p] {
			t.Errorf("parent uuid %q does not map to any rewritten line (chain broken)", p)
		}
	}
	// Distinct old uuids map to distinct new uuids.
	if len(seen) != len(uuids) {
		t.Errorf("uuid collision: %d uuids, %d unique", len(uuids), len(seen))
	}
}

func TestSessionFromNeutral(t *testing.T) {
	tr := testTranscript()
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	out := string(SessionFromNeutral(tr, "new-id", "/new/dir", now))

	for _, want := range []string{
		`"sessionId":"new-id"`,
		`"cwd":"/new/dir"`,
		"Fix the login bug",
		"On it.",
		`"type":"last-prompt"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("synthesized session missing %q", want)
		}
	}

	// parentUuid chain: every parent references an earlier emitted uuid.
	var order []string
	childOf := map[string]string{} // uuid -> parent uuid
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		var obj map[string]any
		if json.Unmarshal([]byte(line), &obj) != nil {
			continue
		}
		if obj["type"] != "user" && obj["type"] != "assistant" {
			continue
		}
		u, _ := obj["uuid"].(string)
		p, _ := obj["parentUuid"].(string)
		order = append(order, u)
		childOf[u] = p
	}
	if len(order) < 3 {
		t.Fatalf("expected at least 3 message lines, got %d", len(order))
	}
	seen := map[string]bool{"": true}
	for _, u := range order {
		if !seen[childOf[u]] {
			t.Errorf("uuid %q references unseen parent %q", u, childOf[u])
		}
		seen[u] = true
	}
}

func TestSessionFromNeutralEmpty(t *testing.T) {
	tr := testTranscript()
	tr.Entries = nil
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	out := string(SessionFromNeutral(tr, "new-id", "/new/dir", now))
	if !strings.Contains(out, "Cross-agent session handoff") {
		t.Error("empty transcript should embed the handoff text")
	}
}

// ---- index ----

func TestIndexUpsertAndWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, IndexFile)

	idx, err := ReadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	idx.UpsertEntry(IndexEntry{SessionID: "s1", FirstPrompt: "one", MessageCount: 1})
	idx.UpsertEntry(IndexEntry{SessionID: "s2", FirstPrompt: "two", MessageCount: 2})
	if err := idx.WriteIndex(path); err != nil {
		t.Fatal(err)
	}

	// Newest entry first.
	back, err := ReadIndex(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(back.Entries) != 2 || back.Entries[0].SessionID != "s2" {
		t.Fatalf("entries = %+v, want s2 first", back.Entries)
	}

	// Upsert replaces, not appends.
	back.UpsertEntry(IndexEntry{SessionID: "s1", FirstPrompt: "one-updated", MessageCount: 9})
	if len(back.Entries) != 2 {
		t.Fatalf("upsert appended instead of replacing: %+v", back.Entries)
	}
	for _, e := range back.Entries {
		if e.SessionID == "s1" && e.MessageCount != 9 {
			t.Errorf("s1 not updated: %+v", e)
		}
	}
}

func TestReadIndexMissingFile(t *testing.T) {
	idx, err := ReadIndex(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("missing index file should not error: %v", err)
	}
	if idx.Version == 0 {
		t.Error("version should default to 1")
	}
}

func TestProjectDirName(t *testing.T) {
	for _, tc := range []struct {
		path string
		want string
	}{
		{path: "/Users/foo/bar", want: "-Users-foo-bar"},
		{path: `C:\Users\foo\bar`, want: "C--Users-foo-bar"},
		{path: "C:/Users/foo/bar", want: "C--Users-foo-bar"},
	} {
		if got := ProjectDirName(tc.path); got != tc.want {
			t.Errorf("ProjectDirName(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// ---- load + verify round trip against a fake home ----

func writeFakeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	projectDir := ProjectDirName("/old/dir")
	dir := filepath.Join(home, ProjectsDir, projectDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sess-1.jsonl"), []byte(claudeSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	idx := &Index{Version: 1}
	idx.UpsertEntry(IndexEntry{
		SessionID:   "sess-1",
		FullPath:    filepath.Join(dir, "sess-1.jsonl"),
		FirstPrompt: "Fix the login bug",
		ProjectPath: "/old/dir",
	})
	if err := idx.WriteIndex(filepath.Join(dir, IndexFile)); err != nil {
		t.Fatal(err)
	}
	return home
}

func TestLoadSession(t *testing.T) {
	home := writeFakeHome(t)
	data, err := LoadSession(home, "sess-1")
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if data.SessionID != "sess-1" {
		t.Errorf("session id = %q", data.SessionID)
	}
	if data.CWD != "/old/dir" {
		t.Errorf("cwd = %q", data.CWD)
	}
	if data.IndexEntry == nil || data.IndexEntry.FirstPrompt != "Fix the login bug" {
		t.Errorf("index entry = %+v", data.IndexEntry)
	}
	if data.Title == "" {
		t.Error("title should fall back to first prompt")
	}
	if data.Neutral.SourceAgent != "claude" {
		t.Errorf("neutral source = %q", data.Neutral.SourceAgent)
	}
}

func TestLoadSessionNotFound(t *testing.T) {
	home := writeFakeHome(t)
	if _, err := LoadSession(home, "no-such-session"); err == nil {
		t.Error("missing session should fail")
	}
}

func TestVerify(t *testing.T) {
	home := writeFakeHome(t)

	res := Verify(home, "sess-1", "/old/dir")
	if res.Status != "ok" {
		t.Errorf("verify status = %q, failures = %v", res.Status, res.Failures)
	}

	res = Verify(home, "sess-1", "/wrong/dir")
	if res.Status != "failed" {
		t.Errorf("wrong cwd should fail: %+v", res)
	}

	res = Verify(home, "missing-id", "")
	if res.Status != "failed" {
		t.Errorf("missing session should fail: %+v", res)
	}
}

// ---- summarize ----

func TestSummarizeSession(t *testing.T) {
	s := SummarizeSession([]byte(claudeSrc))
	if s.CWD != "/old/dir" {
		t.Errorf("cwd = %q", s.CWD)
	}
	if s.FirstUserText != "Fix the login bug" {
		t.Errorf("first user text = %q", s.FirstUserText)
	}
	if s.MessageCount == 0 {
		t.Error("message count should be positive")
	}
}
