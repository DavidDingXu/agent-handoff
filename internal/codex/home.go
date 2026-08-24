// Package codex reads and writes Codex task state (~/.codex): session
// rollouts, session_index.jsonl, and the state_5.sqlite threads table.
package codex

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // pure-Go sqlite driver
)

// SessionIndexFile is the rollout index inside a Codex home.
const SessionIndexFile = "session_index.jsonl"

// StateSQLiteFile is the Codex desktop state database.
const StateSQLiteFile = "state_5.sqlite"

// DefaultHome returns ~/.codex.
func DefaultHome() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".codex"), nil
}

// ResolveHome resolves the Codex home: explicit flag > CODEX_HOME > ~/.codex.
func ResolveHome(flagHome string) (string, error) {
	if flagHome != "" {
		return filepath.Abs(flagHome)
	}
	if env := os.Getenv("CODEX_HOME"); env != "" {
		return filepath.Abs(env)
	}
	return DefaultHome()
}

// ResolveThread maps "current" (or empty) to a real thread id. Priority:
// CODEX_THREAD_ID/CODEX_SESSION_ID env (injected by the Codex agent shell),
// then the newest sqlite thread whose cwd matches the working directory,
// then the globally newest sqlite thread, then session_index.jsonl.
func ResolveThread(home, requested string) (string, error) {
	req := strings.TrimSpace(requested)
	if req != "" && req != "current" {
		return req, nil
	}
	for _, key := range []string{"CODEX_THREAD_ID", "CODEX_SESSION_ID"} {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v, nil
		}
	}
	if wd, _ := os.Getwd(); wd != "" {
		if id, err := latestThreadWhere(home, "cwd = ?", wd); err == nil && id != "" {
			return id, nil
		}
	}
	if id, err := latestThreadWhere(home, "", ""); err == nil && id != "" {
		return id, nil
	}
	entries, err := ReadSessionIndex(home)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", errors.New("no Codex tasks found")
	}
	return newestIndexEntry(entries)
}

func latestThreadWhere(home, where, arg string) (string, error) {
	dbPath := filepath.Join(home, StateSQLiteFile)
	if !fileExists(dbPath) {
		return "", os.ErrNotExist
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return "", err
	}
	defer func() { _ = db.Close() }()

	orderBy := "id desc"
	if cols, err := SQLiteColumns(db, "threads"); err == nil {
		have := map[string]bool{}
		for _, c := range cols {
			have[c] = true
		}
		switch {
		case have["updated_at_ms"]:
			orderBy = "updated_at_ms desc"
		case have["updated_at"]:
			orderBy = "updated_at desc"
		}
	}
	query := "SELECT id FROM threads " + where + " ORDER BY " + orderBy + " LIMIT 1"
	var id string
	if where == "" {
		err = db.QueryRow(query).Scan(&id)
	} else {
		err = db.QueryRow(query, arg).Scan(&id)
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// ReadSessionIndex parses session_index.jsonl into maps.
func ReadSessionIndex(home string) ([]map[string]any, error) {
	data, err := os.ReadFile(filepath.Join(home, SessionIndexFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []map[string]any
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var m map[string]any
		if jsonUnmarshal(line, &m) == nil {
			entries = append(entries, m)
		}
	}
	return entries, nil
}

// AppendSessionIndex appends one entry to session_index.jsonl.
func AppendSessionIndex(home string, entry map[string]any) error {
	data, err := jsonMarshal(entry)
	if err != nil {
		return err
	}
	path := filepath.Join(home, SessionIndexFile)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open session index: %w", err)
	}
	if _, err := f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return fmt.Errorf("append session index: %w", err)
	}
	return f.Close()
}

func newestIndexEntry(entries []map[string]any) (string, error) {
	var bestID string
	var bestAt int64
	for _, e := range entries {
		id, _ := e["id"].(string)
		if id == "" {
			continue
		}
		var at int64
		if ms, ok := e["updated_at_ms"].(float64); ok {
			at = int64(ms)
		} else if s, ok := e["updated_at"].(string); ok {
			at = parseIndexTime(s)
		}
		if bestID == "" || at > bestAt {
			bestID, bestAt = id, at
		}
	}
	if bestID == "" {
		return "", errors.New("could not determine the current task")
	}
	return bestID, nil
}

// ---- sqlite helpers (schema-aware) ----

// SQLiteColumns returns the column names of a table.
func SQLiteColumns(db *sql.DB, table string) ([]string, error) {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var cols []string
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var dflt any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols = append(cols, name)
	}
	return cols, rows.Err()
}

// QuoteIdent quotes a SQL identifier.
func QuoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// ---- small io helpers ----

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
