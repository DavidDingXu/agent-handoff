package codex

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const compressedThreadID = "0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001"
const compressedSession = `{"type":"session_meta","payload":{"id":"0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001","cwd":"/src/project"}}
{"type":"event_msg","payload":{"type":"user_message","message":"Compressed history survives"}}
`

func TestLoadThreadCompressedRollout(t *testing.T) {
	for _, tc := range []struct {
		name       string
		storedPath string
		plain      bool
	}{
		{name: "canonical path", storedPath: "canonical"},
		{name: "compressed path", storedPath: "compressed"},
		{name: "discovered path", storedPath: "discovered"},
		{name: "prefer plain canonical path", storedPath: "canonical", plain: true},
		{name: "prefer plain compressed path", storedPath: "compressed", plain: true},
		{name: "prefer plain discovered path", storedPath: "discovered", plain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, path := compressedTestHome(t, tc.storedPath)
			writeCompressedRollout(t, path+".zst", []byte(compressedSession))
			want := "Compressed history survives"
			if tc.plain {
				want = "Newer plain history survives"
				plain := strings.ReplaceAll(compressedSession, "Compressed history survives", want)
				if err := os.WriteFile(path, []byte(plain), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			data, err := LoadThread(home, compressedThreadID)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(data.SessionBytes, []byte(want)) {
				t.Fatalf("export did not preserve %q: %s", want, data.SessionBytes)
			}
			if !tc.plain {
				if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("reading compressed rollout created a plain file: %v", err)
				}
			}
		})
	}
}

func TestLoadThreadCompressedPaginationBase(t *testing.T) {
	home, path := compressedTestHome(t, "canonical")
	baseID := "0192bbbb-cccc-7ddd-8eee-ffff00001111"
	basePath := filepath.Join(filepath.Dir(path), "rollout-2026-08-01T09-00-00-"+baseID+".jsonl.zst")
	base := `{"type":"session_meta","payload":{"id":"` + baseID + `","cwd":"/src/project"}}
{"type":"event_msg","payload":{"type":"user_message","message":"压缩之前的历史"}}
`
	// end_byte_offset counts decompressed UTF-8 bytes, not compressed bytes
	// or characters. The base may have later turns outside the fork snapshot.
	writeCompressedRollout(t, basePath, []byte(base+`{"type":"event_msg","payload":{"type":"user_message","message":"Excluded later base turn"}}`+"\n"))
	tail := fmt.Sprintf(`{"type":"session_meta","payload":{"id":%q,"cwd":"/src/project","history_mode":"paginated","history_base":{"thread_id":%q,"end_byte_offset":%d}}}
{"type":"event_msg","payload":{"type":"user_message","message":"Current tail"}}
`, compressedThreadID, baseID, len(base))
	writeCompressedRollout(t, path+".zst", []byte(tail))
	data, err := LoadThread(home, compressedThreadID)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data.SessionBytes)
	if !strings.Contains(got, "压缩之前的历史") || !strings.Contains(got, "Current tail") {
		t.Fatalf("history is incomplete: %s", got)
	}
	if strings.Index(got, "压缩之前的历史") > strings.Index(got, "Current tail") {
		t.Fatalf("history is out of order: %s", got)
	}
	for _, unwanted := range []string{"Excluded later base turn", "history_base", "paginated"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("standalone export contains %q", unwanted)
		}
	}
}

func TestLoadThreadRejectsCorruptCompressedRollout(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "invalid frame", data: []byte("not a zstd frame")},
		{name: "truncated frame", data: []byte{0x28, 0xb5, 0x2f, 0xfd, 0x20}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home, path := compressedTestHome(t, "compressed")
			if err := os.WriteFile(path+".zst", tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			data, err := LoadThread(home, compressedThreadID)
			if err == nil || data != nil {
				t.Fatalf("corrupt compression was accepted: data=%v err=%v", data, err)
			}
			if !strings.Contains(err.Error(), "decompress") {
				t.Fatalf("error should identify failed decompression: %v", err)
			}
		})
	}
}

func TestVerifyCompressedRollout(t *testing.T) {
	home, path := compressedTestHome(t, "canonical")
	writeCompressedRollout(t, path+".zst", []byte(compressedSession))
	if got := Verify(home, compressedThreadID, "/src/project"); got.Status != "ok" {
		t.Fatalf("compressed rollout failed verification: %+v", got)
	}
	wrong := strings.ReplaceAll(compressedSession, "/src/project", "/wrong/project")
	writeCompressedRollout(t, path+".zst", []byte(wrong))
	if got := Verify(home, compressedThreadID, "/src/project"); got.Status != "failed" || !strings.Contains(strings.Join(got.Failures, " "), "session cwd mismatch") {
		t.Fatalf("compressed rollout cwd mismatch was not detected: %+v", got)
	}
}

func TestLoadThreadWithoutStateDatabase(t *testing.T) {
	home, path := compressedTestHome(t, "canonical")
	if err := os.Remove(filepath.Join(home, StateSQLiteFile)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(compressedSession), 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := LoadThread(home, compressedThreadID)
	if err != nil {
		t.Fatal(err)
	}
	if data.Title != "Compression test" || data.CWD != "/src/project" || !bytes.Contains(data.SessionBytes, []byte("Compressed history survives")) {
		t.Fatalf("rollout and index were not used without a database: %+v", data)
	}
}

func TestLoadThreadRejectsMissingRowInExistingDatabase(t *testing.T) {
	home, path := compressedTestHome(t, "canonical")
	if err := os.WriteFile(path, []byte(compressedSession), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(home, StateSQLiteFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM threads`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if data, err := LoadThread(home, compressedThreadID); err == nil || data != nil {
		t.Fatalf("missing row in existing database was ignored: data=%v err=%v", data, err)
	}
}

func compressedTestHome(t *testing.T, storedPath string) (string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(home, "sessions", "2026", "08", "01", "rollout-2026-08-01T10-00-00-"+compressedThreadID+".jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	index := `{"id":"` + compressedThreadID + `","thread_name":"Compression test"}` + "\n"
	if err := os.WriteFile(filepath.Join(home, SessionIndexFile), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	rowPath := path
	switch storedPath {
	case "compressed":
		rowPath += ".zst"
	case "discovered":
		rowPath = ""
	}
	db, err := sql.Open("sqlite", filepath.Join(home, StateSQLiteFile))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE threads (id TEXT PRIMARY KEY, title TEXT, cwd TEXT, rollout_path TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO threads (id, title, cwd, rollout_path) VALUES (?, ?, ?, ?)`, compressedThreadID, "Compression test", "/src/project", rowPath); err != nil {
		t.Fatal(err)
	}
	return home, path
}

func writeCompressedRollout(t *testing.T, path string, data []byte) {
	t.Helper()
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = encoder.Close() }()
	if err := os.WriteFile(path, encoder.EncodeAll(data, nil), 0o600); err != nil {
		t.Fatal(err)
	}
}
