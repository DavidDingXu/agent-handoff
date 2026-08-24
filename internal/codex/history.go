package codex

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

type historyBase struct {
	ThreadID      string
	EndByteOffset int64
}

func materializePaginatedHistory(home, currentPath string, content []byte, visited map[string]bool) ([]byte, error) {
	clean := session.DetachCodexHistory(content)
	base, ok := paginatedHistoryBase(content)
	if !ok {
		return clean, nil
	}
	if visited[currentPath] {
		return nil, fmt.Errorf("pagination cycle at %s", currentPath)
	}
	visited[currentPath] = true
	defer delete(visited, currentPath)

	basePath, err := findBaseRollout(home, currentPath, base.ThreadID)
	if err != nil {
		return nil, err
	}
	baseContent, err := os.ReadFile(basePath)
	if err != nil {
		return nil, fmt.Errorf("read source rollout: %w", err)
	}
	if base.EndByteOffset <= 0 || base.EndByteOffset > int64(len(baseContent)) {
		return nil, fmt.Errorf("invalid source byte offset %d for %s", base.EndByteOffset, basePath)
	}
	baseContent = baseContent[:base.EndByteOffset]
	if len(baseContent) > 0 && baseContent[len(baseContent)-1] != '\n' {
		return nil, fmt.Errorf("source byte offset %d is not a line boundary", base.EndByteOffset)
	}
	baseContent, err = materializePaginatedHistory(home, basePath, baseContent, visited)
	if err != nil {
		return nil, err
	}
	return mergeRollouts(clean, baseContent), nil
}

func paginatedHistoryBase(content []byte) (historyBase, bool) {
	var out historyBase
	session.IterLines(content, func(line session.Line) {
		if out.ThreadID != "" || line.Obj == nil || session.Type(line.Obj) != "session_meta" {
			return
		}
		payload := session.Payload(line.Obj)
		history, _ := payload["history_base"].(map[string]any)
		out.ThreadID, _ = history["thread_id"].(string)
		switch offset := history["end_byte_offset"].(type) {
		case float64:
			out.EndByteOffset = int64(offset)
		case int64:
			out.EndByteOffset = offset
		case int:
			out.EndByteOffset = int64(offset)
		}
	})
	return out, out.ThreadID != "" && out.EndByteOffset > 0
}

func findBaseRollout(home, currentPath, threadID string) (string, error) {
	root := filepath.Join(home, "sessions")
	wantSuffix := "-" + threadID + ".jsonl"
	var exact, fallback string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == currentPath {
			return nil
		}
		if strings.HasSuffix(d.Name(), wantSuffix) {
			exact = path
			return fs.SkipAll
		}
		if fallback == "" && strings.Contains(d.Name(), threadID) && strings.HasSuffix(d.Name(), ".jsonl") {
			fallback = path
		}
		return nil
	})
	if exact != "" {
		return exact, nil
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("source rollout %s not found", threadID)
}

func mergeRollouts(current, base []byte) []byte {
	var meta string
	var body []string
	appendLines := func(content []byte, keepMeta bool) {
		session.IterLines(content, func(line session.Line) {
			if line.Obj != nil && session.Type(line.Obj) == "session_meta" {
				if keepMeta && meta == "" {
					meta = line.Raw
				}
				return
			}
			body = append(body, line.Raw)
		})
	}
	appendLines(current, true)
	currentBody := body
	body = nil
	appendLines(base, false)
	body = append(body, currentBody...)
	if meta != "" {
		body = append([]string{meta}, body...)
	}
	return []byte(strings.Join(body, "\n") + "\n")
}
