# Claude local handoff entry

The root plugin loads this Mod automatically through `hooks/hooks.json`; a
second plugin installation is unnecessary. With Claude Code v2.1.287 or later,
run `/handoff codex` or `/handoff claude`. Add `--include-code` to include eligible
tracked text changes. The ordinary handoff skill remains available for sharing.

The immediate command calls the local `agent-handoff switch` executable with the
current `$.session.id()` and `$.session.cwd()`. It does not use a model or submit a
prompt, so source model quota is unnecessary. The native target history is
created and an exact resume command is shown. Opening another application's
interface is a separate user action; the Mod has no documented terminal handoff
or Codex window API.

The plugin uses its bundled core automatically; no separate CLI installation is
needed. For development, `AGENT_HANDOFF_BIN` can override it with an absolute path.
If the current session has not been persisted, the command reports that condition
and never substitutes a nearby task. The core's secret scan remains enforced.

For isolated validation with the installed Claude Code CLI:

```sh
claude plugin validate plugins/claude --strict
claude plugin test plugins/claude
```

Tests run in Claude's native Mod test harness, without sign-in, a live session,
network access, or model calls. v2.1.322 was validated on macOS; Windows/Linux and
the full interactive UI still require their own host checks. The standalone
manifest exists to run these scoped tests; normal installation uses the root
manifest and the same hooks module.

The default binary uses the documented `$.plugin.root`. Windows uses the bundled
amd64 executable; macOS/Linux detect their bundle with `uname -s -m` via an argv
array. The Mod does not depend on Node or a shell.

Official references: [Mods](https://code.claude.com/docs/en/plugins/mods),
[commands and process API](https://code.claude.com/docs/en/plugins/mods/api),
[native tests](https://code.claude.com/docs/en/plugins/mods/test).
