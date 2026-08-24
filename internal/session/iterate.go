// Package session provides parsing and analysis helpers for Codex rollout
// session files (session.jsonl). It is the lowest layer of agent-handoff and
// depends only on the standard library.
package session

import (
	"bufio"
	"encoding/json"
	"io"
	"strings"
)

// MaxLineBytes caps one session jsonl line during iteration to bound memory
// when a session embeds very large payloads.
const MaxLineBytes = 32 << 20

// Line is one parsed session line: either a JSON object (Obj != nil) or a
// non-JSON line kept verbatim (Obj == nil, Raw carries the text).
type Line struct {
	Raw string
	Obj map[string]any
}

// IterLines walks a session jsonl and invokes fn for every line. Malformed
// JSON lines are passed through with Obj == nil so callers can preserve or
// skip them as appropriate.
func IterLines(content []byte, fn func(line Line)) {
	r := bufio.NewReader(strings.NewReader(string(content)))
	for {
		raw, err := readLine(r)
		if raw == "" && err != nil {
			return
		}
		trimmed := strings.TrimSpace(raw)
		var obj map[string]any
		if trimmed != "" {
			_ = json.Unmarshal([]byte(trimmed), &obj)
		}
		fn(Line{Raw: raw, Obj: obj})
		if err != nil {
			return
		}
	}
}

// Payload returns the line's payload object, or nil.
func Payload(obj map[string]any) map[string]any {
	if obj == nil {
		return nil
	}
	p, _ := obj["payload"].(map[string]any)
	return p
}

// Type returns the top-level "type" field of a line object.
func Type(obj map[string]any) string {
	if obj == nil {
		return ""
	}
	t, _ := obj["type"].(string)
	return t
}

// PayloadType returns the payload's "type" field of a line object.
func PayloadType(obj map[string]any) string {
	p := Payload(obj)
	if p == nil {
		return ""
	}
	t, _ := p["type"].(string)
	return t
}

func readLine(r *bufio.Reader) (string, error) {
	var sb strings.Builder
	for {
		chunk, err := r.ReadString('\n')
		sb.WriteString(chunk)
		if err != nil {
			return sb.String(), err
		}
		if sb.Len() > MaxLineBytes {
			return sb.String(), nil
		}
		if strings.HasSuffix(chunk, "\n") {
			return sb.String(), nil
		}
	}
}

// Ensure io import stays used on platforms where ReadString behavior differs.
var _ = io.EOF
