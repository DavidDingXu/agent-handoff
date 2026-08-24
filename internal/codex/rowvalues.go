package codex

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// upsertThreadRow inserts a new row into the threads table, writing only
// columns that exist on the receiver's schema. Values are derived by
// threadRowValues (clone source row, overlay import-specific fields). When
// the receiver has no state db yet, a minimal one is created so the imported
// task still shows in the task list.
func upsertThreadRow(home string, in RestoreInput, newID, title, targetSession, targetCWD string, now time.Time, crossAgent bool) error {
	dbPath := filepath.Join(home, StateSQLiteFile)
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		if err := createThreadsTable(dbPath); err != nil {
			return fmt.Errorf("create state db: %w", err)
		}
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()

	cols, err := SQLiteColumns(db, "threads")
	if err != nil {
		return err
	}

	var rowVals map[string]any
	if crossAgent {
		rowVals = synthesizedRowValues(in.Neutral, newID, title, targetSession, targetCWD, now)
	} else {
		rowVals = threadRowValues(in.ThreadRow, newID, title, targetSession, targetCWD, now)
	}

	var names, placeholders []string
	var values []any
	for _, c := range cols {
		v, ok := rowVals[c]
		if !ok {
			continue // let defaults/triggers fill it
		}
		names = append(names, QuoteIdent(c))
		placeholders = append(placeholders, "?")
		values = append(values, v)
	}
	if len(names) == 0 {
		return fmt.Errorf("no insertable columns found in threads table")
	}
	stmt := fmt.Sprintf("INSERT OR REPLACE INTO threads (%s) VALUES (%s)",
		joinQuoted(names), joinPlaceholders(placeholders))
	_, err = db.Exec(stmt, values...)
	return err
}

// createThreadsTable creates a minimal state db with a threads table for
// receivers that have never run Codex desktop. The column set mirrors the
// common Codex desktop schema so sender-row metadata survives the clone.
func createThreadsTable(dbPath string) error {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS threads (
		id TEXT PRIMARY KEY,
		title TEXT,
		cwd TEXT,
		rollout_path TEXT,
		model TEXT,
		model_provider TEXT,
		effort TEXT,
		source TEXT,
		git_branch TEXT,
		git_origin_url TEXT,
		cli_version TEXT,
		sandbox_policy TEXT,
		approval_mode TEXT,
		history_mode TEXT,
		memory_mode TEXT,
		tokens_used INTEGER DEFAULT 0,
		preview TEXT,
		first_user_message TEXT,
		created_at INTEGER,
		created_at_ms INTEGER,
		updated_at INTEGER,
		updated_at_ms INTEGER,
		recency_at INTEGER,
		recency_at_ms INTEGER,
		archived INTEGER DEFAULT 0,
		archived_at INTEGER,
		is_pinned INTEGER DEFAULT 0,
		has_user_event INTEGER DEFAULT 0
	)`)
	return err
}

func joinQuoted(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}

func joinPlaceholders(ps []string) string {
	out := ""
	for i, p := range ps {
		if i > 0 {
			out += ", "
		}
		out += p
	}
	return out
}

// threadRowValues clones the sender's thread row so sender-side metadata
// (model, git info, effort settings, and any future columns) survives the
// import, then overlays import-specific values and fills defaults only
// where the source row has nothing to offer.
func threadRowValues(src map[string]any, newID, title, targetSession, targetCWD string, now time.Time) map[string]any {
	vals := map[string]any{}
	for k, v := range src {
		if v != nil {
			vals[k] = v
		}
	}
	return overlayImportValues(vals, newID, title, targetSession, targetCWD, now)
}

// synthesizedRowValues builds a thread row for cross-agent imports where no
// codex source row exists.
func synthesizedRowValues(t neutral.Transcript, newID, title, targetSession, targetCWD string, now time.Time) map[string]any {
	vals := map[string]any{
		"source":         "agent-handoff",
		"model_provider": t.SourceAgent,
	}
	return overlayImportValues(vals, newID, title, targetSession, targetCWD, now)
}

func overlayImportValues(vals map[string]any, newID, title, targetSession, targetCWD string, now time.Time) map[string]any {
	preview := ""
	if t, ok := vals["preview"].(string); ok {
		preview = t
	}

	nowUnix := now.Unix()
	nowMS := now.UnixMilli()

	// Identity, location, and recency describe the imported copy.
	vals["id"] = newID
	vals["rollout_path"] = targetSession
	vals["cwd"] = targetCWD
	vals["title"] = title
	// Import time as creation time so the task lands on top of the task
	// list (desktop groups by created_at).
	vals["created_at"] = nowUnix
	vals["created_at_ms"] = nowMS
	vals["updated_at"] = nowUnix
	vals["updated_at_ms"] = nowMS
	vals["recency_at"] = nowUnix
	vals["recency_at_ms"] = nowMS
	// Fresh state on the receiver. The source column also encodes subagent
	// lineage on some Codex versions (a JSON object), so it is only set here
	// when not already a plain string from a same-agent clone.
	if s, ok := vals["source"].(string); !ok || s == "" {
		if _, has := vals["source"]; !has {
			vals["source"] = "vscode"
		}
	}
	vals["archived"] = 0
	vals["archived_at"] = nil
	vals["is_pinned"] = 0
	vals["has_user_event"] = 1

	// Defaults for fields the sender's row may not carry.
	defaults := map[string]any{
		"model_provider":     "OpenAI",
		"sandbox_policy":     `{"type":"disabled"}`,
		"approval_mode":      "never",
		"history_mode":       "legacy",
		"memory_mode":        "enabled",
		"tokens_used":        0,
		"preview":            preview,
		"first_user_message": preview,
	}
	for k, v := range defaults {
		if _, ok := vals[k]; !ok {
			vals[k] = v
		}
	}
	return vals
}

// FirstUserMessagePreview extracts the first user message for the preview
// columns.
func FirstUserMessagePreview(sessionBytes []byte) string {
	return session.FirstUserMessage(sessionBytes)
}
