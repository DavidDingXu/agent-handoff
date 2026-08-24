package claude

import (
	"github.com/DavidDingXu/agent-handoff/internal/neutral"
)

func testTranscript() neutral.Transcript {
	return neutral.Transcript{
		Schema:      neutral.Schema,
		SourceAgent: "codex",
		SourceID:    "t1",
		Title:       "Login fix",
		Entries: []neutral.Entry{
			{Kind: neutral.KindMessage, Role: "user", Text: "Fix the login bug"},
			{Kind: neutral.KindTool, Tool: "shell", Status: "completed", Input: "grep login", Output: "auth.go:42"},
			{Kind: neutral.KindMessage, Role: "assistant", Text: "On it."},
		},
	}
}
