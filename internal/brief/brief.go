// Package brief extracts bounded, attributed handoff context without a model.
// Excerpts are recorded text, not claims about completion or a generated summary.
package brief

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const Schema = "agent-handoff.brief.v1"

const (
	MaxTextBytes     = 8192
	MaxNotesBytes    = 8192
	MaxEvidence      = 6
	MaxEvidenceBytes = 4096
)

// Excerpt references a zero-based entry in agent/neutral.json.
type Excerpt struct {
	EntryIndex int    `json:"entry_index"`
	Text       string `json:"text"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type Evidence struct {
	EntryIndex int    `json:"entry_index"`
	Tool       string `json:"tool"`
	Status     string `json:"status,omitempty"`
	Input      string `json:"input,omitempty"`
	Output     string `json:"output,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
}

type Brief struct {
	Schema        string     `json:"schema"`
	Objective     *Excerpt   `json:"objective,omitempty"`
	LatestRequest *Excerpt   `json:"latest_request,omitempty"`
	LatestUpdate  *Excerpt   `json:"latest_update,omitempty"`
	Evidence      []Evidence `json:"evidence"`
	Notes         string     `json:"notes,omitempty"`
}

// Generate copies the first/last user message, last assistant message, and
// six most recent tool records. It never modifies the source transcript.
func Generate(t neutral.Transcript) Brief {
	b := Brief{Schema: Schema, Evidence: []Evidence{}}
	for i, e := range t.Entries {
		if e.Kind == neutral.KindMessage && e.Text != "" {
			text, truncated := bounded(e.Text, MaxTextBytes)
			x := &Excerpt{EntryIndex: i, Text: text, Truncated: truncated}
			if e.Role == "user" {
				if b.Objective == nil {
					first := *x
					b.Objective = &first
				}
				b.LatestRequest = x
			} else if e.Role == "assistant" {
				b.LatestUpdate = x
			}
		}
		if e.Kind == neutral.KindTool {
			input, ti := bounded(e.Input, MaxEvidenceBytes)
			output, to := bounded(e.Output, MaxEvidenceBytes)
			tool, tt := bounded(e.Tool, 256)
			status, ts := bounded(e.Status, 64)
			b.Evidence = append(b.Evidence, Evidence{EntryIndex: i, Tool: tool, Status: status, Input: input, Output: output, Truncated: ti || to || tt || ts})
			if len(b.Evidence) > MaxEvidence {
				b.Evidence = b.Evidence[1:]
			}
		}
	}
	return b
}

// WithNotes adds caller-supplied plain text. Validate enforces the note limit;
// user notes are never silently truncated.
func WithNotes(t neutral.Transcript, notes string) Brief { b := Generate(t); b.Notes = notes; return b }

func bounded(s string, n int) (string, bool) {
	if len(s) <= n {
		return s, false
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n], true
}

func Validate(b Brief) error {
	if b.Schema != Schema {
		return fmt.Errorf("unsupported brief schema %q", b.Schema)
	}
	if len(b.Notes) > MaxNotesBytes || !utf8.ValidString(b.Notes) {
		return fmt.Errorf("brief notes exceed %d bytes or contain invalid UTF-8", MaxNotesBytes)
	}
	for _, x := range []*Excerpt{b.Objective, b.LatestRequest, b.LatestUpdate} {
		if x != nil && (x.EntryIndex < 0 || len(x.Text) > MaxTextBytes || !utf8.ValidString(x.Text)) {
			return fmt.Errorf("invalid brief excerpt")
		}
	}
	if len(b.Evidence) > MaxEvidence {
		return fmt.Errorf("too many brief evidence entries")
	}
	last := -1
	for _, e := range b.Evidence {
		if e.EntryIndex <= last || len(e.Tool) > 256 || len(e.Status) > 64 || len(e.Input) > MaxEvidenceBytes || len(e.Output) > MaxEvidenceBytes || !utf8.ValidString(e.Tool+e.Status+e.Input+e.Output) {
			return fmt.Errorf("invalid brief evidence")
		}
		last = e.EntryIndex
	}
	return nil
}

// ValidateTranscript verifies provenance and exact bounded excerpts. Notes are
// the sole caller-authored field; recorded excerpts cannot be overridden with
// inferred claims, even when their JSON and Markdown agree.
func ValidateTranscript(b Brief, t neutral.Transcript) error {
	if err := Validate(b); err != nil {
		return err
	}
	want := Generate(t)
	for _, pair := range [][2]*Excerpt{{b.Objective, want.Objective}, {b.LatestRequest, want.LatestRequest}, {b.LatestUpdate, want.LatestUpdate}} {
		if (pair[0] == nil) != (pair[1] == nil) || pair[0] != nil && *pair[0] != *pair[1] {
			return fmt.Errorf("brief excerpt does not match recorded transcript")
		}
	}
	if len(b.Evidence) != len(want.Evidence) {
		return fmt.Errorf("brief evidence does not match recorded transcript")
	}
	for i, e := range b.Evidence {
		if e != want.Evidence[i] {
			return fmt.Errorf("brief evidence does not match recorded transcript")
		}
	}
	return nil
}

// RenderMarkdown quotes all supplied text to keep its status as untrusted
// recorded evidence apparent, including Markdown-looking user notes.
func RenderMarkdown(b Brief) string {
	var out strings.Builder
	out.WriteString("# Handoff brief / 交接简报\n\nRecorded excerpts only; no inferred completion status.\n仅摘录已有记录，不推断完成状态。 Entry indices are zero-based in agent/neutral.json.\n\n")
	for _, section := range []struct {
		name string
		x    *Excerpt
	}{{"Objective / 原始目标", b.Objective}, {"Latest request / 最新请求", b.LatestRequest}, {"Latest update / 最近回复", b.LatestUpdate}} {
		if section.x == nil {
			continue
		}
		fmt.Fprintf(&out, "## %s (entry %d)\n\n", section.name, section.x.EntryIndex)
		quote(&out, section.x.Text)
		if section.x.Truncated {
			out.WriteString("\n[Excerpt truncated / 摘录已截断]\n")
		}
		out.WriteString("\n")
	}
	if len(b.Evidence) > 0 {
		out.WriteString("## Recent tool records / 最近工具记录\n\n")
	}
	for _, e := range b.Evidence {
		fmt.Fprintf(&out, "### Tool record (entry %d)\n\n", e.EntryIndex)
		quote(&out, e.Tool+" ("+e.Status+")")
		if e.Input != "" {
			out.WriteString("\nInput / 输入:\n\n")
			quote(&out, e.Input)
		}
		if e.Output != "" {
			out.WriteString("\nOutput / 输出:\n\n")
			quote(&out, e.Output)
		}
		if e.Truncated {
			out.WriteString("\n[Excerpt truncated / 摘录已截断]\n")
		}
		out.WriteString("\n")
	}
	if b.Notes != "" {
		out.WriteString("## User notes / 用户补充\n\n")
		quote(&out, b.Notes)
	}
	return out.String()
}

func quote(out *strings.Builder, s string) {
	for _, line := range strings.Split(s, "\n") {
		out.WriteString("> " + line + "\n")
	}
}
