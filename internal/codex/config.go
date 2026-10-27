package codex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// targetModelProvider reads the receiver's base configuration. Command-line
// overrides and named profile files are not available to the importer.
func targetModelProvider(home string) (string, error) {
	path := filepath.Join(home, "config.toml")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "openai", nil
	}
	if err != nil {
		return "", fmt.Errorf("read Codex config %s: %w", path, err)
	}
	var config struct {
		ModelProvider *string `toml:"model_provider"`
		Profile       *string `toml:"profile"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("parse Codex config %s: %w", path, err)
	}
	// Current Codex rejects this legacy selector instead of resolving [profiles].
	if config.Profile != nil {
		return "", fmt.Errorf("codex config %s: legacy profile = %q is no longer supported; Codex now uses --profile with a named .config.toml file", path, *config.Profile)
	}
	if config.ModelProvider == nil {
		return "openai", nil
	}
	if *config.ModelProvider == "" {
		return "", fmt.Errorf("codex config %s: model_provider must not be empty", path)
	}
	return *config.ModelProvider, nil
}
