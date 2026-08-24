# agent-handoff

English | [简体中文](README.zh-CN.md)

[![CI](https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml/badge.svg)](https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml)
[![CodeQL](https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml/badge.svg)](https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/DavidDingXu/agent-handoff)](https://github.com/DavidDingXu/agent-handoff/releases/latest)
[![License](https://img.shields.io/github/license/DavidDingXu/agent-handoff)](LICENSE)

**Move the actual coding session, not a hand-written handoff summary.** Share a Codex or Claude Code task as a portable file or end-to-end encrypted link; the receiver imports it as a new native task and continues the conversation.

Conversation, tool evidence, images, and task metadata travel together. Same-agent transfers preserve native event structure; cross-agent transfers use a neutral transcript. No screenshots, copy-paste, account, or server-side conversation history.

## Install in 30 seconds

```sh
# Claude Code
claude plugin marketplace add https://github.com/DavidDingXu/agent-handoff
claude plugin install agent-handoff@agent-handoff

# Codex
codex plugin marketplace add https://github.com/DavidDingXu/agent-handoff
codex plugin add agent-handoff@agent-handoff
```

Restart the agent, then say **“share the current task”** / **「分享当前任务」**. The receiver installs the same plugin and says **“import &lt;file/link&gt;”** / **「导入 &lt;文件/链接&gt;」**. Both sides need the plugin; no Go toolchain or Cloudflare account is required.

## Highlights

- **Four-quadrant handoff** — codex→codex, codex→claude, claude→codex, claude→claude. Same-agent imports preserve native events and content while rewriting the identity fields required for a new task; cross-agent imports are semantic via an agent-neutral transcript.
- **Zero-config encrypted links** — `--format link` encrypts locally with AES-256-GCM and uploads ciphertext to the project-operated free service. If that service is unavailable, the CLI automatically tries an anonymous multi-provider relay; if every upload fails, it keeps a local zip. Teams can select their own Worker with `--endpoint`. See [docs/link-service.md](docs/link-service.md).
- **Secret scan before export** — six high-confidence rules (OpenAI/Anthropic keys, AWS keys, GitHub tokens, private key blocks, bearer JWTs) block a share until you explicitly confirm. Findings are always reported, redacted for display.
- **Never touches your data** — import appends one new task and nothing else. Existing threads, indexes, and database rows are never modified; automatic backups are taken before every write.
- **One static binary** — pure Go (CGO-free sqlite), builds for macOS/Linux on amd64/arm64 and Windows amd64. JSON output for agents and humans alike.

## Why not just write a handoff note?

| Method | Conversation and tool evidence | Opens as a native task | File or encrypted link | Codex ↔ Claude Code |
| --- | --- | --- | --- | --- |
| Copy/paste or summary | Partial | No | Manual | Manual rewrite |
| Generic handoff prompt | Summarized | No | Usually text | Summarized |
| **agent-handoff** | **Included** | **Yes** | **Both** | **Built in** |

The product loop is intentionally receiver-driven: every shared file or link carries the installation path, so a teammate can install once and continue the task without asking the sender to reformat it.

## Other install options

**Standalone CLI — macOS/Linux:**

```sh
curl -fsSL https://raw.githubusercontent.com/DavidDingXu/agent-handoff/main/install.sh | sh
```

**Standalone CLI — Windows PowerShell:**

```powershell
irm https://raw.githubusercontent.com/DavidDingXu/agent-handoff/main/install.ps1 | iex
```

Or download an archive from [Releases](https://github.com/DavidDingXu/agent-handoff/releases/latest). Both installers require and verify the published SHA-256 checksum.

**From source (Go 1.25+):**

```sh
git clone https://github.com/DavidDingXu/agent-handoff && cd agent-handoff
make build                    # → bin/agent-handoff
make install                  # → $(go env GOPATH)/bin/agent-handoff
```

## Quick start

Once the plugin is installed, everything is conversational — your agent drives the bundled agent-handoff skill.

### Share the current task

> You: “share the current task” / 「分享当前任务」

The agent produces `fix-flaky-retry-test.agent-handoff.zip` and shows a card: title, size, 27 messages, 2 images. Send the file over IM/email.

### Share as a link

> You: “create a share link” / 「生成分享链接」

The agent returns an end-to-end encrypted HTTPS link without an account, token, or deployment. The project-operated service is used first; anonymous providers are automatic fallbacks. The decryption capability stays in the URL fragment, so always send the **full link**. Links live ~10 minutes by default and the hosted service is best-effort. For team-controlled storage and up to 24 h, configure `AGENT_HANDOFF_ENDPOINT` or pass `--endpoint` for a self-hosted Worker. See [docs/link-service.md](docs/link-service.md).

### Import on the receiving side

Your teammate just tells their agent (Codex or Claude Code, with the plugin installed):

> “import /path/to/fix-flaky-retry-test.agent-handoff.zip” / 「导入 <文件>」
> “import https://share.example.com/s/Wi5x…#k=Qm9…” / 「导入 <链接>」

The agent shows a preview card (title, origin, time range, first/last message), then imports it as a **native new task** at the top of the task list, ready to continue. Re-importing the same share returns `duplicate` instead of silently forking.

### Cross-agent

Source and target agents are auto-detected (e.g. importing a codex-exported bundle inside Claude Code) — no manual flags needed. Same-agent → native-fidelity restore with fresh task identity; cross-agent → semantic conversion (visible conversation and tool evidence preserved; model, git branch, timestamps carried over).

Prefer the CLI directly? The Commands section below is what every conversation above runs under the hood.

## Commands

| Command | Purpose |
| --- | --- |
| `share` | Export a thread/session as `.agent-handoff.zip` (or an encrypted link) |
| `preview` | Inspect a bundle (file or URL) without importing |
| `import` | Dry-run or `--execute` an import into a target agent |
| `verify` | Confirm an imported task is queryable in the target agent's state |

Key flags (see `agent-handoff` with no arguments for the full list):

- `share`: `--source codex|claude`, `--thread <id>|current`, `--format zip|link`, `--out FILE`, `--endpoint URL`, `--token TOKEN`, `--ttl SECONDS`, `--include-secrets`
- `import`: `--target codex|claude`, `--cwd DIR`, `--execute`, `--allow-duplicate`, `--home DIR`
- `verify`: `--thread ID`, `--source codex|claude`, `--cwd DIR`

Source/target agents are auto-detected from `CODEX_THREAD_ID` / `CLAUDE_SESSION_ID`-style environment variables, defaulting to codex. Homes resolve from `CODEX_HOME` / `CLAUDE_CONFIG_DIR` or `~/.codex` / `~/.claude`.

Environment: `AGENT_HANDOFF_ENDPOINT` (optional self-hosted worker origin), `AGENT_HANDOFF_TOKEN` (its upload bearer token), `AGENT_HANDOFF_RESOLVER` (optional static resolver page for anonymous links).

## How it works

```
sender                                        receiver
──────                                        ────────
~/.codex/sessions/…/rollout.jsonl   ─┐        ┌─> new rollout.jsonl (fresh UUIDv7 id)
~/.codex/state_5.sqlite (threads row)│  zip   │   threads row appended (schema-aware)
        neutral transcript  ←────────┼──────> │   ~/.claude/projects/…/<uuid>.jsonl
        images, manifest, checksums  ─┘  or   │   index entry + ledger record
                                           link (AES-256-GCM, capability in URL fragment)
```

A bundle is a deterministic zip: `manifest.json`, the raw source session, a neutral transcript, agent metadata, images, a secret-scan report, per-file SHA-256 checksums, and an `AGENT_README.md` written for the receiving agent. Full spec: [docs/bundle-format.md](docs/bundle-format.md).

Import is deliberately conservative:

1. **Checksums first** — integrity is verified before anything is written.
2. **Duplicate ledger** — a per-home ledger records every import; re-importing the same share returns `status: duplicate` instead of silently forking.
3. **Append-only writes** — backups land next to the originals; on failure the error is reported verbatim.
4. **Untrusted content stays data** — nothing inside a shared session is ever executed or fetched by agent-handoff itself.

## Security

- Link payloads are AES-256-GCM encrypted client-side; the server stores ciphertext only and cannot decrypt.
- Export is blocked on high-confidence secret findings unless you pass `--include-secrets`.
- Bundle checksums make tampering detectable; `preview` reports `checksum_status`.
- Please report vulnerabilities privately — see [SECURITY.md](SECURITY.md).

## Development

```sh
make test      # unit + integration (four-quadrant round trips, link crypto)
make lint      # golangci-lint
make build     # bin/agent-handoff
```

CI runs tests with `-race` on Ubuntu/macOS/Windows. Architecture: `internal/bundle` (container format), `internal/{codex,claude}` (agent adapters), `internal/neutral` (cross-agent transcript), `internal/link` (E2E crypto + worker client), `internal/safety` (secret scan), `internal/cli`. Adding an agent = one adapter package + one entry in `bundle.SupportedAgents`.

Contributions welcome — see [CONTRIBUTING.md](CONTRIBUTING.md) ([中文](docs/CONTRIBUTING.zh-CN.md)). Apache-2.0 licensed.
