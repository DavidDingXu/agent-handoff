package cli

import (
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/brief"
	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/qoder"
)

func qoderPaths(database, home string) (string, string, error) {
	if home == "" {
		home = os.Getenv("AGENT_HANDOFF_QODER_RUNTIME")
	}
	if home == "" {
		home = os.Getenv("QODER_CONFIG_DIR")
	}
	if home == "" {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", "", err
		}
		home = filepath.Join(userHome, ".qoder")
	}
	runtimeHome, err := filepath.Abs(home)
	if err != nil {
		return "", "", err
	}
	if database == "" {
		database = os.Getenv("AGENT_HANDOFF_QODER_DATABASE")
	}
	if database == "" {
		data, err := os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
		database = filepath.Join(data, "com.qoder.app.stable", "main.sqlite")
	}
	database, err = filepath.Abs(database)
	return database, runtimeHome, err
}

func cmdQoder(args []string) error {
	if len(args) == 0 || args[0] != "list" {
		return fmt.Errorf("usage: agent-handoff qoder list [--query TEXT] [--database FILE]")
	}
	fs := flag.NewFlagSet("qoder list", flag.ContinueOnError)
	database := fs.String("database", "", "Qoder desktop main.sqlite")
	query := fs.String("query", "", "filter by task ID, title, or workspace")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected argument to qoder list")
	}
	path, _, err := qoderPaths(*database, "")
	if err != nil {
		return err
	}
	tasks, err := qoder.List(path, *query)
	if err != nil {
		return err
	}
	if tasks == nil {
		tasks = []qoder.Task{}
	}
	return printJSON(map[string]any{"status": "ok", "source_agent": bundle.AgentQoder, "tasks": tasks})
}

func exportQoder(database, home, id string) (*bundle.Manifest, []byte, neutral.Transcript, error) {
	if id == "" || id == "current" {
		id = strings.TrimSpace(os.Getenv("QODER_SESSION_ID"))
		if id == "" {
			return nil, nil, neutral.Transcript{}, fmt.Errorf("select a task with agent-handoff qoder list and pass --thread ID")
		}
	}
	database, home, err := qoderPaths(database, home)
	if err != nil {
		return nil, nil, neutral.Transcript{}, err
	}
	task, history, transcript, err := qoder.Load(database, home, id)
	if err != nil {
		return nil, nil, neutral.Transcript{}, err
	}
	if err := validateQoderEntries(transcript); err != nil {
		return nil, nil, neutral.Transcript{}, err
	}
	messageCount := 0
	for _, e := range transcript.Entries {
		if e.Kind == neutral.KindMessage {
			messageCount++
		}
	}
	m := &bundle.Manifest{
		FormatVersion: bundle.Version2, ArtifactType: bundle.ArtifactType,
		SourceAgent: bundle.AgentQoder, SourceThreadID: task.ID, Title: task.Title,
		SourceCWD: task.CWD, SourceCLI: "qoder-desktop", CreatedAt: idgen.NowRFC3339(),
		MessageCount:  messageCount,
		TargetSupport: []string{bundle.AgentCodex, bundle.AgentClaude, bundle.AgentQoder},
	}
	transcriptJSON, err := json.MarshalIndent(transcript, "", "  ")
	if err != nil {
		return nil, nil, neutral.Transcript{}, err
	}
	if len(history) <= bundle.MaxDSHNative && len(transcriptJSON) <= bundle.MaxBridgeNeutral {
		m.TargetSupport = append(m.TargetSupport, bundle.AgentDSH)
	}
	return m, history, transcript, nil
}

func validateQoderTranscript(b *bundle.ReadResult) error {
	if b.Neutral.SourceAgent != b.Manifest.SourceAgent || b.Neutral.SourceID != b.Manifest.SourceThreadID {
		return fmt.Errorf("transcript identity does not match the handoff")
	}
	if err := validateQoderEntries(b.Neutral); err != nil {
		return err
	}
	if b.Manifest.SourceAgent == bundle.AgentQoder {
		task := qoder.Task{ID: b.Manifest.SourceThreadID, Title: b.Manifest.Title, CWD: b.Manifest.SourceCWD}
		parsed, err := qoder.ToNeutral(task, b.Session)
		if err != nil {
			return fmt.Errorf("invalid native Qoder history: %w", err)
		}
		if !slices.Equal(parsed.Entries, b.Neutral.Entries) {
			return fmt.Errorf("native Qoder history does not match the reviewed transcript")
		}
	}
	return nil
}

func validateQoderEntries(transcript neutral.Transcript) error {
	if err := bundle.ValidateReadNeutral(transcript); err != nil {
		return err
	}
	for _, entry := range transcript.Entries {
		if entry.Kind == neutral.KindMessage && entry.Role == "user" {
			return nil
		}
	}
	return fmt.Errorf("handoff has no user message to restore")
}

func importIntoQoder(b *bundle.ReadResult, path, home, cwd, fingerprint string, execute, allowDuplicate bool) error {
	path, home, err := qoderPaths(path, home)
	if err != nil {
		return err
	}
	if b.Neutral.Schema == "" && b.Manifest.SourceAgent != bundle.AgentQoder {
		b.Neutral = neutralFrom(b.Manifest, b.Session)
	}
	if err := validateQoderTranscript(b); err != nil {
		return err
	}
	if !execute {
		return printJSON(map[string]any{
			"status": "planned", "dry_run": true, "target_agent": bundle.AgentQoder,
			"target_cwd": cwd, "target_database": path, "source_agent": b.Manifest.SourceAgent,
			"source_thread_id": b.Manifest.SourceThreadID, "brief": b.Brief, "code": codeManifest(b),
		})
	}
	result, err := qoder.Restore(b.Neutral, b.Manifest.Title, cwd, qoder.RestoreOptions{
		DatabasePath: path, RuntimeHome: home, Fingerprint: fingerprint,
		AllowDuplicate: allowDuplicate, NativeSession: sameAgentSession(b, bundle.AgentQoder),
	})
	if err != nil {
		return err
	}
	out := map[string]any{
		"status": "ok", "target_agent": bundle.AgentQoder, "thread_id": result.SessionID,
		"session_id": result.SessionID, "title": result.Title, "target_cwd": result.CWD,
		"source_agent": b.Manifest.SourceAgent, "source_thread_id": b.Manifest.SourceThreadID,
		"cross_agent": b.Manifest.SourceAgent != bundle.AgentQoder,
	}
	if result.BackupDir != "" {
		out["recovery_path"] = result.BackupDir
	}
	if b.Brief != nil && b.Brief.Notes != "" || b.Code != nil {
		sidecar := filepath.Join(home, "agent-handoff-assets", result.SessionID, "brief.md")
		if !result.Duplicate {
			if b.Brief == nil {
				v := brief.Generate(b.Neutral)
				b.Brief = &v
			}
			sidecar, err = writeBriefAudit(home, result.SessionID, b)
			if err != nil {
				out["warning"] = "Task restored. The original bundle retains optional notes and code evidence because the sidecar could not be saved."
			}
		}
		if sidecar != "" {
			if info, err := os.Stat(sidecar); err == nil && info.Mode().IsRegular() {
				out["brief_path"] = sidecar
			}
		}
		if b.Code != nil {
			out["code"] = codeManifest(b)
			out["code_applied"] = false
		}
	}
	if result.Duplicate {
		out["status"] = "duplicate"
		out["existing_thread_id"] = result.SessionID
		out["existing_title"] = result.Title
	}
	return printJSON(out)
}

func reviewFingerprint(b *bundle.ReadResult) string {
	if checksum := b.Files[bundle.ChecksumsEntry]; len(checksum) > 0 {
		return fmt.Sprintf("%x", sha256.Sum256(checksum))
	}
	return handoffFingerprint(b)
}
