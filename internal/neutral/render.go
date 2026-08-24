package neutral

import (
	"encoding/json"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

func marshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func isTrue(v any) bool {
	b, ok := v.(bool)
	return ok && b
}

func clip(s string, n int) string {
	return session.Truncate(s, n)
}

// DedupeMessages drops consecutive duplicate message entries produced by
// dual-representation sessions (event_msg + response_item pairs).
func DedupeMessages(t Transcript) Transcript {
	out := make([]Entry, 0, len(t.Entries))
	var lastMsg string
	for _, e := range t.Entries {
		if e.Kind == KindMessage {
			if e.Text == lastMsg {
				continue
			}
			lastMsg = e.Text
		}
		out = append(out, e)
	}
	t.Entries = out
	return t
}

// VisibleExcerpt returns up to n message entries clipped to limit chars,
// for restore notes and previews.
func VisibleExcerpt(t Transcript, n, limit int) []Entry {
	var out []Entry
	for _, e := range t.Entries {
		if e.Kind != KindMessage {
			continue
		}
		if len(out) == n {
			break
		}
		c := e
		c.Text = clip(e.Text, limit)
		out = append(out, c)
	}
	return out
}

// FirstUserText returns the first user message text.
func FirstUserText(t Transcript) string {
	for _, e := range t.Entries {
		if e.Kind == KindMessage && e.Role == "user" {
			return e.Text
		}
	}
	return ""
}

// LastAssistantText returns the last assistant message text.
func LastAssistantText(t Transcript) string {
	for i := len(t.Entries) - 1; i >= 0; i-- {
		e := t.Entries[i]
		if e.Kind == KindMessage && e.Role == "assistant" {
			return e.Text
		}
	}
	return ""
}

// CountMessages counts message entries.
func CountMessages(t Transcript) int {
	n := 0
	for _, e := range t.Entries {
		if e.Kind == KindMessage {
			n++
		}
	}
	return n
}

// RestoreMarkdown renders the post-import continuation context document that
// travels inside the bundle.
func RestoreMarkdown(t Transcript, targetAgent string) string {
	var sb strings.Builder
	sb.WriteString("# Restore Context\n\n")
	sb.WriteString("- Source agent: " + t.SourceAgent + "\n")
	sb.WriteString("- Source session: " + t.SourceID + "\n")
	if t.Title != "" {
		sb.WriteString("- Title: " + t.Title + "\n")
	}
	if t.SourceCWD != "" {
		sb.WriteString("- Source cwd: " + t.SourceCWD + "\n")
	}
	sb.WriteString("- Target agent: " + targetAgent + "\n")
	sb.WriteString("- Lossless level: same-agent raw, cross-agent semantic\n\n")

	if first := FirstUserText(t); first != "" {
		sb.WriteString("## First User Message\n\n")
		sb.WriteString(first + "\n\n")
	}
	if last := LastAssistantText(t); last != "" {
		sb.WriteString("## Last Agent Message\n\n")
		sb.WriteString(last + "\n\n")
	}
	excerpt := VisibleExcerpt(t, 8, 600)
	if len(excerpt) > 0 {
		sb.WriteString("## Visible Excerpt\n\n")
		for _, e := range excerpt {
			sb.WriteString("### [" + strings.ToUpper(e.Role) + "]\n\n")
			sb.WriteString(e.Text + "\n\n")
		}
	}
	return sb.String()
}
