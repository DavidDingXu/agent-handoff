// Command agent-handoff shares coding-agent tasks across machines and agents.
package main

import (
	"fmt"
	"os"

	"github.com/DavidDingXu/agent-handoff/internal/cli"
)

func main() {
	if err := cli.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agent-handoff: %v\n", err)
		os.Exit(1)
	}
}
