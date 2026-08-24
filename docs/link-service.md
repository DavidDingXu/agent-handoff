# Link sharing

[English] | [简体中文](link-service.zh-CN.md)

`agent-handoff share --format link` turns a bundle into a URL you can paste anywhere. The bundle is encrypted **on your machine** with AES-256-GCM. With no configuration, the CLI uses the project-operated Worker and automatically falls back to anonymous temporary-file providers. Passing `--endpoint` selects only your self-hosted Worker instead.

## Threat model

- Storage providers and self-hosted storage APIs cannot read shared tasks: the 256-bit key is generated client-side and travels **only in the URL fragment** (`#h=…` for anonymous relay links, `#k=…` for self-hosted links). Browsers and HTTP clients do not send fragments in requests. A resolver page opened in a browser can read its own fragment, so use the built-in audited page or another resolver origin you trust; CLI import never fetches the resolver.
- Ciphertext integrity is enforced twice: GCM authentication on decrypt, plus a SHA-256 recorded in the link manifest and checked on download.
- Loss of confidentiality is limited to people who hold the **full link** (including fragment). Send it over a channel you trust — the link *is* the capability.
- Providers can observe IP addresses, upload/download times, and ciphertext sizes; they cannot forge or tamper content undetectably.

## Zero-configuration mode

With no endpoint configured, the CLI first uploads ciphertext to the project-operated Worker at `agent-handoff-link.798148655.workers.dev`. It requires no account or token, defaults to a 10-minute lifetime, limits each link to 10 downloads, and caps the public pool at 800 uploads per day, 20,000 per month, and 512 MiB of live ciphertext. It is a best-effort free service; when its budget is exhausted the CLI moves to the fallback pool.

If the project service is unavailable, the CLI encrypts once and concurrently uploads the same ciphertext to up to two providers selected from [Filebin](https://filebin.net/api), [tmpfiles.org](https://tmpfiles.org/api), [Uguu](https://uguu.se/api), and [temp.sh](https://temp.sh/). Provider failures are isolated: import tries each recorded replica until one passes size, SHA-256, and AES-GCM checks. If every upload fails, the CLI keeps and returns the local zip as `fallback_zip`.

Anonymous fallback links default to 24 hours and clamp `--ttl` to 60 seconds–7 days. Providers have their own retention policies, so a recorded replica may disappear before the link's logical expiry. The CLI tries every replica and rejects the whole link after its logical expiry. These free services are best-effort and can change limits or availability without notice; use a zip or self-hosted endpoint when delivery must be guaranteed.

The resulting URL uses the static `/r` resolver page and carries a compact manifest in `#h=`:

```json
{
  "v": 1,
  "r": [
    { "p": "tmpfiles.org", "u": "https://tmpfiles.org/dl/…/bundle.enc" },
    { "p": "uguu.se", "u": "https://d.uguu.se/….enc" }
  ],
  "k": "base64url AES key",
  "n": "base64url GCM nonce",
  "s": "ciphertext sha256",
  "b": 48213,
  "e": "2026-08-24T03:20:00Z"
}
```

The CLI accepts only HTTPS URLs on the exact supported provider hosts, rejects duplicate/expired/oversized manifests before downloading, and never fetches the resolver URL. `AGENT_HANDOFF_RESOLVER` may replace the built-in resolver page; it must be HTTPS and serve the same static fragment-reading UI.

## Self-hosted mode

Set `AGENT_HANDOFF_ENDPOINT` or pass `--endpoint` to bypass anonymous providers and use the Worker protocol below.

### Share lifecycle

```
sender                                    worker (Cloudflare)
──────                                    ──────────────────
1. build zip bundle
2. AES-256-GCM encrypt  ── ciphertext ──> POST /v1/shares (multipart)
3. receive share_url                     store blob in R2/KV, manifest in DO
4. append #k=<key> to share_url          (server never sees key)

receiver
────────
5. GET /v1/shares/:id          → link manifest (JSON)
6. GET /v1/shares/:id/blob     → ciphertext (size + sha256 checked)
7. decrypt with key from fragment → zip → normal import path
```

Limits enforced by the worker: 32 MiB per blob (25 MiB on KV), link TTL of 10 minutes by default (request 60 s – 24 h via `ttl_seconds`; the sender CLI flag is `--ttl <seconds>`), 10 downloads per share, 4 GiB live bytes, plus monthly put/get quotas via the BudgetGate Durable Object. On KV the TTL is enforced natively (the blob self-destructs); on R2 it is enforced via `expires_at` plus an hourly cron that deletes expired shares.

### HTTP API

| Route | Method | Purpose |
| --- | --- | --- |
| `/v1/capabilities` | GET | Service limits and whether uploads require a token |
| `/v1/shares` | POST | Upload (multipart fields: `share_id`, `manifest`, file `blob`) → `201` with `share_url`, `manifest_url`, `expires_at` |
| `/v1/shares/:id` | GET | Link manifest JSON |
| `/v1/shares/:id/blob` | GET | Encrypted bundle bytes |
| `/s/:id` | GET | Human share page (import instructions) |
| `/s/:id.agent.md` | GET | Agent handoff markdown |
| `/s/:id.agent.json` | GET | Agent handoff JSON |
| `/r` | GET | Static resolver page for anonymous `#h=` links; does not use storage bindings |

Uploads may require a bearer token (`Authorization: Bearer …`) depending on deployment; the CLI sends one from `--token` or `AGENT_HANDOFF_TOKEN`.

### Self-hosted link manifest

```json
{
  "schema": "agent-handoff.link.v1",
  "thread": { "id": "0192…", "title": "fix flaky retry test" },
  "bundle": { "url": "https://share.example.com/v1/shares/Wi5x…/blob", "sha256": "…", "bytes": 48213 },
  "crypto": { "alg": "AES-256-GCM", "nonce": "…", "key_ref": "url-fragment:k" },
  "ttl_seconds": 600,
  "expires_at": "2026-08-21T09:51:12Z"
}
```

`Validate` on the client side enforces the schema, algorithm, nonce presence, and `key_ref` — the manifest never contains the key itself.

### Deploy your own

The worker is a single file (`deploy/worker/src/index.js`, no build step) using a Durable Object plus a blob store: **Workers KV** (no credit card required; free tier 1 GB storage / 1k writes / 100k reads per day — fine for 10-minute links) or **R2** (requires a payment card; blobs up to 32 MiB). It auto-detects whichever binding is present.

Option A — KV, no card:

```sh
cd deploy/worker
npm ci
npx wrangler login
cp wrangler.toml.example wrangler.toml
npx wrangler kv namespace create SHARE_KV   # put the printed id into wrangler.toml
npx wrangler secret put SHARE_UPLOAD_TOKEN  # optional: require an upload token
npx wrangler deploy
```

On a Cloudflare account that has never used Workers, open **Workers & Pages** in the Cloudflare dashboard once after `wrangler login` and create or confirm the account's `workers.dev` subdomain. Cloudflare performs this one-time account initialization from the dashboard; otherwise `wrangler deploy` fails with API error `10063`. Use `npx wrangler whoami` to confirm that the dashboard and Wrangler are using the same account.

Option B — R2, card on file:

```sh
npx wrangler r2 bucket create agent-handoff   # swap the KV block for the R2 block in wrangler.toml
npx wrangler deploy
```

The default URL is `<worker-name>.<your-subdomain>.workers.dev` — no custom domain needed. Then point clients at your instance:

```sh
export AGENT_HANDOFF_ENDPOINT=https://agent-handoff-link.<your-subdomain>.workers.dev
export AGENT_HANDOFF_TOKEN=<the-same-value-stored-in-SHARE_UPLOAD_TOKEN>
curl -fsS "$AGENT_HANDOFF_ENDPOINT/v1/capabilities"
agent-handoff share --format link
```

If you omit the optional Worker secret, leave `AGENT_HANDOFF_TOKEN` unset. Keep `wrangler.toml` local: it contains your account-specific namespace id and is ignored by Git.

The stock deployment is sized for a team on the free tier; adjust `LIMITS` in `index.js` before running a public instance. The worker is intentionally simple to audit (~600 lines, no dependencies).

## CLI integration

- With no endpoint, the CLI uses the project-operated Worker and emits a `#k=` link; if it is unavailable, the anonymous provider pool emits a `#h=` link.
- `--endpoint URL` / `AGENT_HANDOFF_ENDPOINT` — use a self-hosted worker and emit a `#k=` link.
- `--token TOKEN` / `AGENT_HANDOFF_TOKEN` — bearer token for uploads.
- `AGENT_HANDOFF_RESOLVER` — optional HTTPS resolver page for anonymous links.
- When every applicable upload fails, the CLI returns the local zip (`status: "fallback_zip"`), so a share never silently disappears.
- `import <url>` accepts a full link (fragment included), downloads, verifies, decrypts, and runs the normal import path — including the dry-run/`--execute` split and duplicate detection.
