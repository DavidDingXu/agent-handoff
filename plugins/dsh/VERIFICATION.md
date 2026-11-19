# DSH verification receipt

Validated 2026-11-19 against `@deepseek-ai/*` **0.2.2-rc.5**, upstream
`2e39e70496aa02986ca6a820c5d497e816b69d7e`, Cordis 4.1.0, Node 24.14.1.
All fixtures use synthetic conversations and disposable storage/profiles.

## Automated checks

- `go test ./... -count=1 -race`: passed, including existing adapters and the
  actual built-CLI DSH → Codex/Claude regression. `go vet ./...`: passed.
- `make cross VERSION=v0.4.6`: built all five packaged cores.
- `npm run check` in this directory: typecheck, **69 tests / 7 files**, and both
  Host/Client builds passed.
- `npm run package` and `npm run check:package`: passed; package contains both
  native entries, declarations, locale, icon, licenses and five matching cores.
- The CI DSH job repeats typecheck/tests/build/package on Ubuntu, macOS and
  Windows with the pinned Node and lockfile. CI results remain authoritative
  for those runners; this local receipt records macOS execution.

## Transfer coverage

| Source → destination | Evidence |
| --- | --- |
| Codex → Codex | Existing Go adapter and native-client regressions |
| Codex → Claude | Existing Go adapter and native-client regressions |
| Claude → Codex | Existing Go adapter and native-client regressions |
| Claude → Claude | Existing Go adapter and native-client regressions |
| DSH → Codex | Actual built CLI, temporary destination, preserved existing task and raw audit |
| DSH → Claude | Actual built CLI, temporary destination, preserved existing task and raw audit |
| Codex → DSH | Real rc5 JSONL/controller/AgentLoop, conversation import, two cold reopens and local continuation |
| Claude → DSH | Real rc5 JSONL/controller/AgentLoop, conversation import, two cold reopens and local continuation |
| DSH → DSH | Supported passive native fixture plus explicit ordinary-log conversation fallback, cold reopens and local continuation |

The local mock model receives historical conversation/tool evidence. Imported
tool execution count remains zero. No external model or storage credentials are
used. SDK runtime tests instantiate the actual public controller implementation;
the separate installed-profile check exercises the full Host/API composition.

## Installed native UI and Host

Installed the generated `.tgz` using DSH's own `dsh plugin ... add` command into
a disposable Web profile. Its DSH home, shared agent home, documents directory,
workspace and sessions are isolated. The installed plugin provides:

- Sidebar Import/Task handoff entries while preserving New session.
- The official workspace picker, source selection and verified content preview.
- An explicit native/conversation choice followed by create-new-task confirmation.
- A new visible native session with the original synthetic conversation, an idle
  composer and a working Share task header action.
- The official Share modal, plaintext-file warning, ZIP creation and browser save.
  The saved file was reread by the Go bridge and passed checksum verification.

The final packaged build was reinstalled and booted. Its authenticated API check
confirmed unauthorized HTTP 401, native import, ordinary-log conversation-only
import, durable duplicate detection after restart, concurrent imported/duplicate
results sharing one fresh ID, summary count, share and ZIP download.

Screenshot: [native Share UI](../../documentation/dsh-native-share.jpg).

## Regression and review boundaries

Tests cover malformed/truncated/unknown/checksum/size failures, decoded secret
scan, explicit secret-snapshot confirmation, immutable previews, workspace/mode
binding, expiry, recovery IDs, dead/live writer locks, request/body/subprocess
cancellation and plugin unload. UI tests use actual official controls and the
real Cordis SlotRegistry, including desktop fetch-carrier and URL-prefix handling.

Real lifecycle testing caught and fixed the SDK's protected first system-head
requirement before continuation. Native relationship validation rejects missing
tool results before persistence. Human transcript conversion excludes model-only
surface replacement summaries. Independent review findings were fixed and
covered by regression tests.

This is experimental support for one exact DSH preview version. Ordinary logs
usually contain authority/runtime events, so native import is intentionally
limited to reviewed passive history; conversation import requires an explicit
choice. Full media, provider replay, presets, permissions and queued work are
excluded. No real Electron, physical Windows desktop, or paid model acceptance
run is claimed. The npm package has not been published and no real user DSH
profile was installed or changed.
