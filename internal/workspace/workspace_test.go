package workspace

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func gitTest(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+filepath.Join(t.TempDir(), "absent-config"))
	b, e := c.CombinedOutput()
	if e != nil {
		t.Fatalf("git %v: %v: %s", args, e, b)
	}
	return strings.TrimSpace(string(b))
}
func put(t *testing.T, root, path, text string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(path))
	if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte(text), 0600); e != nil {
		t.Fatal(e)
	}
}
func repo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	gitTest(t, root, "init", "-q")
	gitTest(t, root, "config", "core.autocrlf", "false")
	gitTest(t, root, "config", "user.name", "Synthetic")
	gitTest(t, root, "config", "user.email", "synthetic@example.invalid")
	put(t, root, "src/main.txt", "before\n")
	put(t, root, "untouched.txt", "keep\n")
	gitTest(t, root, "add", ".")
	gitTest(t, root, "commit", "-qm", "base")
	return root
}

// Missing selection boundaries or HEAD-relative capture would leak other working tree changes.
func TestCaptureOnlySelectedAndApplyInIndependentWorktree(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after\n")
	put(t, root, "untouched.txt", "private edit\n")
	put(t, root, "new file.txt", "chosen\n")
	put(t, root, "not-chosen.txt", "private\n")
	p, e := Capture(root, []string{"src/main.txt", "new file.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Manifest.Files) != 2 || !strings.Contains(string(p.Patch), "+after") || strings.Contains(string(p.Patch), "private") {
		t.Fatalf("incorrect selection: %+v %s", p.Manifest, p.Patch)
	}
	entries, e := p.Entries()
	if e != nil {
		t.Fatal(e)
	}
	parsed, e := Parse(entries)
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "new-worktree")
	if e := Apply(root, dest, parsed); e != nil {
		t.Fatal(e)
	}
	for path, want := range map[string]string{"src/main.txt": "after\n", "new file.txt": "chosen\n", "untouched.txt": "keep\n"} {
		got, e := os.ReadFile(filepath.Join(dest, path))
		if e != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", path, got, e)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(root, "untouched.txt")); string(b) != "private edit\n" {
		t.Fatal("source worktree overwritten")
	}
	if e := Apply(root, dest, parsed); e == nil {
		t.Fatal("overwrote existing destination")
	}
}

func TestInspectAndCaptureExcludeUnsafeFiles(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after\n")
	put(t, root, ".env", "PASSWORD=secret\n")
	put(t, root, "node_modules/dep/a.js", "module\n")
	put(t, root, "blob.dat", "a\x00b")
	put(t, root, "large.txt", strings.Repeat("x", (1<<20)+1))
	symlink := runtime.GOOS != "windows"
	if symlink {
		if e := os.Symlink("src/main.txt", filepath.Join(root, "link.txt")); e != nil {
			t.Fatal(e)
		}
	}
	in, e := Inspect(root)
	if e != nil {
		t.Fatal(e)
	}
	found := map[string]Candidate{}
	for _, c := range in.Files {
		found[c.Path] = c
	}
	for _, name := range []string{".env", "node_modules/dep/a.js", "blob.dat", "large.txt"} {
		if c, ok := found[name]; !ok || c.Eligible || c.Reason == "" {
			t.Errorf("unsafe file missing/exposed: %s %+v", name, c)
		}
		if _, e := Capture(root, []string{name}); e == nil {
			t.Errorf("captured %s", name)
		}
	}
	if symlink {
		if _, e := Capture(root, []string{"link.txt"}); e == nil {
			t.Fatal("captured symlink")
		}
	}
	for _, name := range []string{"../escape", ".git/config", "C:/escape", "src\\main.txt", "-bad", "dir/CON"} {
		if _, e := Capture(root, []string{name}); e == nil {
			t.Errorf("accepted unsafe path %q", name)
		}
	}
	if !found["src/main.txt"].Eligible || in.BaseCommit == "" {
		t.Fatalf("missing eligible change: %+v", in)
	}
}

// Malicious archive data must fail before a destination or native task can be created.
func TestValidateRejectsPatchEscapesSpecialModesAndUnlistedFiles(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after\n")
	p, e := Capture(root, []string{"src/main.txt"})
	if e != nil {
		t.Fatal(e)
	}
	for name, patch := range map[string]string{
		"escape":    strings.ReplaceAll(string(p.Patch), "src/main.txt", "../escape"),
		"symlink":   strings.Replace(string(p.Patch), "100644", "120000", 1),
		"submodule": strings.Replace(string(p.Patch), "100644", "160000", 1),
		"binary":    "diff --git a/src/main.txt b/src/main.txt\nGIT binary patch\nliteral 1\nA\n",
		"extra":     string(p.Patch) + "diff --git a/other b/other\n--- a/other\n+++ b/other\n@@ -1 +1 @@\n-a\n+b\n",
	} {
		t.Run(name, func(t *testing.T) {
			bad := *p
			bad.Patch = []byte(patch)
			if e := Validate(&bad); e == nil {
				t.Fatal("accepted unsafe patch")
			}
		})
	}
	entries, e := p.Entries()
	if e != nil {
		t.Fatal(e)
	}
	entries["code/files/../escape"] = []byte("oops")
	if _, e := Parse(entries); e == nil {
		t.Fatal("accepted extra file")
	}
}

func TestApplyMissingBaseAndConflictRollBackOnlyNewWorktree(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after\n")
	p, e := Capture(root, []string{"src/main.txt"})
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "import")
	missing := *p
	missing.Manifest = p.Manifest
	missing.Manifest.BaseCommit = strings.Repeat("f", 40)
	if e := Apply(root, dest, &missing); e == nil {
		t.Fatal("accepted unavailable base")
	}
	if _, e := os.Lstat(dest); !os.IsNotExist(e) {
		t.Fatal("created destination for missing base")
	}
	bad := *p
	bad.Patch = []byte(strings.Replace(string(p.Patch), "-before", "-conflict", 1))
	if e := Apply(root, dest, &bad); e == nil {
		t.Fatal("applied conflicting patch")
	}
	if _, e := os.Lstat(dest); !os.IsNotExist(e) {
		t.Fatal("failed worktree not rolled back")
	}
	if b, _ := os.ReadFile(filepath.Join(root, "src/main.txt")); string(b) != "after\n" {
		t.Fatal("source changed during rollback")
	}
}

func TestParseRejectsManifestPathTraversalAndMissingNewContent(t *testing.T) {
	root := repo(t)
	put(t, root, "new.txt", "content\n")
	p, e := Capture(root, []string{"new.txt"})
	if e != nil {
		t.Fatal(e)
	}
	entries, e := p.Entries()
	if e != nil {
		t.Fatal(e)
	}
	delete(entries, "code/files/new.txt")
	if _, e := Parse(entries); e == nil {
		t.Fatal("accepted missing content")
	}
	entries, e = p.Entries()
	if e != nil {
		t.Fatal(e)
	}
	var m map[string]any
	if e := json.Unmarshal(entries[ManifestEntry], &m); e != nil {
		t.Fatal(e)
	}
	m["files"] = []map[string]any{{"path": "../escape", "status": "untracked", "bytes": 8}}
	entries[ManifestEntry], _ = json.Marshal(m)
	if _, e := Parse(entries); e == nil {
		t.Fatal("accepted path traversal")
	}
}

// Repository attributes must stay data: inspection/checkout must never run a configured filter or hook.
func TestCaptureAndApplyDoNotExecuteGitFiltersOrHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell fixture; filter disabling itself is platform independent")
	}
	root := repo(t)
	marker := filepath.Join(t.TempDir(), "executed")
	script := filepath.Join(t.TempDir(), "dangerous-filter")
	put(t, filepath.Dir(script), filepath.Base(script), "#!/bin/sh\nprintf invoked >> '"+marker+"'\ncat\n")
	if e := os.Chmod(script, 0700); e != nil {
		t.Fatal(e)
	}
	gitTest(t, root, "config", "filter.danger.clean", script)
	gitTest(t, root, "config", "filter.danger.smudge", script)
	// Write a tracked attributes rule directly into a new commit without invoking
	// filters during fixture creation, then install the source filter afterwards.
	gitTest(t, root, "config", "--unset", "filter.danger.clean")
	gitTest(t, root, "config", "--unset", "filter.danger.smudge")
	put(t, root, ".gitattributes", "*.txt filter=danger\n")
	gitTest(t, root, "add", ".gitattributes")
	gitTest(t, root, "commit", "-qm", "attributes")
	gitTest(t, root, "config", "filter.danger.clean", script)
	gitTest(t, root, "config", "filter.danger.smudge", script)
	hooks := t.TempDir()
	put(t, hooks, "post-checkout", "#!/bin/sh\nprintf hook >> '"+marker+"'\n")
	if e := os.Chmod(filepath.Join(hooks, "post-checkout"), 0700); e != nil {
		t.Fatal(e)
	}
	gitTest(t, root, "config", "core.hooksPath", hooks)
	put(t, root, "src/main.txt", "after\n")
	p, e := Capture(root, []string{"src/main.txt"})
	if e != nil {
		t.Fatal(e)
	}
	if e := Apply(root, filepath.Join(t.TempDir(), "import"), p); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(marker); !os.IsNotExist(e) {
		t.Fatalf("executed configured code: %q, %v", b, e)
	}
}

func TestApplyRejectsSymlinkAncestorInLocalBase(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires POSIX privileges")
	}
	root := repo(t)
	outside := t.TempDir()
	if e := os.Symlink(outside, filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	gitTest(t, root, "add", "escape")
	gitTest(t, root, "commit", "-qm", "symlink")
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: gitTest(t, root, "rev-parse", "HEAD"), Files: []File{{Path: "escape/new.txt", Status: "untracked", Bytes: 4}}}, Files: map[string][]byte{"escape/new.txt": []byte("data")}}
	dest := filepath.Join(t.TempDir(), "new")
	if e := Apply(root, dest, p); e == nil {
		t.Fatal("applied through base symlink")
	}
	if _, e := os.Lstat(filepath.Join(outside, "new.txt")); !os.IsNotExist(e) {
		t.Fatal("wrote outside new worktree")
	}
	if _, e := os.Lstat(dest); !os.IsNotExist(e) {
		t.Fatal("failed destination left behind")
	}
}

func TestCaptureStagedAddedDeletedSpaceAndNoNewline(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after")
	put(t, root, "added space.txt", "new")
	gitTest(t, root, "add", "src/main.txt", "added space.txt")
	if e := os.Remove(filepath.Join(root, "untouched.txt")); e != nil {
		t.Fatal(e)
	}
	p, e := Capture(root, []string{"src/main.txt", "added space.txt", "untouched.txt"})
	if e != nil {
		t.Fatal(e)
	}
	dest := filepath.Join(t.TempDir(), "new")
	if e := Apply(root, dest, p); e != nil {
		t.Fatal(e)
	}
	if b, e := os.ReadFile(filepath.Join(dest, "added space.txt")); e != nil || string(b) != "new" {
		t.Fatalf("new content %q: %v", b, e)
	}
	if b, e := os.ReadFile(filepath.Join(dest, "src/main.txt")); e != nil || string(b) != "after" {
		t.Fatalf("modified content %q: %v", b, e)
	}
	if _, e := os.Stat(filepath.Join(dest, "untouched.txt")); !os.IsNotExist(e) {
		t.Fatal("deleted file restored")
	}
}

func TestValidateRejectsLimitsAndCaseCollisions(t *testing.T) {
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: strings.Repeat("a", 40), Files: []File{{Path: "one.txt", Status: "untracked", Bytes: 1}, {Path: "ONE.txt", Status: "untracked", Bytes: 1}}}, Files: map[string][]byte{"one.txt": []byte("a"), "ONE.txt": []byte("b")}}
	if e := Validate(p); e == nil {
		t.Fatal("accepted Windows path collision")
	}
	p.Manifest.Files = []File{{Path: "one.txt", Status: "untracked", Bytes: MaxFileBytes + 1}}
	delete(p.Files, "ONE.txt")
	if e := Validate(p); e == nil {
		t.Fatal("accepted oversized file")
	}
	p.Manifest.Files = make([]File, MaxFiles+1)
	if e := Validate(p); e == nil {
		t.Fatal("accepted too many files")
	}
}

func TestApplyRejectsOversizedResultEvenWhenMetadataLies(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", "after\n")
	p, e := Capture(root, []string{"src/main.txt"})
	if e != nil {
		t.Fatal(e)
	}
	p.Manifest.Files[0].Bytes = 1
	p.Patch = []byte(strings.Replace(string(p.Patch), "+after", "+"+strings.Repeat("x", MaxFileBytes+1), 1))
	dest := filepath.Join(t.TempDir(), "new")
	if e := Apply(root, dest, p); e == nil {
		t.Fatal("accepted oversized content hidden by false metadata")
	}
	if _, e := os.Stat(dest); !os.IsNotExist(e) {
		t.Fatal("invalid destination left behind")
	}
}

func TestCaptureAndApplyAcrossGitLineEndingConfiguration(t *testing.T) {
	root := repo(t)
	gitTest(t, root, "config", "core.autocrlf", "true")
	put(t, root, "src/main.txt", "after\r\n")
	p, e := Capture(root, []string{"src/main.txt"})
	if e != nil {
		t.Fatal(e)
	}
	// A receiver may use LF even though the sender uses CRLF.
	gitTest(t, root, "config", "core.autocrlf", "false")
	dest := filepath.Join(t.TempDir(), "new")
	if e := Apply(root, dest, p); e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(filepath.Join(dest, "src/main.txt"))
	if e != nil {
		t.Fatal(e)
	}
	if strings.ReplaceAll(string(data), "\r\n", "\n") != "after\n" {
		t.Fatalf("text changed: %q", data)
	}
}

func TestInspectExcludesOversizedBaseEvenWhenCurrentFileIsSmall(t *testing.T) {
	root := repo(t)
	put(t, root, "src/main.txt", strings.Repeat("x", MaxFileBytes+1)+"\n")
	gitTest(t, root, "add", "src/main.txt")
	gitTest(t, root, "commit", "-qm", "large base")
	put(t, root, "src/main.txt", "small\n")
	in, e := Inspect(root)
	if e != nil {
		t.Fatal(e)
	}
	if len(in.Files) != 1 || in.Files[0].Eligible {
		t.Fatalf("oversized base eligible: %+v", in.Files)
	}
	if _, e := Capture(root, []string{"src/main.txt"}); e == nil {
		t.Fatal("captured oversized base")
	}
}

func TestCaptureRejectsPortableGitMetadataAliasesAndCredentialFiles(t *testing.T) {
	for _, path := range []string{"git~1/config", ".git~1/config", ".g\u200cit/config", "dir/CON .txt", "COM¹.txt"} {
		if e := validPath(path); e == nil {
			t.Errorf("accepted metadata/device alias %q", path)
		}
	}
	for _, path := range []string{"secrets.toml", "credentials.yaml", ".git-credentials", ".ssh/config"} {
		if reason := excludedPath(path); reason == "" {
			t.Errorf("credential path is eligible: %s", path)
		}
	}
}

func TestReadBoundedRejectsSymlinkFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires POSIX privileges")
	}
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret")
	put(t, filepath.Dir(outside), filepath.Base(outside), "secret")
	if e := os.Symlink(outside, filepath.Join(root, "linked")); e != nil {
		t.Fatal(e)
	}
	if _, e := readBounded(root, "linked", MaxFileBytes); e == nil {
		t.Fatal("read symlink content")
	}
}

func TestApplyDoesNotLazyFetchMissingPromisorCommit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX executable fixture")
	}
	root := repo(t)
	marker := filepath.Join(t.TempDir(), "fetch-ran")
	script := filepath.Join(t.TempDir(), "upload-pack")
	put(t, filepath.Dir(script), filepath.Base(script), "#!/bin/sh\nprintf fetched > '"+marker+"'\nexit 1\n")
	if e := os.Chmod(script, 0700); e != nil {
		t.Fatal(e)
	}
	gitTest(t, root, "config", "core.repositoryformatversion", "1")
	gitTest(t, root, "config", "extensions.partialClone", "origin")
	gitTest(t, root, "config", "remote.origin.promisor", "true")
	gitTest(t, root, "config", "remote.origin.url", t.TempDir())
	gitTest(t, root, "config", "remote.origin.uploadpack", script)
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: strings.Repeat("f", 40), Files: []File{{Path: "new.txt", Status: "untracked", Bytes: 1}}}, Files: map[string][]byte{"new.txt": []byte("a")}}
	if e := Apply(root, filepath.Join(t.TempDir(), "new"), p); e == nil {
		t.Fatal("accepted unavailable commit")
	}
	if b, e := os.ReadFile(marker); !os.IsNotExist(e) {
		t.Fatalf("automatically fetched missing base: %q, %v", b, e)
	}
}

func TestValidateRejectsOversizedManifestMetadata(t *testing.T) {
	path := "dir/" + strings.Repeat("&", 50000) + ".txt"
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: strings.Repeat("a", 40), Files: []File{{Path: path, Status: "untracked", Bytes: 1}}}, Files: map[string][]byte{path: []byte("x")}}
	if e := Validate(p); e == nil {
		t.Fatal("accepted metadata exceeding parser limit")
	}
}

func TestApplyRejectsBaseSymlinkEvenWhenGitChecksItOutAsText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture requires POSIX privileges")
	}
	root := repo(t)
	if e := os.Symlink("before", filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	gitTest(t, root, "add", "link")
	gitTest(t, root, "commit", "-qm", "base symlink")
	gitTest(t, root, "config", "core.symlinks", "false")
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: gitTest(t, root, "rev-parse", "HEAD"), Files: []File{{Path: "link", Status: "modified", Bytes: 5}}}, Patch: []byte("diff --git a/link b/link\nindex 0000001..0000002 100644\n--- a/link\n+++ b/link\n@@ -1 +1 @@\n-before\n\\ No newline at end of file\n+after\n\\ No newline at end of file\n"), Files: map[string][]byte{}}
	if e := Apply(root, filepath.Join(t.TempDir(), "new"), p); e == nil {
		t.Fatal("accepted base symlink represented as text")
	}
}
