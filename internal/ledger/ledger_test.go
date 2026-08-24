package ledger

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintStableAndDistinct(t *testing.T) {
	a1 := Fingerprint("codex", "t1", []byte("session"))
	a2 := Fingerprint("codex", "t1", []byte("session"))
	if a1 != a2 {
		t.Error("same input should give same fingerprint")
	}
	if Fingerprint("codex", "t1", []byte("session-v2")) == a1 {
		t.Error("content change should change fingerprint")
	}
	if Fingerprint("claude", "t1", []byte("session")) == a1 {
		t.Error("agent change should change fingerprint")
	}
	if Fingerprint("codex", "t2", []byte("session")) == a1 {
		t.Error("thread change should change fingerprint")
	}
}

func TestRecordAndFind(t *testing.T) {
	home := t.TempDir()
	fp := Fingerprint("codex", "t1", []byte("session"))

	// Empty ledger: no hit.
	if rec := Find(home, fp); rec != nil {
		t.Errorf("empty ledger returned %v", rec)
	}

	if err := Record_(home, fp, "codex", "t1", "imported-1", "Title"); err != nil {
		t.Fatalf("Record_: %v", err)
	}
	rec := Find(home, fp)
	if rec == nil {
		t.Fatal("recorded fingerprint not found")
	}
	if rec.ImportedThread != "imported-1" || rec.Title != "Title" || rec.SourceThread != "t1" || rec.SourceAgent != "codex" {
		t.Errorf("record = %+v", rec)
	}
	if rec.ImportedAt == "" {
		t.Error("imported_at should be set")
	}

	// Unknown fingerprint still misses.
	if rec := Find(home, "unknown"); rec != nil {
		t.Errorf("unknown fingerprint returned %v", rec)
	}
}

func TestRecordAppendsMultiple(t *testing.T) {
	home := t.TempDir()
	for i := 0; i < 3; i++ {
		fp := Fingerprint("codex", "t1", []byte{byte(i)})
		if err := Record_(home, fp, "codex", "t1", "imported-"+string(rune('0'+i)), "T"); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(Path(home))
	if err != nil {
		t.Fatal(err)
	}
	// Three jsonl lines.
	lines := 0
	for _, l := range splitLines(string(data)) {
		if l != "" {
			lines++
		}
	}
	if lines != 3 {
		t.Errorf("ledger lines = %d, want 3", lines)
	}
	// The middle entry is retrievable.
	mid := Fingerprint("codex", "t1", []byte{1})
	if Find(home, mid) == nil {
		t.Error("middle entry not found")
	}
}

func TestPathLayout(t *testing.T) {
	if got := Path("/home/x"); got != filepath.Join("/home/x", "agent-handoff", "imports.jsonl") {
		t.Errorf("Path = %q", got)
	}
}

func TestFindToleratesCorruptLines(t *testing.T) {
	home := t.TempDir()
	path := Path(home)
	os.MkdirAll(filepath.Dir(path), 0o755)
	fp := Fingerprint("codex", "t1", []byte("s"))
	os.WriteFile(path, []byte("not json\n{\"fingerprint\":\""+fp+"\",\"imported_thread\":\"x\"}\n\n"), 0o644)
	rec := Find(home, fp)
	if rec == nil || rec.ImportedThread != "x" {
		t.Errorf("corrupt-line tolerance failed: %v", rec)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
