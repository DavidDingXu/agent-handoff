package codex

import (
	"encoding/json"
	"strings"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

// NormalizeActiveSession removes rolled-back turns from a session jsonl so
// the export matches what the sender actually sees in the UI. Codex keeps
// forked/rolled-back turns in the log; without this the receiver would see
// ghost branches.
//
// Turns are delimited by event_msg task_started/task_complete pairs.
// thread_rolled_back events carry payload.num_turns = how many completed
// turns to drop. Multiple session_meta lines (fork artifacts) collapse to
// the one matching threadID. An in-flight final turn is preserved.
func NormalizeActiveSession(content []byte, threadID string) []byte {
	var prefix []string      // before first task_started (session_meta, turn_context, ...)
	var completed [][]string // finished turns
	var current []string     // in-flight turn

	flushCurrent := func() {
		if len(current) > 0 {
			completed = append(completed, current)
			current = nil
		}
	}

	metaSeen := false
	session.IterLines(content, func(line session.Line) {
		obj := line.Obj
		if obj == nil {
			// Non-JSON junk before the first turn belongs to the prefix.
			if len(completed) == 0 && len(current) == 0 {
				prefix = append(prefix, line.Raw)
			}
			return
		}
		switch session.Type(obj) {
		case "session_meta":
			// Keep only the first meta line (matching threadID when present).
			if metaSeen {
				return
			}
			id, _ := session.Payload(obj)["id"].(string)
			if threadID != "" && id != "" && id != threadID {
				return
			}
			metaSeen = true
			prefix = append(prefix, line.Raw)
		case "event_msg":
			switch session.PayloadType(obj) {
			case "task_started":
				flushCurrent()
				current = append(current, line.Raw)
			case "task_complete":
				current = append(current, line.Raw)
				flushCurrent()
			case "thread_rolled_back":
				n := rollbackTurns(obj)
				if len(current) > 0 {
					current = nil
					if n > 0 {
						n--
					}
				}
				if n > 0 && n <= len(completed) {
					completed = completed[:len(completed)-n]
				} else if n > len(completed) {
					completed = nil
				}
			default:
				if len(current) > 0 {
					current = append(current, line.Raw)
				} else if len(completed) == 0 {
					prefix = append(prefix, line.Raw)
				}
			}
		default:
			if len(current) > 0 {
				current = append(current, line.Raw)
			} else if len(completed) == 0 {
				prefix = append(prefix, line.Raw)
			}
		}
	})
	flushCurrent()

	var out []string
	out = append(out, prefix...)
	for _, turn := range completed {
		out = append(out, turn...)
	}
	out = append(out, current...)
	if len(out) == 0 {
		return content
	}
	return []byte(strings.Join(out, "\n") + "\n")
}

func rollbackTurns(obj map[string]any) int {
	n, _ := session.Payload(obj)["num_turns"].(float64)
	return int(n)
}

// ---- self-export turn removal ----
//
// When a user asks the agent to share "this session", the share command's
// own execution (reading the skill, running the CLI) lands in the final
// turn. Shipping that turn to the receiver is noise and leaks sender
// environment detail. We drop the last turn when it is a *pure* self-export
// turn: the user asked to share/export, and every tool call in the turn is
// a agent-handoff command or a harmless probe. When in doubt, keep the turn.

var shareRequestKeywords = []string{"share", "export", "session", "thread", "分享", "导出", "分享当前", "这个会话", "当前会话"}
var shareCommandHints = []string{"agent-handoff share", "agent-handoff preview", "agent-handoff version"}
var probeCommandHints = []string{"command -v", "which agent-handoff", "agent-handoff version", "uname -", "ls "}

// DropSelfExportTurn removes the trailing turn when it only exists to
// produce this very share. It is a no-op when the turn looks like anything
// else (false positives are worse than false negatives here).
func DropSelfExportTurn(content []byte) []byte {
	lines := splitLines(content)
	if len(lines) == 0 {
		return content
	}

	// Find turn boundaries: line indexes of task_started events.
	var starts []int
	for i, raw := range lines {
		obj := parseLine(raw)
		if obj == nil {
			continue
		}
		if session.Type(obj) == "event_msg" && session.PayloadType(obj) == "task_started" {
			starts = append(starts, i)
		}
	}
	if len(starts) == 0 {
		return content
	}
	lastStart := starts[len(starts)-1]
	turn := lines[lastStart:]
	if !isPureSelfExportTurn(turn) {
		return content
	}
	kept := lines[:lastStart]
	return []byte(strings.Join(kept, "\n") + "\n")
}

func isPureSelfExportTurn(turnLines []string) bool {
	hasExportRequest := false
	for _, raw := range turnLines {
		obj := parseLine(raw)
		if obj == nil {
			continue
		}
		switch session.Type(obj) {
		case "user_message":
			text, _ := session.Payload(obj)["message"].(string)
			if isShareRequest(text) {
				hasExportRequest = true
			}
		case "event_msg":
			// Real rollouts carry user text as event_msg/user_message.
			if session.PayloadType(obj) == "user_message" {
				text, _ := session.Payload(obj)["message"].(string)
				if isShareRequest(text) {
					hasExportRequest = true
				}
			}
		case "response_item":
			payload := session.Payload(obj)
			switch session.PayloadType(obj) {
			case "message":
				if role, _ := payload["role"].(string); role == "user" {
					if isShareRequest(session.MessageText(payload)) {
						hasExportRequest = true
					}
				}
			case "function_call", "custom_tool_call", "local_shell_call":
				if !isAllowedToolCall(payload) {
					return false
				}
			}
		}
	}
	return hasExportRequest
}

func isShareRequest(text string) bool {
	if text == "" || session.IsHiddenUserText(text) {
		return false
	}
	lower := strings.ToLower(text)
	for _, kw := range shareRequestKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func isAllowedToolCall(payload map[string]any) bool {
	cmd := shellCommandOf(payload)
	if cmd == "" {
		return true // non-shell tool calls (write_stdin etc.) are fine
	}
	lower := strings.ToLower(cmd)
	for _, h := range shareCommandHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	for _, h := range probeCommandHints {
		if strings.Contains(lower, h) {
			return true
		}
	}
	return false
}

func shellCommandOf(payload map[string]any) string {
	if args, ok := payload["arguments"].(string); ok && args != "" {
		// Arguments may be a JSON object whose "command" is an argv array:
		// {"command":["agent-handoff","share","--thread","current"]}.
		var parsed struct {
			Command []string `json:"command"`
		}
		if err := json.Unmarshal([]byte(args), &parsed); err == nil && len(parsed.Command) > 0 {
			return strings.Join(parsed.Command, " ")
		}
		return args
	}
	if cmd, ok := payload["command"].(string); ok {
		return cmd
	}
	if in, ok := payload["input"].(string); ok {
		return in
	}
	return ""
}

func splitLines(content []byte) []string {
	s := strings.TrimRight(string(content), "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func parseLine(raw string) map[string]any {
	var obj map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &obj); err != nil {
		return nil
	}
	return obj
}
