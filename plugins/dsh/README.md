# DSH Agent Handoff

English | [简体中文](README.zh-CN.md)

Native **DeepSeek Harness** UI for sharing tasks as ZIP files or encrypted links,
and importing received DSH, Codex, or Claude Code tasks into a new DSH session.

## Compatibility

Experimental first version: **DSH 0.2.2-rc.5**, Node **>=24.9.0**. Other DSH
versions are rejected until their session and plugin APIs have been verified.
Bundled transfer cores cover macOS/Linux x64 and arm64, and Windows x64.

DSH → DSH offers native restoration only for the reviewed passive event set.
Ordinary DSH logs often contain system prompts, provider configuration, permission
or plugin state; those require an explicit **Conversation and tool evidence**
import. Codex/Claude → DSH always uses that mode. Source permissions, presets,
queued work, credentials, media files and provider state are never restored.
The original source log is retained privately for audit, not executed.

## Build and install

This package is not published to npm yet. Build it from the repository:

```sh
make cross VERSION=v0.4.6
cd plugins/dsh
npm ci
npm run check
npm run package
npm run check:package
```

Install the resulting `dist/dsh-agent-handoff-0.1.0.tgz` into your chosen DSH
profile using its absolute path (pnpm must be available):

```sh
dsh plugin --profile web add /absolute/path/dsh-agent-handoff-0.1.0.tgz
```

Restart DSH. For Desktop, fully quit it first and use the command bundled with
that Desktop installation, selecting `--profile desktop`. Do not reset an
existing profile with `--from-default-profile` when installing.

## Use

- Open a session and click **Share task** in its header. Choose a plaintext ZIP
  or encrypted link. Secret findings block export until explicitly acknowledged.
  An unavailable upload produces a downloadable ZIP fallback.
- Click **Import task** in the sidebar, or open **Task handoff**. Choose a ZIP,
  encrypted link, or absolute path on the DSH host; select a local workspace.
- Preview the title, source, message count, checksum and warnings. Select an
  available import mode and click **Import as a new task**, then **Open task**.

Only the owner connection can use these actions. Existing tasks are never
overwritten. Repeated imports return the earlier imported session. A partial
failure exposes the created session for recovery instead of silently creating
another one. Preview/secret/download tickets expire after ten minutes.

ZIP upload/download is limited to 16 MiB. A preview binds the verified content
and selected workspace; changing either requires another preview. Opening an
imported task does not send a prompt or run an imported tool.

## Host configuration

The managed profile supplies the default private state directory. Override the
plugin's `agent-handoff` row in the profile's `cordis.patch.yml` when needed:

```yaml
- id: agent-handoff
  config:
    stateDir: /absolute/private/path/agent-handoff
    # corePath: /absolute/path/agent-handoff
    # endpoint: https://your-share-service.example
    # configPath: /absolute/path/provider-config.json
    ttlSeconds: 3600
    timeoutMs: 60000
```

On Windows use an absolute Windows path. An unmanaged runtime must configure
`stateDir`. Tokens belong in the existing provider configuration/environment,
never in a handoff. See the repository's [provider documentation](../../docs/link-service.md).

## Verification

`npm test` covers native controls/slots, authenticated-route behavior, preview
binding, duplicate/partial recovery, secret confirmation, subprocess limits and
the pinned SDK's persistence/resume lifecycle with synthetic sessions and a local
mock model. The Go tests cover the existing four directions and DSH → Codex/Claude;
the plugin tests cover the other three. No model credentials are needed.
The web UI and Desktop transport contract are tested separately; this does not
claim an Electron or physical Windows desktop acceptance run.
