# DSH Agent Handoff

English | [简体中文](README.zh-CN.md)

Native **DeepSeek Harness** UI for sharing tasks as ZIP files or encrypted links,
and importing received DSH, Codex, or Claude Code tasks into a new DSH session.

## Install

For Desktop, open **Plugins → Add plugin**, enter
`dsh-agent-handoff`, install and enable it. Restart DSH if the new entry does not
appear. Package: [dsh-agent-handoff on npm](https://www.npmjs.com/package/dsh-agent-handoff).

To upgrade, uninstall the old third-party plugin, then install and enable the new
version using the same steps. If a regional mirror cannot find a new release,
select the official npm registry in the installer.

For offline installation, download `dsh-agent-handoff-0.6.0.tgz` and
`checksums.txt` from [GitHub Releases](https://github.com/DavidDingXu/agent-handoff/releases).
Compare the file's SHA-256 with the published checksum, then enter the tarball's
absolute path in the same installer.

For a Web profile (pnpm must be available):

```sh
dsh plugin --profile web add dsh-agent-handoff
```

The Web command also accepts an absolute tarball path. The npm CLI in rc.1 cannot
manage the Desktop profile. Never reset an existing profile with
`--from-default-profile` when installing.

### Build from source

The matching Go cores are included in the repository; no Go toolchain is needed:

```sh
git clone https://github.com/DavidDingXu/agent-handoff.git
cd agent-handoff/plugins/dsh
npm ci
npm run package
npm run check:package
```

Install the generated `dist/dsh-agent-handoff-0.6.0.tgz` as above. For development,
run `npm run check` and `npm run check:compat` before packaging.

## Use

- Open **Task handoff → Export task**. Search and select by title or workspace path in the task dropdown, then choose a plaintext ZIP or
  encrypted link. Secret findings require explicit acknowledgment; an unavailable
  upload produces a downloadable ZIP. The session-header Share action is a shortcut.
- Open **Task handoff → Import task**. Choose a ZIP or encrypted link, then select a local workspace.
- Review the content and available import modes, confirm the new task, then open it.
- **Switch locally:** choose **Switch agent** and the target app to create a Codex/Claude native task.
  Run the displayed continue command in its directory on the DSH machine. No source model turn or
  continuation prompt is sent.
- **Brief and notes:** review recorded requests, response and tool evidence, and add an optional note.
  Receivers can decrypt and read encrypted links locally in a browser without installing first.
- **Carry code:** off by default. Select text changes, with new files initially unchecked. The receiver
  reviews files and the new directory; only **Apply code in a new workspace** creates a separate
  Git worktree, preserving the original workspace.

Only the owner connection can use these actions. Existing tasks are never
overwritten. Repeated imports return the earlier imported session. A partial
failure exposes the created session for recovery instead of silently creating
another one. Preview/secret/download tickets expire after ten minutes.

ZIP upload/download is limited to 16 MiB. A preview binds the verified content
and selected workspace; changing either requires another preview. Opening an
imported task does not send a prompt or run an imported tool.
Code is limited to 256 files, 1 MiB per file and 8 MiB patch/new content. The receiving repository
must already contain the base commit. Missing commits or conflicts can be handled by importing
the task without applying code. [Full workflow and recovery](../../docs/usage.md).

## Compatibility

Verified with **DSH 0.2.2-rc.1 / rc.5**, Node **>=24.9.0**. Other DSH
versions are rejected until their session and plugin APIs have been verified.
Bundled transfer cores cover macOS/Linux x64 and arm64, and Windows x64.
For handoffs involving DSH, both sides need agent-handoff 0.5.0 or later.

DSH → DSH offers native restoration only for the reviewed passive event set.
Ordinary DSH logs often contain system prompts, provider configuration, permission
or plugin state; those require an explicit **Conversation and tool evidence**
import. Codex/Claude → DSH always uses that mode. Source permissions, presets,
queued work, credentials, media files and provider state are never restored.
The original source log is retained privately for audit, not executed.

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
never in a handoff. See the repository's [provider documentation](https://github.com/DavidDingXu/agent-handoff/blob/main/docs/link-service.md).

## Verification

`npm test` covers native controls/slots, authenticated-route behavior, preview
binding, duplicate/partial recovery, secret confirmation, subprocess limits and
the pinned SDK's persistence/resume lifecycle with synthetic sessions and a local
mock model. The Go tests cover the existing four directions and DSH → Codex/Claude;
the plugin tests cover the other three. No model credentials are needed.
CI covers both supported SDK versions. The 0.5.0 macOS Desktop rc.1 acceptance
covers installation, all three source imports, opening and ZIP export; that ZIP
also completes new turns in current Codex and Claude clients using local model
fixtures. No paid model or physical Windows Desktop acceptance is claimed.
See the [full verification receipt](https://github.com/DavidDingXu/agent-handoff/blob/main/plugins/dsh/VERIFICATION.md).
