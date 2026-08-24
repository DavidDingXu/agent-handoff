# Contributing to agent-handoff

[English] | [简体中文](docs/CONTRIBUTING.zh-CN.md)

Thanks for your interest in improving agent-handoff. This document covers the workflow, expectations, and architecture map you need to contribute effectively.

Read [AGENTS.md](AGENTS.md) before changing code. It is the repository-wide contract for coding agents and contributors.

## Ground rules

- **Be conservative with user data.** agent-handoff reads and writes real agent state (`~/.codex`, `~/.claude`). Import must remain append-only; existing threads, indexes, and database rows are never modified. Every write path keeps a backup.
- **Secrets stay scary.** New export features must go through the safety scan; new scan rules must be high-confidence (we prefer false negatives over blocking legitimate shares).
- **JSON output is a contract.** Agents parse CLI output; field names and shapes change only with strong reason.

## Development workflow

```sh
git clone <your-fork>
cd agent-handoff
make test      # unit + integration, run before anything else
make lint      # golangci-lint (errcheck, govet, staticcheck, unused, misspell, copyloopvar)
make build     # bin/agent-handoff
```

1. Fork, create a topic branch from `main`. Use `feat/<name>` for features, `fix/<name>` for fixes, and `docs/<name>` for documentation.
2. Make your change. Add or extend tests — the integration suite (`internal/integration_test`) covers the four-quadrant round trips and the link handoff; cross-agent or format changes must update it.
3. Ensure `make test lint` is green locally. CI runs the matrix (Ubuntu/macOS/Windows, `-race`) plus lint and worker syntax checks.
4. Open a pull request with a clear description: what changed, why, and how you verified it.

Commit messages follow the repo history: `type: summary` (feat / fix / test / build / docs / refactor). Keep the subject line under ~72 characters.

## Branch and release policy

- `main` is the only long-lived branch and must remain releasable.
- All normal changes enter through a pull request from a short-lived `feat/*`, `fix/*`, or `docs/*` branch.
- Required CI checks must pass before merge. Maintainers may merge without a second approval while the project has only one active maintainer.
- Force pushes and branch deletion are disabled for `main`.
- Releases are immutable `v*` tags cut from a green `main`; there is no long-lived release branch.

## Architecture map

```
main.go                     entrypoint
internal/cli/               command surface: share, preview, import, verify
internal/bundle/            the .agent-handoff.zip container (v2 + v1 read compat)
internal/codex/             Codex adapter: read rollout, normalize, restore, verify
internal/claude/            Claude adapter: read session/index, restore, verify
internal/neutral/           agent-neutral transcript (cross-agent bridge)
internal/session/           shared jsonl iteration/analysis utilities
internal/safety/            pre-export secret scan
internal/link/              AES-256-GCM + compatible link services and providers
internal/images/            image asset collection
internal/ledger/            import duplicate ledger
internal/idgen/             UUIDv7/v4, titles, paths
deploy/worker/              Cloudflare Worker link service (single-file JS)
skills/agent-handoff/         agent skill (SKILL.md) bundled with the plugin
```

### Extension points

- **New source/target agent**: follow [docs/adding-agent.md](docs/adding-agent.md). It covers native read/restore, neutral conversion, CLI and host integration, bundle compatibility, and the required N x N test matrix.
- **Custom file provider**: use declarative `config.json`; no Go change is needed for multipart/raw services covered by the current schema. New generic fields require tests and a security review. See [docs/extensions.md](docs/extensions.md) and [docs/link-service.md](docs/link-service.md#custom-providers).
- **New scan rule**: add to `internal/safety/scan.go` `rules` (mind ordering — specific patterns before generic ones) with a test.
- **Bundle format changes**: additive fields are minor; anything incompatible bumps `format_version` and needs a reader path for the old version (see v1 compat in `internal/bundle/zip.go`).

### Testing conventions

- Unit tests live next to their package (`foo_test.go`).
- Cross-package behavior goes in `internal/integration_test` with synthetic agent homes built in temp dirs — never touch the developer's real `~/.codex` or `~/.claude`.
- Compatible-service crypto tests use an in-process HTTP server (`httptest`), no network.

## Release process (maintainers)

Tags `v*` trigger the release workflow: tests run, then goreleaser builds the matrix (darwin/linux amd64/arm64, windows amd64), archives with checksums, and attaches them to a GitHub release. `install.sh` consumes those archives.

## Reporting issues

Open an issue with: agent-handoff version (`agent-handoff version`), OS, the command you ran, and the JSON output (redact anything sensitive). For security issues, see [SECURITY.md](SECURITY.md) — do not open public issues for vulnerabilities.

Feature priorities and planned agent integrations are tracked in the
[roadmap](ROADMAP.md).
