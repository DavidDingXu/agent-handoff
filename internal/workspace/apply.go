package workspace

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Apply creates a detached, independent worktree at an already local base.
// It refuses an existing destination, checks the patch before applying it,
// and removes only its newly created worktree if any step fails. No fetch,
// hook, submodule initialization, or command from the payload is run.
func Apply(root, destination string, p *Payload) (err error) {
	if err = Validate(p); err != nil {
		return err
	}
	top, err := git(root, nil, "rev-parse", "--show-toplevel")
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(strings.TrimSpace(string(top)))
	if err != nil {
		return err
	}
	if _, err = git(root, nil, "cat-file", "-e", p.Manifest.BaseCommit+"^{commit}"); err != nil {
		return fmt.Errorf("base commit is not available locally; no fetch was attempted: %w", err)
	}
	// Git on Windows may represent a symlink as an ordinary text file when
	// core.symlinks=false. Validate the stored tree modes, not just checkout
	// filesystem modes, before creating anything.
	for _, f := range p.Manifest.Files {
		entry, e := git(root, nil, "ls-tree", "-z", p.Manifest.BaseCommit, "--", f.Path)
		if e != nil {
			return e
		}
		newFile := f.Status == "added" || f.Status == "untracked"
		if newFile && len(entry) > 0 || !newFile && len(entry) == 0 {
			return fmt.Errorf("file %q operation disagrees with local base", f.Path)
		}
		if len(entry) > 0 && !bytes.HasPrefix(entry, []byte("100644 ")) && !bytes.HasPrefix(entry, []byte("100755 ")) {
			return fmt.Errorf("base path %q is not a regular Git file", f.Path)
		}
	}
	abs, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, e := os.Lstat(abs); !os.IsNotExist(e) {
		return fmt.Errorf("destination already exists or cannot be inspected")
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(abs))
	if err != nil {
		return fmt.Errorf("destination parent must exist: %w", err)
	}
	destination = filepath.Join(parent, filepath.Base(abs))
	// Atomically reserve a destination that belongs to this operation. Git
	// permits adding a worktree into this newly created empty directory.
	if err = os.Mkdir(destination, 0o700); err != nil {
		return fmt.Errorf("reserve new worktree: %w", err)
	}
	created := false
	defer func() {
		if err == nil {
			return
		}
		if created {
			_, _ = git(root, nil, "worktree", "remove", "--force", "--", destination)
		}
		_ = os.RemoveAll(destination)
	}()
	// Filters are local executable configuration. Disable every configured
	// clean/smudge/process driver before checkout; attributes stay as data.
	filterArgs, e := disabledFilters(root)
	if e != nil {
		return e
	}
	args := append(filterArgs, "worktree", "add", "--detach", "--", destination, p.Manifest.BaseCommit)
	if _, err = git(root, nil, args...); err != nil {
		return fmt.Errorf("create independent worktree: %w", err)
	}
	created = true
	worktreeRoot, err := os.OpenRoot(destination)
	if err != nil {
		return err
	}
	defer func() { _ = worktreeRoot.Close() }()
	for _, f := range p.Manifest.Files {
		if err = noSymlinkAncestors(destination, f.Path); err != nil {
			return err
		}
		full := filepath.Join(destination, filepath.FromSlash(f.Path))
		info, e := os.Lstat(full)
		if f.Status == "untracked" || f.Status == "added" {
			if !os.IsNotExist(e) {
				return fmt.Errorf("new file %q already exists in base", f.Path)
			}
		} else if e != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("tracked file %q is missing or not regular in base", f.Path)
		}
		if f.Status == "modified" || f.Status == "deleted" {
			data, e := readBounded(destination, f.Path, MaxFileBytes)
			if e != nil || !isText(data) {
				return fmt.Errorf("base file %q is not bounded UTF-8 text", f.Path)
			}
		}
	}
	if len(p.Patch) > 0 {
		if _, err = gitUnfiltered(destination, p.Patch, "apply", "--check", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("patch conflicts with base: %w", err)
		}
		if _, err = gitUnfiltered(destination, p.Patch, "apply", "--whitespace=nowarn", "-"); err != nil {
			return fmt.Errorf("apply reviewed patch: %w", err)
		}
	}
	for _, f := range p.Manifest.Files {
		if f.Status != "untracked" {
			continue
		}
		if err = noSymlinkAncestors(destination, f.Path); err != nil {
			return err
		}
		localPath := filepath.FromSlash(f.Path)
		if err = worktreeRoot.MkdirAll(filepath.Dir(localPath), 0o700); err != nil {
			return err
		}
		var out *os.File
		out, err = worktreeRoot.OpenFile(localPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, writeErr := out.Write(p.Files[f.Path])
		closeErr := out.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Recheck declared resulting content after applying, while rollback still
	// owns the new worktree. Metadata cannot bypass the file size/text limits.
	for _, f := range p.Manifest.Files {
		full := filepath.Join(destination, filepath.FromSlash(f.Path))
		if f.Status == "deleted" {
			if _, e := os.Lstat(full); !os.IsNotExist(e) {
				return fmt.Errorf("deleted file %q remains present", f.Path)
			}
			continue
		}
		if err = noSymlinkAncestors(destination, f.Path); err != nil {
			return err
		}
		data, e := readBounded(destination, f.Path, MaxFileBytes)
		if e != nil {
			return e
		}
		// Tracked text follows Git's line ending/attribute normalization, so
		// sender byte counts are informational and can differ across systems.
		if !isText(data) || f.Status == "untracked" && int64(len(data)) != f.Bytes {
			return fmt.Errorf("resulting file %q disagrees with declared text content", f.Path)
		}
	}
	return nil
}

func disabledFilters(root string) ([]string, error) {
	// --get-regexp exits 1 when no matching key exists; list all names instead
	// to keep a missing filter configuration a normal result.
	data, err := git(root, nil, "config", "--name-only", "--list", "-z")
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, raw := range bytes.Split(data, []byte{0}) {
		name := string(raw)
		lower := strings.ToLower(name)
		if !strings.HasPrefix(lower, "filter.") {
			continue
		}
		pos := strings.LastIndex(name, ".")
		if pos <= len("filter.") {
			continue
		}
		prefix := name[:pos]
		if seen[prefix] {
			continue
		}
		seen[prefix] = true
		for _, field := range []string{"clean=", "smudge=", "process=", "required=false"} {
			out = append(out, "-c", prefix+"."+field)
		}
	}
	return out, nil
}
