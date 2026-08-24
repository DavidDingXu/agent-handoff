// Package cli implements the agent-handoff command line: share, preview,
// import, verify, version.
package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
)

// Version is set at build time via -ldflags.
var Version = "dev"

// Run dispatches a command line and returns a process exit code.
func Run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	switch args[0] {
	case "share":
		return cmdShare(args[1:])
	case "preview":
		return cmdPreview(args[1:])
	case "import":
		return cmdImport(args[1:])
	case "verify":
		return cmdVerify(args[1:])
	case "version":
		fmt.Printf("agent-handoff %s (%s/%s)\n", Version, runtime.GOOS, runtime.GOARCH)
		return nil
	default:
		return fmt.Errorf("unknown command %q (expected share|preview|import|verify)", args[0])
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `agent-handoff: share a coding-agent task, import a shared task.

Usage:
  agent-handoff share [--source codex|claude] [--thread current] [--format zip|link]
                    [--out FILE] [--endpoint URL] [--token TOKEN] [--ttl SECONDS]
                    [--include-secrets]
  agent-handoff preview FILE.agent-handoff.zip
  agent-handoff import FILE.agent-handoff.zip|URL [--source codex|claude] [--target codex|claude]
                 [--cwd DIR] [--execute] [--allow-duplicate]
  agent-handoff verify --thread THREAD_ID [--source codex|claude] [--cwd DIR]`)
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// ---- shared option parsing ----

func sourceFlag(fs *flag.FlagSet) *string {
	return fs.String("source", "", "source agent: codex or claude (defaults to auto-detect)")
}

func targetFlag(fs *flag.FlagSet) *string {
	return fs.String("target", "", "import target agent: codex or claude (defaults to the current agent)")
}

func homeFlag(fs *flag.FlagSet, agent string) *string {
	return fs.String("home", "", agent+" home (defaults to CODEX_HOME/CLAUDE_CONFIG_DIR)")
}

// detectSource picks the default source agent: Claude when CLAUDE_CONFIG_DIR
// is set or the cwd is inside a Claude session context, else Codex.
func detectSource(explicit string) (string, error) {
	if v := strings.TrimSpace(explicit); v != "" {
		if !bundle.IsSupportedAgent(v) {
			return "", fmt.Errorf("unsupported --source %q (expected codex|claude)", v)
		}
		return v, nil
	}
	// The running agent injects its session id env var.
	for _, key := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"} {
		if os.Getenv(key) != "" {
			return bundle.AgentCodex, nil
		}
	}
	for _, key := range []string{"CLAUDE_SESSION_ID", "CLAUDE_CODE_SESSION_ID"} {
		if os.Getenv(key) != "" {
			return bundle.AgentClaude, nil
		}
	}
	if os.Getenv("CLAUDECODE") == "1" {
		return bundle.AgentClaude, nil
	}
	return bundle.AgentCodex, nil
}

func resolveTarget(explicit string) (string, error) {
	if v := strings.TrimSpace(explicit); v != "" {
		if !bundle.IsSupportedAgent(v) {
			return "", fmt.Errorf("unsupported --target %q (expected codex|claude)", v)
		}
		return v, nil
	}
	return detectSource("")
}

// absolutePath canonicalizes a path for JSON output; agents should never
// misread it against other cwd fields.
func absolutePath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return abs
}

func defaultCWD() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Abs(wd)
}

// isURL reports whether the source argument is a share link.
func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

var errPrinted = errors.New("agent-handoff: error already printed to stdout")
