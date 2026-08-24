package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/ledger"
)

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	target := targetFlag(fs)
	home := fs.String("home", "", "target agent home")
	cwd := fs.String("cwd", "", "target cwd for the imported task (defaults to current directory)")
	execute := fs.Bool("execute", false, "write the imported task into local agent history")
	allowDuplicate := fs.Bool("allow-duplicate", false, "import even if this exact share was already imported before")
	// Positional arg may come before flags: extract it first.
	path := ""
	rest := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		path = args[0]
		rest = args[1:]
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if path == "" {
		if fs.NArg() != 1 {
			return fmt.Errorf("usage: agent-handoff import FILE.agent-handoff.zip|URL [--target codex|claude] [--cwd DIR] [--execute]")
		}
		path = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: agent-handoff import FILE.agent-handoff.zip|URL [--target codex|claude] [--cwd DIR] [--execute]")
	}

	loaded, err := loadBundleArg(path)
	if err != nil {
		return err
	}
	b := loaded.Bundle
	targetAgent, err := resolveTarget(*target)
	if err != nil {
		return err
	}

	var targetHome string
	switch targetAgent {
	case bundle.AgentClaude:
		targetHome, err = claude.ResolveHome(*home)
	default:
		targetHome, err = codex.ResolveHome(*home)
	}
	if err != nil {
		return err
	}

	// Duplicate detection against the target agent's ledger.
	fingerprint := ledger.Fingerprint(b.Manifest.SourceAgent, b.Manifest.SourceThreadID, b.Session)
	if prev := ledger.Find(targetHome, fingerprint); prev != nil && !*allowDuplicate {
		return printJSON(map[string]any{
			"status":             "duplicate",
			"existing_thread_id": prev.ImportedThread,
			"existing_title":     prev.Title,
			"imported_at":        prev.ImportedAt,
			"source_agent":       b.Manifest.SourceAgent,
			"source_thread_id":   b.Manifest.SourceThreadID,
			"note":               "this share was already imported; re-run with --allow-duplicate to import a copy anyway",
		})
	}

	targetCWD := *cwd
	if targetCWD == "" {
		targetCWD, err = os.Getwd()
		if err != nil {
			return err
		}
	}
	targetCWD, err = filepath.Abs(targetCWD)
	if err != nil {
		return err
	}

	if !*execute {
		newID := previewNewID(targetAgent)
		return printJSON(map[string]any{
			"source_agent":     b.Manifest.SourceAgent,
			"source_thread_id": b.Manifest.SourceThreadID,
			"status":           "planned",
			"target_agent":     targetAgent,
			"thread_id":        newID,
			"target_home":      targetHome,
			"target_cwd":       targetCWD,
			"dry_run":          true,
			"note":             "dry run; re-run with --execute to write",
		})
	}

	// Checksum integrity before writing anything.
	if err := b.VerifyChecksums(); err != nil {
		return fmt.Errorf("share file integrity check failed: %w", err)
	}

	var res any
	var newID, title string
	switch targetAgent {
	case bundle.AgentClaude:
		r, err := claude.Restore(claude.RestoreInput{
			SourceSessionID: b.Manifest.SourceThreadID,
			Title:           b.Manifest.Title,
			SessionBytes:    sameAgentSession(b, bundle.AgentClaude),
			Neutral:         b.Neutral,
		}, claude.RestoreOptions{
			Home:      targetHome,
			TargetCWD: targetCWD,
			Execute:   true,
		})
		res = r
		newID = r.SessionID
		title = r.Title
		if err != nil {
			return importError(r, err)
		}
	default:
		r, err := codex.Restore(codex.RestoreInput{
			SourceThreadID: b.Manifest.SourceThreadID,
			Title:          b.Manifest.Title,
			SessionBytes:   sameAgentSession(b, bundle.AgentCodex),
			ThreadRow:      threadRowMeta(b),
			Neutral:        b.Neutral,
			Images:         b.Images,
			ImageData:      b.ImageData,
		}, codex.RestoreOptions{
			Home:      targetHome,
			TargetCWD: targetCWD,
			Execute:   true,
		})
		res = r
		newID = r.ThreadID
		title = r.Title
		if err != nil {
			return importError(r, err)
		}
	}

	// Record the import for duplicate detection.
	if err := ledger.Record_(targetHome, fingerprint, b.Manifest.SourceAgent,
		b.Manifest.SourceThreadID, newID, title); err != nil {
		if m, ok := res.(map[string]any); ok {
			m["ledger_warning"] = err.Error()
		}
	}
	return printJSON(res)
}

func importError(result any, err error) error {
	_ = printJSON(result)
	return err
}

// sameAgentSession returns the raw session bytes when the bundle was
// exported by the given agent (lossless path); nil triggers synthesis.
func sameAgentSession(b *bundle.ReadResult, agent string) []byte {
	if b.Manifest.SourceAgent == agent {
		return b.Session
	}
	return nil
}

// threadRowMeta extracts the sender's threads-table row from bundle meta.
func threadRowMeta(b *bundle.ReadResult) map[string]any {
	if b.Meta == nil {
		return nil
	}
	// v2 stores the row directly; older bundles may wrap it.
	if _, ok := b.Meta["id"]; ok {
		return b.Meta
	}
	return nil
}

func previewNewID(targetAgent string) string {
	if targetAgent == bundle.AgentClaude {
		// preview only; a fresh id is generated on execute
		return ""
	}
	return ""
}
