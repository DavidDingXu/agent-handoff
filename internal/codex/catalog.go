package codex

import (
	"bufio"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/klauspost/compress/zstd"
)

// CatalogEntry identifies a local task without choosing it for the caller.
type CatalogEntry struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	CWD       string `json:"cwd"`
	UpdatedAt string `json:"updated_at"`
}

// ListTasks reads local task metadata. It never changes the active task or
// invokes an agent, and the optional cwd filter matches a whole path only.
func ListTasks(home, cwd string) ([]CatalogEntry, error) {
	byID := map[string]CatalogEntry{}
	index, err := ReadSessionIndex(home)
	if err != nil {
		return nil, err
	}
	for _, row := range index {
		id, _ := row["id"].(string)
		if id == "" {
			continue
		}
		title, _ := row["thread_name"].(string)
		updated, _ := row["updated_at"].(string)
		if at, err := time.Parse(time.RFC3339Nano, updated); err == nil {
			updated = at.UTC().Format(time.RFC3339Nano)
		} else if ms, ok := row["updated_at_ms"].(float64); ok && ms > 0 {
			updated = time.UnixMilli(int64(ms)).UTC().Format(time.RFC3339Nano)
		}
		byID[id] = CatalogEntry{ID: id, Title: title, UpdatedAt: updated}
	}
	path := filepath.Join(home, StateSQLiteFile)
	if _, err := os.Stat(path); err == nil {
		db, err := sql.Open("sqlite", catalogSQLiteURI(path))
		if err != nil {
			return nil, err
		}
		defer func() { _ = db.Close() }()
		cols, err := SQLiteColumns(db, "threads")
		if err != nil {
			return nil, err
		}
		have := map[string]bool{}
		for _, col := range cols {
			have[col] = true
		}
		if have["id"] {
			column := func(name string) string {
				if have[name] {
					return "COALESCE(" + QuoteIdent(name) + ", '')"
				}
				return "''"
			}
			updatedCol := "0"
			if have["updated_at_ms"] {
				updatedCol = "COALESCE(updated_at_ms, 0)"
			} else if have["updated_at"] {
				updatedCol = "COALESCE(updated_at, 0) * 1000"
			}
			rows, err := db.Query("SELECT id, " + column("title") + ", " + column("cwd") + ", " + updatedCol + " FROM threads")
			if err != nil {
				return nil, err
			}
			defer func() { _ = rows.Close() }()
			for rows.Next() {
				var id, title, taskCWD string
				var updated int64
				if err := rows.Scan(&id, &title, &taskCWD, &updated); err != nil {
					return nil, err
				}
				entry := byID[id]
				entry.ID, entry.CWD = id, taskCWD
				if entry.Title == "" {
					entry.Title = title
				}
				if updated > 0 {
					entry.UpdatedAt = time.UnixMilli(updated).UTC().Format(time.RFC3339Nano)
				}
				byID[id] = entry
			}
			if err := rows.Err(); err != nil {
				return nil, err
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read Codex catalog: %w", err)
	}
	err = filepath.WalkDir(filepath.Join(home, "sessions"), func(path string, file fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if file.IsDir() || !strings.HasSuffix(plainRolloutPath(file.Name()), ".jsonl") {
			return nil
		}
		if strings.HasSuffix(path, ".zst") && fileExists(plainRolloutPath(path)) {
			return nil
		}
		meta, err := catalogRolloutEntry(path)
		if err != nil || meta.ID == "" {
			// A partial or malformed file is not an independent selectable task.
			return nil
		}
		entry := byID[meta.ID]
		entry.ID = meta.ID
		if entry.CWD == "" {
			entry.CWD = meta.CWD
		}
		if entry.UpdatedAt == "" {
			entry.UpdatedAt = meta.UpdatedAt
		}
		byID[meta.ID] = entry
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]CatalogEntry, 0, len(byID))
	for _, entry := range byID {
		if cwd != "" && filepath.Clean(entry.CWD) != filepath.Clean(cwd) {
			continue
		}
		if entry.Title == "" {
			entry.Title = entry.ID
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		iTime, _ := time.Parse(time.RFC3339Nano, out[i].UpdatedAt)
		jTime, _ := time.Parse(time.RFC3339Nano, out[j].UpdatedAt)
		if iTime.Equal(jTime) {
			return out[i].ID < out[j].ID
		}
		return iTime.After(jTime)
	})
	return out, nil
}

func catalogSQLiteURI(path string) string {
	path = filepath.ToSlash(path)
	if len(path) >= 3 && path[1] == ':' && path[2] == '/' {
		path = "/" + path
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	return uri.String()
}

// A catalog needs only the native session header, not the complete history or
// pagination chain. Bound the read even when an unrelated file is malformed.
func catalogRolloutEntry(path string) (CatalogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return CatalogEntry{}, err
	}
	defer func() { _ = f.Close() }()
	var reader io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		decoder, err := zstd.NewReader(f, zstd.WithDecoderConcurrency(1))
		if err != nil {
			return CatalogEntry{}, err
		}
		defer decoder.Close()
		reader = decoder
	}
	scanner := bufio.NewScanner(io.LimitReader(reader, 1<<20))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var line struct {
			Type    string `json:"type"`
			Payload struct {
				ID  string `json:"id"`
				CWD string `json:"cwd"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &line) != nil || line.Type != "session_meta" {
			continue
		}
		entry := CatalogEntry{ID: line.Payload.ID, CWD: line.Payload.CWD}
		if info, err := f.Stat(); err == nil {
			entry.UpdatedAt = info.ModTime().UTC().Format(time.RFC3339Nano)
		}
		return entry, nil
	}
	return CatalogEntry{}, scanner.Err()
}
