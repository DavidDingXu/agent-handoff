// Package neutral defines the agent-neutral transcript format used to move
// conversations between coding agents (Codex <-> Claude). It is lossy by
// design: visible messages and tool evidence survive; reasoning traces and
// agent-specific event structures do not. The raw source session always
// travels alongside it in the bundle for audit.
package neutral

import (
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// Schema is the neutral transcript schema identifier.
const Schema = "agent-handoff.neutral.v1"

// EntryKind is either "message" or "tool".
type EntryKind string

const (
	KindMessage EntryKind = "message"
	KindTool    EntryKind = "tool"
)

// Entry is one visible transcript item.
type Entry struct {
	Kind      EntryKind `json:"kind"`
	Timestamp string    `json:"timestamp,omitempty"`
	Role      string    `json:"role,omitempty"`   // user | assistant
	Text      string    `json:"text,omitempty"`   // for messages
	Tool      string    `json:"tool,omitempty"`   // for tools
	Status    string    `json:"status,omitempty"` // called | completed | failed
	Input     string    `json:"input,omitempty"`
	Output    string    `json:"output,omitempty"`
}

// Transcript is the neutral representation of a session.
type Transcript struct {
	Schema      string  `json:"schema"`
	SourceAgent string  `json:"source_agent"` // "codex" | "claude"
	SourceID    string  `json:"source_id"`
	Title       string  `json:"title,omitempty"`
	SourceCWD   string  `json:"source_cwd,omitempty"`
	CreatedAt   string  `json:"created_at,omitempty"`
	Entries     []Entry `json:"entries"`
}

// FromCodexSession converts a Codex rollout jsonl into a neutral transcript.
func FromCodexSession(sourceID, title, sourceCWD string, content []byte) Transcript {
	t := Transcript{
		Schema:      Schema,
		SourceAgent: "codex",
		SourceID:    sourceID,
		Title:       title,
		SourceCWD:   sourceCWD,
	}
	pending := map[string]int{} // call_id -> entries index awaiting output

	// appendMessage adds a message entry unless it duplicates the previous
	// one: Codex rollouts carry every message twice (event_msg user_message/
	// agent_message followed by the response_item copy with identical text).
	appendMessage := func(e Entry) {
		if n := len(t.Entries); n > 0 {
			last := t.Entries[n-1]
			if last.Kind == KindMessage && last.Role == e.Role && last.Text == e.Text {
				return
			}
		}
		t.Entries = append(t.Entries, e)
	}

	session.IterLines(content, func(line session.Line) {
		obj := line.Obj
		if obj == nil {
			return
		}
		ts, _ := obj["timestamp"].(string)
		switch session.Type(obj) {
		case "response_item":
			payload := session.Payload(obj)
			if payload == nil {
				return
			}
			switch session.PayloadType(obj) {
			case "message":
				role, _ := payload["role"].(string)
				text := session.MessageText(payload)
				if text == "" {
					return
				}
				if role == "user" && session.IsHiddenUserText(text) {
					return
				}
				if role != "user" && role != "assistant" {
					return
				}
				appendMessage(Entry{
					Kind: KindMessage, Timestamp: ts, Role: role, Text: text,
				})
			case "function_call", "custom_tool_call", "tool_search_call", "local_shell_call":
				name := toolName(payload)
				callID, _ := payload["call_id"].(string)
				idx := len(t.Entries)
				t.Entries = append(t.Entries, Entry{
					Kind: KindTool, Timestamp: ts, Tool: name,
					Status: "called", Input: toolInput(payload),
				})
				if callID != "" {
					pending[callID] = idx
				}
			case "function_call_output", "custom_tool_call_output", "tool_search_output", "local_shell_call_output":
				callID, _ := payload["call_id"].(string)
				output := toolOutput(payload)
				if idx, ok := pending[callID]; ok {
					e := &t.Entries[idx]
					e.Output = output
					e.Status = outputStatus(payload)
					delete(pending, callID)
				} else {
					t.Entries = append(t.Entries, Entry{
						Kind: KindTool, Timestamp: ts, Tool: "unknown",
						Status: outputStatus(payload), Output: output,
					})
				}
			}
		case "user_message":
			text, _ := session.Payload(obj)["message"].(string)
			if text == "" || session.IsHiddenUserText(text) {
				return
			}
			appendMessage(Entry{
				Kind: KindMessage, Timestamp: ts, Role: "user", Text: text,
			})
		case "event_msg":
			if session.PayloadType(obj) == "agent_message" {
				msg, _ := session.Payload(obj)["message"].(string)
				if msg != "" {
					appendMessage(Entry{
						Kind: KindMessage, Timestamp: ts, Role: "assistant", Text: msg,
					})
				}
			}
		}
	})
	return t
}

// FromClaudeSession converts a Claude session jsonl into a neutral transcript.
func FromClaudeSession(sourceID, title, sourceCWD string, content []byte) Transcript {
	t := Transcript{
		Schema:      Schema,
		SourceAgent: "claude",
		SourceID:    sourceID,
		Title:       title,
		SourceCWD:   sourceCWD,
	}
	session.IterLines(content, func(line session.Line) {
		obj := line.Obj
		if obj == nil {
			return
		}
		switch session.Type(obj) {
		case "custom-title":
			if v, _ := obj["customTitle"].(string); v != "" && t.Title == "" {
				t.Title = v
			}
		case "ai-title":
			if v, _ := obj["aiTitle"].(string); v != "" && t.Title == "" {
				t.Title = v
			}
		case "user", "assistant":
			if isTrue(obj["isMeta"]) || isTrue(obj["isSidechain"]) {
				return
			}
			ts, _ := obj["timestamp"].(string)
			msg, _ := obj["message"].(map[string]any)
			if msg == nil {
				return
			}
			role, _ := msg["role"].(string)
			text, hasToolResult := claudeMessageText(msg)
			if text == "" {
				return
			}
			// A user line carrying only a tool_result is harness plumbing.
			if hasToolResult && role == "user" {
				role = "assistant"
			}
			t.Entries = append(t.Entries, Entry{
				Kind: KindMessage, Timestamp: ts, Role: role, Text: text,
			})
		}
	})
	return t
}

// HandoffText renders a transcript as plain text for cross-agent handoff.
// When the target agent receives an empty transcript, this text is embedded
// as the single user message so the model still sees full context.
func HandoffText(t Transcript, targetAgent string) string {
	var sb strings.Builder
	sb.WriteString("Cross-agent session handoff\n")
	sb.WriteString("Source agent: " + t.SourceAgent + "\n")
	sb.WriteString("Source session: " + t.SourceID + "\n")
	if t.Title != "" {
		sb.WriteString("Title: " + t.Title + "\n")
	}
	if t.SourceCWD != "" {
		sb.WriteString("Source cwd: " + t.SourceCWD + "\n")
	}
	sb.WriteString("Target agent: " + targetAgent + "\n")
	sb.WriteString("Continue from the transcript below. The raw source session is stored locally as a sidecar for audit and deeper recovery.\n")
	sb.WriteString("Transcript:\n")
	for _, e := range t.Entries {
		switch e.Kind {
		case KindMessage:
			sb.WriteString("[" + e.Role + "] " + e.Text + "\n")
		case KindTool:
			sb.WriteString("[tool] " + e.Tool + " (" + e.Status + ")\n")
			if e.Input != "" {
				sb.WriteString("  input: " + clip(e.Input, 2000) + "\n")
			}
			if e.Output != "" {
				sb.WriteString("  output: " + clip(e.Output, 4000) + "\n")
			}
		}
	}
	return sb.String()
}

// AppendEntries merges entries (used when composing a synthesized session).
func (t *Transcript) AppendEntries(entries []Entry) {
	t.Entries = append(t.Entries, entries...)
}

func claudeMessageText(msg map[string]any) (string, bool) {
	var sb strings.Builder
	hasToolResult := false
	switch content := msg["content"].(type) {
	case string:
		return content, false
	case []any:
		for _, c := range content {
			cm, ok := c.(map[string]any)
			if !ok {
				continue
			}
			ctype, _ := cm["type"].(string)
			switch ctype {
			case "text":
				if txt, _ := cm["text"].(string); txt != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(txt)
				}
			case "tool_use":
				name, _ := cm["name"].(string)
				sb.WriteString("\n[external_agent_tool_call: " + name + "]")
				if d, _ := cm["description"].(string); d != "" {
					sb.WriteString(" " + clip(d, 200))
				} else if in, ok := cm["input"].(string); ok && in != "" {
					sb.WriteString(" " + clip(in, 2000))
				}
			case "tool_result":
				hasToolResult = true
				text := toolResultText(cm)
				if text != "" {
					sb.WriteString("\n[external_agent_tool_result" + errorSuffix(cm) + "] " + text)
				}
			case "thinking":
				// dropped
			default:
				sb.WriteString("\n[external unsupported block: " + ctype + "]")
			}
		}
	}
	return sb.String(), hasToolResult
}

func toolResultText(cm map[string]any) string {
	switch c := cm["content"].(type) {
	case string:
		return clip(c, 4000)
	case []any:
		var parts []string
		for _, p := range c {
			if pm, ok := p.(map[string]any); ok {
				if txt, _ := pm["text"].(string); txt != "" {
					parts = append(parts, txt)
				}
			}
		}
		return clip(strings.Join(parts, "\n"), 4000)
	}
	return ""
}

func errorSuffix(cm map[string]any) string {
	if isTrue(cm["is_error"]) {
		return ": error"
	}
	return ""
}

func toolName(payload map[string]any) string {
	name, _ := payload["name"].(string)
	if ns, _ := payload["namespace"].(string); ns != "" && name != "" {
		return ns + "." + name
	}
	return name
}

func toolInput(payload map[string]any) string {
	if args, ok := payload["arguments"].(string); ok && args != "" {
		return args
	}
	if in, ok := payload["input"].(string); ok {
		return in
	}
	if in, ok := payload["input"].(map[string]any); ok {
		return marshalCompact(in)
	}
	return ""
}

func toolOutput(payload map[string]any) string {
	if out, ok := payload["output"].(string); ok {
		return out
	}
	if out, ok := payload["output"].(map[string]any); ok {
		return marshalCompact(out)
	}
	return ""
}

func outputStatus(payload map[string]any) string {
	if isTrue(payload["is_error"]) {
		return "failed"
	}
	return "completed"
}
