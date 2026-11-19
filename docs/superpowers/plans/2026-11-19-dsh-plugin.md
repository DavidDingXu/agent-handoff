# DSH native plugin implementation plan

**Goal:** deliver an installable DSH plugin with native share/import UI, existing Go transfer core, and verified new-session continuation.

**Architecture:** a small TypeScript DSH bundle owns host services and UI. An explicit Go `bridge share/read` protocol owns portable data and transport. Keep the current Codex/Claude filesystem adapters and avoid a generic runtime adapter framework.

**Tech stack:** Go 1.26; Node >=24.9.0; TypeScript; DSH SDK/runtime 0.2.2-rc.1 and rc.5; native DSH Client/Host extension APIs.

**Spec:** `docs/superpowers/specs/2026-11-19-dsh-plugin-design.md`. The maintainer already instructed execution and added UI to the first-version scope; proceed without another plan-approval round.

## Global constraints

- Use synthetic sessions and isolated profiles for automated tests. The maintainer
  subsequently authorized installation and synthetic-only acceptance in their real
  Desktop profile; do not inspect or overwrite existing task contents.
- Existing tasks and user configuration are never overwritten; imported data grants no authority.
- Keep macOS/Linux/Windows support, existing JSON contracts and bilingual docs.
- SDK peers remain external; no duplicate DSH runtime in the plugin.
- Bridge schemas and limits are fixed in the spec and shared across Go/TS tests.
- UI must be part of the real installable DSH bundle.

## Review focus

- Malformed, oversized, tampered and mixed-secret inputs fail without partial success or leaking raw diagnostics.
- Native plugin-state/authority events cannot restore permissions or silently lose fidelity.
- Confirmation consumes the exact previewed artifact and workspace; duplicate/concurrent actions create at most one task.
- Partial persistence failures report recovery state and never overwrite a pre-existing session.
- Packaged built entries and binaries work independently of development directories on all supported platforms.

## Task 1: Go bridge and DSH bundle source

Files: `internal/cli/bridge.go`, `bridge_test.go`, CLI routing/share transport/import/preview, bundle format and focused validation tests, relevant integration tests.

- [x] Write and run failing strict-input, secret-scan, verified-read and DSH-source round-trip tests.
- [x] Implement `bridge share --input FILE|-` and `bridge read FILE|URL` with the spec's exact wire result schemas and fields.
- [x] Reuse existing share transport/fallback with a minimal extraction; keep local adapter selection restricted to Codex/Claude.
- [x] Verify DSH source imports into temporary Codex and Claude homes, integrity-before-success, native audit and no command execution.
- [x] Run Go race tests/vet and record changed interfaces plus evidence.

## Task 2: Pinned DSH Host plugin and real runtime tests

Files: `plugins/dsh/src/{index,host,session,bridge,types}.ts`, host/conversion/runtime tests, pinned package/build configuration.

Consumes the Task 1 bridge schemas. Produces a typed owner-only service for share, preview, confirm/import, workspace listing and task recovery, plus types consumed by the Client.

- [x] Pin and reuse/download only required SDK dependencies after adding Git ignores; verify installed rc.1 software through disposable test overlays.
- [x] Write failing tests against real pinned Session validators and disposable persistence before mapping/restoring events.
- [x] Implement selected-session export, inert neutral tool evidence, supported native restore, fresh identity, private sidecar, owner-bound preview tickets and duplicate ledger.
- [x] Wire core invocation via argv/stdin with bounded output, cancellation and sanitized errors.
- [x] Verify DSH->DSH and Codex/Claude->DSH plus a local-fixture continuation without model credentials.

## Task 3: Native Client UI and installable package

Files: `plugins/dsh/src/client.*`, native slot/overlay registration, locale/icon/build/package files and UI tests.

Consumes typed Host service only. Uses real DSH controls and the native session/workspace context; no standalone HTML app.

- [x] Write failing share/import interaction tests including busy, retry, blocked secrets, preview confirmation and duplicate submit cases.
- [x] Add a selected-session share shortcut and unified Export/Import panel with source selection, file/link choice, previews, workspace selector, progress, errors, copy/download and new-session navigation.
- [x] Build Host/Client artifacts with runtime peers external and check package contents and matching core binaries.
- [x] Install into a disposable DSH profile, then perform the explicitly requested
  real Desktop installation and three-source synthetic acceptance; retain the
  final screenshot/test receipt, not intermediate render caches.

## Task 4: Integrate, review and ship through PR

- [x] Review security boundaries and the complete nine-direction matrix; fix any findings.
- [x] Update bilingual support/install docs, #10 status, changelog, CI plugin checks and bundled binaries consistently.
- [x] Run required repository and plugin checks once on the final relevant code and package.
- [ ] Commit/push, create PR, fix CI, merge after protected checks pass under existing maintainer authorization.
- [ ] Preserve the installable final artifact and source; clean task-owned environments, failed artifacts and isolated test profiles after validation.
