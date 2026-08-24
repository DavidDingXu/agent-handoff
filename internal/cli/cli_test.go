package cli

import (
	"testing"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
)

func TestResolveTargetUsesCurrentAgent(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		env      map[string]string
		want     string
	}{
		{
			name: "codex session",
			env:  map[string]string{"CODEX_THREAD_ID": "codex-thread"},
			want: bundle.AgentCodex,
		},
		{
			name: "claude session id",
			env:  map[string]string{"CLAUDE_CODE_SESSION_ID": "claude-session"},
			want: bundle.AgentClaude,
		},
		{
			name: "claude shell marker",
			env:  map[string]string{"CLAUDECODE": "1"},
			want: bundle.AgentClaude,
		},
		{
			name:     "explicit target wins",
			explicit: bundle.AgentClaude,
			env:      map[string]string{"CODEX_THREAD_ID": "codex-thread"},
			want:     bundle.AgentClaude,
		},
		{
			name: "plain terminal defaults to codex",
			want: bundle.AgentCodex,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{
				"CODEX_THREAD_ID", "CODEX_SESSION_ID", "CLAUDE_SESSION_ID",
				"CLAUDE_CODE_SESSION_ID", "CLAUDECODE",
			} {
				t.Setenv(key, "")
			}
			for key, value := range tt.env {
				t.Setenv(key, value)
			}

			got, err := resolveTarget(tt.explicit)
			if err != nil {
				t.Fatalf("resolveTarget: %v", err)
			}
			if got != tt.want {
				t.Errorf("resolveTarget = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTargetRejectsUnsupportedAgent(t *testing.T) {
	if _, err := resolveTarget("cursor"); err == nil {
		t.Fatal("resolveTarget should reject unsupported agents")
	}
}
