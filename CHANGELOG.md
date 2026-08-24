# Changelog

All notable changes to this project are documented in this file.
The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.3.1] - 2026-08-24

### Fixed

- Claude Code imports now encode Windows drive-letter paths using Claude's
  `C--Users-...` project-directory convention, so both same-agent and
  Codex-to-Claude imports work on Windows.
- Added black-box CLI coverage for all four Codex/Claude Code handoff
  directions and a contract test for native Codex/Claude question tools.

## [0.3.0] - 2026-08-24

### Added

- Zero-configuration encrypted links: the CLI encrypts locally and uses the project-operated Worker by default, with concurrent anonymous-provider failover (including Filebin) and a private local zip as the final fallback. Anonymous links encode the replica list, key, nonce, checksum, size, and expiry in a `#h=` fragment.
- A storage-independent Worker `/r` page for anonymous links. Its audited script reads the fragment only in the browser, does not transmit it, and exposes a ready-to-copy import command; the fragment is absent from the HTTP request.
- Worker dual-mode storage: auto-detected R2 (`SHARE_BUCKET`, 32 MiB blobs, card required) or Workers KV (`SHARE_KV`, 25 MiB blobs, free tier with no credit card). On KV the link TTL is enforced natively (`expirationTtl`); on R2 via `expires_at` + hourly cron.
- Short-lived links by default: TTL clamped to 60 s – 24 h with a 10-minute default; sender CLI flag `share --ttl <seconds>`, manifest field `ttl_seconds`, and `expires_at` echoed in the share output so agents can tell users when the link dies.
- Endpoint precedence `--endpoint > AGENT_HANDOFF_ENDPOINT > project-operated DefaultEndpoint`; an explicit endpoint selects only that self-hosted service, while failure of the built-in service triggers the anonymous provider pool. All-provider failure still returns `fallback_zip`.
- Cross-agent skill UX: when the share format or an import safety decision needs confirmation, use Codex `request_user_input` or Claude Code `AskUserQuestion` with their native schemas; link results surface the 10-minute expiry.

### Fixed

- Worker cleanup now awaits and paginates Durable Object storage, deletes expired blobs and metadata, and releases their live-byte quota.
- Upload commits recheck live-byte and monthly-put limits; failed uploads no longer release quota that was never reserved, and live bytes survive monthly counter rollover.
- Bundle readers cap archive entries and decompressed bytes before checksum verification to prevent memory exhaustion from malicious zip files.
- CI runs Worker behavior tests and uses platform-neutral Go build and smoke commands.
- Import defaults to the current Codex or Claude Code host instead of the bundle's source agent, so conversational cross-agent imports take the correct restore path.
- Link mode builds the plaintext zip in memory and writes a private `0600` archive only when file mode is selected or every upload provider fails.
- Anonymous fallback links now default to 24 hours and support up to 7 days, while keeping provider retention best-effort.
- Added repository guidance, a complete third-party agent adapter guide, and direct self-hosted endpoint setup for macOS, Linux, and Windows.
- Anonymous provider uploads run concurrently and return after a short replica grace period, so one slow provider no longer blocks later healthy providers.
- Codex rollout paths use platform-native separators on Windows; POSIX-only permission assertions no longer misclassify inherited Windows ACLs as public files.

## [0.2.0] - 2026-08-21

### Added

- Cross-agent sharing: codex→claude, claude→codex, claude→claude alongside codex→codex. Same-agent imports preserve native events and content while assigning a fresh task identity; cross-agent imports synthesize a native session from an agent-neutral transcript (schema `agent-handoff.neutral.v1`).
- Bundle format v2: per-agent directories (`codex/`, `claude/`), neutral transcript, agent metadata, secret-scan report, per-entry SHA-256 checksums, and an `AGENT_README.md` written for the receiving agent. v1 bundles remain readable.
- Link handoff (`share --format link`): AES-256-GCM client-side encryption, self-hostable Cloudflare Worker link service (`deploy/worker/`) with R2 blob storage, Durable Object quota budgeting, TTL + download limits, and hourly cleanup. The decryption key travels only in the URL fragment (`#k=`); the server never sees it. `import <url>` downloads, verifies, decrypts, and imports.
- Secret scan blocking export on high-confidence findings (OpenAI/Anthropic keys, AWS keys, GitHub tokens, private key blocks, bearer JWTs), with explicit `--include-secrets` override and redacted hints.
- Import safety: checksum verification before any write, per-home duplicate ledger with `status: duplicate` and `--allow-duplicate`, automatic backups, append-only writes, and dry-run (`import` without `--execute`) by default.
- Claude adapter: session index management, projects-directory layout (`<project-dir>/<uuid>.jsonl`), UUID chain rewrite, last-prompt handoff records.
- Codex adapter: rollout normalization (dropped rolled-back turns, trailing self-export turn, in-flight turns), threads-table row clone with schema-aware column detection, and bootstrap of `state_5.sqlite` on receivers without it.
- Full test suite: per-package unit tests plus integration tests covering four-quadrant round trips against synthetic agent homes and the URL import path with an in-process fake worker.
- Build/release engineering: Makefile, golangci-lint config, goreleaser v2 (darwin/linux amd64/arm64, windows amd64), GitHub Actions CI (test matrix with `-race`, lint, worker syntax check) and release workflow, `install.sh` with checksum verification.
- Documentation: bilingual README (EN/zh-CN), bundle format spec, link service guide, CONTRIBUTING, SECURITY.

### Fixed

- Anthropic key pattern (`sk-ant-api03-…`) not matching due to unhandled digits; rule ordering so Anthropic keys are not shadowed by the generic OpenAI pattern.
- Duplicate messages in neutral transcripts (Codex rollouts carry every message twice — `event_msg` plus `response_item` copy).
- Self-export turn detection blind to `event_msg`/`user_message` lines; shell tool-call argv arrays in JSON form (`{"command":["agent-handoff","share"]}`) now parsed.
- v1 bundle decode order: agent fallback applied before source-agent validation.
- Manifest checksum self-reference (`checksums.json` no longer listed as covered by itself).
- Sender cwd leaking into Claude `last-prompt` handoff records; the first user message text is used instead.
- Thread-row metadata lost when the receiver had no `state_5.sqlite`; a minimal `threads` table is now bootstrapped so imported tasks are listed immediately.

## [0.1.0] - 2026-08-20

### Added

- Initial release: single-package CLI with codex→codex share/preview/import/verify, v1 bundle format, secret scan, and ledger-based duplicate detection.
