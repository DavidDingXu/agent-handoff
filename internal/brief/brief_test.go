package brief

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

// A generator that invents a summary, selects old updates, or mutates history breaks this contract.
func TestGenerateUsesRecordedExcerptsWithProvenance(t *testing.T) {
	tr := neutral.Transcript{Entries: []neutral.Entry{
		{Kind: neutral.KindMessage, Role: "user", Text: "Fix login"},
		{Kind: neutral.KindMessage, Role: "assistant", Text: "Looking"},
		{Kind: neutral.KindTool, Tool: "go test", Status: "failed", Output: "login fails"},
		{Kind: neutral.KindMessage, Role: "user", Text: "Keep the API"},
		{Kind: neutral.KindMessage, Role: "assistant", Text: "Added regression"},
	}}
	before := append([]neutral.Entry(nil), tr.Entries...)
	b := Generate(tr)
	if b.Schema != "agent-handoff.brief.v1" || b.Objective.Text != "Fix login" || b.Objective.EntryIndex != 0 || b.LatestRequest.Text != "Keep the API" || b.LatestRequest.EntryIndex != 3 || b.LatestUpdate.Text != "Added regression" || b.LatestUpdate.EntryIndex != 4 {
		t.Fatalf("wrong excerpts: %+v", b)
	}
	if len(b.Evidence) != 1 || b.Evidence[0].EntryIndex != 2 || b.Evidence[0].Output != "login fails" || b.Evidence[0].Status != "failed" {
		t.Fatalf("wrong evidence: %+v", b.Evidence)
	}
	if !reflect.DeepEqual(before, tr.Entries) {
		t.Fatal("mutated transcript")
	}
	b.Notes = "Hand over after review"
	md := RenderMarkdown(b)
	if !strings.Contains(md, "Keep the API") || !strings.Contains(md, "entry 3") || !strings.Contains(md, "Hand over after review") {
		t.Fatalf("missing recorded context: %s", md)
	}
}

func TestGenerateBoundsUnicodeAndRecentEvidence(t *testing.T) {
	tr := neutral.Transcript{Entries: []neutral.Entry{{Kind: neutral.KindMessage, Role: "user", Text: strings.Repeat("中", 20000)}}}
	for i := 0; i < 20; i++ {
		tr.Entries = append(tr.Entries, neutral.Entry{Kind: neutral.KindTool, Tool: "test", Output: strings.Repeat("z", 100000)})
	}
	b := Generate(tr)
	if !b.Objective.Truncated || !utf8.ValidString(b.Objective.Text) || len(b.Objective.Text) > 8192 {
		t.Fatal("excerpt not bounded UTF-8")
	}
	if len(b.Evidence) != 6 || b.Evidence[0].EntryIndex != 15 || len(b.Evidence[0].Output) > 4096 {
		t.Fatalf("evidence not recent or bounded: %+v", b.Evidence)
	}
	if err := Validate(b); err != nil {
		t.Fatal(err)
	}
	b.Objective.EntryIndex = -1
	if err := Validate(b); err == nil {
		t.Fatal("accepted invalid provenance")
	}
}
