package main

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPanelUploadPreviewsOnlyTheCompleteLocalZIP(t *testing.T) {
	const payload = "synthetic zip"
	previewCalls := 0
	app := newServer(t.TempDir(), func(_ context.Context, args ...string) (map[string]any, error) {
		previewCalls++
		value, err := os.ReadFile(args[1])
		if err != nil || string(value) != payload {
			t.Fatalf("preview payload = %q, error = %v", value, err)
		}
		return map[string]any{"checksum_status": "ok", "fingerprint": "reviewed"}, nil
	})
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	opened, err := app.upload(map[string]string{"size": "13"})
	if err != nil {
		t.Fatal(err)
	}
	id := opened["upload_id"].(string)
	first, err := app.upload(map[string]string{"upload_id": id, "data": base64.StdEncoding.EncodeToString([]byte("synthetic"))})
	if err != nil || first["source"] != nil {
		t.Fatalf("partial upload = %v, %v", first, err)
	}
	if _, err := app.preview(context.Background(), uploadPrefix+id, workspace); err == nil || previewCalls != 0 {
		t.Fatal("an incomplete ZIP reached preview")
	}
	final, err := app.upload(map[string]string{"upload_id": id, "data": base64.StdEncoding.EncodeToString([]byte(" zip"))})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := app.preview(context.Background(), final["source"].(string), workspace)
	if err != nil || preview["ticket"] == nil || previewCalls != 1 {
		t.Fatalf("final preview = %v, %v", preview, err)
	}
	app.expireUploads(time.Now().Add(ticketLifetime * 2))
	if _, err := app.uploadedZIP(uploadPrefix+id, time.Now()); err == nil {
		t.Fatal("expired upload remained available")
	}
}
