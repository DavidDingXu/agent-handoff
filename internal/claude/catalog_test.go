package claude

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Lexicographic timestamp order puts whole seconds after later fractions.
func TestCatalogSortsClaudeTimesChronologically(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ProjectsDir, "synthetic")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, task := range []struct{ id, at string }{{"old", "2026-11-19T10:00:00Z"}, {"new", "2026-11-19T10:00:00.500Z"}} {
		line := fmt.Sprintf(`{"type":"user","sessionId":%q,"cwd":"/synthetic/project","timestamp":%q,"message":{"role":"user","content":"Synthetic task"}}`, task.id, task.at)
		if err := os.WriteFile(filepath.Join(dir, task.id+".jsonl"), []byte(line+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tasks, err := ListTasks(home, "/synthetic/project")
	if err != nil || len(tasks) != 2 || tasks[0].ID != "new" {
		t.Fatalf("chronological catalog=%+v, error=%v", tasks, err)
	}
}
