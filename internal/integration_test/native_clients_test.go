package integration_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	nativeUserMarker      = "NATIVE_HANDOFF_USER_7c93"
	nativeAssistantMarker = "NATIVE_HANDOFF_ASSISTANT_3e81"
)

// These opt-in tests exercise installed clients, not just our interpretation of
// their file formats. CI installs the latest CLIs; local overrides are optional:
// AGENT_HANDOFF_NATIVE_TESTS=1 CODEX_BIN=/path/to/codex CLAUDE_BIN=/path/to/claude
// go test ./internal/integration_test -run TestNativeClients -count=1 -v
func TestNativeClients(t *testing.T) {
	if os.Getenv("AGENT_HANDOFF_NATIVE_TESTS") != "1" {
		t.Skip("set AGENT_HANDOFF_NATIVE_TESTS=1 to test installed Codex and Claude Code")
	}
	binary := buildCLIBinary(t)
	t.Run("codex", func(t *testing.T) {
		client := nativeClient(t, "CODEX_BIN", "codex")
		for _, config := range []struct {
			name, text, provider string
		}{
			{name: "default", provider: "openai"},
			{name: "top_level_provider", provider: "handoff-test-provider", text: `model_provider = "handoff-test-provider"
model = "handoff-test-model"
`},
		} {
			for _, initialized := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/initialized_home_%t", config.name, initialized), func(t *testing.T) {
					cwd, home := t.TempDir(), t.TempDir()
					if config.text != "" {
						contents := config.text + `
[model_providers.handoff-test-provider]
name = "Handoff test"
base_url = "http://127.0.0.1:1/v1"
wire_api = "responses"
`
						if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(contents), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					env := nativeEnvironment(t)
					env = append(env, "CODEX_HOME="+home)
					if initialized {
						server := startNativeCodex(t, client, cwd, env)
						server.call(t, "thread/list", map[string]any{"limit": 1})
						server.close()
					}
					// For the fresh-home case, the importer runs before the native
					// client has created any database or applied any migrations.
					imported := nativeShareImport(t, binary, "claude", "codex", home, cwd, env)
					threadID := nativeID(t, imported, "thread_id")
					server := startNativeCodex(t, client, cwd, env)
					read := server.call(t, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true})
					var history struct {
						Thread struct {
							Turns []struct {
								Status string          `json:"status"`
								Items  json.RawMessage `json:"items"`
							} `json:"turns"`
						} `json:"thread"`
					}
					if err := json.Unmarshal(read, &history); err != nil {
						t.Fatal(err)
					}
					if len(history.Thread.Turns) == 0 {
						t.Fatalf("thread/read returned no imported turns: %s", read)
					}
					var items []byte
					for _, turn := range history.Thread.Turns {
						if turn.Status != "completed" {
							t.Errorf("imported turn status = %q, want completed", turn.Status)
						}
						items = append(items, turn.Items...)
					}
					for _, marker := range []string{nativeUserMarker, nativeAssistantMarker} {
						if !strings.Contains(string(items), marker) {
							t.Errorf("thread/read lost imported history %q: %s", marker, read)
						}
					}
					resumed := server.call(t, "thread/resume", map[string]any{"threadId": threadID, "cwd": cwd, "excludeTurns": true})
					var resume struct {
						ModelProvider string `json:"modelProvider"`
					}
					if err := json.Unmarshal(resumed, &resume); err != nil {
						t.Fatal(err)
					}
					if resume.ModelProvider != config.provider {
						t.Errorf("thread/resume modelProvider = %q, want receiver's configured provider %q", resume.ModelProvider, config.provider)
					}
					listed := server.call(t, "thread/list", map[string]any{"limit": 20, "sourceKinds": []string{}})
					var list struct {
						Data []struct {
							ID string `json:"id"`
						} `json:"data"`
					}
					if err := json.Unmarshal(listed, &list); err != nil {
						t.Fatal(err)
					}
					for _, thread := range list.Data {
						if thread.ID == threadID {
							return
						}
					}
					t.Fatalf("imported thread %s is missing from native thread/list: %s", threadID, listed)
				})
			}
		}
	})
	t.Run("claude", func(t *testing.T) {
		client := nativeClient(t, "CLAUDE_BIN", "claude")
		cwd, home := filepath.Join(t.TempDir(), "receiver's project"), t.TempDir()
		if err := os.MkdirAll(cwd, 0o700); err != nil {
			t.Fatal(err)
		}
		cwd, err := filepath.EvalSymlinks(cwd)
		if err != nil {
			t.Fatal(err)
		}
		otherCWD, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		env := append(nativeEnvironment(t), "CLAUDE_CONFIG_DIR="+home)
		imported := nativeShareImport(t, binary, "codex", "claude", home, cwd, env)
		// Run the returned shell command from another directory. Add only fixed
		// noninteractive options; the native client must load history and enter
		// the receiver's project through the command users actually receive.
		fallback, _ := imported["fallback_command"].(string)
		if !strings.Contains(fallback, "claude ") {
			t.Fatalf("unexpected fallback command: %q", fallback)
		}
		command := strings.Replace(fallback, "claude ", "claude --bare -p 'Continue the saved conversation.' --model claude-sonnet-4-5-20250929 --output-format json --tools= --permission-mode plan --setting-sources= ", 1)
		shell, _ := imported["fallback_shell"].(string)
		var args []string
		switch shell {
		case "sh":
			args = []string{"-c", command}
		case "powershell":
			args = []string{"-NoProfile", "-NonInteractive", "-Command", command}
		default:
			t.Fatalf("unexpected fallback shell: %q", shell)
		}
		var mu sync.Mutex
		var history []byte
		var system strings.Builder
		api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var request struct {
				Messages json.RawMessage `json:"messages"`
				Stream   bool            `json:"stream"`
				System   []struct {
					Text string `json:"text"`
				} `json:"system"`
			}
			if err := json.Unmarshal(body, &request); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if strings.Contains(r.URL.Path, "count_tokens") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"input_tokens":20}`)
				return
			}
			if !strings.HasSuffix(r.URL.Path, "/messages") {
				http.NotFound(w, r)
				return
			}
			mu.Lock()
			history = append(history, request.Messages...)
			for _, part := range request.System {
				system.WriteString(part.Text)
			}
			mu.Unlock()
			writeNativeAnthropicResponse(w, request.Stream)
		}))
		defer api.Close()
		env = append(env, "ANTHROPIC_BASE_URL="+api.URL, "ANTHROPIC_API_KEY=native-test-local-key",
			"DISABLE_AUTOUPDATER=1", "DISABLE_TELEMETRY=1", "DISABLE_ERROR_REPORTING=1",
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1", "PATH="+filepath.Dir(client)+string(os.PathListSeparator)+os.Getenv("PATH"))
		nativeCommand(t, shell, otherCWD, env, args...)
		mu.Lock()
		defer mu.Unlock()
		for _, marker := range []string{nativeUserMarker, nativeAssistantMarker} {
			if !strings.Contains(string(history), marker) {
				t.Errorf("native Claude request lost imported history %q: %s", marker, history)
			}
		}
		if !strings.Contains(system.String(), cwd) || strings.Contains(system.String(), otherCWD) {
			t.Errorf("native Claude did not use receiver cwd %q (started in %q): %s", cwd, otherCWD, system.String())
		}
	})
}

func nativeClient(t *testing.T, override, name string) string {
	t.Helper()
	path := os.Getenv(override)
	if path == "" {
		path = name
	}
	path, err := exec.LookPath(path)
	if err != nil {
		t.Fatalf("native tests require %s (or set %s): %v", name, override, err)
	}
	t.Logf("%s: %s", name, strings.TrimSpace(string(nativeCommand(t, path, t.TempDir(), nativeEnvironment(t), "--version"))))
	return path
}

// Do not inherit credentials, agent settings, proxy credentials, Node preload
// hooks, or real user directories. Both clients run outside the checkout, with
// synthetic homes and an unreachable proxy for non-loopback HTTP requests.
func nativeEnvironment(t *testing.T) []string {
	t.Helper()
	var env []string
	for _, key := range []string{"PATH", "SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "TMPDIR", "TMP", "TEMP", "LANG", "LC_ALL"} {
		if value := os.Getenv(key); value != "" {
			env = append(env, key+"="+value)
		}
	}
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		env = append(env, key+"="+home)
	}
	return append(env, "HTTP_PROXY=http://127.0.0.1:1", "HTTPS_PROXY=http://127.0.0.1:1", "NO_PROXY=127.0.0.1,localhost")
}

func nativeCommand(t *testing.T, binary, cwd string, env []string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Dir, cmd.Env, cmd.WaitDelay = cwd, env, 3*time.Second
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v (context: %v)\n%s", binary, args, err, ctx.Err(), out)
	}
	return out
}

func nativeShareImport(t *testing.T, binary, source, target, home, cwd string, env []string) map[string]any {
	t.Helper()
	sender, id := buildCLIHome(t, source)
	// Existing fixtures contain tool evidence as well as both conversation
	// roles. Distinct markers detect loss of either role independently.
	err := filepath.WalkDir(sender, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		content := strings.ReplaceAll(string(data), "Fix the login bug", nativeUserMarker)
		content = strings.ReplaceAll(content, "Found the bug in auth.go:42.", nativeAssistantMarker)
		return os.WriteFile(path, []byte(content), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	zip := filepath.Join(t.TempDir(), "synthetic.agent-handoff.zip")
	nativeCommand(t, binary, cwd, env, "share", "--source", source, "--thread", id, "--home", sender, "--format", "zip", "--out", zip)
	out := nativeCommand(t, binary, cwd, env, "import", zip, "--target", target, "--home", home, "--cwd", cwd, "--execute")
	var imported map[string]any
	if err := json.Unmarshal(out, &imported); err != nil {
		t.Fatal(err)
	}
	if imported["status"] != "ok" {
		t.Fatalf("import failed: %s", out)
	}
	return imported
}

func nativeID(t *testing.T, result map[string]any, key string) string {
	t.Helper()
	id, _ := result[key].(string)
	if id == "" {
		t.Fatalf("missing %s: %#v", key, result)
	}
	return id
}

type nativeCodex struct {
	stdin     io.WriteCloser
	responses chan json.RawMessage
	done      chan struct{}
	cancel    context.CancelFunc
	closeOnce sync.Once
	nextID    int
}

func startNativeCodex(t *testing.T, binary, cwd string, env []string) *nativeCodex {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	cmd := exec.CommandContext(ctx, binary, "app-server", "--stdio")
	cmd.Dir, cmd.Env, cmd.WaitDelay = cwd, env, 3*time.Second
	stderrPath := filepath.Join(t.TempDir(), "app-server.stderr")
	stderr, err := os.Create(stderrPath)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	cmd.Stderr = stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		stderr.Close()
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		stdin.Close()
		stderr.Close()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		cancel()
		stdin.Close()
		stdout.Close()
		stderr.Close()
		t.Fatal(err)
	}
	s := &nativeCodex{stdin: stdin, responses: make(chan json.RawMessage, 32), done: make(chan struct{}), cancel: cancel}
	go func() {
		defer close(s.responses)
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 8<<20)
		for scanner.Scan() {
			line := append(json.RawMessage(nil), scanner.Bytes()...)
			select {
			case s.responses <- line:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		_ = cmd.Wait()
		close(s.done)
	}()
	t.Cleanup(func() {
		s.close()
		stderr.Close()
		if t.Failed() {
			data, _ := os.ReadFile(stderrPath)
			t.Logf("app-server stderr: %s", data)
		}
	})
	s.call(t, "initialize", map[string]any{"clientInfo": map[string]string{"name": "agent_handoff_native_test", "version": "1"}, "capabilities": map[string]bool{"experimentalApi": true}})
	if _, err := io.WriteString(stdin, "{\"method\":\"initialized\"}\n"); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *nativeCodex) close() {
	s.closeOnce.Do(func() {
		s.stdin.Close()
		select {
		case <-s.done:
		case <-time.After(3 * time.Second):
			s.cancel()
			<-s.done
		}
		s.cancel()
	})
}

func (s *nativeCodex) call(t *testing.T, method string, params any) json.RawMessage {
	t.Helper()
	s.nextID++
	request, err := json.Marshal(map[string]any{"id": s.nextID, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.stdin.Write(append(request, '\n')); err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case line, ok := <-s.responses:
			if !ok {
				t.Fatalf("app-server exited before responding to %s", method)
			}
			var response struct {
				ID     int             `json:"id"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := json.Unmarshal(line, &response); err != nil {
				t.Fatalf("invalid app-server JSON: %v: %s", err, line)
			}
			if response.ID != s.nextID {
				continue
			}
			if len(response.Error) != 0 {
				t.Fatalf("%s: %s", method, response.Error)
			}
			return response.Result
		case <-timeout.C:
			t.Fatalf("app-server timed out responding to %s", method)
		}
	}
}

func writeNativeAnthropicResponse(w http.ResponseWriter, stream bool) {
	message := `{"id":"msg_native_test","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929","content":[{"type":"text","text":"Local test response."}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":20,"output_tokens":4}}`
	if !stream {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, message)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, data := range []string{
		`{"type":"message_start","message":{"id":"msg_native_test","type":"message","role":"assistant","model":"claude-sonnet-4-5-20250929","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":20,"output_tokens":0}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Local test response."}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn","stop_sequence":null},"usage":{"output_tokens":4}}`,
		`{"type":"message_stop"}`,
	} {
		var event struct {
			Type string `json:"type"`
		}
		_ = json.Unmarshal([]byte(data), &event)
		_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event.Type, data)
	}
}
