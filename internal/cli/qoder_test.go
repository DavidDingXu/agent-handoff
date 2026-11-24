package cli

import (
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

func TestQoderAgentRouting(t *testing.T) {
	for _, resolve := range []func(string) (string, error){detectSource, resolveTarget} {
		got, err := resolve("qoder")
		if err != nil || got != "qoder" {
			t.Fatalf("Qoder routing = %q, %v", got, err)
		}
	}
	if !bundle.IsSupportedAgent("qoder") {
		t.Fatal("Qoder bundles cannot be read")
	}
}

func TestQoderNativeHistoryMatchesReviewedTranscript(t *testing.T) {
	id := "40e494f1-f072-4b86-b6cf-1ee5d51f5419"
	history := []byte(`{"type":"user","sessionId":"` + id + `","uuid":"40e494f1-f072-4b86-b6cf-1ee5d51f5420","parentUuid":null,"message":{"role":"user","content":"native question"}}` + "\n")
	b := &bundle.ReadResult{
		Manifest: &bundle.Manifest{SourceAgent: bundle.AgentQoder, SourceThreadID: id},
		Session:  history,
		Neutral: neutral.Transcript{Schema: neutral.Schema, SourceAgent: bundle.AgentQoder, SourceID: id,
			Entries: []neutral.Entry{{Kind: neutral.KindMessage, Role: "user", Text: "reviewed question"}}},
	}
	if err := validateQoderTranscript(b); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatched native history was accepted: %v", err)
	}
	b.Neutral.Entries[0].Text = "native question"
	if err := validateQoderTranscript(b); err != nil {
		t.Fatal(err)
	}
}
