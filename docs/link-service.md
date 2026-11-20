# Link sharing

[English] | [简体中文](link-service.zh-CN.md)

`agent-handoff share --format link` turns a bundle into a URL you can paste anywhere. The bundle is encrypted **on your machine** with AES-256-GCM. With no configuration, the CLI uses the project-operated service and automatically falls back to anonymous temporary-file providers. The core is not coupled to Cloudflare: users can select any compatible HTTP endpoint or connect an existing file service through declarative provider configuration.

## Threat model

- Storage providers and self-hosted storage APIs cannot read shared tasks: the 256-bit key is generated client-side and travels **only in the URL fragment** (`#h=…` for anonymous relay links, `#k=…` for self-hosted links). Browsers and HTTP clients do not send fragments in requests. A resolver page opened in a browser can read its own fragment, so use the built-in audited page or another resolver origin you trust; CLI import never fetches the resolver.
- Ciphertext integrity is enforced twice: GCM authentication on decrypt, plus a SHA-256 recorded in the link manifest and checked on download.
- Loss of confidentiality is limited to people who hold the **full link** (including fragment). Send it over a channel you trust — the link *is* the capability.
- Providers can observe IP addresses, upload/download times, and ciphertext sizes; they cannot forge or tamper content undetectably.

## Browser reader

The Worker implementation in this repository provides the same local reader for hosted `/s/:id#k=…` links and anonymous `/r#h=…` links. After downloading ciphertext, the page checks its size and SHA-256, decrypts with AES-GCM in the browser, and verifies ZIP paths, expansion limits, CRCs, and all v2 file checksums before displaying bundle content. Older v1 bundles can be read with an explicit warning that file checksums are unavailable. This describes the repository implementation; an existing resolver deployment must be updated to provide this interface.

The English / Simplified Chinese interface displays recorded task excerpts, caller notes, paginated conversation, expandable tool inputs and outputs, and any included code file list with its branch and base commit. All bundle text is inert: the page does not interpret HTML or Markdown, load linked images or URLs, execute commands, or apply code. The complete import command includes the fragment key and creates a new native task when run locally.

**Download ZIP** becomes available after successful decryption, including when the browser cannot safely preview the archive or a file checksum fails. A failed integrity check displays no bundle content; such a download is for inspection. To continue, copy the complete link into your agent; the terminal command is collapsed by default. Each built-in anonymous replica is tried directly, then through a same-origin ciphertext download if browser access fails, before moving to the next replica. The fallback accepts only the four built-in storage providers, with bounded redirects, time and size. No server receives the fragment, decryption key, or plaintext. Custom configured providers still need browser CORS support or local import; arbitrary provider URLs are never proxied. Without a successfully decrypted payload, the browser cannot offer a ZIP download; the CLI can still try the same encrypted replicas locally.

Browser preview limits are 64 MiB for the encrypted download and ZIP, 128 MiB per expanded file, 256 MiB total expanded data, 1,024 ZIP entries, 16 MiB per parsed JSON/native transcript, and 100,000 transcript records. Conversation pages show at most 200 records; individual message/tool previews are shortened to 32,000 characters, with full text preserved in the ZIP. Replica locators must use public HTTPS domain names without credentials, fragments or custom ports. The built-in ciphertext fallback validates every redirect; custom-provider redirects and IP-address endpoints require local import. These browser limits do not change the CLI's archive contract.

## Zero-configuration mode

With no endpoint configured, the CLI first uploads ciphertext to the optional project-operated Worker at `agent-handoff-link.798148655.workers.dev`. It requires no account or token and defaults to a 10-minute lifetime. The Worker enforces only configured retention and payload-size bounds; Cloudflare applies its own account/platform limits. It is a best-effort free service, and any failure automatically moves the CLI to the fallback pool.

If the project service is unavailable, the CLI encrypts once and concurrently uploads the same ciphertext to up to two providers selected from [Filebin](https://filebin.net/api), [tmpfiles.org](https://tmpfiles.org/api), [Uguu](https://uguu.se/api), and [temp.sh](https://temp.sh/). Provider failures are isolated: import tries each recorded replica until one passes size, SHA-256, and AES-GCM checks. If every upload fails, the CLI keeps and returns the local zip as `fallback_zip`.

Anonymous fallback links default to 24 hours and clamp `--ttl` to 60 seconds–7 days. Providers have their own retention policies, so a recorded replica may disappear before the link's logical expiry. The CLI tries every replica and rejects the whole link after its logical expiry. These free services are best-effort and can change limits or availability without notice; use a zip or self-hosted endpoint when delivery must be guaranteed.

### Built-in routing configuration

The released CLI carries this zero-configuration route. It is product data, not a requirement imposed on custom providers:

1. Try the project-operated compatible endpoint.
2. If that default endpoint fails, start all four anonymous uploads concurrently.
3. Record at most two successful replicas. One valid replica is enough.
4. If none succeeds, return the local zip.

An explicit `--endpoint` or `AGENT_HANDOFF_ENDPOINT` selects another compatible hosted service and does not send data to anonymous providers. A `config.json` selects the declarative providers described below and also replaces the built-in route for that share.

| Built-in anonymous provider | Upload shape | Download URL handling | Local account/token |
| --- | --- | --- | --- |
| Filebin | raw encrypted bytes | generated object URL | No |
| tmpfiles.org | multipart field `file` | JSON URL normalized to `/dl/...` | No |
| Uguu | multipart field `files[]` | first URL in the JSON file list | No |
| temp.sh | multipart field `file` | plain-text URL | No |

The provider adapters above handle unstable public API differences; they do not add vendor fields to the declarative schema. Their retention and availability belong to each external service and may change independently of a CLI release.

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

For built-in providers, the CLI accepts HTTPS URLs only on the exact supported hosts. User-configured providers may return public HTTPS URLs. Before download, the CLI rejects malformed provider identifiers and duplicate, expired, or oversized manifests; it never fetches the resolver URL. `AGENT_HANDOFF_RESOLVER` may replace the built-in resolver page; it must be HTTPS and serve the same static fragment-reading UI.

## Custom providers

Connect an existing domestic object store, enterprise file platform, or ordinary upload API through declarative JSON; no third-party program is executed. Default config paths are:

- macOS: `~/Library/Application Support/agent-handoff/config.json`
- Linux: `${XDG_CONFIG_HOME:-~/.config}/agent-handoff/config.json`
- Windows: `%AppData%\agent-handoff\config.json`

Use `--config <file>` to select another config for one share. Multipart example:

```json
{
  "providers": [{
    "name": "my-service",
    "upload_url": "https://files.example.com/api/upload",
    "upload_type": "multipart",
    "file_field": "file",
    "headers": { "Authorization": "Bearer ${MY_FILE_TOKEN}" },
    "form_fields": { "expire": "{ttl_seconds}" },
    "response_type": "json",
    "url_json_pointer": "/data/url"
  }]
}
```

Raw-byte upload with a plain-text URL response:

```json
{
  "providers": [{
    "name": "raw-store",
    "upload_url": "https://files.example.com/upload/{filename}",
    "upload_type": "raw",
    "response_type": "text"
  }]
}
```

The schema contains only generic HTTP concepts:

| Field | Required | Contract |
| --- | --- | --- |
| `name` | Yes | Stable lowercase provider ID using letters, digits, `.`, `_`, or `-` |
| `upload_url` | Yes | Public HTTPS upload URL; templates are allowed |
| `upload_type` | Yes | `multipart` or `raw`; both use HTTP `POST` |
| `file_field` | Multipart only | Multipart field receiving the encrypted file |
| `headers` | No | HTTP headers; unsafe transport headers are rejected |
| `form_fields` | No | Additional multipart text fields |
| `response_type` | Yes | `text` or `json` |
| `url_json_pointer` | JSON only | RFC 6901 pointer to the public download URL, including array paths such as `/files/0/url` |

URLs, headers, and form values may reference `{filename}`, `{bytes}`, `{sha256}`, `{ttl_seconds}`, and local environment variables as `${ENV_NAME}`. Do not put tokens directly in the config. Multiple entries in `providers` are uploaded concurrently, and at most two successful replicas are kept.

The returned download URL must be public HTTPS and allow an unauthenticated `GET` of the exact ciphertext bytes. Config parsing is strict and rejects unknown fields. Services requiring multi-step login, dynamic request signing, or custom download requests are outside the first declarative contract; propose reusable fields through an issue rather than injecting shell commands.

The presence of a config explicitly selects custom-provider mode: the CLI does not try the project Worker or the four anonymous providers. Multiple providers upload concurrently and up to two successful replicas are recorded. If every provider fails, the CLI returns a local zip. To keep the visible link off the default `workers.dev` resolver as well, configure a trusted `AGENT_HANDOFF_RESOLVER`; CLI import never requests the resolver.

## Optional hosted-service implementation

`deploy/worker` is one compatible hosted-service implementation maintained by this project; it is not part of the provider schema and is not required by the CLI. A self-hosted deployment keeps its behavior in `deploy/worker/wrangler.toml`, copied from `wrangler.toml.example`:

| Variable | Default | Meaning |
| --- | ---: | --- |
| `SHARE_DEFAULT_TTL_SECONDS` | `600` | Lifetime used when a request omits TTL |
| `SHARE_MAX_TTL_SECONDS` | `86400` | Maximum accepted lifetime |
| `SHARE_MAX_BLOB_BYTES` | `26214400` | Maximum encrypted blob size, still capped by the selected storage platform |

The implementation does not define project download counts or project upload/storage quotas. The hosting account and storage platform enforce their own limits. Other compatible hosted services may use completely different deployment files and infrastructure while preserving the same client-facing link contract.

## CLI integration

- By default, the project-operated Worker emits a `#k=` link; if it is unavailable, Filebin, tmpfiles, Uguu, and temp.sh emit a `#h=` link.
- The default config file or `--config FILE` — explicitly use one or more custom HTTP providers and emit a `#h=` link.
- `AGENT_HANDOFF_RESOLVER` — optional HTTPS resolver page for relay links.
- When every applicable upload fails, the CLI returns the local zip (`status: "fallback_zip"`), so a share never silently disappears.
- `import <url>` accepts a full link (fragment included), downloads, verifies, decrypts, and runs the normal import path — including the dry-run/`--execute` split and duplicate detection.
