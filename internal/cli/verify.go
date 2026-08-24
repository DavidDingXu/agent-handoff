package cli

import (
	"flag"
	"fmt"

	"github.com/DavidDingXu/agent-handoff/internal/bundle"
	"github.com/DavidDingXu/agent-handoff/internal/claude"
	"github.com/DavidDingXu/agent-handoff/internal/codex"
)

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	source := sourceFlag(fs)
	home := fs.String("home", "", "agent home")
	thread := fs.String("thread", "", "thread/session id to verify")
	cwd := fs.String("cwd", "", "expected cwd")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *thread == "" {
		return fmt.Errorf("missing --thread")
	}
	sourceAgent, err := detectSource(*source)
	if err != nil {
		return err
	}

	switch sourceAgent {
	case bundle.AgentClaude:
		claudeHome, err := claude.ResolveHome(*home)
		if err != nil {
			return err
		}
		return printJSON(claude.Verify(claudeHome, *thread, *cwd))
	default:
		codexHome, err := codex.ResolveHome(*home)
		if err != nil {
			return err
		}
		return printJSON(codex.Verify(codexHome, *thread, *cwd))
	}
}
