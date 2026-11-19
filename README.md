<h1 align="center">Agent Handoff</h1>
<p align="center"><strong>Share a task. Continue it in your coding agent.</strong></p>
<p align="center">Codex · Claude Code · DeepSeek Harness</p>
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

```sh
# Codex
codex plugin marketplace add https://github.com/DavidDingXu/agent-handoff
codex plugin add agent-handoff@agent-handoff

# Claude Code
claude plugin marketplace add https://github.com/DavidDingXu/agent-handoff
claude plugin install agent-handoff@agent-handoff
```

Restart the agent. Sender and receiver both need the plugin.

### DeepSeek Harness · experimental

Supports **DSH 0.2.2-rc.1 / rc.5**, Node **>=24.9.0**.

1. Download `dsh-agent-handoff-0.5.0.tgz` from [Releases](https://github.com/DavidDingXu/agent-handoff/releases).
2. In DSH, open **Plugins → Add plugin → Install third-party plugin**.
3. Enter the downloaded file's absolute path and enable the plugin.

No npm package name is available yet. Web profile commands and source builds:
[DSH guide](plugins/dsh/README.md).

## Use

| Host | Share | Receive |
| --- | --- | --- |
| Codex / Claude Code | Say “share the current task” | Say “import <file/link>” |
| DSH | **Task handoff → Export task** | **Task handoff → Import task** |

Choose a file or encrypted link. The receiver selects a local workspace,
reviews the content and confirms a new task. Repeated imports return the
previously imported task.

![Native DSH task handoff](documentation/dsh-native-handoff.png)

## What travels

- **Conversation and tool evidence:** all nine transfer directions among the
  three hosts are tested. Same-host native history is preserved where supported.
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
