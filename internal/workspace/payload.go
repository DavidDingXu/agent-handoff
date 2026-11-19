package workspace

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

var commitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var indexPattern = regexp.MustCompile(`^index [0-9a-f]+\.\.[0-9a-f]+(?: (?:100644|100755))?$`)
var hunkPattern = regexp.MustCompile(`^@@ -([0-9]+)(?:,([0-9]+))? \+([0-9]+)(?:,([0-9]+))? @@(?:.*)$`)

// Entries returns the exact plaintext archive content for validation and the
// caller's pre-export secret scan. It never exposes absolute local paths.
func (p *Payload) Entries() (map[string][]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	manifest, err := json.MarshalIndent(p.Manifest, "", "  ")
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{ManifestEntry: append(manifest, '\n'), PatchEntry: append([]byte(nil), p.Patch...)}
	for path, data := range p.Files {
		out[FilesPrefix+path] = append([]byte(nil), data...)
	}
	return out, nil
}

// Parse accepts a complete archive entry map and ignores entries outside
// code/. All code entries must be accounted for by the manifest.
func Parse(entries map[string][]byte) (*Payload, error) {
	data, ok := entries[ManifestEntry]
	if !ok {
		for name := range entries {
			if strings.HasPrefix(name, "code/") {
				return nil, fmt.Errorf("code entries require a manifest")
			}
		}
		return nil, nil
	}
	if len(data) > 256<<10 {
		return nil, fmt.Errorf("code manifest too large")
	}
	p := &Payload{Files: map[string][]byte{}}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p.Manifest); err != nil {
		return nil, fmt.Errorf("invalid code manifest: %w", err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("trailing data in code manifest")
	}
	patch, ok := entries[PatchEntry]
	if !ok {
		return nil, fmt.Errorf("missing code patch")
	}
	p.Patch = patch
	for name, data := range entries {
		if name == ManifestEntry || name == PatchEntry || !strings.HasPrefix(name, "code/") {
			continue
		}
		if !strings.HasPrefix(name, FilesPrefix) {
			return nil, fmt.Errorf("unexpected code entry %q", name)
		}
		path := strings.TrimPrefix(name, FilesPrefix)
		if err := validPath(path); err != nil {
			return nil, err
		}
		p.Files[path] = data
	}
	if err := Validate(p); err != nil {
		return nil, err
	}
	return p, nil
}

// Validate checks all untrusted payload data without accessing a repository.
// No patch command or executable content is interpreted during validation.
func Validate(p *Payload) error {
	if p == nil {
		return fmt.Errorf("missing code payload")
	}
	if p.Manifest.Schema != Schema {
		return fmt.Errorf("unsupported code schema %q", p.Manifest.Schema)
	}
	if !commitPattern.MatchString(p.Manifest.BaseCommit) {
		return fmt.Errorf("invalid base commit")
	}
	if len(p.Manifest.Branch) > 1024 || !utf8.ValidString(p.Manifest.Branch) || strings.ContainsAny(p.Manifest.Branch, "\r\n\x00") {
		return fmt.Errorf("invalid branch metadata")
	}
	if len(p.Manifest.Files) == 0 || len(p.Manifest.Files) > MaxFiles {
		return fmt.Errorf("code payload requires 1..%d files", MaxFiles)
	}
	metadata, err := json.MarshalIndent(p.Manifest, "", "  ")
	if err != nil || len(metadata)+1 > 256<<10 {
		return fmt.Errorf("code manifest exceeds size limit")
	}
	if len(p.Patch) > MaxTotalBytes || !isText(p.Patch) {
		return fmt.Errorf("patch exceeds size limit or is not UTF-8 text")
	}
	seen := map[string]bool{}
	tracked := map[string]File{}
	expectedNew := map[string]bool{}
	total := int64(len(p.Patch))
	var declaredTotal int64
	for _, f := range p.Manifest.Files {
		if err := validPath(f.Path); err != nil {
			return err
		}
		if reason := excludedPath(f.Path); reason != "" {
			return fmt.Errorf("excluded code path %q: %s", f.Path, reason)
		}
		key := strings.ToLower(f.Path)
		if seen[key] {
			return fmt.Errorf("duplicate or case-colliding code path %q", f.Path)
		}
		seen[key] = true
		if f.Bytes < 0 || f.Bytes > MaxFileBytes {
			return fmt.Errorf("file %q exceeds size limit", f.Path)
		}
		declaredTotal += f.Bytes
		if declaredTotal > MaxTotalBytes {
			return fmt.Errorf("selected file content exceeds 8 MiB")
		}
		switch f.Status {
		case "untracked":
			data, ok := p.Files[f.Path]
			if !ok || int64(len(data)) != f.Bytes || !isText(data) {
				return fmt.Errorf("missing, mismatched, or non-text new file %q", f.Path)
			}
			total += int64(len(data))
			expectedNew[f.Path] = true
		case "modified", "added", "deleted":
			tracked[f.Path] = f
		default:
			return fmt.Errorf("unsupported code status %q", f.Status)
		}
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("code payload exceeds 8 MiB")
	}
	for path := range p.Files {
		if !expectedNew[path] {
			return fmt.Errorf("unlisted new file %q", path)
		}
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i := 1; i < len(keys); i++ {
		if strings.HasPrefix(keys[i], keys[i-1]+"/") {
			return fmt.Errorf("overlapping file paths")
		}
	}
	return validatePatch(p.Patch, tracked)
}

func validatePatch(patch []byte, files map[string]File) error {
	if len(patch) == 0 {
		if len(files) > 0 {
			return fmt.Errorf("missing tracked changes")
		}
		return nil
	}
	if patch[len(patch)-1] != '\n' {
		return fmt.Errorf("unterminated patch")
	}
	expected := map[string]File{}
	for path, f := range files {
		expected["diff --git a/"+path+" b/"+path] = f
	}
	lines := strings.Split(strings.TrimSuffix(string(patch), "\n"), "\n")
	seen := map[string]bool{}
	for i := 0; i < len(lines); {
		f, ok := expected[lines[i]]
		if !ok || seen[f.Path] {
			return fmt.Errorf("patch references an unlisted or duplicate path")
		}
		seen[f.Path] = true
		i++
		start := i
		for i < len(lines) && !strings.HasPrefix(lines[i], "diff --git ") {
			i++
		}
		if err := validateBlock(lines[start:i], f); err != nil {
			return fmt.Errorf("invalid patch for %q: %w", f.Path, err)
		}
	}
	if len(seen) != len(files) {
		return fmt.Errorf("patch does not cover selected tracked files")
	}
	return nil
}

func validateBlock(lines []string, f File) error {
	oldHeader, newHeader := false, false
	newFile, deletedFile := false, false
	i := 0
	for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
		line := lines[i]
		i++
		switch {
		case indexPattern.MatchString(line):
		case line == "old mode 100644" || line == "old mode 100755" || line == "new mode 100644" || line == "new mode 100755":
			if f.Status != "modified" {
				return fmt.Errorf("mode change has wrong status")
			}
		case line == "new file mode 100644" || line == "new file mode 100755":
			newFile = true
		case line == "deleted file mode 100644" || line == "deleted file mode 100755":
			deletedFile = true
		case strings.HasPrefix(line, "--- "):
			want := "--- a/" + f.Path
			if f.Status == "added" {
				want = "--- /dev/null"
			}
			if oldHeader || strings.TrimSuffix(line, "\t") != want {
				return fmt.Errorf("invalid old file header")
			}
			oldHeader = true
		case strings.HasPrefix(line, "+++ "):
			want := "+++ b/" + f.Path
			if f.Status == "deleted" {
				want = "+++ /dev/null"
			}
			if newHeader || strings.TrimSuffix(line, "\t") != want {
				return fmt.Errorf("invalid new file header")
			}
			newHeader = true
		default:
			return fmt.Errorf("unsupported patch metadata or file mode")
		}
	}
	if newFile != (f.Status == "added") || deletedFile != (f.Status == "deleted") {
		return fmt.Errorf("file operation disagrees with manifest")
	}
	if i < len(lines) && (!oldHeader || !newHeader) {
		return fmt.Errorf("missing file headers")
	}
	if i == len(lines) && len(lines) == 0 {
		return fmt.Errorf("empty patch block")
	}
	for i < len(lines) {
		match := hunkPattern.FindStringSubmatch(lines[i])
		if match == nil {
			return fmt.Errorf("invalid hunk header")
		}
		i++
		oldCount, newCount := 1, 1
		if match[2] != "" {
			v, err := strconv.Atoi(match[2])
			if err != nil {
				return err
			}
			oldCount = v
		}
		if match[4] != "" {
			v, err := strconv.Atoi(match[4])
			if err != nil {
				return err
			}
			newCount = v
		}
		for i < len(lines) && !strings.HasPrefix(lines[i], "@@ ") {
			line := lines[i]
			i++
			if line == "\\ No newline at end of file" {
				continue
			}
			if len(line) == 0 {
				return fmt.Errorf("empty unprefixed hunk line")
			}
			switch line[0] {
			case ' ':
				oldCount--
				newCount--
			case '-':
				oldCount--
			case '+':
				newCount--
			default:
				return fmt.Errorf("unsupported hunk line")
			}
			if oldCount < 0 || newCount < 0 {
				return fmt.Errorf("hunk length mismatch")
			}
		}
		if oldCount != 0 || newCount != 0 {
			return fmt.Errorf("hunk length mismatch")
		}
	}
	return nil
}
