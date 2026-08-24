# Link sharing

[English] | [简体中文](link-service.zh-CN.md)

`agent-handoff share --format link` turns a bundle into a URL you can paste anywhere. The bundle is encrypted **on your machine** with AES-256-GCM. With no configuration, the CLI uses the project-operated service and automatically falls back to anonymous temporary-file providers. Advanced users can connect an existing HTTP file service through declarative configuration without depending on Cloudflare.

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

`upload_type` is `multipart` or `raw`. Responses may be `text`, or `json` with an RFC 6901 JSON Pointer to the download URL; arrays use paths such as `/files/0/url`. URLs, headers, and form values may reference `{filename}`, `{bytes}`, `{sha256}`, `{ttl_seconds}`, and local environment variables as `${ENV_NAME}`. Do not put tokens directly in the config.

The returned download URL must be public HTTPS and allow an unauthenticated `GET` of the exact ciphertext bytes. Config parsing is strict and rejects unknown fields. Services requiring multi-step login, dynamic request signing, or custom download requests are outside the first declarative contract; propose reusable fields through an issue rather than injecting shell commands.

The presence of a config explicitly selects custom-provider mode: the CLI does not try the project Worker or the four anonymous providers. Multiple providers upload concurrently and up to two successful replicas are recorded. If every provider fails, the CLI returns a local zip. To keep the visible link off the default `workers.dev` resolver as well, configure a trusted `AGENT_HANDOFF_RESOLVER`; CLI import never requests the resolver.

## CLI integration

- By default, the project-operated Worker emits a `#k=` link; if it is unavailable, Filebin, tmpfiles, Uguu, and temp.sh emit a `#h=` link.
- The default config file or `--config FILE` — explicitly use one or more custom HTTP providers and emit a `#h=` link.
- `AGENT_HANDOFF_RESOLVER` — optional HTTPS resolver page for relay links.
- When every applicable upload fails, the CLI returns the local zip (`status: "fallback_zip"`), so a share never silently disappears.
- `import <url>` accepts a full link (fragment included), downloads, verifies, decrypts, and runs the normal import path — including the dry-run/`--execute` split and duplicate detection.
