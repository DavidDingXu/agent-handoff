package qoder

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/idgen"
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	_ "modernc.org/sqlite"
)

// RestoreOptions selects the initialized desktop profile and runtime home.
type RestoreOptions struct {
	DatabasePath   string
	RuntimeHome    string
	Fingerprint    string
	AllowDuplicate bool
	NativeSession  []byte
}

// RestoreResult describes a newly created or previously imported Qoder task.
type RestoreResult struct {
	SessionID    string
	DatabasePath string
	RuntimePath  string
	WorkspaceID  string
	Title        string
	CWD          string
	BackupDir    string
	Duplicate    bool
}

// Restore materializes a local Qoder task and its runtime history. It publishes
// the completed history before committing the desktop catalog transaction, so
// a visible task always has a runtime history available to resume.
func Restore(t neutral.Transcript, title, cwd string, opts RestoreOptions) (RestoreResult, error) {
	var result RestoreResult
	if opts.DatabasePath == "" || opts.RuntimeHome == "" {
		return result, errors.New("restoring to Qoder requires a desktop database path and runtime home")
	}
	if strings.TrimSpace(cwd) == "" {
		return result, errors.New("restoring to Qoder requires a target working directory")
	}
	cwd, err := filepath.Abs(cwd)
	if err != nil {
		return result, fmt.Errorf("resolve Qoder working directory: %w", err)
	}
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return result, fmt.Errorf("resolve Qoder working directory: %w", err)
	}
	if info, err := os.Stat(cwd); err != nil || !info.IsDir() {
		return result, fmt.Errorf("the Qoder working directory is unavailable: %s", cwd)
	}
	entries := restoreConversation(t)
	if len(opts.NativeSession) > 0 {
		if t.SourceAgent != "qoder" || t.SourceID == "" {
			return result, errors.New("the Qoder native history requires a Qoder source identity")
		}
		entries, err = restoreNativeConversation(t.SourceID, opts.NativeSession)
		if err != nil {
			return result, err
		}
	}
	if len(entries) == 0 {
		return result, errors.New("restoring to Qoder requires visible conversation history")
	}
	databasePath, err := filepath.Abs(opts.DatabasePath)
	if err != nil {
		return result, err
	}
	runtimeHome, err := filepath.Abs(opts.RuntimeHome)
	if err != nil {
		return result, err
	}
	result.DatabasePath = databasePath
	db, err := sql.Open("sqlite", restoreDatabaseURI(databasePath, "rw"))
	if err != nil {
		return result, err
	}
	defer func() { _ = db.Close() }()
	ctx := context.Background()
	if !opts.AllowDuplicate {
		if existing, err := findRestoreDuplicate(ctx, db, result.DatabasePath, runtimeHome, opts.Fingerprint); err != nil || existing.Duplicate {
			return existing, err
		}
	}

	now := time.Now().UTC()
	result.SessionID = idgen.NewUUID()
	result.Title = idgen.ImportedTitle(title, "导入的 Qoder 任务")
	result.CWD = cwd
	result.RuntimePath = restoreRuntimePath(runtimeHome, cwd, result.SessionID)
	history, messages, err := buildRestoreHistory(entries, result, now)
	if err != nil {
		return result, err
	}
	// _txlock=immediate serializes duplicate detection with inserts, also when
	// another handoff process is restoring to the same profile.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return result, err
	}
	defer func() { _ = tx.Rollback() }()
	if !opts.AllowDuplicate {
		if existing, err := findRestoreDuplicate(ctx, tx, result.DatabasePath, runtimeHome, opts.Fingerprint); err != nil || existing.Duplicate {
			return existing, err
		}
	}
	var workspaceCreated bool
	result.WorkspaceID, workspaceCreated, err = restoreWorkspace(ctx, tx, cwd, now)
	if err != nil {
		return result, err
	}
	if err := insertRestoredSession(ctx, tx, result, messages, opts.Fingerprint, now); err != nil {
		return result, err
	}
	result.BackupDir, err = writeRestoreRecovery(result, workspaceCreated, now)
	if err != nil {
		return result, fmt.Errorf("record Qoder restore recovery information: %w", err)
	}
	keepRecovery := false
	defer func() {
		if !keepRecovery {
			_ = os.Remove(filepath.Join(result.BackupDir, "restore.json"))
			_ = os.Remove(result.BackupDir)
		}
	}()
	if err := publishRestoreHistory(result.RuntimePath, history); err != nil {
		return result, fmt.Errorf("publish Qoder runtime history: %w", err)
	}
	if err := tx.Commit(); err != nil {
		// COMMIT may return an error after SQLite made its outcome durable.
		// Preserve the file if the row was committed or cannot be checked.
		rowErr := db.QueryRowContext(ctx, "SELECT 1 FROM chat_sessions WHERE session_id = ?", result.SessionID).Scan(new(int))
		if errors.Is(rowErr, sql.ErrNoRows) {
			if cleanupErr := os.Remove(result.RuntimePath); cleanupErr != nil && !errors.Is(cleanupErr, os.ErrNotExist) {
				keepRecovery = true
				return result, errors.Join(err, cleanupErr)
			}
		} else if rowErr == nil {
			keepRecovery = true
			return result, nil
		} else {
			keepRecovery = true
		}
		return result, fmt.Errorf("commit Qoder restore: %w", err)
	}
	keepRecovery = true
	return result, nil
}

func restoreDatabaseURI(path, mode string) string {
	path = filepath.ToSlash(path)
	if runtime.GOOS == "windows" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	query := url.Values{
		"mode":    {mode},
		"_pragma": {"busy_timeout(5000)", "foreign_keys(ON)"},
		"_txlock": {"immediate"},
	}
	return (&url.URL{Scheme: "file", Path: path, RawQuery: query.Encode()}).String()
}

type restoreQueryRow interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func findRestoreDuplicate(ctx context.Context, querier restoreQueryRow, databasePath, runtimeHome, fingerprint string) (RestoreResult, error) {
	result := RestoreResult{DatabasePath: databasePath}
	if fingerprint == "" {
		return result, nil
	}
	err := querier.QueryRowContext(ctx, `
		SELECT session_id, title, cwd, COALESCE(workspace_id, '')
		FROM chat_sessions
		WHERE deleted_at IS NULL AND json_extract(
			CASE WHEN json_valid(extra_json) THEN extra_json ELSE '{}' END,
			'$.agentHandoffFingerprint') = ?
		ORDER BY created_at DESC, session_id LIMIT 1`, fingerprint).
		Scan(&result.SessionID, &result.Title, &result.CWD, &result.WorkspaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("query Qoder duplicate restore: %w", err)
	}
	result.Duplicate = true
	result.RuntimePath = restoreRuntimePath(runtimeHome, result.CWD, result.SessionID)
	return result, nil
}

// Existing task and workspace rows are untouched. SQLite protects uncommitted
// changes, and this receipt identifies the new runtime file and catalog rows if
// the process stops between publishing history and committing the database.
func writeRestoreRecovery(result RestoreResult, workspaceCreated bool, now time.Time) (string, error) {
	root := filepath.Join(filepath.Dir(result.DatabasePath), "backups_state", "agent-handoff")
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp(root, now.Format("20060102T150405Z")+"-"+result.SessionID+"-")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "restore.json")
	data, err := json.Marshal(map[string]any{
		"session_id": result.SessionID, "database_path": result.DatabasePath,
		"runtime_path": result.RuntimePath, "workspace_id": result.WorkspaceID,
		"workspace_created": workspaceCreated, "created_at": now,
	})
	if err == nil {
		err = os.WriteFile(path, data, 0o600)
	}
	if err != nil {
		_ = os.Remove(path)
		_ = os.Remove(dir)
		return "", err
	}
	return dir, nil
}

func restoreWorkspace(ctx context.Context, tx *sql.Tx, cwd string, now time.Time) (string, bool, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT workspace_id, root_paths_json FROM workspaces
		WHERE workspace_source = 'local' AND product_mode = 'coding'
		  AND archived = 0 AND deleted_at IS NULL
		ORDER BY updated_at DESC, workspace_id`)
	if err != nil {
		return "", false, fmt.Errorf("query Qoder workspaces: %w", err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id, rootsJSON string
		if err := rows.Scan(&id, &rootsJSON); err != nil {
			return "", false, err
		}
		var roots []string
		if json.Unmarshal([]byte(rootsJSON), &roots) == nil && slices.Contains(roots, cwd) {
			return id, false, nil
		}
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	id := idgen.NewUUID()
	roots, err := json.Marshal([]string{cwd})
	if err != nil {
		return "", false, err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO workspaces (workspace_id, name, workspace_source,
			root_paths_json, product_mode, created_at, updated_at)
		VALUES (?, ?, 'local', ?, 'coding', ?, ?)`,
		id, filepath.Base(cwd), string(roots), now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return "", false, fmt.Errorf("create Qoder workspace: %w", err)
	}
	return id, true, nil
}

func insertRestoredSession(ctx context.Context, tx *sql.Tx, result RestoreResult, messages []restoreMessage, fingerprint string, now time.Time) error {
	target, err := json.Marshal(map[string]any{
		"mode":      "local",
		"host":      map[string]string{"id": "local", "kind": "local", "label": "Local"},
		"workspace": map[string]string{"id": "ws_" + restoreDigest([]byte("local\x00"+result.CWD)), "path": result.CWD},
	})
	if err != nil {
		return err
	}
	extra, err := json.Marshal(map[string]any{
		"source": "workspace", "cliSessionReady": true, "titleResolved": true,
		"titleSource": "custom", "titleRevision": 0, "provisionalTitle": result.Title,
		"agentHandoffFingerprint": fingerprint,
	})
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO chat_sessions (session_id, session_kind, conversation_mode, product_mode,
			title, cwd, execution_kind, workspace_id, model, permission_mode, extra_json,
			created_at, updated_at, execution_target_json, message_envelope_version)
		VALUES (?, 'standard', 'normal', 'coding', ?, ?, 'local', ?, 'auto', 'default',
			?, ?, ?, ?, 1)`, result.SessionID, result.Title, result.CWD, result.WorkspaceID,
		string(extra), now.UnixMilli(), now.UnixMilli(), string(target))
	if err != nil {
		return fmt.Errorf("insert Qoder desktop session: %w", err)
	}
	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO chat_session_messages (session_id, message_id, turn_id, sequence,
			payload_json, status, source, created_at, updated_at, role, turn_started_at,
			is_compaction_boundary, has_visible_content, payload_bytes, payload_hash, payload_schema_version)
		VALUES (?, ?, ?, ?, ?, 'completed', 'cli-import', ?, ?, ?, ?, 0, 1, ?, ?, 2)`)
	if err != nil {
		return fmt.Errorf("prepare Qoder desktop message insert: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for index, message := range messages {
		if _, err := stmt.ExecContext(ctx, result.SessionID, message.ID, message.TurnID, index+1,
			string(message.Payload), message.Timestamp.UnixMilli(), message.Timestamp.UnixMilli(),
			message.Role, message.TurnStartedAt, len(message.Payload), restoreDigest(message.Payload)); err != nil {
			return fmt.Errorf("insert Qoder desktop message %d: %w", index+1, err)
		}
	}
	return nil
}

func restoreDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
