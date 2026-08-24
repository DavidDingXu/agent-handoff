// Package safety scans session content for high-confidence secrets before
// sharing. Findings block an export unless the user explicitly overrides.
package safety

import (
	"regexp"

	"github.com/DavidDingXu/agent-handoff/internal/session"
)

type rule struct {
	Name   string
	Regex  *regexp.Regexp
	Redact *regexp.Regexp
}

// High-confidence patterns only: OpenAI/Anthropic keys, AWS keys, GitHub
// tokens, private key headers, generic long bearer/JWT-style assignments.
var rules = []rule{
	{
		// Must precede openai_api_key: sk-ant-... also matches the generic sk- pattern.
		// The api03-/admin02-style prefix is the real-world Anthropic key layout.
		Name:   "anthropic_api_key",
		Regex:  regexp.MustCompile(`sk-ant-(?:api|admin)[0-9]*-[A-Za-z0-9_-]{20,}`),
		Redact: regexp.MustCompile(`sk-ant-(?:api|admin)[0-9]*-[A-Za-z0-9_-]{20,}`),
	},
	{
		Name:   "openai_api_key",
		Regex:  regexp.MustCompile(`sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}`),
		Redact: regexp.MustCompile(`sk-(?:proj-|svcacct-)?[A-Za-z0-9_-]{20,}`),
	},
	{
		Name:   "aws_access_key",
		Regex:  regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`),
		Redact: regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`),
	},
	{
		Name:   "github_token",
		Regex:  regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`),
		Redact: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}`),
	},
	{
		Name:   "private_key_block",
		Regex:  regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`),
		Redact: nil, // header alone carries no secret material
	},
	{
		Name:   "bearer_jwt",
		Regex:  regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`),
		Redact: regexp.MustCompile(`(?i)(bearer\s+)[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`),
	},
}

// Finding describes one scan hit. Hint is a redacted excerpt safe to print.
type Finding struct {
	Rule string `json:"rule"`
	Line int    `json:"line"`
	Hint string `json:"hint,omitempty"`
}

// Scan scans a Codex session jsonl for high-confidence secrets. Only
// user-visible text (user/assistant messages, tool outputs, session meta)
// is scanned; model reasoning traces are skipped.
func Scan(content []byte) []Finding {
	var findings []Finding
	lineNo := 0
	session.IterLines(content, func(line session.Line) {
		lineNo++
		obj := line.Obj
		if obj == nil || !lineCarriesUserVisibleText(obj) {
			return
		}
		for _, r := range rules {
			if r.Regex.MatchString(line.Raw) {
				findings = append(findings, Finding{
					Rule: r.Name,
					Line: lineNo,
					Hint: session.Truncate(redact(line.Raw, r), 120),
				})
				break // one finding per line
			}
		}
	})
	return findings
}

// ScanPlain scans arbitrary text (e.g. a Claude session line) with the same
// rule set, skipping nothing.
func ScanPlain(text string) []Finding {
	var findings []Finding
	for _, r := range rules {
		if r.Regex.MatchString(text) {
			findings = append(findings, Finding{
				Rule: r.Name,
				Hint: session.Truncate(redact(text, r), 120),
			})
		}
	}
	return findings
}

func redact(s string, r rule) string {
	if r.Redact == nil {
		return s
	}
	return r.Redact.ReplaceAllString(s, "[REDACTED]")
}

// lineCarriesUserVisibleText reports whether a session line contains
// user-typed or tool-produced text worth scanning.
func lineCarriesUserVisibleText(obj map[string]any) bool {
	typ := session.Type(obj)
	if typ != "response_item" && typ != "event_msg" && typ != "user_message" {
		return typ == "session_meta"
	}
	payload := session.Payload(obj)
	if payload == nil {
		return false
	}
	switch session.PayloadType(obj) {
	case "message", "function_call_output", "local_shell_call_output", "custom_tool_call_output":
		return true
	}
	return typ == "user_message" || typ == "event_msg"
}

// Status derives the aggregate safety status from findings.
func Status(findings []Finding) string {
	if len(findings) == 0 {
		return "ok"
	}
	return "blocked"
}

// Rules returns the names of the active scan rules (for docs/CLI help).
func Rules() []string {
	names := make([]string, 0, len(rules))
	for _, r := range rules {
		names = append(names, r.Name)
	}
	return names
}
