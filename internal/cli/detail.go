package cli

import (
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// claudeDetail extracts a preview Detail from a Claude session jsonl by
// mapping it onto the same shape used for Codex sessions.
func claudeDetail(content []byte) session.Detail {
	var d session.Detail
	var firstUser, lastMsg string
	session.IterLines(content, func(line session.Line) {
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
		isMeta := isBoolTrue(obj["isMeta"]) || isBoolTrue(obj["isSidechain"])
		if isMeta {
			return
		}
		msg, _ := obj["message"].(map[string]any)
		if msg == nil {
			return
		}
		role, _ := msg["role"].(string)
		text := claudeMessageText(msg)
		if text == "" {
			return
		}
		if role == "user" && firstUser == "" && !session.IsHiddenUserText(text) {
			firstUser = text
		}
		lastMsg = text
	})
	d.FirstUserMessage = session.Truncate(firstUser, 200)
	d.LastMessage = session.Truncate(lastMsg, 200)
	return d
}

func claudeMessageText(msg map[string]any) string {
	if s, ok := msg["content"].(string); ok {
		return s
	}
	parts, _ := msg["content"].([]any)
	var texts []string
	for _, p := range parts {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if t, _ := pm["type"].(string); t == "text" {
			if txt, _ := pm["text"].(string); txt != "" {
				texts = append(texts, txt)
			}
		}
	}
	return strings.Join(texts, "\n")
}

func isBoolTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
