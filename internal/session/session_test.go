package session

import (
	"os"
	"strings"
	"testing"
	"time"
)

func mustRead(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func TestIterLinesCountsAndMalformed(t *testing.T) {
	content := append(mustRead(t, "sample-session.jsonl"), []byte("not json at all\n")...)
	n := 0
	malformed := 0
	IterLines(content, func(line Line) {
		n++
		if line.Obj == nil {
			malformed++
		}
	})
	if n != 18 {
		t.Errorf("line count = %d, want 18", n)
	}
	if malformed != 1 {
		t.Errorf("malformed = %d, want 1", malformed)
	}
}

func TestCountUserAssistantMessages(t *testing.T) {
	// sample: 2 user (event+response each counted? no: response_item message x2 user + x2 assistant,
	// event_msg user_message x2 + agent_message x2 => 8
	got := CountUserAssistantMessages(mustRead(t, "sample-session.jsonl"))
	if got != 8 {
		t.Errorf("count = %d, want 8", got)
	}
}

func TestExtractDetail(t *testing.T) {
	d := ExtractDetail(mustRead(t, "sample-session.jsonl"))
	if d.FirstUserMessage != "Fix the login bug" {
		t.Errorf("first user = %q", d.FirstUserMessage)
	}
	if d.LastMessage != "Tests added." {
		t.Errorf("last = %q", d.LastMessage)
	}
	if d.FirstTimestamp != "2026-08-01T10:00:00.000Z" {
		t.Errorf("first ts = %q", d.FirstTimestamp)
	}
}

func TestHiddenUserTextFiltered(t *testing.T) {
	if !IsHiddenUserText("# AGENTS.md instructions for this repo") {
		t.Error("AGENTS.md prefix should be hidden")
	}
	if !IsHiddenUserText("<environment_context>foo") {
		t.Error("environment_context should be hidden")
	}
	if IsHiddenUserText("Fix the login bug") {
		t.Error("normal user text must not be hidden")
	}
}

func TestFirstSessionCWD(t *testing.T) {
	if got := FirstSessionCWD(mustRead(t, "sample-session.jsonl")); got != "/src/project" {
		t.Errorf("cwd = %q", got)
	}
}

func TestSessionCWDs(t *testing.T) {
	cwds := SessionCWDs(mustRead(t, "sample-session.jsonl"))
	if len(cwds) != 1 || !cwds["/src/project"] {
		t.Errorf("cwds = %v", cwds)
	}
}

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello..."},
		{"  spaced  ", 100, "spaced"},
	}
	for _, c := range cases {
		if got := Truncate(c.in, c.n); got != c.want {
			t.Errorf("Truncate(%q,%d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

func TestRewriteForImport(t *testing.T) {
	now := parseTimeHelper(t, "2026-08-02T00:00:00Z")
	out := RewriteForImport(mustRead(t, "sample-session.jsonl"), ImportRewrite{
		TargetCWD: "/new/dir",
		TargetID:  "NEW-ID",
		ImagePaths: map[string]string{
			"/src/project/img/x.png": "/assets/img/x.png",
		},
		Now: now,
	})
	s := string(out)

	if strings.Contains(s, "/src/project") {
		t.Error("old cwd leaked into rewrite output")
	}
	if !strings.Contains(s, `"id":"NEW-ID"`) {
		t.Error("new thread id missing in rewrite output")
	}
	if !strings.Contains(s, "/new/dir") {
		t.Error("new cwd missing in rewrite output")
	}
	// timestamps rewritten to import time
	if !strings.Contains(s, "2026-08-02T00:00:00.000Z") {
		t.Error("import-time timestamp missing")
	}
	// malformed line preserved
	if !strings.Contains(s, "not json") {
		out2 := RewriteForImport([]byte("garbage\n"), ImportRewrite{TargetCWD: "/x"})
		if !strings.Contains(string(out2), "garbage") {
			t.Error("non-json line should pass through")
		}
	}
}

func parseTimeHelper(t *testing.T, s string) time.Time {
	t.Helper()
	p, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return p
}
