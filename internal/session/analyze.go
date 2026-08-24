package session

import (
	"strings"
	"unicode/utf8"
)

// Truncate trims and clips s to at most n bytes, appending "..." when cut.
// It never splits a multi-byte rune.
func Truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := s[:n]
	for !utf8.ValidString(cut) && len(cut) > 0 {
		cut = cut[:len(cut)-1]
	}
	return cut + "..."
}

// hiddenPrefixes marks synthetic session lines (injected instructions,
// environment context, skill wrappers) that are noise for previews and
// cross-agent handoffs.
var hiddenPrefixes = []string{
	"# AGENTS.md instructions for",
	"<environment_context>",
	"<skill>",
	"<turn_aborted>",
	"<INSTRUCTIONS>",
	"<codex_internal_context",
	"<recommended_plugins>",
	"<oai-mem-citation",
}

// IsHiddenUserText reports whether a user-message text is synthetic context
// injected by the harness rather than something the human typed.
func IsHiddenUserText(text string) bool {
	t := strings.TrimSpace(text)
	for _, p := range hiddenPrefixes {
		if strings.HasPrefix(t, p) {
			return true
		}
	}
	return false
}

// CountUserAssistantMessages counts visible user and assistant messages.
func CountUserAssistantMessages(content []byte) int {
	n := 0
	IterLines(content, func(line Line) {
		obj := line.Obj
		if obj == nil {
			return
		}
		switch Type(obj) {
		case "response_item":
			if PayloadType(obj) == "message" {
				role, _ := Payload(obj)["role"].(string)
				if role == "user" || role == "assistant" {
					n++
				}
			}
		case "user_message":
			n++
		case "event_msg":
			switch PayloadType(obj) {
			case "agent_message", "user_message":
				n++
			}
		}
	})
	return n
}

// Detail is the preview summary of a session.
type Detail struct {
	FirstUserMessage string
	LastMessage      string
	FirstTimestamp   string
	LastTimestamp    string
}

// ExtractDetail pulls the first visible user message, the last message and
// the session time range out of a session jsonl.
func ExtractDetail(content []byte) Detail {
	var d Detail
	IterLines(content, func(line Line) {
		obj := line.Obj
		if obj == nil {
			return
		}
		if ts, _ := obj["timestamp"].(string); ts != "" {
			if d.FirstTimestamp == "" {
				d.FirstTimestamp = ts
			}
			d.LastTimestamp = ts
		}
		switch Type(obj) {
		case "response_item":
			if PayloadType(obj) != "message" {
				return
			}
			role, _ := Payload(obj)["role"].(string)
			text := MessageText(Payload(obj))
			if role == "user" {
				if d.FirstUserMessage == "" && text != "" && !IsHiddenUserText(text) {
					d.FirstUserMessage = text
				}
			}
			if text != "" {
				d.LastMessage = text
			}
		case "user_message":
			text, _ := Payload(obj)["message"].(string)
			if text != "" {
				if d.FirstUserMessage == "" && !IsHiddenUserText(text) {
					d.FirstUserMessage = text
				}
				d.LastMessage = text
			}
		case "event_msg":
			if PayloadType(obj) == "agent_message" {
				if msg, _ := Payload(obj)["message"].(string); msg != "" {
					d.LastMessage = msg
				}
			}
		}
	})
	d.FirstUserMessage = Truncate(d.FirstUserMessage, 200)
	d.LastMessage = Truncate(d.LastMessage, 200)
	return d
}

// MessageText concatenates the text parts of a response_item message payload.
func MessageText(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	content, _ := payload["content"].([]any)
	var sb strings.Builder
	for _, c := range content {
		cm, ok := c.(map[string]any)
		if !ok {
			continue
		}
		t, _ := cm["type"].(string)
		if t != "input_text" && t != "output_text" && t != "text" {
			continue
		}
		if txt, ok := cm["text"].(string); ok {
			sb.WriteString(txt)
		}
	}
	return sb.String()
}

// FirstUserMessage returns the first visible user message text (long form).
func FirstUserMessage(content []byte) string {
	var found string
	IterLines(content, func(line Line) {
		if found != "" || line.Obj == nil {
			return
		}
		if Type(line.Obj) == "response_item" && PayloadType(line.Obj) == "message" {
			if role, _ := Payload(line.Obj)["role"].(string); role == "user" {
				text := MessageText(Payload(line.Obj))
				if text != "" && !IsHiddenUserText(text) {
					found = text
				}
			}
		}
	})
	if found == "" {
		IterLines(content, func(line Line) {
			if found != "" || line.Obj == nil {
				return
			}
			if Type(line.Obj) == "user_message" {
				if text, _ := Payload(line.Obj)["message"].(string); text != "" && !IsHiddenUserText(text) {
					found = text
				}
			}
		})
	}
	return Truncate(found, 500)
}

// FirstSessionCWD extracts the cwd recorded in session_meta.
func FirstSessionCWD(content []byte) string {
	var cwd string
	IterLines(content, func(line Line) {
		if cwd != "" || line.Obj == nil {
			return
		}
		if Type(line.Obj) == "session_meta" {
			if c, _ := Payload(line.Obj)["cwd"].(string); c != "" {
				cwd = c
			}
		}
	})
	return cwd
}

// SessionCWDs returns the deduplicated set of cwds appearing in
// session_meta/turn_context payloads — used by verify to catch leftover
// sender paths after an import rewrite.
func SessionCWDs(content []byte) map[string]bool {
	out := map[string]bool{}
	IterLines(content, func(line Line) {
		if line.Obj == nil {
			return
		}
		switch Type(line.Obj) {
		case "session_meta", "turn_context":
			if c, _ := Payload(line.Obj)["cwd"].(string); c != "" {
				out[c] = true
			}
		}
	})
	return out
}
