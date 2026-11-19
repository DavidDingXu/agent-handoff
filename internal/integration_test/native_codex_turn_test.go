package integration_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const nativeContinuationMarker = "NATIVE_HANDOFF_CONTINUED_fd02"
const nativeContinuationInput = "NATIVE_HANDOFF_NEW_USER_9e21"

func runNativeCodexContinuations(t *testing.T, binary string) {
	t.Run("codex_local_continuation", func(t *testing.T) {
		client := nativeClient(t, "CODEX_BIN", "codex")
		cases := []struct {
			name, source, zip string
			markers           []string
		}{
			{name: "codex", source: bundle.AgentCodex, markers: []string{nativeUserMarker, nativeAssistantMarker}},
			{name: "claude", source: bundle.AgentClaude, markers: []string{nativeUserMarker, nativeAssistantMarker}},
			{name: "dsh", source: bundle.AgentDSH, markers: []string{nativeUserMarker, nativeAssistantMarker, nativeToolMarker}},
		}
		if zip := os.Getenv("AGENT_HANDOFF_NATIVE_DSH_BUNDLE"); zip != "" {
			zip, err := filepath.Abs(zip)
			if err != nil {
				t.Fatal(err)
			}
			raw := nativeCommand(t, binary, t.TempDir(), nativeEnvironment(t), "bridge", "read", zip)
			var read struct {
				Neutral neutral.Transcript `json:"neutral"`
			}
			if err = json.Unmarshal(raw, &read); err != nil {
				t.Fatal(err)
			}
			var markers []string
			for _, role := range []string{"user", "assistant"} {
				for _, e := range read.Neutral.Entries {
					if e.Kind == neutral.KindMessage && e.Role == role && e.Text != "" {
						markers = append(markers, e.Text)
						break
					}
				}
			}
			if len(markers) != 2 {
				t.Fatal("external synthetic bundle must contain both roles")
			}
			cases = append(cases, struct {
				name, source, zip string
				markers           []string
			}{name: "actual_dsh_plugin_export", source: bundle.AgentDSH, zip: zip, markers: markers})
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				var mu sync.Mutex
				var requests []byte
				var modelCalls int
				api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if !strings.HasSuffix(r.URL.Path, "/responses") {
						http.NotFound(w, r)
						return
					}
					body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
					if err != nil {
						http.Error(w, "read failed", 400)
						return
					}
					mu.Lock()
					requests = append(requests, body...)
					modelCalls++
					mu.Unlock()
					writeNativeOpenAIResponse(w)
				}))
				defer api.Close()
				cwd, home := t.TempDir(), t.TempDir()
				env := append(nativeEnvironment(t), "CODEX_HOME="+home, "OPENAI_API_KEY=native-test-local-key")
				// Override the standard source provider by name, so no source credentials
				// or configuration need to be transferred to continue same-agent history.
				config := fmt.Sprintf("model = \"gpt-5.2-codex\"\nmodel_provider = \"handoff-native-local\"\n[model_providers.handoff-native-local]\nname = \"Local native fixture\"\nbase_url = %q\nwire_api = \"responses\"\nrequires_openai_auth = false\nsupports_websockets = false\n", api.URL+"/v1")
				if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(config), 0600); err != nil {
					t.Fatal(err)
				}
				var imported map[string]any
				if tc.zip != "" {
					imported = nativeImportBundle(t, binary, tc.zip, bundle.AgentCodex, home, cwd, env)
				} else {
					imported = nativeShareImport(t, binary, tc.source, bundle.AgentCodex, home, cwd, env, "handoff-native-local")
				}
				id := nativeID(t, imported, "thread_id")
				server := startNativeCodex(t, client, cwd, env)
				server.call(t, "thread/resume", map[string]any{"threadId": id, "cwd": cwd, "excludeTurns": true, "modelProvider": "handoff-native-local"})
				server.call(t, "turn/start", map[string]any{"threadId": id, "input": []map[string]any{{"type": "text", "text": nativeContinuationInput}}, "model": "gpt-5.2-codex", "approvalPolicy": "never"})
				deadline := time.Now().Add(15 * time.Second)
				for {
					read := server.call(t, "thread/read", map[string]any{"threadId": id, "includeTurns": true})
					var result struct {
						Thread struct {
							Turns []struct {
								Status string          `json:"status"`
								Items  json.RawMessage `json:"items"`
							} `json:"turns"`
						} `json:"thread"`
					}
					if err := json.Unmarshal(read, &result); err != nil {
						t.Fatal(err)
					}
					if len(result.Thread.Turns) > 0 {
						last := result.Thread.Turns[len(result.Thread.Turns)-1]
						if last.Status == "completed" && strings.Contains(string(last.Items), nativeContinuationMarker) {
							break
						}
					}
					if time.Now().After(deadline) {
						t.Fatalf("local Responses turn did not complete: %s", read)
					}
					time.Sleep(25 * time.Millisecond)
				}
				mu.Lock()
				captured := string(requests)
				calls := modelCalls
				mu.Unlock()
				if calls != 1 {
					t.Errorf("model calls=%d, want exactly one local continuation", calls)
				}
				for _, marker := range append(append([]string{}, tc.markers...), nativeContinuationInput) {
					if !strings.Contains(captured, marker) {
						t.Errorf("local Responses request lost history %q: %s", marker, captured)
					}
				}
				server.close()
				// Cold-open the persisted native history after the completed new turn.
				cold := startNativeCodex(t, client, cwd, env)
				markers := append(append([]string{}, tc.markers[:2]...), nativeContinuationInput, nativeContinuationMarker)
				read := cold.call(t, "thread/read", map[string]any{"threadId": id, "includeTurns": true})
				for _, marker := range markers {
					if !strings.Contains(string(read), marker) {
						t.Errorf("cold native history lost %q: %s", marker, read)
					}
				}
				cold.close()
				mu.Lock()
				finalCalls := modelCalls
				mu.Unlock()
				if finalCalls != 1 {
					t.Errorf("cold history read unexpectedly made model calls: %d", finalCalls)
				}
			})
		}
	})
}

func writeNativeOpenAIResponse(w http.ResponseWriter) {
	item := map[string]any{"id": "msg_native_fixture", "type": "message", "status": "completed", "role": "assistant", "content": []map[string]any{{"type": "output_text", "text": nativeContinuationMarker, "annotations": []any{}}}}
	response := map[string]any{"id": "resp_native_fixture", "object": "response", "created_at": 1, "status": "completed", "model": "gpt-5.2-codex", "output": []any{item}, "usage": map[string]any{"input_tokens": 20, "output_tokens": 4, "total_tokens": 24}}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, event := range []map[string]any{
		{"type": "response.created", "response": map[string]any{"id": "resp_native_fixture", "object": "response", "created_at": 1, "status": "in_progress", "model": "gpt-5.2-codex", "output": []any{}}},
		{"type": "response.output_item.added", "output_index": 0, "item": map[string]any{"id": "msg_native_fixture", "type": "message", "status": "in_progress", "role": "assistant", "content": []any{}}},
		{"type": "response.output_text.delta", "item_id": "msg_native_fixture", "output_index": 0, "content_index": 0, "delta": nativeContinuationMarker},
		{"type": "response.output_item.done", "output_index": 0, "item": item},
		{"type": "response.completed", "response": response},
	} {
		data, _ := json.Marshal(event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event["type"], data)
	}
}
