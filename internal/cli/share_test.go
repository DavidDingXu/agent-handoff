package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
)

func TestAddZipFallbackReturnsExistingArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.agent-handoff.zip")
	if err := os.WriteFile(path, []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := map[string]any{"title": "Task"}

	if err := addZipFallback(result, path, "no link service configured"); err != nil {
		t.Fatal(err)
	}

	if result["status"] != "fallback_zip" {
		t.Errorf("status = %v", result["status"])
	}
	if result["fallback"] != "no link service configured" {
		t.Errorf("fallback = %v", result["fallback"])
	}
	if result["path"] != path {
		t.Errorf("path = %v, want %s", result["path"], path)
	}
	if result["size_bytes"] != int64(3) {
		t.Errorf("size_bytes = %v", result["size_bytes"])
	}
}

func TestWriteZipFallbackCreatesPrivateArtifact(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task.agent-handoff.zip")
	result := map[string]any{"title": "Task"}
	in := bundle.WriterInput{Manifest: &bundle.Manifest{
		FormatVersion: bundle.Version2,
		ArtifactType:  bundle.ArtifactType,
		SourceAgent:   bundle.AgentCodex,
	}}

	if err := writeZipFallback(result, path, in, "providers unavailable"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("fallback mode = %o, want 600", got)
	}
}
