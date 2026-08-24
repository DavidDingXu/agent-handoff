// Package ledger records imported share fingerprints locally so the same
// share can be detected (and optionally skipped) on re-import.
package ledger

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Record is one import entry.
type Record struct {
	Fingerprint    string `json:"fingerprint"`
	SourceAgent    string `json:"source_agent"`
	SourceThread   string `json:"source_thread"`
	ImportedThread string `json:"imported_thread"`
	Title          string `json:"title"`
	ImportedAt     string `json:"imported_at"`
}

// Path returns the ledger file path inside an agent home.
func Path(home string) string {
	return filepath.Join(home, "agent-handoff", "imports.jsonl")
}

// Fingerprint identifies a share by its origin and full content. Re-sharing
// the same thread later (with new messages) yields a different fingerprint,
// which correctly imports as a new task.
func Fingerprint(sourceAgent, sourceThreadID string, sessionBytes []byte) string {
	h := sha256.New()
	h.Write([]byte(sourceAgent))
	h.Write([]byte{0})
	h.Write([]byte(sourceThreadID))
	h.Write([]byte{0})
	h.Write(sessionBytes)
	return hex.EncodeToString(h.Sum(nil))
}

// Find returns the record for an already-imported fingerprint, or nil.
func Find(home, fingerprint string) *Record {
	data, err := os.ReadFile(Path(home))
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var rec Record
		if json.Unmarshal([]byte(line), &rec) == nil && rec.Fingerprint == fingerprint {
			return &rec
		}
	}
	return nil
}

// Record appends an import entry.
func Record_(home, fingerprint, sourceAgent, sourceThread, importedThread, title string) error {
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	rec := Record{
		Fingerprint:    fingerprint,
		SourceAgent:    sourceAgent,
		SourceThread:   sourceThread,
		ImportedThread: importedThread,
		Title:          title,
		ImportedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(data, '\n'))
	return err
}
