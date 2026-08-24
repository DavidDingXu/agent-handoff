# Roadmap

agent-handoff exists to make switching people, machines, and coding agents feel
like continuing the same task, without turning private conversations into a
cloud account. The roadmap is organized around user outcomes rather than a
fixed feature checklist. Dates are planning windows, not release promises.

## Product principles

- A receiver gets a new native task they can continue, not a transcript dump.
- The default path works without an account, deployment, or provider token.
- Shared links contain locally encrypted ciphertext; plaintext stays local.
- Imports remain append-only and never rewrite an existing task.
- Configuration is preferred over executable plugins. Runtime extension points
  are introduced only after multiple real integrations prove a stable contract.
- macOS, Linux, and Windows remain first-class targets.

## Now: make the first handoff dependable (2026 Q3)

**Outcome:** enable a new Codex or Claude Code user to install the plugin and
complete a file or encrypted-link handoff without reading implementation docs.

Planned work:

- Ship declarative HTTP provider configuration for users who already have an
  object store, enterprise file platform, or domestic file service.
- Keep the public Worker and anonymous provider pool as zero-configuration,
  best-effort defaults, with a local zip as the final fallback.
- Add a real end-to-end usage capture, a light architecture diagram, and a
  short troubleshooting path to the primary README.
- Add [`doctor`, `config validate`, and provider connectivity checks](https://github.com/DavidDingXu/agent-handoff/issues/11) so setup
  failures produce an actionable diagnosis instead of a generic upload error.
- Simplify the public Worker policy around two operational controls only: blob
  size and expiry cleanup, both managed through deployment configuration.

Success signals:

- Plugin install smoke tests pass for Codex and Claude Code on every release.
- File and link handoffs pass the four-quadrant test matrix on macOS, Linux, and
  Windows.
- An unavailable link service always produces either another valid encrypted
  route or an explicit local zip result.
- A cold-start walkthrough can be completed from the README alone.

## Next: continue tasks across more agents (2026 Q4)

**Outcome:** enable users to move a live task among the coding agents they
actually use while preserving each host's native resume experience.

Priority integrations:

1. **[OpenCode](https://github.com/anomalyco/opencode)** ([#9](https://github.com/DavidDingXu/agent-handoff/issues/9)): add native session discovery, export, preview, restore, and
   verification. Use its supported session/export contracts where possible and
   keep direct storage access schema-aware and read-only during export.
2. **[DeepSeek Harness (`dsh`)](https://github.com/deepseek-ai/deepseek-harness)** ([#10](https://github.com/DavidDingXu/agent-handoff/issues/10)): prototype against its append-only session event
   stream, then promote support after its developer-preview storage and session
   APIs are stable enough for safe native restore.
3. Evaluate Gemini CLI, GitHub Copilot CLI, and other agents from real user
   requests. Model-provider compatibility alone is not sufficient; the host
   must expose a durable session that can be resumed and verified.

Every new agent must satisfy the same acceptance contract:

- Same-agent handoff preserves native events under a fresh identity.
- Cross-agent handoff preserves visible conversation and tool evidence through
  the versioned neutral transcript.
- Import creates one new task, supports preview and duplicate detection, and is
  verified by reopening it through the target host.
- The full N x N matrix runs in CI; three agents mean 9 paths, four mean 16.

The adapter boundary will be extracted only after OpenCode or DeepSeek Harness
provides the third real implementation. That evidence should shape an adapter
SDK; a speculative generic interface should not shape the integrations.

## Then: make frequent handoffs faster and clearer (2027 Q1)

**Outcome:** enable teams to use handoffs during normal development without
uncertainty about what will be shared, how long it will live, or what failed.

Candidate work:

- [A richer pre-share preview](https://github.com/DavidDingXu/agent-handoff/issues/12) for message count, images, changed-file evidence,
  detected secrets, estimated upload size, route, and expiry.
- Guided redaction and selective attachment inclusion before the bundle is
  created, while keeping the original task untouched.
- Structured progress and retry reporting that agents can render as native UI,
  with stable machine-readable error codes for automation.
- Provider configuration diagnostics and reusable, reviewed recipes for common
  S3-compatible, enterprise, and regional file services.
- Compatibility fixtures for historical Codex, Claude Code, OpenCode, and DSH
  session formats so host upgrades fail in CI before they fail for users.

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
