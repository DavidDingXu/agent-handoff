# Codex project action

Install an `agent-handoff` build that supports `switch` and put both it and
`claude` on the integrated terminal's PATH. In the project's local environment
settings, add an Action named **Handoff to Claude** and paste the command from
[handoff-to-claude.txt](handoff-to-claude.txt).

The action runs in the integrated terminal without sending a model prompt.
An exact Codex session ID supplied by the host is used when available. Otherwise
the terminal asks you to choose a local task by number; the workspace directory
only filters that list. Dismissing it creates no task. The target CLI opens the
new history without submitting a continuation prompt.

This is a project toolbar action, not a global Codex plugin button or a global
keyboard shortcut. The official documentation describes configuring Actions in
the app; it does not define a portable configuration-file schema or guarantee a
current task ID in an Action's environment. No user configuration is installed
by these files.

To create history and get its resume command without opening the CLI, omit
`--open`. To bind a known source explicitly, add `--thread EXACT_ID`.

Source: [official local environments documentation](https://learn.chatgpt.com/docs/environments/local-environment).
