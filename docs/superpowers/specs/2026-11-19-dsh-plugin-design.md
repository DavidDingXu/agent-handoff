# DSH native handoff plugin

The maintainer approved implementing the previously discussed DSH-native plugin, and explicitly requires UI in the first version. Keep the existing Go handoff core and Codex/Claude behavior. The plugin must run inside DSH, not in a mock standalone web page.

## Result

An installable third-party DSH bundle exposes a native share action and an import panel. A person can share the selected session as a file or encrypted link, preview a received bundle, select a local workspace, and explicitly create a new resumable session. Progress, useful errors, retries, secret warnings, copy/download actions, and Chinese/English text belong in the UI.

Target `@deepseek-ai/*` SDK and runtime `0.2.2-rc.5`, upstream revision `2e39e70496aa02986ca6a820c5d497e816b69d7e`, Node >=24.9.0. Declare host packages as peers; never bundle a second DSH runtime or edit a user's profile during development. Reuse cached dependencies where compatible. Test with synthetic sessions and disposable profiles, never real user session homes or model credentials.

## Boundaries

- DSH plugin: native UI, owner authorization, selected-session access, DSH event/neutral mapping, preview confirmation, workspace selection, fresh native restore, duplicate ledger and audit sidecar.
- Go core: strict versioned bridge JSON, existing v2 bundle, secret scan, checksums, encrypted upload/download, provider fallback. No DSH filesystem adapter, daemon, or generic plugin loader.
- Current CLI `share --source`, `import --target`, and `verify --source` continue to address only Codex/Claude local adapters. DSH is a recognized bundle source and restores through the plugin.
- Existing Codex/Claude bundles import into DSH semantically; DSH bundles import into Codex/Claude through the existing neutral restore paths. Test all nine directions relevant to three supported hosts.

## Bridge wire contract

`agent-handoff bridge share --input FILE|-` accepts one UTF-8 JSON object:

```json
{"schema":"agent-handoff.bridge.share.v1","source_thread_id":"source-id","title":"Task","source_cwd":"/source/project","source_cli":"dsh@0.2.2-rc.5","native":{"schema":"agent-handoff.dsh.native.v1","runtime_version":"0.2.2-rc.5","session_format_version":4,"header":{},"inherited_event_count":0,"events":[]},"neutral":{"schema":"agent-handoff.neutral.v1","source_agent":"dsh","source_id":"source-id","entries":[{"kind":"message","role":"user","text":"Question"}]}}
```

The plugin supplies an actual validated header/event snapshot, not the empty illustrative values above. Go treats the SDK payload as bounded data, validates its envelope and identity, and never evaluates it. The plugin validates events against the pinned SDK before restore. Only known inert source metadata is accepted; runtime config, credentials, permission presets, and live approval state never become import configuration.

Share flags reuse `--format zip|link`, `--out`, `--endpoint`, `--token`, `--ttl`, `--config`, and an explicit `--include-secrets` override. Result schema is `agent-handoff.bridge.share-result.v1`, with existing `status`, file/link, expiry, count, and redacted safety fields. `fallback_zip` is a usable successful fallback, not a failed operation. Scan decoded strings across the entire input before writes or network; findings identify rule/field paths and never include raw secret excerpts.

`agent-handoff bridge read FILE|URL` verifies integrity before returning `agent-handoff.bridge.read-result.v1`: `status`, `checksum_status`, `fingerprint`, typed `manifest`, `neutral`, DSH-only `native` (null for other sources), `source_session` (raw native session string for an inert private audit sidecar), and `warnings`. V1 checksum absence is explicitly `not_available_v1`; synthesize missing legacy neutral data using the existing source converters. Never return archive instructions, credentials, arbitrary file maps, or follow URLs within session content.

Limits: request 32 MiB, native 16 MiB, neutral 8 MiB, encoded response 32 MiB, 100,000 neutral entries, 4 KiB title, 8 KiB identity/cwd. Reject invalid/trailing JSON, invalid schemas/identities/entries and excessive data before emitting a partial result. Fatal bridge failures emit one sanitized JSON object with stable `error_code` and return nonzero. Existing archive limits still apply.

## Native restore and UI confirmation

Use official DSH session services, not offline log/database writes. Each import gets a fresh host-generated ID and uses the selected existing local workspace. Verify the result by reading it back before recording a successful duplicate ledger entry. Preserve the original source session as a private inert sidecar. No imported prompt, tool record, link, plugin state, or metadata executes a command or changes credentials, permissions, sandbox policy, or plugin installation.

Native fidelity must be demonstrated for a supported event vocabulary. Unsupported authority/plugin-state events fail closed; an explicitly selected semantic import may retain visible conversation and tool evidence without claiming native fidelity. Never silently drop unsupported native state. Preserve seeded-history boundaries in the source envelope and validate any lineage normalization required for a fresh session.

Preview creates an opaque, expiring owner-bound ticket containing the validated payload and artifact digest. The confirm action consumes that same payload, target workspace and import mode, so changing a source file after preview cannot change what is imported. Deduplicate by core fingerprint. Serialize concurrent imports, and report a created session ID on any failure after creation instead of retrying invisibly or overwriting existing sessions.

## UI direction

Use DSH's native slots, controls, theme variables, typography, locale and overlay behavior. Share shows selected session and file/link choice, redacted scan findings, busy state, generated link/expiry or file download, and retry/copy actions. Import accepts a file or link, shows source/title/message count/fidelity warnings and local workspace, and requires an explicit create-new-session action. Disable duplicate submits, expose keyboard focus and accessible status text, and preserve useful input after recoverable failure.

## Verification and delivery

Use failing regression tests before new behavior. Go tests cover strict bridge input, integrity, secret scanning (including escaped/mixed secrets), private fallback, DSH-to-Codex/Claude and current adapter regressions. Plugin tests use the real pinned Session validation/persistence services and a temporary profile for DSH native/semantic restore, identity, duplicate/concurrent imports, rejection of untrusted authority state, and continuation through a local model fixture without paid requests. UI tests exercise real controls and server boundaries; inspect the installed plugin in an isolated DSH instance. Package checks must prove the installable bundle contains its built entries, locale/assets and a working matching core binary. Update both languages, support scope, CI and bundled binaries before merging. Do not publish npm or install into a real user's DSH profile implicitly.
