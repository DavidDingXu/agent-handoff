package codex

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

func TestResolveCurrentThreadPrefersWorkingDirectory(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "")
	t.Setenv("CODEX_SESSION_ID", "")
	home := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, StateSQLiteFile))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY, cwd TEXT, updated_at_ms INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('current-project', ?, 1), ('other-project', '/unrelated', 2)`, wd); err != nil {
		t.Fatal(err)
	}
	id, err := ResolveThread(home, "current")
	if err != nil {
		t.Fatal(err)
	}
	if id != "current-project" {
		t.Fatalf("selected %q instead of the current project", id)
	}
}

func TestImportLeavesNewCodexDatabaseToNativeMigrations(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	r, err := Restore(RestoreInput{Neutral: neutral.Transcript{SourceAgent: "claude"}}, RestoreOptions{Home: home, TargetCWD: cwd, Execute: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, StateSQLiteFile)); !os.IsNotExist(err) {
		t.Fatalf("import must not create an unmanaged native database: %v", err)
	}
	for _, path := range r.Writes {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("reported nonexistent write %s: %v", path, err)
		}
	}
	if got := Verify(home, r.ThreadID, cwd); got.Status != "ok" {
		t.Fatalf("rollout-only import did not verify: %+v", got)
	}
}

func TestCrossAgentCodexHistoryHasNativeProviderAndClosedTurns(t *testing.T) {
	tr := neutral.Transcript{SourceAgent: "claude", Entries: []neutral.Entry{
		{Kind: neutral.KindMessage, Role: "user", Text: "first"},
		{Kind: neutral.KindTool, Tool: "Read", Input: `{}`, Output: "a"},
		{Kind: neutral.KindTool, Tool: "Read", Input: `{}`, Output: "b"},
		{Kind: neutral.KindMessage, Role: "assistant", Text: "answer"},
		{Kind: neutral.KindMessage, Role: "user", Text: "second"},
		{Kind: neutral.KindMessage, Role: "assistant", Text: "done"},
	}}
	data := SessionFromNeutral(tr, "0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001", t.TempDir(), "openai", time.Now())
	openTurn := ""
	completed := 0
	calls := map[string]bool{}
	session.IterLines(data, func(line session.Line) {
		p := session.Payload(line.Obj)
		if session.Type(line.Obj) == "session_meta" {
			if p["model_provider"] != "openai" {
				t.Errorf("invalid receiver provider: %v", p["model_provider"])
			}
			if p["source"] != "vscode" {
				t.Errorf("source hidden from native task list: %v", p["source"])
			}
		}
		switch session.PayloadType(line.Obj) {
		case "task_started":
			if openTurn != "" {
				t.Errorf("new turn starts before %s completes", openTurn)
			}
			openTurn, _ = p["turn_id"].(string)
		case "task_complete":
			if p["turn_id"] != openTurn || openTurn == "" {
				t.Errorf("completion %v does not match active turn %q", p["turn_id"], openTurn)
			}
			openTurn = ""
			completed++
		case "function_call":
			id, _ := p["call_id"].(string)
			if id == "" || calls[id] {
				t.Errorf("tool call identity reused: %q", id)
			}
			calls[id] = true
		}
	})
	if completed != 2 || openTurn != "" {
		t.Fatalf("history has %d completed turns; active=%q", completed, openTurn)
	}
}
