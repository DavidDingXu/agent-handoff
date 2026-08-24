package codex

import (
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// ExportData is everything read about a thread on the sender side.
type ExportData struct {
	ThreadID     string
	Title        string
	CWD          string
	SessionPath  string
	SessionBytes []byte
	ThreadRow    map[string]any
}

// LoadThread loads title, cwd, session bytes and the sqlite row for a thread.
func LoadThread(home, threadID string) (*ExportData, error) {
	title := ""
	entries, err := ReadSessionIndex(home)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if id, _ := e["id"].(string); id == threadID {
			title, _ = e["thread_name"].(string)
			break
		}
	}

	row, err := ReadThreadRow(home, threadID)
	if err != nil {
		return nil, fmt.Errorf("read thread row: %w", err)
	}
	rolloutPath, _ := row["rollout_path"].(string)
	cwd, _ := row["cwd"].(string)

	sessionPath := rolloutPath
	if sessionPath != "" && !filepath.IsAbs(sessionPath) {
		sessionPath = filepath.Join(home, sessionPath)
	}
	if sessionPath == "" {
		sessionPath, err = findRolloutByThreadID(home, threadID)
		if err != nil {
			return nil, err
		}
	}
	sessionBytes, err := os.ReadFile(sessionPath)
	if err != nil {
		return nil, fmt.Errorf("read session file: %w", err)
	}

	if title == "" {
		title, _ = row["title"].(string)
	}
	if title == "" {
		title = threadID
	}
	if cwd == "" {
		cwd = session.FirstSessionCWD(sessionBytes)
	}
	return &ExportData{
		ThreadID:     threadID,
		Title:        title,
		CWD:          cwd,
		SessionPath:  sessionPath,
		SessionBytes: sessionBytes,
		ThreadRow:    row,
	}, nil
}

// ReadThreadRow reads one row from the threads table, normalized
// ([]byte -> string).
func ReadThreadRow(home, threadID string) (map[string]any, error) {
	dbPath := filepath.Join(home, StateSQLiteFile)
	if _, err := os.Stat(dbPath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("thread %s not found (no %s)", threadID, StateSQLiteFile)
	}
	db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer func() { _ = db.Close() }()

	cols, err := SQLiteColumns(db, "threads")
	if err != nil {
		return nil, err
	}
	names := make([]string, len(cols))
	for i, c := range cols {
		names[i] = QuoteIdent(c)
	}
	stmt := "SELECT " + strings.Join(names, ", ") + " FROM threads WHERE id = ?"
	rows, err := db.Query(stmt, threadID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return nil, fmt.Errorf("thread %s not found in %s", threadID, StateSQLiteFile)
	}
	values := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range values {
		ptrs[i] = &values[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for i, c := range cols {
		v := values[i]
		if b, ok := v.([]byte); ok {
			v = string(b)
		}
		out[c] = v
	}
	return out, rows.Err()
}

// findRolloutByThreadID scans the sessions tree for a rollout file whose
// name embeds the thread id.
func findRolloutByThreadID(home, threadID string) (string, error) {
	var found string
	root := filepath.Join(home, "sessions")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if strings.Contains(d.Name(), threadID) && strings.HasSuffix(d.Name(), ".jsonl") {
			found = path
		}
		return nil
	})
	if found == "" {
		return "", fmt.Errorf("session file for thread %s not found", threadID)
	}
	return found, nil
}

// VerifyResult reports import integrity.
type VerifyResult struct {
	Status            string   `json:"status"`
	ThreadID          string   `json:"thread_id"`
	ThreadName        string   `json:"thread_name,omitempty"`
	SQLiteRowExists   bool     `json:"sqlite_row_exists"`
	IndexEntryExists  bool     `json:"index_entry_exists"`
	SessionFileExists bool     `json:"session_file_exists"`
	SQLiteCWD         string   `json:"sqlite_cwd,omitempty"`
	Failures          []string `json:"failures,omitempty"`
}

// Verify checks that an imported thread is consistent across the three
// storage locations. With expectedCWD non-empty, it additionally asserts the
// session contains exactly one cwd (the rewritten one) — catching any
// leftover sender paths after an import rewrite.
func Verify(home, threadID, expectedCWD string) *VerifyResult {
	res := &VerifyResult{Status: "ok", ThreadID: threadID}

	entries, _ := ReadSessionIndex(home)
	for _, e := range entries {
		if id, _ := e["id"].(string); id == threadID {
			res.IndexEntryExists = true
			res.ThreadName, _ = e["thread_name"].(string)
			break
		}
	}

	row, err := ReadThreadRow(home, threadID)
	if err == nil {
		res.SQLiteRowExists = true
		res.SQLiteCWD, _ = row["cwd"].(string)
	} else if !strings.Contains(err.Error(), "not found") {
		res.Failures = append(res.Failures, "read thread row: "+err.Error())
	}

	sessionPath := ""
	if row != nil {
		if p, _ := row["rollout_path"].(string); p != "" {
			sessionPath = p
			if !filepath.IsAbs(sessionPath) {
				sessionPath = filepath.Join(home, sessionPath)
			}
		}
	}
	if sessionPath == "" {
		sessionPath, _ = findRolloutByThreadID(home, threadID)
	}
	if sessionPath != "" && fileExists(sessionPath) {
		res.SessionFileExists = true
	}

	if !res.IndexEntryExists {
		res.Failures = append(res.Failures, "session_index.jsonl entry missing")
	}
	if !res.SQLiteRowExists && fileExists(filepath.Join(home, StateSQLiteFile)) {
		res.Failures = append(res.Failures, "sqlite thread row missing")
	}
	if !res.SessionFileExists {
		res.Failures = append(res.Failures, "session rollout file missing")
	}

	if expectedCWD != "" && res.SessionFileExists {
		if data, err := os.ReadFile(sessionPath); err == nil {
			cwds := session.SessionCWDs(data)
			if len(cwds) != 1 || !cwds[expectedCWD] {
				res.Failures = append(res.Failures, fmt.Sprintf(
					"session cwd mismatch: expected exactly %q, found %d distinct cwd(s)", expectedCWD, len(cwds)))
			}
		}
		if res.SQLiteCWD != "" && res.SQLiteCWD != expectedCWD {
			res.Failures = append(res.Failures, "sqlite cwd mismatch: "+res.SQLiteCWD)
		}
	}

	if len(res.Failures) > 0 {
		res.Status = "failed"
	}
	return res
}
