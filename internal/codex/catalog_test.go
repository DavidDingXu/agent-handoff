package codex

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestCatalogFindsRolloutWithoutIndexOrDatabase(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "11", "19", "rollout-"+compressedThreadID+".jsonl.zst")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	writeCompressedRollout(t, path, []byte(compressedSession))
	tasks, err := ListTasks(home, "/src/project")
	if err != nil || len(tasks) != 1 || tasks[0].ID != compressedThreadID || tasks[0].UpdatedAt == "" {
		t.Fatalf("rollout catalog=%+v, error=%v", tasks, err)
	}
}

func TestCatalogReadOnlyDatabasePathKeepsURLCharacters(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home # literal")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, StateSQLiteFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE threads(id TEXT PRIMARY KEY, title TEXT, cwd TEXT, updated_at_ms INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('literal-id', 'Literal task', '/workspace', 20000)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads VALUES ('newer-id', 'Newer task', '/workspace', 20500)`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	tasks, err := ListTasks(home, "/workspace")
	if err != nil || len(tasks) != 2 || tasks[0].ID != "newer-id" || tasks[1].ID != "literal-id" {
		t.Fatalf("literal path catalog=%+v, error=%v", tasks, err)
	}
}

func TestCatalogReadOnlyURIKeepsWindowsDriveInPath(t *testing.T) {
	got := catalogSQLiteURI("C:/Users/Test User/home # literal/state_5.sqlite")
	want := "file:///C:/Users/Test%20User/home%20%23%20literal/state_5.sqlite?mode=ro"
	if got != want {
		t.Fatalf("read-only SQLite URI=%q, want %q", got, want)
	}
}
