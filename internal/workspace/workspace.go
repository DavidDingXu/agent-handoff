// Package workspace carries explicitly selected, bounded text changes relative
// to a local Git commit. It never fetches or runs commands from a payload.
package workspace

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	Schema        = "agent-handoff.code.v1"
	ManifestEntry = "code/manifest.json"
	PatchEntry    = "code/changes.patch"
	FilesPrefix   = "code/files/"
	MaxFiles      = 256
	MaxFileBytes  = 1 << 20
	MaxTotalBytes = 8 << 20
)

type File struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Bytes  int64  `json:"bytes"`
}

type Manifest struct {
	Schema     string `json:"schema"`
	BaseCommit string `json:"base_commit"`
	Branch     string `json:"branch,omitempty"`
	Files      []File `json:"files"`
}

type Payload struct {
	Manifest Manifest          `json:"manifest"`
	Patch    []byte            `json:"patch_base64"`
	Files    map[string][]byte `json:"files_base64"` // repository-relative paths; untracked content only
}

type Candidate struct {
	Path     string `json:"path"`
	Status   string `json:"status"`
	Bytes    int64  `json:"bytes"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

type Inspection struct {
	Root       string      `json:"root"`
	BaseCommit string      `json:"base_commit"`
	Branch     string      `json:"branch"`
	Files      []Candidate `json:"files"`
}

// Inspect lists HEAD-relative tracked changes and nonignored untracked files.
// Ignored files are never read; excluded candidates carry a reason.
func Inspect(root string) (*Inspection, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	top, err := git(abs, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	abs, err = filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	if err != nil {
		return nil, err
	}
	base, err := git(abs, nil, "rev-parse", "--verify", "HEAD^{commit}")
	if err != nil {
		return nil, fmt.Errorf("workspace needs an existing base commit: %w", err)
	}
	branch, _ := git(abs, nil, "symbolic-ref", "--quiet", "--short", "HEAD")
	in := &Inspection{Root: abs, BaseCommit: strings.TrimSpace(string(base)), Branch: strings.TrimSpace(string(branch)), Files: []Candidate{}}
	changed, err := gitUnfiltered(abs, nil, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--name-status", "-z", "HEAD", "--")
	if err != nil {
		return nil, err
	}
	parts := bytes.Split(changed, []byte{0})
	seen := map[string]bool{}
	for i := 0; i+1 < len(parts); i += 2 {
		status := map[string]string{"M": "modified", "A": "added", "D": "deleted", "T": "type_changed", "U": "conflicted"}[string(parts[i])]
		if status == "" {
			status = "unsupported"
		}
		p := string(parts[i+1])
		if p == "" {
			continue
		}
		seen[p] = true
		in.Files = append(in.Files, inspectFile(abs, in.BaseCommit, p, status))
	}
	untracked, err := git(abs, nil, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return nil, err
	}
	for _, p := range bytes.Split(untracked, []byte{0}) {
		if len(p) > 0 && !seen[string(p)] {
			in.Files = append(in.Files, inspectFile(abs, in.BaseCommit, string(p), "untracked"))
		}
	}
	sort.Slice(in.Files, func(i, j int) bool { return in.Files[i].Path < in.Files[j].Path })
	return in, nil
}

func inspectFile(root, base, path, status string) Candidate {
	c := Candidate{Path: path, Status: status}
	if err := validPath(path); err != nil {
		c.Reason = err.Error()
		return c
	}
	if reason := excludedPath(path); reason != "" {
		c.Reason = reason
		return c
	}
	if status != "modified" && status != "added" && status != "deleted" && status != "untracked" {
		c.Reason = "unsupported change type"
		return c
	}
	if err := noSymlinkAncestors(root, path); err != nil {
		c.Reason = err.Error()
		return c
	}
	// Reject a symlink/submodule in either the base or current index. A type
	// change to a regular file must not disguise a dangerous original entry.
	for j, args := range [][]string{{"ls-tree", "-l", "-z", base, "--", path}, {"ls-files", "--stage", "-z", "--", path}} {
		data, err := git(root, nil, args...)
		if err != nil {
			c.Reason = "cannot inspect Git file mode"
			return c
		}
		for _, record := range bytes.Split(data, []byte{0}) {
			if len(record) == 0 {
				continue
			}
			if !bytes.HasPrefix(record, []byte("100644 ")) && !bytes.HasPrefix(record, []byte("100755 ")) {
				c.Reason = "symlink, submodule, or unsupported Git mode"
				return c
			}
			if j == 0 {
				fields := strings.Fields(strings.SplitN(string(record), "\t", 2)[0])
				if len(fields) != 4 {
					c.Reason = "cannot inspect base file size"
					return c
				}
				size, e := strconv.ParseInt(fields[3], 10, 64)
				if e != nil || size > MaxFileBytes {
					c.Reason = "base file exceeds 1 MiB"
					return c
				}
			}
		}
	}
	var data []byte
	var err error
	if status == "deleted" {
		data, err = git(root, nil, "show", base+":"+path)
	} else {
		info, e := os.Lstat(filepath.Join(root, filepath.FromSlash(path)))
		if e != nil {
			c.Reason = "cannot read file"
			return c
		}
		if !info.Mode().IsRegular() {
			c.Reason = "not a regular file"
			return c
		}
		c.Bytes = info.Size()
		if c.Bytes > MaxFileBytes {
			c.Reason = "file exceeds 1 MiB"
			return c
		}
		data, err = readBounded(root, path, MaxFileBytes)
	}
	if err != nil {
		c.Reason = "cannot read bounded file content"
		return c
	}
	c.Bytes = int64(len(data))
	if len(data) > MaxFileBytes {
		c.Reason = "file exceeds 1 MiB"
		return c
	}
	if !isText(data) {
		c.Reason = "binary or non-UTF-8 file"
		return c
	}
	if status != "untracked" {
		patch, e := diff(root, base, []string{path})
		if e != nil {
			c.Reason = "cannot capture diff"
			return c
		}
		if bytes.Contains(patch, []byte("\nBinary files ")) || bytes.Contains(patch, []byte("GIT binary patch")) {
			c.Reason = "binary diff"
			return c
		}
		if len(patch) > MaxTotalBytes {
			c.Reason = "diff exceeds 8 MiB"
			return c
		}
	}
	c.Eligible = true
	return c
}

// Capture requires exact repository-relative paths; it never selects a whole
// tree implicitly. Both staged and unstaged changes are captured against HEAD.
func Capture(root string, paths []string) (*Payload, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("select at least one changed file")
	}
	if len(paths) > MaxFiles {
		return nil, fmt.Errorf("selected files exceed %d", MaxFiles)
	}
	for _, path := range paths {
		if err := validPath(path); err != nil {
			return nil, err
		}
	}
	in, err := Inspect(root)
	if err != nil {
		return nil, err
	}
	byPath := map[string]Candidate{}
	for _, c := range in.Files {
		byPath[c.Path] = c
	}
	selected := append([]string(nil), paths...)
	sort.Strings(selected)
	p := &Payload{Manifest: Manifest{Schema: Schema, BaseCommit: in.BaseCommit, Branch: in.Branch, Files: []File{}}, Files: map[string][]byte{}}
	var tracked []string
	for i, path := range selected {
		if i > 0 && selected[i-1] == path {
			return nil, fmt.Errorf("duplicate selected file %q", path)
		}
		c, ok := byPath[path]
		if !ok {
			return nil, fmt.Errorf("selected path %q is not a changed candidate", path)
		}
		if !c.Eligible {
			return nil, fmt.Errorf("selected path %q is excluded: %s", path, c.Reason)
		}
		p.Manifest.Files = append(p.Manifest.Files, File{Path: path, Status: c.Status, Bytes: c.Bytes})
		if c.Status == "untracked" {
			data, e := readBounded(in.Root, path, MaxFileBytes)
			if e != nil {
				return nil, e
			}
			p.Files[path] = data
		} else {
			tracked = append(tracked, path)
		}
	}
	if len(tracked) > 0 {
		p.Patch, err = diff(in.Root, in.BaseCommit, tracked)
		if err != nil {
			return nil, err
		}
	}
	if err := Validate(p); err != nil {
		return nil, err
	}
	return p, nil
}

func diff(root, base string, paths []string) ([]byte, error) {
	args := []string{"-c", "core.quotePath=false", "diff", "--no-ext-diff", "--no-textconv", "--no-renames", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--unified=3", base, "--"}
	return gitUnfiltered(root, nil, append(args, paths...)...)
}

func gitUnfiltered(root string, stdin []byte, args ...string) ([]byte, error) {
	overrides, err := disabledFilters(root)
	if err != nil {
		return nil, err
	}
	return git(root, stdin, append(overrides, args...)...)
}

func git(root string, stdin []byte, args ...string) ([]byte, error) {
	// Static options precede every command; payload content is passed only as
	// arguments after -- or stdin, never through a shell.
	all := append([]string{"--no-pager", "--literal-pathspecs", "-c", "core.hooksPath=" + os.DevNull, "-c", "diff.external=", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false"}, args...)
	c := exec.Command("git", all...)
	c.Dir = root
	c.Stdin = bytes.NewReader(stdin)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(env), "GIT_") {
			c.Env = append(c.Env, env)
		}
	}
	c.Env = append(c.Env, "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1")
	stdout := limitedOutput{limit: MaxTotalBytes + (1 << 20)}
	stderr := limitedOutput{limit: 64 << 10}
	c.Stdout = &stdout
	c.Stderr = &stderr
	err := c.Run()
	if stdout.exceeded {
		return nil, fmt.Errorf("git output exceeds inspection size limit")
	}
	if err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", commandName(args), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Drain subprocess pipes while bounding memory. Discarding overflow avoids
// deadlocking a child on a full pipe; no oversized output is returned.
type limitedOutput struct {
	bytes.Buffer
	limit    int
	exceeded bool
}

func (b *limitedOutput) Write(data []byte) (int, error) {
	n := len(data)
	remaining := b.limit - b.Len()
	if n > remaining {
		b.exceeded = true
		data = data[:remaining]
	}
	_, _ = b.Buffer.Write(data)
	return n, nil
}

func commandName(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "command"
}

func readBounded(root, path string, limit int64) ([]byte, error) {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { _ = dir.Close() }()
	before, err := dir.Lstat(filepath.FromSlash(path))
	if err != nil || !before.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular readable file")
	}
	f, err := dir.Open(filepath.FromSlash(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || !os.SameFile(before, info) {
		return nil, fmt.Errorf("not a regular readable file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	return data, nil
}

func isText(data []byte) bool {
	if !utf8.Valid(data) {
		return false
	}
	for _, b := range data {
		if b == 0 || b < 0x20 && (b != '\t' && b != '\n' && b != '\r' && b != '\f') {
			return false
		}
	}
	return true
}

func validPath(path string) error {
	if path == "" || strings.HasPrefix(path, "-") || strings.ContainsAny(path, "\\:\x00\"<>|?*\r\n\t") || strings.HasPrefix(path, "/") || !utf8.ValidString(path) {
		return fmt.Errorf("unsafe code path %q", path)
	}
	for _, part := range strings.Split(path, "/") {
		lower := strings.ToLower(part)
		stem := strings.TrimRight(strings.SplitN(lower, ".", 2)[0], " ")
		metadata := strings.Map(func(r rune) rune {
			if r >= 0x200c && r <= 0x200f || r >= 0x202a && r <= 0x202e || r >= 0x206a && r <= 0x206f || r == 0xfeff {
				return -1
			}
			return r
		}, lower)
		deviceSuffix := ""
		if strings.HasPrefix(stem, "com") || strings.HasPrefix(stem, "lpt") {
			deviceSuffix = stem[3:]
		}
		reserved := stem == "con" || stem == "prn" || stem == "aux" || stem == "nul" || utf8.RuneCountInString(deviceSuffix) == 1 && strings.ContainsAny(deviceSuffix, "123456789¹²³")
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || metadata == ".git" || strings.HasPrefix(metadata, "git~") || strings.HasPrefix(metadata, ".git~") || reserved {
			return fmt.Errorf("unsafe code path %q", path)
		}
		for _, r := range part {
			if r < 32 || r == 127 {
				return fmt.Errorf("unsafe code path %q", path)
			}
		}
	}
	return nil
}

func excludedPath(path string) string {
	for _, part := range strings.Split(strings.ToLower(path), "/") {
		switch part {
		case "node_modules", "vendor", ".venv", "venv", ".cache", "__pycache__", "dist", "build", "target", "coverage", ".next", ".nuxt":
			return "dependency, cache, or build directory"
		}
		stem := strings.SplitN(part, ".", 2)[0]
		if part == ".env" || strings.HasPrefix(part, ".env.") || part == ".npmrc" || part == ".pypirc" || part == ".netrc" || part == "_netrc" || part == ".git-credentials" || part == ".ssh" || part == ".gnupg" || stem == "credentials" || stem == "secrets" || strings.HasPrefix(part, "id_rsa") || strings.HasPrefix(part, "id_ed25519") || strings.HasSuffix(part, ".pem") || strings.HasSuffix(part, ".key") || strings.HasSuffix(part, ".p12") || strings.HasSuffix(part, ".pfx") {
			return "potential secret or credential file"
		}
	}
	return ""
}

func noSymlinkAncestors(root, path string) error {
	dir, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	current := ""
	for _, part := range strings.Split(path, "/") {
		current = filepath.Join(current, part)
		info, err := dir.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink path is excluded")
		}
	}
	return nil
}
