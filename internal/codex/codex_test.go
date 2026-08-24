package codex

import (
	"strings"
	"testing"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const rawSession = `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 1"}}
{"timestamp":"2026-08-01T10:00:03.000Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-08-01T10:01:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}
{"timestamp":"2026-08-01T10:01:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 2"}}
{"timestamp":"2026-08-01T10:01:02.000Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-08-01T10:02:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t3"}}
{"timestamp":"2026-08-01T10:02:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 3"}}
{"timestamp":"2026-08-01T10:02:02.000Z","type":"event_msg","payload":{"type":"task_complete"}}
`

func lineText(content []byte, needle string) bool {
	return strings.Contains(string(content), needle)
}

func TestNormalizeKeepsAllTurns(t *testing.T) {
	out := NormalizeActiveSession([]byte(rawSession), "t1")
	for _, want := range []string{"Question 1", "Question 2", "Question 3"} {
		if !lineText(out, want) {
			t.Errorf("normalized session missing %q", want)
		}
	}
}

func TestNormalizeDropsRolledBackTurns(t *testing.T) {
	// A rollback of 2 turns after turn 3 drops turns 2 and 3.
	withRollback := rawSession + `{"timestamp":"2026-08-01T10:03:00.000Z","type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":2}}`
	out := NormalizeActiveSession([]byte(withRollback), "t1")
	if !lineText(out, "Question 1") {
		t.Error("turn 1 should survive")
	}
	if lineText(out, "Question 2") || lineText(out, "Question 3") {
		t.Error("rolled-back turns should be dropped")
	}
}

func TestNormalizeDropsInFlightTurnOnRollback(t *testing.T) {
	// Turn 4 is in flight when the rollback arrives: it is dropped, and
	// since num_turns=1 is consumed by that in-flight turn, all three
	// completed turns survive.
	withRollback := rawSession + `{"timestamp":"2026-08-01T10:03:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t4"}}
{"timestamp":"2026-08-01T10:03:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 4"}}
{"timestamp":"2026-08-01T10:04:00.000Z","type":"event_msg","payload":{"type":"thread_rolled_back","num_turns":1}}`
	out := NormalizeActiveSession([]byte(withRollback), "t1")
	if !lineText(out, "Question 1") || !lineText(out, "Question 2") || !lineText(out, "Question 3") {
		t.Error("completed turns should survive when num_turns is consumed by the in-flight turn")
	}
	if lineText(out, "Question 4") {
		t.Error("in-flight turn at rollback point should be dropped")
	}
}

func TestNormalizeKeepsInFlightFinalTurn(t *testing.T) {
	// No task_complete on the final turn: it is still the active turn.
	withOpen := rawSession + `{"timestamp":"2026-08-01T10:03:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t4"}}
{"timestamp":"2026-08-01T10:03:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 4"}}`
	out := NormalizeActiveSession([]byte(withOpen), "t1")
	if !lineText(out, "Question 4") {
		t.Error("in-flight final turn should be preserved")
	}
}

func TestNormalizeCollapsesForkMetaLines(t *testing.T) {
	forked := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project"}}
{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t-other","cwd":"/other/project"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Question 1"}}
{"timestamp":"2026-08-01T10:00:03.000Z","type":"event_msg","payload":{"type":"task_complete"}}
`
	out := NormalizeActiveSession([]byte(forked), "t1")
	if lineText(out, "/other/project") {
		t.Error("fork session_meta with a different id should be dropped")
	}
	if !lineText(out, "/src/project") {
		t.Error("matching session_meta should be kept")
	}
}

const sessionWithSelfExportTurn = `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Fix the login bug"}}
{"timestamp":"2026-08-01T10:00:03.000Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-08-01T10:01:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}
{"timestamp":"2026-08-01T10:01:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"share this session please"}}
{"timestamp":"2026-08-01T10:01:02.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c1","arguments":"{\"command\":[\"agent-handoff\",\"share\",\"--thread\",\"current\"]}"}}
{"timestamp":"2026-08-01T10:01:03.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"{\"status\":\"ok\"}"}}
{"timestamp":"2026-08-01T10:01:04.000Z","type":"event_msg","payload":{"type":"task_complete"}}
`

func TestDropSelfExportTurnRemovesPureShareTurn(t *testing.T) {
	out := DropSelfExportTurn([]byte(sessionWithSelfExportTurn))
	if !lineText(out, "Fix the login bug") {
		t.Error("the real work turn must survive")
	}
	if lineText(out, "share this session please") || lineText(out, "agent-handoff") {
		t.Error("the self-export turn should be removed")
	}
}

func TestDropSelfExportTurnKeepsUnrelatedLastTurn(t *testing.T) {
	unrelated := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t1"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Fix the login bug"}}
{"timestamp":"2026-08-01T10:00:03.000Z","type":"event_msg","payload":{"type":"task_complete"}}
{"timestamp":"2026-08-01T10:01:00.000Z","type":"event_msg","payload":{"type":"task_started","turn_id":"t2"}}
{"timestamp":"2026-08-01T10:01:01.000Z","type":"event_msg","payload":{"type":"user_message","message":"share this session please"}}
{"timestamp":"2026-08-01T10:01:02.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"c1","arguments":"{\"command\":[\"rm\",\"-rf\",\"unrelated\"]}"}}
{"timestamp":"2026-08-01T10:01:03.000Z","type":"event_msg","payload":{"type":"task_complete"}}
`
	out := DropSelfExportTurn([]byte(unrelated))
	if !lineText(out, "share this session please") {
		t.Error("a turn with non-share tool calls must be kept")
	}
}

func TestDropSelfExportTurnNoTurns(t *testing.T) {
	plain := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/x"}}`
	out := DropSelfExportTurn([]byte(plain))
	if !lineText(out, "session_meta") {
		t.Error("session without turns must pass through unchanged")
	}
}

// ---- row values ----

func testTime() time.Time {
	return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
}

func TestThreadRowValuesClonesSenderMetadata(t *testing.T) {
	src := map[string]any{
		"id":                   "old-id",
		"title":                "Old title",
		"model":                "gpt-5",
		"effort":               "high",
		"git_branch":           "feature/login-fix",
		"git_origin_url":       "https://github.com/org/repo",
		"custom_future_column": "preserved",
	}
	vals := threadRowValues(src, "new-id", "New title", "/sessions/2026/08/02/rollout-x.jsonl", "/target/dir", testTime())

	if vals["model"] != "gpt-5" {
		t.Errorf("model = %v, want sender value", vals["model"])
	}
	if vals["effort"] != "high" {
		t.Errorf("effort = %v, want sender value", vals["effort"])
	}
	if vals["git_branch"] != "feature/login-fix" {
		t.Errorf("git_branch = %v, want sender value", vals["git_branch"])
	}
	if vals["custom_future_column"] != "preserved" {
		t.Error("unknown sender columns must survive the clone")
	}

	// Import-specific overlays.
	if vals["id"] != "new-id" {
		t.Errorf("id = %v, want new-id", vals["id"])
	}
	if vals["title"] != "New title" {
		t.Errorf("title = %v", vals["title"])
	}
	if vals["cwd"] != "/target/dir" {
		t.Errorf("cwd = %v", vals["cwd"])
	}
	if vals["rollout_path"] != "/sessions/2026/08/02/rollout-x.jsonl" {
		t.Errorf("rollout_path = %v", vals["rollout_path"])
	}
	if vals["created_at"] != testTime().Unix() {
		t.Errorf("created_at = %v", vals["created_at"])
	}
	if vals["updated_at_ms"] != testTime().UnixMilli() {
		t.Errorf("updated_at_ms = %v", vals["updated_at_ms"])
	}
	if vals["archived"] != 0 || vals["is_pinned"] != 0 || vals["has_user_event"] != 1 {
		t.Errorf("fresh-state flags = %v %v %v", vals["archived"], vals["is_pinned"], vals["has_user_event"])
	}
}

func TestThreadRowValuesSenderSourcePreserved(t *testing.T) {
	src := map[string]any{"source": "vscode"}
	vals := threadRowValues(src, "new-id", "T", "/s.jsonl", "/d", testTime())
	if vals["source"] != "vscode" {
		t.Errorf("plain-string source must survive: %v", vals["source"])
	}
}

func TestThreadRowValuesDefaults(t *testing.T) {
	vals := threadRowValues(nil, "new-id", "T", "/s.jsonl", "/d", testTime())
	for _, k := range []string{"model_provider", "sandbox_policy", "approval_mode", "history_mode", "memory_mode", "tokens_used"} {
		if _, ok := vals[k]; !ok {
			t.Errorf("default %q missing", k)
		}
	}
	if vals["model_provider"] != "OpenAI" {
		t.Errorf("model_provider default = %v", vals["model_provider"])
	}
	if vals["approval_mode"] != "never" {
		t.Errorf("approval_mode default = %v", vals["approval_mode"])
	}
}

func TestSynthesizedRowValues(t *testing.T) {
	tr := neutral.Transcript{Schema: neutral.Schema, SourceAgent: "claude", SourceID: "s1"}
	vals := synthesizedRowValues(tr, "new-id", "T", "/s.jsonl", "/d", testTime())
	if vals["source"] != "agent-handoff" {
		t.Errorf("source = %v, want agent-handoff", vals["source"])
	}
	if vals["model_provider"] != "claude" {
		t.Errorf("model_provider = %v, want the source agent name", vals["model_provider"])
	}
	if vals["id"] != "new-id" {
		t.Errorf("id = %v", vals["id"])
	}
}

// ---- neutral -> codex synthesis ----

func TestSessionFromNeutral(t *testing.T) {
	tr := neutral.Transcript{
		Schema:      neutral.Schema,
		SourceAgent: "claude",
		SourceID:    "s1",
		Entries: []neutral.Entry{
			{Kind: neutral.KindMessage, Role: "user", Text: "Fix the bug"},
			{Kind: neutral.KindTool, Tool: "Bash", Status: "completed", Input: "grep login", Output: "auth.go:42"},
			{Kind: neutral.KindMessage, Role: "assistant", Text: "Fixed."},
		},
	}
	out := string(SessionFromNeutral(tr, "new-id", "/target/dir", testTime()))

	for _, want := range []string{
		`"type":"session_meta"`,
		`"id":"new-id"`,
		`"/target/dir"`,
		`"user_message"`,
		"Fix the bug",
		`"function_call"`,
		"Bash",
		`"function_call_output"`,
		"auth.go:42",
		`"agent_message"`,
		"Fixed.",
		`"task_complete"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("synthesized session missing %q", want)
		}
	}
	if strings.Contains(out, `", "`) {
		t.Error("synthesized session should be compact json lines")
	}
}

func TestSessionFromNeutralEmptyTranscript(t *testing.T) {
	tr := neutral.Transcript{Schema: neutral.Schema, SourceAgent: "claude", SourceID: "s1"}
	out := string(SessionFromNeutral(tr, "new-id", "/d", testTime()))
	if !strings.Contains(out, "Cross-agent session handoff") {
		t.Errorf("empty transcript should embed the handoff text: %q", out[:200])
	}
}

// ---- misc helpers ----

func TestQuoteIdent(t *testing.T) {
	if got := QuoteIdent(`we"ird`); got != `"we""ird"` {
		t.Errorf("QuoteIdent = %q", got)
	}
	if got := QuoteIdent("plain"); got != `"plain"` {
		t.Errorf("QuoteIdent = %q", got)
	}
}
