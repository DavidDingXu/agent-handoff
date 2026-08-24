package session

import (
	"encoding/json"
	"strings"
	"time"
)

// ImportRewrite describes how to rewrite a session for the receiving machine.
type ImportRewrite struct {
	TargetCWD  string
	TargetID   string // new thread/session id ("" keeps the original)
	ImagePaths map[string]string
	Now        time.Time // zero means time.Now()
}

// RewriteForImport rewrites a Codex session jsonl for the receiver:
//   - session_meta.payload.{id,session_id,timestamp} -> new values
//   - session_meta/turn_context payload.cwd -> target cwd
//   - image source paths -> restored asset paths (both quote styles)
func RewriteForImport(content []byte, rw ImportRewrite) []byte {
	now := rw.Now
	if now.IsZero() {
		now = time.Now()
	}
	importTime := now.UTC().Format("2006-01-02T15:04:05.000Z07:00")

	var out []string
	IterLines(content, func(line Line) {
		if line.Obj == nil {
			out = append(out, line.Raw)
			return
		}
		obj := line.Obj
		typ := Type(obj)

		if typ == "session_meta" || typ == "turn_context" {
			payload := Payload(obj)
			if payload != nil {
				payload["cwd"] = rw.TargetCWD
				if typ == "session_meta" && rw.TargetID != "" {
					payload["id"] = rw.TargetID
					payload["session_id"] = rw.TargetID
					// The desktop task list sorts/groups by these timestamps;
					// reset them to the import time so the task lands on top.
					payload["timestamp"] = importTime
					if obj["timestamp"] != nil {
						obj["timestamp"] = importTime
					}
				}
				if encoded, err := json.Marshal(obj); err == nil {
					out = append(out, string(encoded))
					return
				}
			}
		}

		rewritten := line.Raw
		for old, newPath := range rw.ImagePaths {
			if strings.Contains(rewritten, old) {
				rewritten = strings.ReplaceAll(rewritten, old, newPath)
			}
		}
		out = append(out, rewritten)
	})
	return []byte(strings.Join(out, "\n") + "\n")
}
