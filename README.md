<h1 align="center">Agent Handoff</h1>
<p align="center"><strong>Share a task. Continue it in your coding agent.</strong></p>
<p align="center">Codex · Claude Code · DeepSeek Harness · Qoder Desktop</p>
<p align="center">
  <a href="#install">Install</a> · <a href="#use">Use</a> · <a href="docs/usage.md">Documentation</a> · <a href="README.zh-CN.md">简体中文</a>
</p>
<p align="center">
  <a href="https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml"><img src="https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml"><img src="https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml/badge.svg" alt="CodeQL"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/DavidDingXu/agent-handoff" alt="Apache-2.0"></a>
</p>

Move conversation and tool evidence to another person, machine or coding agent.
Share a **ZIP file** or **end-to-end encrypted link**; the receiver previews it,
then opens a **new native task**. Existing tasks stay intact.

## Install

### Codex / Claude Code

Install the chat Skill online from this GitHub repository.

```sh
# Codex
codex plugin marketplace add https://github.com/DavidDingXu/agent-handoff
codex plugin add agent-handoff@agent-handoff

# Claude Code
claude plugin marketplace add https://github.com/DavidDingXu/agent-handoff
claude plugin install agent-handoff@agent-handoff
```

Install or update to **0.7.0** on both sides, then restart the agent.

### Codex Desktop panel preview

The **Agent Handoff UI** panel is available as a platform-specific Release ZIP.

1. Download `agent-handoff-codex-ui-v0.7.0-<platform>.zip` for your system.
2. Open **Customize → Plugins → Add → Create plugin** and select the ZIP.
3. In a local chat, mention **Agent Handoff UI** to open the panel. In **Import task**, choose a ZIP or encrypted link, select the destination workspace, and review the preview.

[Release downloads](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0) ·
[Local marketplace and source builds](plugins/codex-ui/README.md)

### DeepSeek Harness

- **Install by npm package name.** Open **Plugins → Add plugin**, select the **official npm registry**, enter `dsh-agent-handoff`, then install and enable it. This installs the [version published on npm](https://www.npmjs.com/package/dsh-agent-handoff).
- **Install v0.7.0 from GitHub.** Download `dsh-agent-handoff-0.7.0.tgz` and enter its absolute path in the same installer. Open **Task handoff** in the sidebar or invoke `/agent-handoff` in chat.

[Download the DSH package](https://github.com/DavidDingXu/agent-handoff/releases/download/v0.7.0/dsh-agent-handoff-0.7.0.tgz) ·
[Web profiles and source builds](plugins/dsh/README.md)

### Qoder Desktop preview

Qoder supports online installation from its marketplace and eligible sharing links. The Agent Handoff preview is available from GitHub as a ZIP. [Qoder plugin installation](https://docs.qoder.com/qoder/plugins)

1. Download `agent-handoff-qoder-v0.7.0-<platform>.zip`. Choose `darwin-universal` for either Apple Silicon or Intel Macs.
2. Open **Extensions → Plugins → Add plugin → Import plugin**, select the ZIP, and enable **Agent Handoff**.
3. Open the Agent Handoff panel or select its Skill from the `/` menu in chat.

[Release downloads](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0) ·
[Qoder installation and compatibility](plugins/qoder/README.md)

## Use

| Host | Share | Receive |
| --- | --- | --- |
| Codex / Claude Code | Say “share the current task” | Say “import <file/link>” |
| DSH | **Task handoff → Export task** | **Task handoff → Import task** |
| Qoder Desktop | Agent Handoff panel or Skill | Agent Handoff panel or Skill |

In DSH and Qoder Desktop, the bundled Agent Handoff Skill opens the share or import view from chat.

- **Export from DSH:** search for and select the task, choose ZIP or encrypted link, then **Create share**. Send the file or complete link to the receiver.
- **Import into DSH:** choose a ZIP or paste a share link, select a local workspace, then **Preview handoff**. Review and confirm, then **Open task** to continue.
- **Switch agents:** select **Switch agent** in DSH to create a Codex/Claude native task, then use its continue command. In a terminal, `agent-handoff switch --target claude --open` needs no source model turn. [Buttons and commands](docs/usage.md#local-agent-switching).
- **Automatic brief:** recorded requests, recent response and tool evidence are extracted locally, with optional notes. Receivers can read encrypted shares directly in a browser.
- **Optional code changes:** select text changes and any new files. Receivers can explicitly apply them in a separate new workspace. Code is off by default.

Open a share link to read it first; no plugin or deployment is needed. To continue the task, preview and confirm the import in your plugin.
Imports create a new task. Repeated imports return the previously imported task.

![Native DSH task handoff](documentation/dsh-native-handoff.png)

## What travels

- **Conversation and tool evidence:** all nine transfer directions among Codex,
  Claude Code and DSH are tested. Qoder Desktop support is in preview. Same-host native history is preserved where supported.
- **Privacy:** links use local AES-256-GCM encryption; secret findings require
  explicit consent. ZIP files contain plaintext and should be kept private.
- **DSH boundary:** reviewed passive history can restore natively; ordinary logs
  with runtime state require explicit conversation import. Credentials,
  permissions, queued work and full media restoration are excluded.

macOS/Linux x64 and arm64; Windows x64. Exact evidence and limitations:
[compatibility](docs/usage.md#compatibility) · [DSH verification](plugins/dsh/VERIFICATION.md).

## Documentation

[Detailed usage & CLI](docs/usage.md) · [DSH install & configuration](plugins/dsh/README.md) ·
[Own file service](docs/link-service.md) · [Bundle format](docs/bundle-format.md) ·
[Development](CONTRIBUTING.md) · [Roadmap](ROADMAP.md) · [Security](SECURITY.md)

Questions and contributions: [open an issue](https://github.com/DavidDingXu/agent-handoff/issues/new/choose).
Maintained by **DavidDingXu**. [Apache-2.0](LICENSE).
