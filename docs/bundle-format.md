# Bundle format (`*.agent-handoff.zip`)

[English] | [简体中文](bundle-format.zh-CN.md)

This document specifies the v2 agent-handoff bundle container: the on-disk layout, every entry, the manifest schema, the checksum file, and the compatibility rules. It is the normative reference for both the Go implementation in `internal/bundle` and any third-party reader.

## Design goals

### Optional brief and code entries (0.6.0)

The format version remains **2**. New writers add `agent/brief.json` and `agent/brief.md`.
The JSON schema is `agent-handoff.brief.v1`: `objective`, `latest_request`, `latest_update`
are optional `{entry_index,text,truncated?}` excerpts; `evidence` contains up to six attributed
tool records, and `notes` is optional caller-authored text (8 KiB). Readers validate excerpts
against the neutral transcript. Briefs never replace native history or claim verified completion.

Explicitly selected code adds `code/manifest.json` (`agent-handoff.code.v1`),
`code/changes.patch` and optional `code/files/<repository-relative-path>` entries for new files.
The manifest records `base_commit`, optional `branch` and `files: [{path,status,bytes}]`.
Tracked byte counts are sender-side context; Git may normalize line endings across platforms.
New-file bytes are exact. All entries are covered by `checksums.json` and the file inventory.

Only selected UTF-8 text is eligible: at most 256 files, 1 MiB per file and 8 MiB total patch/new
content. Unsafe paths, Git metadata aliases, credentials, generated dependencies, binaries,
symlinks, submodules and unsafe patch modes/directives are rejected. Apply requires explicit
consent and an already available base commit in a local repository; it creates a fresh worktree,
disables Git hooks/filters and lazy fetching, and never changes the existing worktree.

Old v1/v2 bundles without these entries remain readable. Old readers may ignore optional entries;
they do not acquire code-application support. Different selected code or user notes produce
different import fingerprints. Automatically generated briefs preserve the legacy session-only
fingerprint when no code or user notes are present.

- **Self-describing**: a bundle can be inspected, previewed, and imported without any network access or external service.
- **Native + semantic in one artifact**: native events and content travel alongside a neutral transcript. A paginated Codex rollout is materialized with its referenced history into one standalone session, without sender-local lineage. Same-agent import reuses native data while rewriting new-task identity/path fields; cross-agent import uses the neutral representation.
- **Tamper-evident**: every entry is covered by SHA-256 checksums verified before any import writes.
- **Deterministic**: identical input produces a byte-identical zip (sorted entries, fixed timestamps), so bundles can be diffed and re-verified.

## Layout

```
manifest.json            bundle manifest (schema, ids, counts, agent info)
AGENT_README.md          instructions for a receiving agent
checksums.json           sha256 of every other file
codex/session.jsonl      portable native session   (agent = source agent)
codex/meta.json          sender-side metadata       (threads-table row)
codex/images.json        image manifest             (codex source only)
codex/images/*           image assets               (codex source only)
agent/neutral.json       agent-neutral transcript
agent/restore.md         post-import continuation context
safety/scan.json         secret scan result at share time
```

The `codex/` directory is per-source-agent: a Claude-sourced bundle uses `claude/session.jsonl` and `claude/meta.json` (the Claude index entry) and carries no images. Entry names for an agent `a` are defined in `internal/bundle/format.go`: `a/session.jsonl`, `a/meta.json`, `a/images.json`, `a/images/*`.

`manifest.json` and `checksums.json` are always at the archive root. All entries are stored with forward slashes regardless of the producing platform.

## DSH source extension

DSH bundles contain `dsh/session.jsonl`: one JSON line with schema
`agent-handoff.dsh.native.v1`, runtime version, session format version, source
header, inherited-event count and events. `dsh/meta.json` must be empty; there are
no DSH attachment entries in the first version. Native bytes remain inert in the
Go core. The pinned DSH plugin validates the event vocabulary before restoring.

`bridge share --input FILE|-` accepts `agent-handoff.bridge.share.v1`;
`bridge read FILE|URL` returns verified `agent-handoff.bridge.read-result.v1`.
Requests and results are capped at 32 MiB, native data at 16 MiB and neutral data
at 8 MiB/100,000 entries. JSON rejects duplicate keys, trailing documents and
invalid UTF-8. Archives reject duplicate or ambiguous entry paths. The plugin's
ZIP upload/download cap is 16 MiB. These are stricter than general archive limits.

The DSH plugin owns preview/confirmation and fresh-session persistence. It never
restores source permissions, provider configuration or queued work. Unsupported
native state requires explicit conversation import. Codex/Claude receivers use
DSH neutral content and retain raw source bytes as a private audit sidecar.
Normal local `--source`/`--target` CLI adapters remain Codex/Claude only.

## manifest.json

```json
{
  "format_version": 2,
  "artifact_type": "agent-handoff",
  "source_agent": "codex",
  "target_support": ["codex", "claude"],
  "source_thread_id": "0192c0de-…",
  "title": "fix flaky retry test",
  "source_cwd": "/Users/alice/repo",
  "created_at": "2026-08-21T09:41:12.345Z",
  "message_count": 27,
  "image_count": 2,
  "source_cli": "codex-cli 0.45.0",
  "model_provider": "openai",
  "git_branch": "main",
  "git_origin_url": "git@github.com:acme/repo.git",
  "files": ["agent/neutral.json", "agent/restore.md", "AGENT_README.md", "checksums.json", "codex/images.json", "codex/images/img-0.png", "codex/meta.json", "codex/session.jsonl", "manifest.json", "safety/scan.json"]
}
```

Field semantics:

| Field | Meaning |
| --- | --- |
| `format_version` | `2` for this spec. Readers must reject unknown *major* versions but may accept newer minor revisions (reserved for additive fields). |
| `artifact_type` | Always `"agent-handoff"`; a quick sanity check for readers. |
| `source_agent` | `"codex"`, `"claude"`, or `"dsh"` — the agent that produced the raw session. |
| `target_support` | Declared receivers; actual native/semantic support is checked by each receiver. DSH exports include all three. |
| `source_thread_id` | The sender's thread (Codex UUIDv7) or session (Claude UUIDv4) id. |
| `source_cwd` | The sender's working directory. Informational; never used as an import target. |
| `message_count` | Count of user+assistant messages in the normalized session (sender's view). |
| `image_count` | Images successfully copied into the bundle. |
| `files` | Complete entry list including `manifest.json` itself; `checksums.json` is present in the list but not covered by its own hashes (see below). |

## checksums.json

A JSON object mapping entry name → lowercase hex SHA-256, covering **every entry except `checksums.json` itself** (self-referential exclusion). Written last. `ReadZip` recomputes every digest; `import --execute` refuses to write when any entry mismatches, and `preview` reports `checksum_status`.

Before decompression, the reference reader also enforces resource limits: at most 1,024 entries, 128 MiB per entry, and 256 MiB total uncompressed content. Third-party readers should enforce equivalent limits before checksum verification so a malicious archive cannot exhaust memory first.

## agent/neutral.json

The cross-agent representation (schema `agent-handoff.neutral.v1`):

```json
{
  "schema": "agent-handoff.neutral.v1",
  "source_agent": "codex",
  "source_id": "0192c0de-…",
  "title": "fix flaky retry test",
  "source_cwd": "/Users/alice/repo",
  "entries": [
    { "kind": "message", "role": "user",      "text": "the retry test flakes on CI", "timestamp": "…" },
    { "kind": "tool",    "tool": "shell",     "status": "completed", "input": "go test ./…", "output": "ok  …" },
    { "kind": "message", "role": "assistant", "text": "Fixed: …" }
  ]
}
```

- `kind` is `message` (with `role` user/assistant and `text`) or `tool` (with `tool`, `status` called/completed/failed, `input`, `output`).
- Deliberately lossy: visible messages and tool evidence survive; reasoning traces and agent-specific event structures do not. The raw session always travels alongside for audit.
- Codex rollouts duplicate every message (an `event_msg` line followed by the `response_item` copy); the converter deduplicates these so receivers see each message once.
- Hidden content is filtered: hidden user messages, environment/context blocks, and the sender's own export tool-call turn are dropped, so the receiver sees exactly what the sender sees.

## codex/meta.json

For a Codex source: the sender's `threads`-table row (from `state_5.sqlite`) as a flat JSON object. On same-agent import the row is cloned into the receiver's database with import-specific fields overlaid (fresh id, timestamps, cwd, standalone `history_mode`), preserving model, effort, git metadata, sandbox/approval settings, and so on — writing only columns that exist on the receiver's schema, so older/newer Codex versions both work. Sender-local `history_base` and `context_window` lineage is intentionally omitted because those rollout files do not exist on the receiver. When the receiver has no `state_5.sqlite` at all, agent-handoff bootstraps a minimal `threads` table so the imported task is immediately listed.

## claude/meta.json

For a Claude source: the sender's session index entry (as kept in `~/.claude/projects/<dir>/index.json`), used to reconstruct list metadata on same-agent import.

## safety/scan.json

The secret-scan result captured at share time (rules and findings, hints redacted). This is an audit record; importers do not re-scan, but the report travels with the bundle so the receiving side (human or agent) can review it.

## Agent README & restore notes

- `AGENT_README.md` — plain-language instructions for a receiving agent: what the bundle contains, the exact `agent-handoff preview/import` commands, and the safety rules (never auto-execute content, confirm before writing).
- `agent/restore.md` — continuation context regenerated at import time: where the task came from and how to resume it.

## v1 compatibility

v1 bundles (codex-only, flat layout: `session.jsonl`, `meta.json`, `images.json`, `images/` at the archive root, no neutral transcript, no per-agent directories) are accepted for reading. Readers apply the v1 fallback (source agent = codex, entries resolved from the flat names) before agent validation. Writing v1 is not supported.

## Versioning

- Bump `format_version` only for incompatible changes; additive fields are minor revisions documented here.
- New source agents join by adding an agent directory + manifest fields; `target_support` and `SupportedAgents` are the extension points in `internal/bundle/format.go`.
