# Roadmap

agent-handoff exists to make switching people, machines, and coding agents feel
like continuing the same task, without turning private conversations into a
cloud account. The roadmap is organized around user outcomes rather than a
fixed feature checklist. It does not promise release dates.

## Current focus (2026-11-19)

- Maintain the existing Codex and Claude Code handoff paths.
- Deliver and validate the experimental [native DSH plugin](plugins/dsh/README.md), pinned to DSH 0.2.2-rc.1 / rc.5, with native share/import UI and safe fresh-session continuation ([#10](https://github.com/DavidDingXu/agent-handoff/issues/10)).
- Defer [OpenCode support (#9)](https://github.com/DavidDingXu/agent-handoff/issues/9), [setup/provider diagnostics (#11)](https://github.com/DavidDingXu/agent-handoff/issues/11), and [richer pre-share preview (#12)](https://github.com/DavidDingXu/agent-handoff/issues/12). These issues are closed as `not_planned` for now and may be reopened when priorities change.

## Product principles

- A receiver gets a new native task they can continue, not a transcript dump.
- The default path works without an account, deployment, or provider token.
- Shared links contain locally encrypted ciphertext; plaintext stays local.
- Imports remain append-only and never rewrite an existing task.
- Configuration is preferred over executable plugins. Runtime extension points
  are introduced only after multiple real integrations prove a stable contract.
- macOS, Linux, and Windows remain first-class targets.

## Maintenance: keep existing handoffs dependable

**Outcome:** enable a new Codex or Claude Code user to install the plugin and
complete a file or encrypted-link handoff without reading implementation docs.

Delivered foundation:

- Ship declarative HTTP provider configuration for users who already have an
  object store, enterprise file platform, or domestic file service.
- Keep the public Worker and anonymous provider pool as zero-configuration,
  best-effort defaults, with a local zip as the final fallback.
- Add a real end-to-end usage capture, a light architecture diagram, and a
  short troubleshooting path to the primary README.
- Simplify the public Worker policy around two operational controls only: blob
  size and expiry cleanup, both managed through deployment configuration.

Success signals:

- Plugin install smoke tests pass for Codex and Claude Code on every release.
- File and link handoffs pass the four-quadrant test matrix on macOS, Linux, and
  Windows.
- An unavailable link service always produces either another valid encrypted
  route or an explicit local zip result.
- A cold-start walkthrough can be completed from the README alone.

## Experimental: DeepSeek Harness integration

**Outcome:** enable users to move a live task among the coding agents they
actually use while preserving each host's native resume experience.

The [native DSH plugin](plugins/dsh/README.md) owns host interaction, native UI and session services; agent-handoff retains bundle validation, neutral transcripts, encryption and transfer. Verify imports and continuation using synthetic sessions and disposable profiles on the pinned runtime. Broader runtime compatibility is follow-up work.

DSH remains in developer preview. Native restoration covers reviewed passive
history only; ordinary logs often need explicit conversation import. Source
authority and queued work are never restored. A cold restart after continuation
is part of acceptance; exporting a log alone does not prove resumability.

Every new agent must satisfy the same acceptance contract:

- Same-agent handoff preserves native events under a fresh identity.
- Cross-agent handoff preserves visible conversation and tool evidence through
  the versioned neutral transcript.
- Import creates one new task, supports preview and duplicate detection, and is
  verified by reopening it through the target host.
- The full N x N matrix runs in CI; three agents mean 9 paths, four mean 16.

The adapter boundary will be extracted only after a third real implementation
proves the common contract. That evidence should shape an adapter
SDK; a speculative generic interface should not shape the integrations.

## Unscheduled candidates: make frequent handoffs clearer

**Outcome:** enable teams to use handoffs during normal development without
uncertainty about what will be shared, how long it will live, or what failed.

These candidates have no current delivery schedule:

- Deferred: [a richer pre-share preview (#12)](https://github.com/DavidDingXu/agent-handoff/issues/12) for message count, images, changed-file evidence,
  detected secrets, estimated upload size, route, and expiry.
- Guided redaction and selective attachment inclusion before the bundle is
  created, while keeping the original task untouched.
- Structured progress and retry reporting that agents can render as native UI,
  with stable machine-readable error codes for automation.
- Deferred: [provider configuration diagnostics (#11)](https://github.com/DavidDingXu/agent-handoff/issues/11) and reusable, reviewed recipes for common
  S3-compatible, enterprise, and regional file services.
- Compatibility fixtures for supported Codex and Claude Code session formats,
  and for additional hosts only after their support is accepted, so host
  upgrades fail in CI before they fail for users.

Success signals:

- A user can identify the shared scope and expiry before confirming an upload.
- Provider/configuration failures identify the failing field or HTTP stage.
- Historical fixture replay covers every supported host version retained by
  the compatibility policy.

## Later: a safe extension ecosystem

**Outcome:** let contributors add agents and organization-specific policy
without weakening the core security and portability guarantees.

- Publish an adapter conformance kit after at least three native adapters prove
  the interface, including fixtures, N x N tests, and host verification hooks.
- Support declarative organization policy profiles for redaction rules,
  retention bounds, and allowed link routes.
- Maintain a reviewed provider recipe catalog. Provider configuration remains
  data, not downloaded executable code.
- Explore signed bundle provenance and optional organization trust policies
  without requiring a central account or making anonymous sharing impossible.

## Explicitly not planned

- Server-side plaintext conversation storage or a mandatory agent-handoff
  account.
- Arbitrary executable file-provider plugins.
- A universal agent adapter abstraction before a third native integration
  demonstrates the common contract.
- Silent telemetry from private coding sessions. Product quality should be
  measured through opt-in issue reports, compatibility fixtures, and CI.

## Contributing to the roadmap

Open a feature request with the user problem, target agent or service, native
session evidence, and an acceptance example. A proposed output is useful, but
the roadmap is prioritized by the user outcome and whether it preserves the
product invariants above.
