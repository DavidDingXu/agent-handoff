package cli

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/brief"
	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/ledger"
	"github.com/DavidDingXu/agent-handoff/internal/workspace"
)

func cmdImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	target := targetFlag(fs)
	database := fs.String("database", "", "Qoder desktop main.sqlite")
	expectedFingerprint := fs.String("expected-fingerprint", "", "fingerprint from the reviewed preview")
	home := fs.String("home", "", "target agent home")
	cwd := fs.String("cwd", "", "target cwd for the imported task (defaults to current directory)")
	execute := fs.Bool("execute", false, "write the imported task into local agent history")
	allowDuplicate := fs.Bool("allow-duplicate", false, "import even if this exact share was already imported before")
	applyCode := fs.Bool("apply-code", false, "apply reviewed code to a new independent Git worktree")
	codeDirectory := fs.String("code-directory", "", "new worktree path (defaults to a fresh sibling of --cwd)")
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
			return fmt.Errorf("usage: agent-handoff import FILE.agent-handoff.zip|URL [--target codex|claude|qoder] [--cwd DIR] [--execute]")
		}
		path = fs.Arg(0)
	} else if fs.NArg() != 0 {
		return fmt.Errorf("usage: agent-handoff import FILE.agent-handoff.zip|URL [--target codex|claude|qoder] [--cwd DIR] [--execute]")
	}

	loaded, err := loadBundleArg(path)
	if err != nil {
		return err
	}
	b := loaded.Bundle
	// Integrity and DSH semantic validation precede every successful outcome,
	// including dry-run and duplicate reporting.
	if err := b.VerifyChecksums(); err != nil {
		return fmt.Errorf("share file integrity check failed: %w", err)
	}
	if err := bundle.ValidateDSHBundle(b); err != nil {
		return err
	}
	if *expectedFingerprint != "" && *expectedFingerprint != reviewFingerprint(b) {
		return fmt.Errorf("handoff changed since preview")
	}
	if b.Manifest.SourceAgent == bundle.AgentQoder {
		if err := validateQoderTranscript(b); err != nil {
			return err
		}
	}
	if *applyCode && b.Code == nil {
		return fmt.Errorf("this handoff has no code changes; import without --apply-code")
	}
	if *codeDirectory != "" && !*applyCode {
		return fmt.Errorf("--code-directory requires --apply-code")
	}
	targetAgent, err := resolveTarget(*target)
	if err != nil {
		return err
	}

	if targetAgent == bundle.AgentQoder {
		if *applyCode {
			return fmt.Errorf("Qoder task import does not apply code changes")
		}
		workspacePath := *cwd
		if workspacePath == "" {
			workspacePath, err = os.Getwd()
			if err != nil {
				return err
			}
		}
		workspacePath, err = filepath.Abs(workspacePath)
		if err != nil {
			return err
		}
		return importIntoQoder(b, *database, *home, workspacePath, handoffFingerprint(b), *execute, *allowDuplicate)
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
	fingerprint := handoffFingerprint(b)
	if *applyCode {
		// An explicit code application is distinct from retaining the same
		// attachment as inert evidence in a conversation-only import.
		sum := sha256.Sum256([]byte(fingerprint + "\x00code-applied"))
		fingerprint = fmt.Sprintf("%x", sum[:])
	}
	if prev := ledger.Find(targetHome, fingerprint); prev != nil && !*allowDuplicate {
		previousCWD := *cwd
		if targetAgent == bundle.AgentCodex {
			if d, e := codex.LoadThread(targetHome, prev.ImportedThread); e == nil {
				previousCWD = d.CWD
			}
		} else {
			if d, e := claude.LoadSession(targetHome, prev.ImportedThread); e == nil {
				previousCWD = d.CWD
			}
		}
		return printJSON(continuationResult(map[string]any{
			"status":             "duplicate",
			"existing_thread_id": prev.ImportedThread,
			"existing_title":     prev.Title,
			"imported_at":        prev.ImportedAt,
			"source_agent":       b.Manifest.SourceAgent,
			"source_thread_id":   b.Manifest.SourceThreadID,
			"note":               "this share was already imported; re-run with --allow-duplicate to import a copy anyway",
		}, targetAgent, prev.ImportedThread, previousCWD))
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
	codePath := ""
	if *applyCode {
		codePath = *codeDirectory
		if codePath == "" {
			codePath = filepath.Join(filepath.Dir(targetCWD), filepath.Base(targetCWD)+"-handoff-"+idgen.NewUUID()[:8])
		}
		codePath, err = filepath.Abs(codePath)
		if err != nil {
			return err
		}
	}

	if !*execute {
		newID := previewNewID(targetAgent)
		plan := map[string]any{
			"source_agent":     b.Manifest.SourceAgent,
			"source_thread_id": b.Manifest.SourceThreadID,
			"status":           "planned",
			"target_agent":     targetAgent,
			"thread_id":        newID,
			"target_home":      targetHome,
			"target_cwd":       targetCWD,
			"dry_run":          true,
			"note":             "dry run; re-run with --execute to write",
			"brief":            b.Brief, "code": codeManifest(b),
		}
		if *applyCode {
			plan["code_directory"] = codePath
			plan["code_apply_planned"] = true
		}
		return printJSON(plan)
	}
	if *applyCode {
		if err := workspace.Apply(targetCWD, codePath, b.Code); err != nil {
			return fmt.Errorf("code was not applied; check the local base commit, then retry or import without --apply-code: %w", err)
		}
		targetCWD = codePath
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
			return importWithCodeError(r, err, codePath)
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
			return importWithCodeError(r, err, codePath)
		}
	}

	// Preserve DSH source bytes as private inert evidence, under a generated
	// receiver identity. Never put sender metadata into runtime configuration.
	if b.Manifest.SourceAgent == bundle.AgentDSH {
		auditPath, err := writeDSHAudit(targetHome, newID, b.Session)
		if err != nil {
			switch r := res.(type) {
			case *codex.RestoreResult:
				r.Status = "error"
				r.Error = "source audit could not be saved"
			case *claude.RestoreResult:
				r.Status = "error"
				r.Error = "source audit could not be saved"
			}
			return importWithCodeError(res, fmt.Errorf("created session %s; source audit could not be saved", newID), codePath)
		}
		switch r := res.(type) {
		case *codex.RestoreResult:
			r.Writes = append(r.Writes, auditPath)
		case *claude.RestoreResult:
			r.Writes = append(r.Writes, auditPath)
		}
	}

	result := continuationResult(res, targetAgent, newID, targetCWD)
	if b.Code != nil {
		result["code"] = codeManifest(b)
		result["code_applied"] = *applyCode
		if *applyCode {
			result["code_directory"] = codePath
		}
	}
	if b.Brief == nil {
		v := brief.Generate(b.Neutral)
		b.Brief = &v
	}
	briefPath, err := writeBriefAudit(targetHome, newID, b)
	if err != nil {
		result["status"] = "error"
		result["error"] = "The task was created but its handoff sidecar could not be saved."
		return importError(result, fmt.Errorf("created task %s; handoff sidecar could not be saved", newID))
	}
	result["brief_path"] = briefPath
	// Only a complete task and saved sidecar count as a successful import.
	if err := ledger.Record_(targetHome, fingerprint, b.Manifest.SourceAgent, b.Manifest.SourceThreadID, newID, title); err != nil {
		result["ledger_warning"] = err.Error()
	}
	return printJSON(result)
}

func importWithCodeError(value any, err error, codePath string) error {
	if codePath == "" {
		return importError(value, err)
	}
	raw, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	result["code_directory"] = codePath
	result["code_applied"] = true
	return importError(result, err)
}

func continuationResult(value any, target, id, cwd string) map[string]any {
	raw, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	args := []string{"resume", id}
	if target == bundle.AgentClaude {
		args = []string{"--resume", id}
	}
	if result["fallback_command"] == nil {
		result["fallback_command"] = target + " " + strings.Join(args, " ")
	}
	result["target_agent"] = target
	result["target_cwd"] = cwd
	result["resume_command"] = map[string]any{"executable": target, "args": args, "cwd": cwd}
	return result
}

func writeBriefAudit(home, id string, b *bundle.ReadResult) (string, error) {
	dir := filepath.Join(home, "agent-handoff-assets", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "brief.md")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, err = f.WriteString(brief.RenderMarkdown(*b.Brief))
	closeErr := f.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if b.Code != nil {
		entries, err := b.Code.Entries()
		if err != nil {
			return "", err
		}
		for name, data := range entries {
			p := filepath.Join(dir, filepath.FromSlash(name))
			if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
				return "", err
			}
			out, e := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
			if e != nil {
				return "", e
			}
			_, e = out.Write(data)
			ce := out.Close()
			if e != nil {
				return "", e
			}
			if ce != nil {
				return "", ce
			}
		}
	}
	return path, nil
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
	if b.Manifest.SourceAgent != bundle.AgentCodex || b.Meta == nil {
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

// writeDSHAudit creates only a private source sidecar, never an executable file.
func writeDSHAudit(home, id string, data []byte) (string, error) {
	dir := filepath.Join(home, "agent-handoff-assets", id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "source-session.jsonl")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return "", err
	}
	return path, f.Close()
}
