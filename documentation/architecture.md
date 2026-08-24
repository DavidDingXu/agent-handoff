# Architecture

## Product boundary

agent-handoff exports one local Codex or Claude Code session into a versioned zip bundle, optionally encrypts and uploads that bundle, and imports it as a new native session. It transfers visible conversation data and selected metadata. It does not transfer repository files, credentials, hidden model state, permissions, memories, or uncommitted changes.

The CLI is a Go static binary. A bundled Skill lets Codex and Claude Code drive the CLI conversationally. The project-operated Cloudflare Worker is the zero-configuration link service; anonymous temporary-file providers are its automatic fallback, and teams can select their own Worker.

## Components

| Component | Responsibility |
| --- | --- |
| `internal/codex`, `internal/claude` | Read and append native agent session state |
| `internal/bundle`, `internal/neutral` | Versioned archive and cross-agent transcript |
| `internal/safety` | High-confidence secret scan before export |
| `internal/link` | AES-256-GCM, upload/download, provider validation and failover |
| `internal/ledger` | Per-agent-home duplicate import records |
| `internal/cli` | User-facing command contract and host detection |
| `skills/agent-handoff` | Agent workflow, confirmations, and platform binary resolution |
| `deploy/worker` | Optional encrypted blob service and static resolver |

## Trust boundaries

- Local agent homes contain private transcripts. The CLI reads them only for an explicit share and writes only when import is confirmed with `--execute`.
- A zip file contains plaintext conversation data. Files are written with `0600` permissions; transport remains the user's responsibility.
- Link payloads are encrypted before network access. Providers receive ciphertext plus network metadata such as IP, time, and size.
- URL fragments contain the decryption capability. They are absent from HTTP requests, but browser code on a resolver page can read its own fragment. CLI import never fetches the resolver.
- Imported bundles and URLs are untrusted. Checksums, size limits, supported-agent checks, HTTPS restrictions, and append-only restore behavior are enforced in code rather than prompts.

## Known risks and assumptions

- Native Codex and Claude Code storage formats are not stable public APIs; adapter tests pin the schemas currently supported.
- Anonymous providers offer no SLA and may change retention or API behavior. Zip export is the durable fallback.
- The default Worker and resolver are project-operated infrastructure and can become unavailable. Link creation then tries the anonymous provider pool and finally keeps a local zip; CLI imports of anonymous links do not fetch the resolver because all capability data is in the URL fragment.
- Prebuilt binaries are currently unsigned. Release checksums provide integrity, but platform code signing would improve first-run trust.
- Secret scanning is intentionally high-confidence and can miss custom secret formats.

No automated email is sent, so there is no `emails.md`. The Worker has no public content that needs search indexing, so there is no `seo.md`.

## Related documents

- [flows.md](flows.md)
- [permissions.md](permissions.md)
- [variables.md](variables.md)
- [tests.md](tests.md)
- [cron.md](cron.md)
- [automation.md](automation.md)
- [Security policy](../SECURITY.md)
- [Bundle format](../docs/bundle-format.md)
- [Link service](../docs/link-service.md)
