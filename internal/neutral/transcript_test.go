package neutral

import (
	"strings"
	"testing"
)

const codexSession = `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project","cli_version":"0.1.0","source":"vscode"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Fix the login bug"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Fix the login bug"}]}}
{"timestamp":"2026-08-01T10:00:04.000Z","type":"response_item","payload":{"type":"function_call","name":"shell","call_id":"call_1","arguments":"{\"command\":[\"grep\",\"-r\",\"login\"]}"}}
{"timestamp":"2026-08-01T10:00:05.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"auth.go:42: bug here","status":"completed"}}
{"timestamp":"2026-08-01T10:00:06.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Found the bug in auth.go:42."}}
{"timestamp":"2026-08-01T10:00:06.000Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Found the bug in auth.go:42."}]}}
`

const claudeSession = `{"type":"user","message":{"role":"user","content":"Fix the login bug"},"cwd":"/src/project","sessionId":"s1","uuid":"u1","timestamp":"2026-08-01T10:00:00Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":"{\"command\":\"grep -r login\"}"}]},"cwd":"/src/project","sessionId":"s1","uuid":"u2","timestamp":"2026-08-01T10:00:01Z"}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"auth.go:42: bug here"}]},"cwd":"/src/project","sessionId":"s1","uuid":"u3","timestamp":"2026-08-01T10:00:02Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Found the bug."}]},"cwd":"/src/project","sessionId":"s1","uuid":"u4","timestamp":"2026-08-01T10:00:03Z"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"thinking","thinking":"internal reasoning"}]},"cwd":"/src/project","sessionId":"s1","uuid":"u5","timestamp":"2026-08-01T10:00:04Z"}
{"type":"user","isMeta":true,"message":{"role":"user","content":"hidden meta"},"sessionId":"s1","uuid":"u6","timestamp":"2026-08-01T10:00:05Z"}
{"type":"custom-title","customTitle":"Login Bug Fix","sessionId":"s1"}
`

func TestFromCodexSession(t *testing.T) {
	tr := FromCodexSession("t1", "Original Title", "/src/project", []byte(codexSession))
	if tr.Schema != Schema {
		t.Errorf("schema = %q", tr.Schema)
	}
	if tr.SourceAgent != "codex" || tr.SourceID != "t1" {
		t.Errorf("source = %s/%s", tr.SourceAgent, tr.SourceID)
	}
	if tr.Title != "Original Title" {
		t.Errorf("title = %q", tr.Title)
	}

	var messages []Entry
	var tools []Entry
	for _, e := range tr.Entries {
		switch e.Kind {
		case KindMessage:
			messages = append(messages, e)
		case KindTool:
			tools = append(tools, e)
		}
	}
	// Each message appears once even though Codex rollouts carry both the
	// event_msg copy and the response_item copy of every message.
	if len(messages) != 2 {
		t.Errorf("messages = %d, want 2: %+v", len(messages), messages)
	}
	if messages[0].Role != "user" || messages[0].Text != "Fix the login bug" {
		t.Errorf("first message = %+v", messages[0])
	}
	if messages[1].Role != "assistant" || messages[1].Text != "Found the bug in auth.go:42." {
		t.Errorf("second message = %+v", messages[1])
	}
	// Tool call and its output merge into one entry.
	if len(tools) != 1 {
		t.Fatalf("tools = %d, want 1: %+v", len(tools), tools)
	}
	if tools[0].Tool != "shell" || tools[0].Status != "completed" {
		t.Errorf("tool entry = %+v", tools[0])
	}
	if !strings.Contains(tools[0].Output, "auth.go:42") {
		t.Errorf("tool output = %q", tools[0].Output)
	}
	if !strings.Contains(tools[0].Input, "grep") {
		t.Errorf("tool input = %q", tools[0].Input)
	}
}

func TestFromCodexSessionFiltersHiddenUserText(t *testing.T) {
	session := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/x"}}
{"timestamp":"2026-08-01T10:00:01.000Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"# AGENTS.md instructions for this repo"}]}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"user_message","payload":{"message":"<environment_context>OS=macOS</environment_context>"}}
{"timestamp":"2026-08-01T10:00:03.000Z","type":"user_message","payload":{"message":"Real question"}}
`
	tr := FromCodexSession("t1", "", "/x", []byte(session))
	if len(tr.Entries) != 1 || tr.Entries[0].Text != "Real question" {
		t.Errorf("entries = %+v, want only the real question", tr.Entries)
	}
}

func TestFromClaudeSession(t *testing.T) {
	tr := FromClaudeSession("s1", "", "/src/project", []byte(claudeSession))
	if tr.SourceAgent != "claude" || tr.SourceID != "s1" {
		t.Errorf("source = %s/%s", tr.SourceAgent, tr.SourceID)
	}
	// The custom-title line wins over the passed (empty) title.
	if tr.Title != "Login Bug Fix" {
		t.Errorf("title = %q, want custom title", tr.Title)
	}

	// Entries: user, assistant(tool_use), assistant(tool_result), assistant(text).
	// thinking and isMeta lines are dropped.
	if len(tr.Entries) != 4 {
		t.Fatalf("entries = %d, want 4: %+v", len(tr.Entries), tr.Entries)
	}
	if tr.Entries[0].Role != "user" || tr.Entries[0].Text != "Fix the login bug" {
		t.Errorf("entry 0 = %+v", tr.Entries[0])
	}
	// tool_use surfaces as external_agent_tool_call marker text.
	if !strings.Contains(tr.Entries[1].Text, "external_agent_tool_call: Bash") {
		t.Errorf("entry 1 = %q", tr.Entries[1].Text)
	}
	// A user line carrying only tool_result is harness plumbing -> assistant.
	if tr.Entries[2].Role != "assistant" {
		t.Errorf("entry 2 role = %q, want assistant", tr.Entries[2].Role)
	}
	if !strings.Contains(tr.Entries[2].Text, "auth.go:42") {
		t.Errorf("entry 2 = %q", tr.Entries[2].Text)
	}
	if tr.Entries[3].Text != "Found the bug." {
		t.Errorf("entry 3 = %q", tr.Entries[3].Text)
	}
}

func TestHandoffText(t *testing.T) {
	tr := FromCodexSession("t1", "My Title", "/src/project", []byte(codexSession))
	text := HandoffText(tr, "claude")
	for _, want := range []string{
		"Cross-agent session handoff",
		"Source agent: codex",
		"Target agent: claude",
		"Title: My Title",
		"[user] Fix the login bug",
		"[tool] shell (completed)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("HandoffText missing %q", want)
		}
	}
}

func TestHandoffTextEmptyTranscript(t *testing.T) {
	tr := Transcript{Schema: Schema, SourceAgent: "codex", SourceID: "t1"}
	text := HandoffText(tr, "claude")
	if !strings.Contains(text, "Cross-agent session handoff") {
		t.Errorf("empty transcript handoff = %q", text)
	}
}

func TestAppendEntries(t *testing.T) {
	tr := Transcript{}
	tr.AppendEntries([]Entry{{Kind: KindMessage, Role: "user", Text: "hi"}})
	if len(tr.Entries) != 1 {
		t.Errorf("entries = %d", len(tr.Entries))
	}
}
