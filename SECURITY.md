# Security Policy

[English] | [简体中文](docs/SECURITY.zh-CN.md)

## Supported versions

Only the latest `main` and the most recent tagged release receive security fixes.

## Reporting a vulnerability

**Please do not report vulnerabilities through public GitHub issues.**

Email the maintainer directly (see the repository owner profile for contact details), or open a GitHub Security Advisory via *Security → Report a vulnerability* on this repository. Include:

- the component involved (CLI, bundle format, link service worker),
- a minimal reproduction or proof of concept,
- the agent-handoff version (`agent-handoff version`) and OS.

You should receive a response within 72 hours. Once a fix is released we will credit you in the changelog unless you prefer to remain anonymous.

## Security properties

agent-handoff handles sensitive material — full agent transcripts — by design. The properties we commit to and test:

- **Link payloads are end-to-end encrypted.** AES-256-GCM with a client-generated 256-bit key; the key travels only in the URL fragment and never reaches storage providers or self-hosted storage APIs. The fragment is absent from HTTP requests to the resolver; if opened in a browser, its page script can read the fragment, so use the built-in audited page or another resolver you trust.
- **Secret scan gates export.** High-confidence patterns (OpenAI/Anthropic/AWS keys, GitHub tokens, private key blocks, bearer JWTs) block a share until the user explicitly opts in with `--include-secrets`.
- **Bundles are tamper-evident.** Every entry is SHA-256 checksummed; `import --execute` verifies checksums before writing anything.
- **Bundle extraction is bounded.** Readers reject archives with more than 1,024 entries, entries larger than 128 MiB, or more than 256 MiB of total uncompressed content before import writes.
- **Import is append-only.** agent-handoff never modifies or deletes existing agent state; it writes backups alongside every file it touches and never executes or fetches content found inside a shared session.
- **Imported content is untrusted data.** Nothing inside a bundle (commands, URLs, tool outputs) is executed automatically.

## Known limitations

- The share link itself is the decryption capability. Anyone who obtains the full URL (including its `#h=` or `#k=` fragment) can decrypt the payload; send links over channels you trust.
- Anonymous storage providers can observe the uploader/downloader IP, times, sizes, and user agent. They receive ciphertext only, but their availability and deletion timing are outside this project's control.
- The project-operated Worker and anonymous fallback pool are best-effort, not durable storage or an SLA. Use a self-hosted endpoint or a zip file when availability must be controlled.
- The secret scan is high-confidence by design and not exhaustive — it can miss non-standard or custom secret formats.
