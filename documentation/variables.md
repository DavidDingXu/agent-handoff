# Variables And Secrets

| Name | Used by | Scope | Source | Rotation | Risk |
| --- | --- | --- | --- | --- | --- |
| `CODEX_HOME` | CLI | Local process | User environment | Not a secret | Redirects reads/writes to another Codex home |
| `CLAUDE_CONFIG_DIR` | CLI | Local process | User environment | Not a secret | Redirects reads/writes to another Claude home |
| `AGENT_HANDOFF_ENDPOINT` | CLI | Local process | User environment | Not a secret | Selects a trusted compatible hosted service |
| `AGENT_HANDOFF_TOKEN` | CLI | Local process | User secret | Rotate in hosted service and clients | Optional bearer credential for a private compatible service; never include in a share |
| `AGENT_HANDOFF_RESOLVER` | CLI | Local process | User environment | Not a secret | Browser-opened resolver code can read URL fragments |
| `SHARE_UPLOAD_TOKEN` | Worker | Server secret | `wrangler secret put` | Rotate and update clients | Controls who can upload ciphertext |
| `SHARE_BUCKET` | Worker | Server binding | Cloudflare configuration | Not applicable | R2 ciphertext storage access |
| `SHARE_KV` | Worker | Server binding | Cloudflare configuration | Not applicable | KV ciphertext storage access |
| `SHARE_BUDGET_GATE` | Worker | Server binding | Cloudflare configuration | Not applicable | Share metadata registry; historical binding name retained for compatibility |

No credential is compiled into client binaries or committed to the repository. The built-in resolver URL is public configuration, not a secret. URL fragments are capabilities and must be treated as secrets even though they are not environment variables.

## Pre-release checklist

- Scan tracked files and Git history for real credentials, account IDs, private paths, and unredacted session data.
- Confirm `wrangler.toml` remains ignored and only `wrangler.toml.example` is committed.
- Confirm Release assets come from the tagged commit and publish checksums.
- Confirm README examples use synthetic links and placeholder credentials.
- For a private deployment, rotate the Worker upload token if it was ever printed or committed. The project-operated public service intentionally has no upload token; its hosting account and storage platform enforce their own limits.
