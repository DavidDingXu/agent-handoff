package integration_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/link"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

// TestURLImportRoundTrip covers the --format link handoff: the zip payload
// is encrypted, uploaded to a (fake) worker, and the share URL alone is
// enough to rebuild and import the bundle on the receiver side.
func TestURLImportRoundTrip(t *testing.T) {
	senderHome := buildCodexHome(t)
	m, sessionBytes, row := exportCodex(t, senderHome)
	tr := neutral.FromCodexSession(m.SourceThreadID, m.Title, m.SourceCWD, sessionBytes)
	path := writeBundle(t, m, sessionBytes, row, tr)
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Fake worker storing one blob.
	var blob []byte
	manifestBody := ""
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/shares", func(w http.ResponseWriter, r *http.Request) {
		mr, err := r.MultipartReader()
		if err != nil {
			w.WriteHeader(400)
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				break
			}
			buf := make([]byte, 0)
			chunk := make([]byte, 32*1024)
			for {
				n, err := part.Read(chunk)
				buf = append(buf, chunk[:n]...)
				if err != nil {
					break
				}
			}
			if part.FormName() == "blob" {
				blob = buf
			} else if part.FormName() == "manifest" {
				manifestBody = string(buf)
			}
		}
		// Patch the manifest to point at the blob URL.
		manifestBody = strings.Replace(manifestBody, `"url": ""`, `"url": "/v1/shares/abc/blob"`, 1)
		w.WriteHeader(201)
		w.Write([]byte(`{"share_url":"/s/abc"}`))
	})
	mux.HandleFunc("GET /v1/shares/abc", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(manifestBody))
	})
	mux.HandleFunc("GET /v1/shares/abc/blob", func(w http.ResponseWriter, r *http.Request) {
		w.Write(blob)
	})
	srv := httptest.NewTLSServer(mux)
	defer srv.Close()

	shareURL, _, err := link.Upload(payload, m.SourceThreadID, m.Title, link.UploadOptions{
		Endpoint: srv.URL,
		Client:   srv.Client(),
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	// Receiver side: parse the URL, download, decrypt, import.
	key, cleanURL, err := link.ParseLinkKey(shareURL)
	if err != nil {
		t.Fatalf("ParseLinkKey: %v", err)
	}
	plaintext, _, err := link.Download(cleanURL, key, srv.Client())
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if string(plaintext) != string(payload) {
		t.Fatal("decrypted payload differs from the original zip")
	}
	b, err := bundle.ReadZipBytes(plaintext)
	if err != nil {
		t.Fatalf("ReadZipBytes: %v", err)
	}
	if err := b.VerifyChecksums(); err != nil {
		t.Fatalf("VerifyChecksums: %v", err)
	}
	if b.Manifest.SourceAgent != bundle.AgentCodex {
		t.Errorf("source agent = %q", b.Manifest.SourceAgent)
	}

	receiverHome := t.TempDir()
	res, err := codex.Restore(codex.RestoreInput{
		SourceThreadID: b.Manifest.SourceThreadID,
		Title:          b.Manifest.Title,
		SessionBytes:   b.Session,
		ThreadRow:      b.Meta,
		Neutral:        b.Neutral,
	}, codex.RestoreOptions{
		Home:      receiverHome,
		TargetCWD: "/recv/project",
		Execute:   true,
		Now:       importTime,
	})
	if err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if v := codex.Verify(receiverHome, res.ThreadID, "/recv/project"); v.Status != "ok" {
		t.Fatalf("verify: %+v", v)
	}
	// The imported rollout exists and carries the conversation.
	rollout := filepath.Join(receiverHome, "sessions", "2026", "08", "02",
		"rollout-2026-08-02T12-00-00-"+res.ThreadID+".jsonl")
	data, err := os.ReadFile(rollout)
	if err != nil {
		t.Fatalf("read rollout: %v", err)
	}
	if !strings.Contains(string(data), "Fix the login bug") {
		t.Error("conversation content lost through the link handoff")
	}
}
