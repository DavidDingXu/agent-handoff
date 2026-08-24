package safety

import (
	"encoding/json"
	"strings"
	"testing"
)

// userLine wraps text in a Codex user_message event line.
func userLine(t *testing.T, text string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{
		"timestamp": "2026-08-01T10:00:00.000Z",
		"type":      "event_msg",
		"payload":   map[string]any{"type": "user_message", "message": text},
	})
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func findingRules(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	return out
}

func TestScanDetectsEachRule(t *testing.T) {
	cases := []struct {
		rule string
		text string
	}{
		{"openai_api_key", "my key is sk-proj-abcdefghijklmnopqrstuvwxyz123456 ok"},
		{"anthropic_api_key", "use sk-ant-api03-ABCDEFGHIJKLMNOPQRSTUVWXYZ123456789 please"},
		{"aws_access_key", "AKIAIOSFODNN7EXAMPLE is the access key"},
		{"github_token", "token: ghp_abcdefghijklmnopqrstuvwxyz1234567890"},
		{"private_key_block", "-----BEGIN RSA PRIVATE KEY-----"},
		{"bearer_jwt", "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N"}, // gitleaks:allow -- deliberate scanner fixture
	}
	for _, c := range cases {
		fs := Scan(userLine(t, c.text))
		rules := findingRules(fs)
		found := false
		for _, r := range rules {
			if r == c.rule {
				found = true
			}
		}
		if !found {
			t.Errorf("Scan(%s) rules = %v, want %q among them", c.rule, rules, c.rule)
		}
	}
}

func TestScanCleanText(t *testing.T) {
	fs := Scan(userLine(t, "Fix the login bug in auth.go and add tests"))
	if len(fs) != 0 {
		t.Errorf("clean text produced findings: %v", fs)
	}
	if got := Status(fs); got != "ok" {
		t.Errorf("Status(clean) = %q, want ok", got)
	}
}

func TestScanRedactsHint(t *testing.T) {
	fs := Scan(userLine(t, "key sk-proj-abcdefghijklmnopqrstuvwxyz123456 here"))
	if len(fs) != 1 {
		t.Fatalf("findings = %d, want 1", len(fs))
	}
	if !strings.Contains(fs[0].Hint, "[REDACTED]") {
		t.Errorf("hint %q should contain [REDACTED]", fs[0].Hint)
	}
	if strings.Contains(fs[0].Hint, "sk-proj-abcdefghijkl") {
		t.Errorf("hint %q leaked the secret prefix", fs[0].Hint)
	}
}

func TestScanSkipsReasoningTraces(t *testing.T) {
	line := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"response_item","payload":{"type":"reasoning","summary":[{"type":"summary_text","text":"sk-proj-abcdefghijklmnopqrstuvwxyz123456"}]}}`
	if fs := Scan([]byte(line + "\n")); len(fs) != 0 {
		t.Errorf("reasoning trace must not be scanned, got %v", fs)
	}
}

func TestScanScansToolOutput(t *testing.T) {
	line := `{"timestamp":"2026-08-01T10:00:00.000Z","type":"response_item","payload":{"type":"function_call_output","call_id":"c1","output":"env AKIAIOSFODNN7EXAMPLE"}}`
	if fs := Scan([]byte(line + "\n")); len(fs) != 1 || fs[0].Rule != "aws_access_key" {
		t.Errorf("tool output scan = %v, want one aws_access_key finding", fs)
	}
}

func TestScanPlain(t *testing.T) {
	fs := ScanPlain("Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJVadQssw5c") // gitleaks:allow -- deliberate scanner fixture
	if len(fs) == 0 {
		t.Error("ScanPlain should find bearer_jwt")
	}
	if fs := ScanPlain("nothing to see"); len(fs) != 0 {
		t.Errorf("ScanPlain clean = %v", fs)
	}
}

func TestStatusBlocked(t *testing.T) {
	fs := Scan(userLine(t, "sk-proj-abcdefghijklmnopqrstuvwxyz123456"))
	if got := Status(fs); got != "blocked" {
		t.Errorf("Status(findings) = %q, want blocked", got)
	}
}

func TestRulesListed(t *testing.T) {
	want := map[string]bool{
		"openai_api_key": false, "anthropic_api_key": false, "aws_access_key": false,
		"github_token": false, "private_key_block": false, "bearer_jwt": false,
	}
	for _, name := range Rules() {
		if _, ok := want[name]; !ok {
			t.Errorf("unexpected rule %q", name)
			continue
		}
		want[name] = true
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("rule %q missing from Rules()", name)
		}
	}
}
