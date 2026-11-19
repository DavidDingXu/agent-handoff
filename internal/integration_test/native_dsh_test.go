package integration_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

const nativeToolMarker = "NATIVE_HANDOFF_TOOL_EVIDENCE_41be"

// This fixture was produced by the pinned rc5 SDK Session append API through
// the plugin's fromNeutral/nativeEnvelope/toNeutral functions. It contains only
// synthetic text and paired historical tool evidence, never a real user log.
func nativeDSHBridgeShare(t *testing.T, binary, cwd string, env []string) string {
	t.Helper()
	input, err := os.ReadFile(filepath.Join("testdata", "dsh-bridge-share.json"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	request, zip := filepath.Join(dir, "request.json"), filepath.Join(dir, "dsh.agent-handoff.zip")
	if err := os.WriteFile(request, input, 0600); err != nil {
		t.Fatal(err)
	}
	out := nativeCommand(t, binary, cwd, env, "bridge", "share", "--input", request, "--out", zip)
	var result map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		t.Fatal(err)
	}
	if result["schema"] != "agent-handoff.bridge.share-result.v1" || result["status"] != "ok" {
		t.Fatalf("bridge export failed: %s", out)
	}
	return zip
}

// These run inside TestNativeClients, so the existing opt-in native CI command
// covers the new directions without requiring a separately enabled test suite.
func runAdditionalNativeSources(t *testing.T, binary string) {
	t.Run("additional_sources", func(t *testing.T) {
		codexClient := nativeClient(t, "CODEX_BIN", "codex")
		for _, source := range []string{bundle.AgentCodex, bundle.AgentDSH} {
			for _, initialized := range []bool{true, false} {
				name := source + "_to_codex/fresh_home"
				if initialized {
					name = source + "_to_codex/initialized_home"
				}
				t.Run(name, func(t *testing.T) {
					cwd, home := t.TempDir(), t.TempDir()
					env := append(nativeEnvironment(t), "CODEX_HOME="+home)
					if initialized {
						server := startNativeCodex(t, codexClient, cwd, env)
						server.call(t, "thread/list", map[string]any{"limit": 1})
						server.close()
					}
					imported := nativeShareImport(t, binary, source, bundle.AgentCodex, home, cwd, env)
					server := startNativeCodex(t, codexClient, cwd, env)
					assertNativeCodexTask(t, server, nativeID(t, imported, "thread_id"), cwd, "openai", []string{nativeUserMarker, nativeAssistantMarker})
				})
			}
		}
		claudeClient := nativeClient(t, "CLAUDE_BIN", "claude")
		for _, source := range []string{bundle.AgentClaude, bundle.AgentDSH} {
			t.Run(source+"_to_claude", func(t *testing.T) {
				cwd, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				other, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				home := t.TempDir()
				env := append(nativeEnvironment(t), "CLAUDE_CONFIG_DIR="+home)
				imported := nativeShareImport(t, binary, source, bundle.AgentClaude, home, cwd, env)
				markers := []string{nativeUserMarker, nativeAssistantMarker}
				if source == bundle.AgentDSH {
					markers = append(markers, nativeToolMarker)
				}
				assertNativeClaudeContinuation(t, claudeClient, imported, cwd, other, env, markers)
			})
		}
	})
	t.Run("external_dsh_plugin_export", func(t *testing.T) {
		zip := os.Getenv("AGENT_HANDOFF_NATIVE_DSH_BUNDLE")
		if zip == "" {
			t.Skip("set AGENT_HANDOFF_NATIVE_DSH_BUNDLE to verify an actual plugin-exported synthetic bundle")
		}
		zip, err := filepath.Abs(zip)
		if err != nil {
			t.Fatal(err)
		}
		// Read only the explicitly supplied artifact, never discover native homes.
		env := nativeEnvironment(t)
		out := nativeCommand(t, binary, t.TempDir(), env, "bridge", "read", zip)
		var read struct {
			Manifest bundle.Manifest    `json:"manifest"`
			Neutral  neutral.Transcript `json:"neutral"`
		}
		if err = json.Unmarshal(out, &read); err != nil {
			t.Fatal(err)
		}
		if read.Manifest.SourceAgent != bundle.AgentDSH {
			t.Fatal("external artifact is not DSH-produced")
		}
		var markers []string
		for _, role := range []string{"user", "assistant"} {
			for _, entry := range read.Neutral.Entries {
				if entry.Kind == neutral.KindMessage && entry.Role == role && entry.Text != "" {
					markers = append(markers, entry.Text)
					break
				}
			}
		}
		if len(markers) != 2 {
			t.Fatal("synthetic artifact must contain both conversation roles")
		}
		for _, target := range []string{bundle.AgentCodex, bundle.AgentClaude} {
			t.Run(target, func(t *testing.T) {
				cwd, err := filepath.EvalSymlinks(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				home := t.TempDir()
				env := nativeEnvironment(t)
				if target == bundle.AgentCodex {
					client := nativeClient(t, "CODEX_BIN", "codex")
					env = append(env, "CODEX_HOME="+home)
					imported := nativeImportBundle(t, binary, zip, target, home, cwd, env)
					server := startNativeCodex(t, client, cwd, env)
					assertNativeCodexTask(t, server, nativeID(t, imported, "thread_id"), cwd, "openai", markers)
				} else {
					client := nativeClient(t, "CLAUDE_BIN", "claude")
					env = append(env, "CLAUDE_CONFIG_DIR="+home)
					imported := nativeImportBundle(t, binary, zip, target, home, cwd, env)
					other, err := filepath.EvalSymlinks(t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					assertNativeClaudeContinuation(t, client, imported, cwd, other, env, markers)
				}
			})
		}
	})
}
