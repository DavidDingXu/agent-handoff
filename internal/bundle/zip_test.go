package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

func testManifest(agent string) *Manifest {
	return &Manifest{
		FormatVersion:  Version2,
		ArtifactType:   ArtifactType,
		SourceAgent:    agent,
		TargetSupport:  SupportedAgents,
		SourceThreadID: "0192aaaa-bbbb-7ccc-8ddd-eeeeffff0001",
		Title:          "Fix the login bug",
		SourceCWD:      "/src/project",
		CreatedAt:      "2026-08-01T10:00:00.000Z",
		MessageCount:   8,
	}
}

const testSession = `{"timestamp":"2026-08-01T10:00:00.000Z","type":"session_meta","payload":{"id":"t1","cwd":"/src/project"}}
{"timestamp":"2026-08-01T10:00:02.000Z","type":"event_msg","payload":{"type":"user_message","message":"Fix the login bug"}}
{"timestamp":"2026-08-01T10:00:06.000Z","type":"event_msg","payload":{"type":"agent_message","message":"Done."}}
`

func writeTestZip(t *testing.T, agent string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.agent-handoff.zip")
	m := testManifest(agent)
	in := WriterInput{
		Manifest: m,
		Session:  []byte(testSession),
		Meta:     map[string]any{"id": "t1", "title": "Fix the login bug"},
		Neutral:  neutral.Transcript{Schema: neutral.Schema, SourceAgent: agent, SourceID: "t1"},
		Images: []ImageAsset{
			{ID: "img1", ZipPath: ImagesPrefix(agent) + "x.png", SHA256: "abc", Status: "copied"},
		},
	}
	in.ImageData = map[string][]byte{in.Images[0].ZipPath: []byte("pngbytes")}
	if err := WriteZip(path, in); err != nil {
		t.Fatalf("WriteZip: %v", err)
	}
	return path
}

func TestWriteZipRoundTripCodex(t *testing.T) {
	path := writeTestZip(t, AgentCodex)
	res, err := ReadZip(path)
	if err != nil {
		t.Fatalf("ReadZip: %v", err)
	}
	if res.Manifest.SourceAgent != AgentCodex {
		t.Errorf("source agent = %q", res.Manifest.SourceAgent)
	}
	if string(res.Session) != testSession {
		t.Errorf("session round-trip mismatch:\n got %q\nwant %q", res.Session, testSession)
	}
	if res.Meta["id"] != "t1" {
		t.Errorf("meta id = %v", res.Meta["id"])
	}
	if got := string(res.ImageData[CodexImagesPrefix()+"x.png"]); got != "pngbytes" {
		t.Errorf("image data = %q", got)
	}
	if len(res.Images) != 1 || !res.Images[0].Copied() {
		t.Errorf("images = %+v", res.Images)
	}
	if err := res.VerifyChecksums(); err != nil {
		t.Errorf("VerifyChecksums: %v", err)
	}
	// Neutral transcript must be present.
	if res.Neutral.Schema != neutral.Schema {
		t.Errorf("neutral schema = %q", res.Neutral.Schema)
	}
}

func CodexImagesPrefix() string { return ImagesPrefix(AgentCodex) }

func TestWriteZipRoundTripClaude(t *testing.T) {
	path := writeTestZip(t, AgentClaude)
	res, err := ReadZip(path)
	if err != nil {
		t.Fatalf("ReadZip: %v", err)
	}
	if res.Manifest.SourceAgent != AgentClaude {
		t.Errorf("source agent = %q", res.Manifest.SourceAgent)
	}
	if string(res.Session) != testSession {
		t.Errorf("session round-trip mismatch")
	}
	if err := res.VerifyChecksums(); err != nil {
		t.Errorf("VerifyChecksums: %v", err)
	}
}

func TestReadZipBytesMatchesFile(t *testing.T) {
	path := writeTestZip(t, AgentCodex)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	fromBytes, err := ReadZipBytes(data)
	if err != nil {
		t.Fatalf("ReadZipBytes: %v", err)
	}
	fromFile, err := ReadZip(path)
	if err != nil {
		t.Fatalf("ReadZip: %v", err)
	}
	if fromBytes.Manifest.SourceThreadID != fromFile.Manifest.SourceThreadID {
		t.Error("manifest mismatch between ReadZipBytes and ReadZip")
	}
	if string(fromBytes.Session) != string(fromFile.Session) {
		t.Error("session mismatch between ReadZipBytes and ReadZip")
	}
}

func TestWriteZipBytesRoundTrip(t *testing.T) {
	in := WriterInput{
		Manifest: testManifest(AgentCodex),
		Session:  []byte(testSession),
		Neutral:  neutral.Transcript{Schema: neutral.Schema, SourceAgent: AgentCodex},
	}
	data, err := WriteZipBytes(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadZipBytes(data)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Session) != testSession {
		t.Fatalf("session = %q, want %q", got.Session, testSession)
	}
}

func TestWriteZipUsesPrivatePermissions(t *testing.T) {
	path := writeTestZip(t, AgentCodex)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("zip mode = %o, want 600", got)
	}
}

func TestReadZipV1FlatLayout(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "v1.agent-handoff.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	write := func(name string, data []byte) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	manifest, _ := json.MarshalIndent(map[string]any{
		"format_version": 1,
		"artifact_type":  ArtifactType,
		"thread_id":      "t1",
		"title":          "Old task",
		"source_cwd":     "/old",
		"created_at":     "2026-01-01T00:00:00.000Z",
		"message_count":  2,
		"files":          []string{"manifest.json", "session.jsonl"},
	}, "", "  ")
	write(ManifestEntry, manifest)
	write("session.jsonl", []byte(testSession))
	write("thread-row.json", []byte(`{"id":"t1","title":"Old task"}`))
	write("images.json", []byte(`{"schema":"agent-handoff.images.v1","count":0,"images":[]}`))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	res, err := ReadZip(path)
	if err != nil {
		t.Fatalf("ReadZip v1: %v", err)
	}
	if res.Manifest.FormatVersion != Version1 {
		t.Errorf("version = %d, want 1", res.Manifest.FormatVersion)
	}
	if string(res.Session) != testSession {
		t.Error("v1 session mismatch")
	}
	if res.Meta["id"] != "t1" {
		t.Errorf("v1 meta id = %v", res.Meta["id"])
	}
	// v1 bundles predate checksums: verification is a no-op.
	if err := res.VerifyChecksums(); err != nil {
		t.Errorf("v1 VerifyChecksums: %v", err)
	}
}

func TestReadZipRejectsGarbage(t *testing.T) {
	dir := t.TempDir()

	// Not a zip at all.
	plain := filepath.Join(dir, "plain.zip")
	os.WriteFile(plain, []byte("this is not a zip"), 0o644)
	if _, err := ReadZip(plain); err == nil {
		t.Error("non-zip input should fail")
	}

	// Zip without manifest.
	nomanifest := filepath.Join(dir, "nomanifest.zip")
	f, _ := os.Create(nomanifest)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("foo.txt")
	w.Write([]byte("bar"))
	zw.Close()
	f.Close()
	if _, err := ReadZip(nomanifest); err != ErrMissingManifest {
		t.Errorf("missing manifest err = %v, want %v", err, ErrMissingManifest)
	}

	// Bad artifact type.
	badType := filepath.Join(dir, "badtype.zip")
	f, _ = os.Create(badType)
	zw = zip.NewWriter(f)
	w, _ = zw.Create(ManifestEntry)
	w.Write([]byte(`{"format_version":2,"artifact_type":"something-else","source_agent":"codex","files":[]}`))
	zw.Close()
	f.Close()
	if _, err := ReadZip(badType); err == nil {
		t.Error("bad artifact type should fail")
	}

	// Unsupported version.
	badVer := filepath.Join(dir, "badver.zip")
	f, _ = os.Create(badVer)
	zw = zip.NewWriter(f)
	w, _ = zw.Create(ManifestEntry)
	w.Write([]byte(`{"format_version":99,"artifact_type":"agent-handoff","source_agent":"codex","files":[]}`))
	zw.Close()
	f.Close()
	if _, err := ReadZip(badVer); err == nil {
		t.Error("unsupported version should fail")
	}
}

func TestReadZipRejectsTooManyEntries(t *testing.T) {
	entries := make([]*zip.File, maxZipEntries+1)
	for i := range entries {
		entries[i] = &zip.File{}
	}
	if _, err := readZipFiles(entries); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("too many entries err = %v, want ErrArchiveLimit", err)
	}
}

func TestReadZipRejectsOversizedDeclaredContent(t *testing.T) {
	entries := []*zip.File{
		{FileHeader: zip.FileHeader{Name: "a", UncompressedSize64: maxZipEntryBytes}},
		{FileHeader: zip.FileHeader{Name: "b", UncompressedSize64: maxZipEntryBytes}},
		{FileHeader: zip.FileHeader{Name: "c", UncompressedSize64: 1}},
	}
	if _, err := readZipFiles(entries); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("oversized total err = %v, want ErrArchiveLimit", err)
	}
}

func TestReadZipEntryLimitsActualDecompressedBytes(t *testing.T) {
	if _, err := readLimitedZipEntry(strings.NewReader("123456789"), "payload", 8); !errors.Is(err, ErrArchiveLimit) {
		t.Fatalf("oversized decompression err = %v, want ErrArchiveLimit", err)
	}
}

func TestChecksumsVerifyDetectsTampering(t *testing.T) {
	files := map[string][]byte{
		"manifest.json": []byte(`{"ok":true}`),
		"session.jsonl": []byte(testSession),
	}
	c := BuildChecksums(files)
	if err := c.Verify(files); err != nil {
		t.Fatalf("clean verify: %v", err)
	}

	// Tamper: modified content.
	tampered := map[string][]byte{
		"manifest.json": []byte(`{"ok":false}`),
		"session.jsonl": files["session.jsonl"],
	}
	if err := c.Verify(tampered); err == nil {
		t.Error("tampered content should fail verification")
	}

	// Tamper: file removed, extra file added.
	removed := map[string][]byte{"manifest.json": files["manifest.json"]}
	if err := c.Verify(removed); err == nil {
		t.Error("missing listed file should fail verification")
	}
	extra := map[string][]byte{
		"manifest.json": files["manifest.json"],
		"session.jsonl": files["session.jsonl"],
		"evil.txt":      []byte("injected"),
	}
	if err := c.Verify(extra); err == nil {
		t.Error("unlisted extra file should fail verification")
	}

	// Skip list exempts entries from coverage (but not from hash checks).
	if err := c.Verify(extra, "evil.txt"); err != nil {
		t.Errorf("exempted extra file should pass: %v", err)
	}
}

func TestParseChecksumsRejectsAlgorithm(t *testing.T) {
	if _, err := ParseChecksums([]byte(`{"algorithm":"md5","files":{}}`)); err != ErrChecksumAlgorithm {
		t.Errorf("md5 algorithm err = %v, want %v", err, ErrChecksumAlgorithm)
	}
	c, err := ParseChecksums([]byte(`{"algorithm":"sha256","files":{"a":"b"}}`))
	if err != nil || c["a"] != "b" {
		t.Errorf("sha256 parse = %v, %v", c, err)
	}
}

func TestVerifyErrorFormatting(t *testing.T) {
	e := &VerifyError{Entry: "session.jsonl", Reason: "checksum mismatch"}
	if !strings.Contains(e.Error(), "session.jsonl") || !strings.Contains(e.Error(), "checksum mismatch") {
		t.Errorf("VerifyError.Error() = %q", e.Error())
	}
}

func TestSessionForAgent(t *testing.T) {
	path := writeTestZip(t, AgentCodex)
	res, err := ReadZip(path)
	if err != nil {
		t.Fatal(err)
	}
	if b, ok := res.SessionForAgent(AgentCodex); !ok || string(b) != testSession {
		t.Error("SessionForAgent(codex) should return the raw session")
	}
	if _, ok := res.SessionForAgent(AgentClaude); ok {
		t.Error("SessionForAgent(claude) should not match a codex bundle")
	}
}

func TestSlugify(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Fix the Login Bug!", "fix-the-login-bug"},
		{"  spaced  out  ", "spaced-out"},
		{"a/b:c d", "a-b-c-d"},
		{"", "codex-task"},
		{"这是一个中文任务", "codex-task"},
	}
	for _, c := range cases {
		if got := Slugify(c.in); got != c.want {
			t.Errorf("Slugify(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultSharePath(t *testing.T) {
	p := DefaultSharePath("Fix It")
	if !strings.HasPrefix(p, "./") || !strings.HasSuffix(p, ".agent-handoff.zip") {
		t.Errorf("DefaultSharePath = %q", p)
	}
}

func TestManifestTargetSupport(t *testing.T) {
	m := testManifest(AgentCodex)
	if !m.TargetSupported(AgentClaude) || !m.TargetSupported(AgentCodex) {
		t.Error("both targets should be supported")
	}
	if m.TargetSupported("cursor") {
		t.Error("unknown target should not be supported")
	}
}

func TestManifestSummaryLine(t *testing.T) {
	m := testManifest(AgentCodex)
	line := m.SummaryLine()
	if !strings.Contains(line, "codex") || !strings.Contains(line, "Fix the login bug") {
		t.Errorf("SummaryLine = %q", line)
	}
}

func TestImageCounters(t *testing.T) {
	imgs := []ImageAsset{
		{Status: "copied"}, {Status: "missing"}, {Status: "copied"},
	}
	if CountCopied(imgs) != 2 {
		t.Errorf("CountCopied = %d, want 2", CountCopied(imgs))
	}
	if CountMissing(imgs) != 1 {
		t.Errorf("CountMissing = %d, want 1", CountMissing(imgs))
	}
}

func TestEntryNames(t *testing.T) {
	if got := SessionEntry(AgentCodex); got != "codex/session.jsonl" {
		t.Errorf("SessionEntry(codex) = %q", got)
	}
	if got := MetaEntry(AgentClaude); got != "claude/meta.json" {
		t.Errorf("MetaEntry(claude) = %q", got)
	}
	if got := ImagesPrefix(AgentCodex); got != "codex/images/" {
		t.Errorf("ImagesPrefix(codex) = %q", got)
	}
}

// tamperedCopy rewrites one entry inside a bundle copy.
func tamperedCopy(t *testing.T, src string, entry string, replacement []byte) string {
	t.Helper()
	res, err := ReadZip(src)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for k, v := range res.Files {
		files[k] = v
	}
	for k, v := range res.ImageData {
		files[k] = v
	}
	files[entry] = replacement
	delete(files, ChecksumsEntry)

	dir := t.TempDir()
	path := filepath.Join(dir, "tampered.agent-handoff.zip")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sortStrings(names)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(files[n])
	}
	zw.Close()
	f.Close()
	return path
}

func TestVerifyChecksumsDetectsTamperedEntry(t *testing.T) {
	src := writeTestZip(t, AgentCodex)
	tampered := tamperedCopy(t, src, SessionEntry(AgentCodex), []byte("injected content"))
	res, err := ReadZip(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if err := res.VerifyChecksums(); err == nil {
		t.Error("tampered session entry should fail checksum verification")
	}
}

func TestChecksumsMarshalDeterministic(t *testing.T) {
	files := map[string][]byte{
		"b.txt": []byte("1"),
		"a.txt": []byte("2"),
	}
	m1 := BuildChecksums(files).Marshal()
	m2 := BuildChecksums(files).Marshal()
	if !bytes.Equal(m1, m2) {
		t.Error("checksums marshal should be deterministic")
	}
}
