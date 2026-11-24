package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestAppEntrypoint(t *testing.T) {
	app := newServer(t.TempDir(), nil)
	result, err := app.handle(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{resourceURI, `"qoder/ui"`, `"type":"global"`, `"visibility":["app"]`} {
		if !strings.Contains(string(data), fragment) {
			t.Fatalf("missing %s in %s", fragment, data)
		}
	}
	result, err = app.handle(context.Background(), "resources/read", json.RawMessage(`{"uri":"`+resourceURI+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	data, _ = json.Marshal(result)
	if !strings.Contains(string(data), "text/html;profile=mcp-app") || !strings.Contains(string(data), "ui/initialize") {
		t.Fatalf("invalid application resource")
	}
}

func TestImportUsesReviewedBytesAndSingleUseTicket(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "fixture.agent-handoff.zip")
	if err := os.WriteFile(path, []byte("reviewed"), 0o600); err != nil {
		t.Fatal(err)
	}
	var calls []string
	app := newServer(t.TempDir(), func(_ context.Context, args ...string) (map[string]any, error) {
		calls = append(calls, args[0])
		data, err := os.ReadFile(args[1])
		if err != nil || string(data) != "reviewed" {
			t.Fatalf("lost reviewed snapshot: %q (%v)", data, err)
		}
		if args[0] == "preview" {
			return map[string]any{"title": "Synthetic fixture", "checksum_status": "ok", "fingerprint": "fixture-fingerprint"}, nil
		}
		want := []string{"--target", "qoder", "--cwd", dir, "--expected-fingerprint", "fixture-fingerprint", "--execute"}
		if !reflect.DeepEqual(args[2:], want) {
			t.Fatalf("import args %v, want %v", args[2:], want)
		}
		return map[string]any{"status": "imported", "thread_id": "fixture-id"}, nil
	})
	preview, err := app.preview(context.Background(), path, dir)
	if err != nil {
		t.Fatal(err)
	}
	ticket, ok := preview["ticket"].(string)
	if !ok || ticket == "" {
		t.Fatal("missing confirmation ticket")
	}
	if err := os.WriteFile(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.importTask(context.Background(), ticket); err != nil {
		t.Fatal(err)
	}
	if _, err := app.importTask(context.Background(), ticket); err == nil {
		t.Fatal("reused confirmation ticket")
	}
	if want := []string{"preview", "import"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls %v, want %v", calls, want)
	}
}

func TestPreviewExpiresAndNeverPreviewsUncheckedBundle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.agent-handoff.zip")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newServer(t.TempDir(), func(_ context.Context, args ...string) (map[string]any, error) {
		return map[string]any{"checksum_status": "mismatch"}, nil
	})
	if _, err := app.preview(context.Background(), path, dir); err == nil {
		t.Fatal("invalid checksum produced a ticket")
	}
	app.tickets["expired"] = previewTicket{created: time.Now().Add(-time.Hour), cwd: dir}
	if _, err := app.importTask(context.Background(), "expired"); err == nil {
		t.Fatal("expired ticket imported")
	}
}

func TestCodexAppEntrypointAndTools(t *testing.T) {
	var calls [][]string
	app := newServer(t.TempDir(), func(_ context.Context, args ...string) (map[string]any, error) {
		calls = append(calls, args)
		return map[string]any{"tasks": []any{
			map[string]any{"id": "1", "title": "First task", "cwd": "/workspace"},
			map[string]any{"id": "2", "title": "Other task", "cwd": "/other"},
		}}, nil
	})
	app.host = "codex"
	result, err := app.handle(context.Background(), "tools/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	discovery, _ := json.Marshal(result)
	for _, value := range []string{"open_handoff", "openai/ui", "list_codex_tasks", "share_codex_task", "import_codex_task", resourceURI} {
		if !strings.Contains(string(discovery), value) {
			t.Fatalf("Codex discovery omitted %s", value)
		}
	}
	if strings.Contains(string(discovery), "qoder/ui") || strings.Contains(string(discovery), "list_qoder_tasks") {
		t.Fatal("Codex discovery exposed Qoder operations")
	}
	list, err := app.callTool(context.Background(), "list_codex_tasks", json.RawMessage(`{"query":"FIRST"}`))
	if err != nil || len(list["tasks"].([]any)) != 1 || len(calls) != 1 || !reflect.DeepEqual(calls[0][:3], []string{"tasks", "--source", "codex"}) {
		t.Fatalf("Codex list: %v, args %v, err %v", list, calls, err)
	}
}
