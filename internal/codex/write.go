package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// ImportedTitleFallback is used when the bundle carries no title.
const ImportedTitleFallback = "导入的 Codex 任务"

// RestoreInput carries everything the importer needs from a loaded bundle.
type RestoreInput struct {
	SourceThreadID string
	Title          string
	SessionBytes   []byte // raw codex session (same-agent path)
	ThreadRow      map[string]any
	Neutral        neutral.Transcript // cross-agent path
	Images         []bundle.ImageAsset
	ImageData      map[string][]byte // zip path -> bytes
}

// RestoreOptions controls a restore.
type RestoreOptions struct {
	Home      string
	TargetCWD string
	Execute   bool
	Now       time.Time // zero means time.Now()
}

// RestoreResult is the outcome of a restore.
type RestoreResult struct {
	Status      string   `json:"status"`
	ThreadID    string   `json:"thread_id"`
	Title       string   `json:"title,omitempty"`
	TargetHome  string   `json:"target_home"`
	TargetCWD   string   `json:"target_cwd"`
	DryRun      bool     `json:"dry_run"`
	CrossAgent  bool     `json:"cross_agent,omitempty"`
	Writes      []string `json:"writes,omitempty"`
	BackupDir   string   `json:"backup_dir,omitempty"`
	Error       string   `json:"error,omitempty"`
	FallbackCmd string   `json:"fallback_command,omitempty"`
}

// Restore writes the shared session into the local Codex home as a new
// native task. Same-agent bundles rewrite the raw session (lossless);
// cross-agent bundles synthesize a Codex session from the neutral transcript.
func Restore(in RestoreInput, opts RestoreOptions) (*RestoreResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	newID := idgen.NewThreadID()
	title := idgen.ImportedTitle(in.Title, ImportedTitleFallback)
	crossAgent := len(in.SessionBytes) == 0

	res := &RestoreResult{
		Status:     "planned",
		ThreadID:   newID,
		Title:      title,
		TargetHome: opts.Home,
		TargetCWD:  opts.TargetCWD,
		DryRun:     !opts.Execute,
		CrossAgent: crossAgent,
	}
	if !opts.Execute {
		return res, nil
	}

	writes, backupDir, err := writeImport(in, opts, newID, title, now, crossAgent)
	res.Writes = writes
	res.BackupDir = backupDir
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		res.FallbackCmd = fmt.Sprintf("codex resume %s", newID)
		return res, err
	}
	res.Status = "ok"
	return res, nil
}

func writeImport(in RestoreInput, opts RestoreOptions, newID, title string, now time.Time, crossAgent bool) ([]string, string, error) {
	home := opts.Home
	targetSession := idgen.RolloutPath(home, newID, now)
	assetDir := filepath.Join(home, "agent-handoff-assets", newID, "images")

	// Assign target paths for bundled images.
	images := in.Images
	for i := range images {
		if images[i].Copied() && images[i].ZipPath != "" {
			images[i].TargetPath = filepath.Join(assetDir, images[i].SHA256+"."+images[i].OriginalExt)
		}
	}

	// Build the session bytes for the receiver.
	var sessionBytes []byte
	if crossAgent {
		sessionBytes = SessionFromNeutral(in.Neutral, newID, opts.TargetCWD, now)
	} else {
		imagePaths := map[string]string{}
		for _, img := range images {
			if img.Copied() && img.TargetPath != "" {
				imagePaths[img.SourcePath] = img.TargetPath
			}
		}
		sessionBytes = session.RewriteForImport(in.SessionBytes, session.ImportRewrite{
			TargetCWD:  opts.TargetCWD,
			TargetID:   newID,
			ImagePaths: imagePaths,
			Now:        now,
		})
	}

	backupDir, err := backupState(home, newID)
	if err != nil {
		return nil, "", fmt.Errorf("backup state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(targetSession), 0o755); err != nil {
		return nil, backupDir, err
	}
	if err := writeFileExclusive(targetSession, sessionBytes); err != nil {
		return nil, backupDir, fmt.Errorf("write rollout: %w", err)
	}
	writes := []string{targetSession}

	// Extract images.
	for _, img := range images {
		if !img.Copied() || img.TargetPath == "" {
			continue
		}
		data, ok := in.ImageData[img.ZipPath]
		if !ok {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(img.TargetPath), 0o755); err != nil {
			return writes, backupDir, err
		}
		if err := writeFileExclusive(img.TargetPath, data); err != nil {
			return writes, backupDir, fmt.Errorf("extract image: %w", err)
		}
		writes = append(writes, img.TargetPath)
	}

	// Append to session_index.jsonl.
	if err := AppendSessionIndex(home, map[string]any{
		"id":          newID,
		"thread_name": title,
		"updated_at":  now.UTC().Format(time.RFC3339Nano),
	}); err != nil {
		return writes, backupDir, err
	}
	writes = append(writes, filepath.Join(home, SessionIndexFile))

	// Insert the sqlite thread row (schema-aware).
	if err := upsertThreadRow(home, in, newID, title, targetSession, opts.TargetCWD, now, crossAgent); err != nil {
		return writes, backupDir, fmt.Errorf("insert thread row: %w", err)
	}
	writes = append(writes, filepath.Join(home, StateSQLiteFile))
	return writes, backupDir, nil
}

// backupState copies the sqlite db (+wal/shm) and session_index.jsonl into
// home/backups_state/agent-handoff/<stamp>-<id>/ before import writes.
func backupState(home, newID string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(home, "backups_state", "agent-handoff", stamp+"-"+newID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	for _, name := range []string{StateSQLiteFile, StateSQLiteFile + "-wal", StateSQLiteFile + "-shm", SessionIndexFile} {
		src := filepath.Join(home, name)
		if !fileExists(src) {
			continue
		}
		if err := copyFile(src, filepath.Join(dir, name)); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := out.ReadFrom(in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func writeFileExclusive(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ---- neutral -> codex session synthesis ----

// SessionFromNeutral synthesizes a valid Codex rollout jsonl from a neutral
// transcript (cross-agent import path).
func SessionFromNeutral(t neutral.Transcript, threadID, targetCWD string, now time.Time) []byte {
	var out []string
	appendLine := func(obj map[string]any) {
		if b, err := jsonMarshalLine(obj); err == nil {
			out = append(out, string(b))
		}
	}
	stamp := now.UTC().Format("2006-01-02T15:04:05.000Z")

	appendLine(map[string]any{
		"timestamp": stamp,
		"type":      "session_meta",
		"payload": map[string]any{
			"id":             threadID,
			"timestamp":      stamp,
			"cwd":            targetCWD,
			"originator":     "agent-handoff",
			"cli_version":    "agent-handoff",
			"source":         "agent-handoff",
			"thread_source":  "imported",
			"model_provider": t.SourceAgent,
		},
	})
	appendLine(map[string]any{
		"timestamp": stamp,
		"type":      "turn_context",
		"payload": map[string]any{
			"cwd":             targetCWD,
			"approval_policy": "never",
			"sandbox_policy":  map[string]any{"type": "disabled"},
		},
	})

	turn := 0
	hasUserTurn := false
	startTurn := func() {
		turn++
		appendLine(eventMsg("task_started", map[string]any{
			"turn_id": fmt.Sprintf("agent-handoff-import-turn-%d", turn),
		}))
	}
	for _, e := range t.Entries {
		switch e.Kind {
		case neutral.KindMessage:
			if e.Role == "user" {
				startTurn()
				hasUserTurn = true
				appendLine(eventMsg("user_message", map[string]any{"message": e.Text}))
				appendLine(responseMessage("user", e.Text))
			} else if hasUserTurn {
				appendLine(eventMsg("agent_message", map[string]any{"message": e.Text}))
				appendLine(responseMessage("assistant", e.Text))
			}
		case neutral.KindTool:
			if !hasUserTurn {
				continue
			}
			callID := fmt.Sprintf("agent_handoff_tool_%03d", turn)
			appendLine(map[string]any{
				"timestamp": stamp,
				"type":      "response_item",
				"payload": map[string]any{
					"type":      "function_call",
					"name":      e.Tool,
					"namespace": "agent_handoff.source",
					"call_id":   callID,
					"arguments": e.Input,
					"status":    e.Status,
				},
			})
			appendLine(map[string]any{
				"timestamp": stamp,
				"type":      "response_item",
				"payload": map[string]any{
					"type":    "function_call_output",
					"call_id": callID,
					"output":  e.Output,
					"status":  "completed",
				},
			})
		}
	}

	if len(t.Entries) == 0 {
		startTurn()
		handoff := neutral.HandoffText(t, "codex")
		appendLine(eventMsg("user_message", map[string]any{"message": handoff}))
		appendLine(responseMessage("user", handoff))
	}

	appendLine(eventMsg("agent_message", map[string]any{
		"message": "<EXTERNAL SESSION IMPORTED>",
	}))
	appendLine(eventMsg("token_count", map[string]any{
		"info": map[string]any{"total_token_usage": map[string]any{"total_tokens": 0}},
	}))
	appendLine(eventMsg("task_complete", nil))
	return []byte(strings.Join(out, "\n") + "\n")
}

func eventMsg(typ string, payload map[string]any) map[string]any {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["type"] = typ
	return map[string]any{"timestamp": "", "type": "event_msg", "payload": payload}
}

func responseMessage(role, text string) map[string]any {
	return map[string]any{
		"timestamp": "",
		"type":      "response_item",
		"payload": map[string]any{
			"type":    "message",
			"role":    role,
			"content": []map[string]any{{"type": textType(role), "text": text}},
		},
	}
}

func textType(role string) string {
	if role == "user" {
		return "input_text"
	}
	return "output_text"
}

func jsonMarshalLine(obj map[string]any) ([]byte, error) {
	return jsonMarshal(obj)
}
