# Extensions and customization

[English] | [简体中文](extensions.zh-CN.md)

agent-handoff does not equate extensibility with loading plugins everywhere. Prefer declarative configuration when it can express the variation. Consider a versioned plugin interface only when implementations differ enough that configuration cannot carry the behavior. This keeps community customization compatible with cross-platform and data-safety requirements.

## Current extension points

| Extension point | Mechanism | Core change | Best for |
| --- | --- | --- | --- |
| File provider | `config.json` | No | Domestic file services, object stores, enterprise upload APIs |
| Resolver page | Static HTTPS page config | No | Replacing the visible link page; CLI import does not depend on it |
| New agent | Go adapter | Yes | Native session formats for OpenCode, DeepSeek Harness, and future agents |
| Secret scan rule | Go rule plus tests | Yes | Another high-confidence credential format |
| Bundle field | Versioned schema | Yes | Additional portable cross-agent metadata |

### File providers

This is the most useful user extension and requires no third-party executable. Configuration describes the upload URL, multipart/raw body, headers, form fields, and how to extract a download URL from a JSON or text response. The core owns encryption, HTTP, URL validation, failure isolation, and zip fallback.

Community provider examples should document:

- whether the service requires an account or token;
- file-size, retention, download-count, and region limits;
- token references through `${ENV_NAME}`, never literal config secrets;
- whether the receiver can directly `GET` the download URL;
- at least one real encrypted upload/download verification result.

See [custom providers](link-service.md#custom-providers).

### Agent adapters

Agent adapters directly read and write native Codex, Claude Code, or future agent state. They own task discovery, native restore, append-only writes, backups, identity rewriting, and verification. Today they are source contributions; see [adding another agent](adding-agent.md).

This is the only capability worth evaluating as a future runtime plugin because agent implementations genuinely differ. [OpenCode](https://github.com/anomalyco/opencode) is the first planned adapter; [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness) follows once its developer-preview session and storage APIs are stable enough for native restore. Two current adapters are not enough to prove a stable shared interface. Revisit the design after a third real adapter lands:

- it must support macOS, Linux, and Windows, so Go's Windows-incompatible `plugin` package is not an option;
- an out-of-process protocol must be versioned and define permissions, backups, and failure atomicity;
- adapters cannot bypass preview, explicit confirmation, duplicate detection, or append-only writes;
- every new agent expands CI to the full N x N handoff matrix.

Until real implementations prove those constraints, explicit Go routing is easier to audit than a premature registry.

### Secret scan rules

Scan customization is better suited to declarative rules than executable plugins. A future organization config may allow custom regexes plus redaction templates, but the core must run every rule before network access and providers cannot disable scanning. Built-in rules remain high-confidence and require false-positive tests.

### Bundle extensions

Bundle v2 accepts backward-compatible fields and agent-specific directories. Open an issue before adding a field and identify its source, consumers, missing-data behavior, and old-reader strategy. Path sanitization, size limits, checksums, format versions, and untrusted-content handling remain core-owned; third parties cannot arbitrarily rewrite the final zip.

## Core contracts that are not plugins

- AES-256-GCM encryption, URL-fragment keys, and checksum verification;
- secret scanning and explicit confirmation before export;
- preview followed by explicit import confirmation;
- append-only native writes, pre-write backups, and the duplicate-import ledger;
- bundle path, size, schema, and source-agent validation;
- native Codex and Claude question-tool mappings.

Making these replaceable would turn customization into a possible key leak, destructive write, or unrecoverable native task.

## Contribution workflow

Open an issue before adding an extension point. State the users, trust model, failure fallback, and cross-platform acceptance criteria. Implement on a short-lived `feat/*` branch from current `main`, then merge through a tested pull request. Incompatible config or protocol changes require a new version and a backward-compatible reader path.
