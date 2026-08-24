package claude

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// ImportedTitleFallback is used when the bundle carries no title.
const ImportedTitleFallback = "导入的 Claude 会话"

// RestoreInput carries everything the importer needs from a loaded bundle.
type RestoreInput struct {
	SourceSessionID string
	Title           string
	SessionBytes    []byte // raw claude session (same-agent path)
	Neutral         neutral.Transcript
}

// RestoreOptions controls a restore.
type RestoreOptions struct {
	Home      string
	TargetCWD string
	Execute   bool
	Now       time.Time
}

// RestoreResult is the outcome of a restore.
type RestoreResult struct {
	Status      string   `json:"status"`
	SessionID   string   `json:"session_id"`
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

// Restore writes the shared session into the local Claude home as a new
// native session. Same-agent bundles rewrite the raw session (uuid chain
// preserved); cross-agent bundles synthesize from the neutral transcript.
func Restore(in RestoreInput, opts RestoreOptions) (*RestoreResult, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	newID := idgen.NewUUID()
	title := idgen.ImportedTitle(in.Title, ImportedTitleFallback)
	crossAgent := len(in.SessionBytes) == 0

	res := &RestoreResult{
		Status:     "planned",
		SessionID:  newID,
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
		res.FallbackCmd = fmt.Sprintf("cd %s && claude --session-id %s", opts.TargetCWD, newID)
		return res, err
	}
	res.Status = "ok"
	res.FallbackCmd = fmt.Sprintf("cd %s && claude --session-id %s", opts.TargetCWD, newID)
	return res, nil
}

func writeImport(in RestoreInput, opts RestoreOptions, newID, title string, now time.Time, crossAgent bool) ([]string, string, error) {
	home := opts.Home
	projectDir := ProjectDirName(opts.TargetCWD)
	sessionPath := filepath.Join(home, ProjectsDir, projectDir, newID+".jsonl")
	indexPath := filepath.Join(home, ProjectsDir, projectDir, IndexFile)

	var sessionBytes []byte
	if crossAgent {
		sessionBytes = SessionFromNeutral(in.Neutral, newID, opts.TargetCWD, now)
	} else {
		sessionBytes = RewriteSession(in.SessionBytes, newID, opts.TargetCWD, now)
	}

	backupDir, err := backupState(home, indexPath, sessionPath)
	if err != nil {
		return nil, "", fmt.Errorf("backup state: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		return nil, backupDir, err
	}
	if err := writeFileExclusive(sessionPath, sessionBytes); err != nil {
		return nil, backupDir, fmt.Errorf("write session: %w", err)
	}
	writes := []string{sessionPath}

	// Update the project index.
	st, _ := os.Stat(sessionPath)
	var mtime int64
	if st != nil {
		mtime = st.ModTime().UnixMilli()
	}
	idx, err := ReadIndex(indexPath)
	if err != nil {
		return writes, backupDir, err
	}
	idx.UpsertEntry(IndexEntry{
		SessionID:    newID,
		FullPath:     sessionPath,
		FileMtime:    mtime,
		FirstPrompt:  session.Truncate(title, 240),
		MessageCount: countMessages(sessionBytes),
		Created:      fmtTime(now),
		Modified:     fmtTime(now),
		ProjectPath:  opts.TargetCWD,
	})
	if err := idx.WriteIndex(indexPath); err != nil {
		return writes, backupDir, fmt.Errorf("write index: %w", err)
	}
	writes = append(writes, indexPath)
	return writes, backupDir, nil
}

// backupState copies the project index and any existing session file into
// home/backups_state/agent-handoff/<stamp>-<id>/.
func backupState(home, indexPath, sessionPath string) (string, error) {
	stamp := time.Now().UTC().Format("20060102T150405Z")
	dir := filepath.Join(home, "backups_state", "agent-handoff", stamp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if _, err := os.Stat(indexPath); err == nil {
		if err := copyFile(indexPath, filepath.Join(dir, IndexFile)); err != nil {
			return "", err
		}
	}
	if _, err := os.Stat(sessionPath); err == nil {
		if err := copyFile(sessionPath, filepath.Join(dir, "existing-session.jsonl")); err != nil {
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

func countMessages(data []byte) int {
	return SummarizeSession(data).MessageCount
}

// RewriteSession rewrites a Claude session for the receiver: new session id,
// target cwd everywhere, and a fully regenerated uuid chain (uuid,
// parentUuid, leafUuid) so the conversation tree stays intact.
func RewriteSession(content []byte, newID, targetCWD string, now time.Time) []byte {
	uuidMap := map[string]string{}
	newUUID := func(old string) string {
		if old == "" {
			return ""
		}
		if v, ok := uuidMap[old]; ok {
			return v
		}
		v := idgen.NewUUID()
		uuidMap[old] = v
		return v
	}

	var out []string
	session.IterLines(content, func(line session.Line) {
		if line.Obj == nil {
			out = append(out, line.Raw)
			return
		}
		obj := line.Obj
		if obj["sessionId"] != nil {
			obj["sessionId"] = newID
		}
		if obj["cwd"] != nil {
			obj["cwd"] = targetCWD
		}
		if obj["version"] != nil {
			obj["version"] = "agent-handoff"
		}
		if v, ok := obj["uuid"].(string); ok {
			obj["uuid"] = newUUID(v)
		}
		if v, ok := obj["parentUuid"].(string); ok {
			obj["parentUuid"] = newUUID(v)
		}
		if v, ok := obj["leafUuid"].(string); ok {
			obj["leafUuid"] = newUUID(v)
		}
		if encoded, err := json.Marshal(obj); err == nil {
			out = append(out, string(encoded))
		} else {
			out = append(out, line.Raw)
		}
	})
	return []byte(strings.Join(out, "\n") + "\n")
}

// SessionFromNeutral synthesizes a Claude session jsonl from a neutral
// transcript (cross-agent import path). Each neutral entry becomes a user or
// assistant message; tool evidence becomes plain text. An empty transcript
// falls back to a single handoff user message.
func SessionFromNeutral(t neutral.Transcript, newID, targetCWD string, now time.Time) []byte {
	var out []string
	appendLine := func(obj map[string]any) {
		b, _ := json.Marshal(obj)
		out = append(out, string(b))
	}
	stamp := now.UTC().Format(time.RFC3339Nano)

	appendLine(map[string]any{
		"type": "queue-operation", "timestamp": stamp, "sessionId": newID,
		"operation": "enqueue",
	})
	appendLine(map[string]any{
		"type": "queue-operation", "timestamp": stamp, "sessionId": newID,
		"operation": "dequeue",
	})

	parent := ""
	leaf := ""
	emit := func(role, text string) string {
		u := idgen.NewUUID()
		msg := map[string]any{
			"role":    role,
			"content": text,
		}
		if role == "assistant" {
			msg["content"] = []map[string]any{{"type": "text", "text": text}}
			msg["model"] = "synthetic"
		}
		line := map[string]any{
			"parentUuid":  parent,
			"isSidechain": false,
			"userType":    "external",
			"cwd":         targetCWD,
			"sessionId":   newID,
			"version":     "agent-handoff",
			"uuid":        u,
			"type":        role,
			"message":     msg,
			"timestamp":   stamp,
		}
		appendLine(line)
		parent = u
		return u
	}

	for _, e := range t.Entries {
		switch e.Kind {
		case neutral.KindMessage:
			leaf = emit(e.Role, e.Text)
		case neutral.KindTool:
			text := fmt.Sprintf("[tool] %s (%s)\ninput: %s\noutput: %s",
				e.Tool, e.Status,
				session.Truncate(e.Input, 2000),
				session.Truncate(e.Output, 4000))
			leaf = emit("assistant", text)
		}
	}
	if len(t.Entries) == 0 {
		handoff := neutral.HandoffText(t, "claude")
		leaf = emit("user", handoff)
	}

	appendLine(map[string]any{
		"type": "last-prompt", "timestamp": stamp, "sessionId": newID,
		"lastPrompt": lastPromptText(t), "leafUuid": leaf,
	})
	return []byte(strings.Join(out, "\n") + "\n")
}

// lastPromptText is the text for the last-prompt line: the first user
// message of the transcript, falling back to the handoff text. The handoff
// text embeds the sender's cwd, which would leak into the receiver's local
// session file.
func lastPromptText(t neutral.Transcript) string {
	for _, e := range t.Entries {
		if e.Kind == neutral.KindMessage && e.Role == "user" {
			return session.Truncate(e.Text, 240)
		}
	}
	return session.Truncate(t.Title, 240)
}

// VerifyResult reports import integrity.
type VerifyResult struct {
	Status            string   `json:"status"`
	SessionID         string   `json:"session_id"`
	SessionFileExists bool     `json:"session_file_exists"`
	IndexEntryExists  bool     `json:"index_entry_exists"`
	Failures          []string `json:"failures,omitempty"`
}

// Verify checks that an imported session file exists and is indexed.
// With expectedCWD non-empty, the session's cwd set must include it.
func Verify(home, sessionID, expectedCWD string) *VerifyResult {
	res := &VerifyResult{Status: "ok", SessionID: sessionID}
	path, err := findSessionPath(home, sessionID)
	if err == nil {
		res.SessionFileExists = true
		if expectedCWD != "" {
			if data, err := os.ReadFile(path); err == nil {
				cwds := sessionCWDs(data)
				if !cwds[expectedCWD] {
					res.Failures = append(res.Failures, "session cwd does not include expected cwd")
				}
			}
		}
	} else {
		res.Failures = append(res.Failures, "session file missing")
	}
	if entry, _, err := findIndexEntry(home, sessionID); err == nil && entry != nil {
		res.IndexEntryExists = true
	} else {
		res.Failures = append(res.Failures, "sessions-index.json entry missing")
	}
	if len(res.Failures) > 0 {
		res.Status = "failed"
	}
	return res
}

func sessionCWDs(data []byte) map[string]bool {
	out := map[string]bool{}
	session.IterLines(data, func(line session.Line) {
		if line.Obj == nil {
			return
		}
		if cwd, _ := line.Obj["cwd"].(string); cwd != "" {
			out[cwd] = true
		}
	})
	return out
}
