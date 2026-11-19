package codex

import (
	"os"
	"strings"
	"testing"
)

// A stale SQLite rollout_path must not relabel another native session as the
// explicitly requested source task.
func TestLoadThreadRejectsMismatchedRolloutIdentity(t *testing.T) {
	home, path := compressedTestHome(t, "canonical")
	wrong := strings.ReplaceAll(compressedSession, compressedThreadID, "0192aaaa-bbbb-7ccc-8ddd-eeeeffff0002")
	if err := os.WriteFile(path, []byte(wrong), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := LoadThread(home, compressedThreadID)
	if err == nil || data != nil || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("mismatched session accepted: data=%+v, error=%v", data, err)
	}
}
