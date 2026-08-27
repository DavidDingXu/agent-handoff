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
//   - event payload.{thread_id,session_id} -> target id when they reference the source session
//   - session_meta/turn_context payload.cwd -> target cwd
//   - image source paths -> restored asset paths (both quote styles)
func RewriteForImport(content []byte, rw ImportRewrite) []byte {
	now := rw.Now
	if now.IsZero() {
		now = time.Now()
	}
	importTime := now.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	sourceIDs := codexSessionIdentityValues(content)

	var out []string
	IterLines(content, func(line Line) {
		if line.Obj == nil {
			out = append(out, line.Raw)
			return
		}
		obj := line.Obj
		typ := Type(obj)
		changed := false
		if rw.TargetID != "" {
			changed = rewriteCodexPayloadIdentity(obj, sourceIDs, rw.TargetID)
		}

		if typ == "session_meta" || typ == "turn_context" {
			payload := Payload(obj)
			if payload != nil {
				payload["cwd"] = rw.TargetCWD
				changed = true
				if typ == "session_meta" && rw.TargetID != "" {
					detachCodexHistory(payload)
					payload["id"] = rw.TargetID
					payload["session_id"] = rw.TargetID
					// The desktop task list sorts/groups by these timestamps;
					// reset them to the import time so the task lands on top.
					payload["timestamp"] = importTime
					if obj["timestamp"] != nil {
						obj["timestamp"] = importTime
					}
				}
			}
		}

		rewritten := line.Raw
		if changed {
			if encoded, err := json.Marshal(obj); err == nil {
				rewritten = string(encoded)
			}
		}
		for old, newPath := range rw.ImagePaths {
			if strings.Contains(rewritten, old) {
				rewritten = strings.ReplaceAll(rewritten, old, newPath)
			}
		}
		out = append(out, rewritten)
	})
	return []byte(strings.Join(out, "\n") + "\n")
}

func codexSessionIdentityValues(content []byte) map[string]struct{} {
	ids := map[string]struct{}{}
	IterLines(content, func(line Line) {
		if line.Obj == nil || Type(line.Obj) != "session_meta" {
			return
		}
		payload := Payload(line.Obj)
		for _, key := range []string{"id", "session_id"} {
			if value, ok := payload[key].(string); ok && value != "" {
				ids[value] = struct{}{}
			}
		}
	})
	return ids
}

func rewriteCodexPayloadIdentity(obj map[string]any, sourceIDs map[string]struct{}, targetID string) bool {
	payload := Payload(obj)
	changed := false
	for _, key := range []string{"thread_id", "session_id"} {
		value, ok := payload[key].(string)
		if !ok {
			continue
		}
		if _, source := sourceIDs[value]; !source {
			continue
		}
		payload[key] = targetID
		changed = true
	}
	return changed
}

// DetachCodexHistory removes sender-local pagination and context lineage from
// a rollout that will be stored as a standalone session on another machine.
func DetachCodexHistory(content []byte) []byte {
	var out []string
	IterLines(content, func(line Line) {
		if line.Obj == nil || Type(line.Obj) != "session_meta" {
			out = append(out, line.Raw)
			return
		}
		payload := Payload(line.Obj)
		if payload == nil {
			out = append(out, line.Raw)
			return
		}
		detachCodexHistory(payload)
		if encoded, err := json.Marshal(line.Obj); err == nil {
			out = append(out, string(encoded))
		} else {
			out = append(out, line.Raw)
		}
	})
	return []byte(strings.Join(out, "\n") + "\n")
}

func detachCodexHistory(payload map[string]any) {
	delete(payload, "history_mode")
	delete(payload, "history_base")
	delete(payload, "context_window")
}
