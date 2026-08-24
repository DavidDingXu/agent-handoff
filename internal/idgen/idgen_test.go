package idgen

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestNewThreadIDUUIDv7Shape(t *testing.T) {
	id := NewThreadID()
	// 36 chars: 8-4-4-4-12 with a version-7 marker in the third group.
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !re.MatchString(id) {
		t.Errorf("NewThreadID = %q, want a UUIDv7", id)
	}
}

func TestNewThreadIDsUnique(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		id := NewThreadID()
		if seen[id] {
			t.Fatalf("duplicate thread id %q", id)
		}
		seen[id] = true
	}
}

func TestNewUUIDShape(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for i := 0; i < 20; i++ {
		id := NewUUID()
		if !re.MatchString(id) {
			t.Fatalf("NewUUID = %q, want a UUIDv4", id)
		}
	}
}

func TestImportedTitle(t *testing.T) {
	if got := ImportedTitle("  Real Title  ", "fallback"); got != "[Handoff] Real Title" {
		t.Errorf("ImportedTitle trim = %q", got)
	}
	if got := ImportedTitle("", "fallback"); got != "[Handoff] fallback" {
		t.Errorf("ImportedTitle empty = %q", got)
	}
	if got := ImportedTitle("   ", "fallback"); got != "[Handoff] fallback" {
		t.Errorf("ImportedTitle blank = %q", got)
	}
	if got := ImportedTitle("[Handoff] Real Title", "fallback"); got != "[Handoff] Real Title" {
		t.Errorf("ImportedTitle duplicate prefix = %q", got)
	}
}

func TestRolloutPath(t *testing.T) {
	ts := time.Date(2026, 8, 2, 15, 4, 5, 0, time.UTC)
	p := RolloutPath("/home/.codex", "abc-123", ts)
	want := filepath.Join("/home/.codex", "sessions", "2026", "08", "02", "rollout-2026-08-02T15-04-05-abc-123.jsonl")
	if p != want {
		t.Errorf("RolloutPath = %q, want %q", p, want)
	}
}

func TestNowRFC3339(t *testing.T) {
	s := NowRFC3339()
	if !strings.HasSuffix(s, "Z") && !strings.Contains(s, "+") {
		t.Errorf("NowRFC3339 = %q", s)
	}
	// Millisecond precision: exactly 3 fraction digits.
	re := regexp.MustCompile(`\.\d{3}`)
	if !re.MatchString(s) {
		t.Errorf("NowRFC3339 = %q, want millisecond precision", s)
	}
}
