package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowserPanelLimitsRPCToItsPrivateOrigin(t *testing.T) {
	const origin = "http://127.0.0.1:4321"
	const path = "/private-test-token/"
	calls := 0
	app := newServer(t.TempDir(), func(_ context.Context, args ...string) (map[string]any, error) {
		calls++
		return map[string]any{"status": "ok", "tasks": []any{}}, nil
	})
	app.host = "codex"
	handler := app.browserHandler(path, origin)

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, origin+path, nil))
	if page.Code != http.StatusOK || page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("browser page response: %v", page.Result())
	}

	for _, tc := range []struct {
		name, target, requestOrigin string
		want                        int
	}{
		{"cross origin", origin + path + "rpc", "https://example.com", http.StatusForbidden},
		{"unknown capability", origin + "/wrong/rpc", origin, http.StatusNotFound},
		{"other host", "http://example.com" + path + "rpc", origin, http.StatusForbidden},
		{"same origin", origin + path + "rpc", origin, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.target, strings.NewReader(
				`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_codex_tasks","arguments":{}}}`))
			req.Header.Set("Origin", tc.requestOrigin)
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, req)
			if response.Code != tc.want {
				t.Fatalf("status = %d, want %d", response.Code, tc.want)
			}
		})
	}
	if calls != 1 {
		t.Fatalf("processed %d catalog operations, want 1", calls)
	}
}

func TestBrowserPanelRequiresUploadedZIP(t *testing.T) {
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(workspace, "local.zip")
	if err := os.WriteFile(path, []byte("synthetic ZIP"), 0o600); err != nil {
		t.Fatal(err)
	}
	app := newServer(t.TempDir(), func(context.Context, ...string) (map[string]any, error) {
		t.Fatal("browser preview must not read an arbitrary local file")
		return nil, nil
	})
	app.browserHandler("/test/", "http://127.0.0.1:1234")
	if _, err := app.preview(context.Background(), path, workspace); err == nil {
		t.Fatal("browser preview accepted a direct local path")
	}
}
