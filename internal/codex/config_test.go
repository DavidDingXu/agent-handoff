package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTargetModelProvider(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		want   string
	}{
		{name: "empty config", want: "openai"},
		{name: "top level", config: `model_provider = "company"`, want: "company"},
		{name: "literal string and comment", config: `model_provider = 'company#gateway' # selected provider`, want: "company#gateway"},
		{name: "unselected profile ignored", config: "[profiles.work]\nmodel_provider = 'company'\n", want: "openai"},
		{name: "unknown fields and sections", config: "model = 'example'\nfeatures = { alpha = true }\n[projects.'/src/project']\nmodel_provider = 'not-top-level'\n", want: "openai"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := targetModelProvider(home)
			if err != nil || got != tc.want {
				t.Fatalf("targetModelProvider = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestTargetModelProviderMissingConfig(t *testing.T) {
	got, err := targetModelProvider(t.TempDir())
	if err != nil || got != "openai" {
		t.Fatalf("missing config = %q, %v; want openai", got, err)
	}
}

func TestTargetModelProviderRejectsInvalidConfig(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
	}{
		{name: "malformed TOML", config: "model_provider = 'unterminated"},
		{name: "provider wrong type", config: "model_provider = 42"},
		{name: "profile wrong type", config: "profile = true"},
		{name: "legacy profile", config: "profile = 'work'\n[profiles.work]\nmodel_provider = 'company'\n"},
		{name: "empty legacy profile", config: "profile = ''"},
		{name: "empty provider", config: "model_provider = ''"},
		{name: "malformed unknown field", config: "unrelated = ["},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte(tc.config), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := targetModelProvider(home)
			if err == nil || got != "" || !strings.Contains(err.Error(), "config.toml") {
				t.Fatalf("invalid config was not reported with context: provider=%q err=%v", got, err)
			}
		})
	}
}

func TestTargetModelProviderExplainsUnsupportedLegacyProfile(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("profile = 'work'"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := targetModelProvider(home)
	if err == nil || !strings.Contains(err.Error(), "legacy") || !strings.Contains(err.Error(), "--profile") {
		t.Fatalf("unsupported profile error does not explain the native config change: %v", err)
	}
}

func TestTargetModelProviderRejectsUnreadableConfig(t *testing.T) {
	home := t.TempDir()
	// A directory is unreadable as a TOML file on every supported platform,
	// including environments where chmod-based permission tests run as root.
	if err := os.Mkdir(filepath.Join(home, "config.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got, err := targetModelProvider(home); err == nil || got != "" {
		t.Fatalf("config read error was ignored: provider=%q err=%v", got, err)
	}
}
