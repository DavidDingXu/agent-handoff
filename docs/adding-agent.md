# Adding another agent

[English] | [简体中文](adding-agent.zh-CN.md)

agent-handoff currently supports Codex and Claude Code as both sources and
targets. Adding an agent is an adapter change, not a new bundle format: bundle
v2 already stores native sessions below an agent-named directory and carries a
neutral transcript for cross-agent imports.

This guide uses `acme` as the example agent name.

## Compatibility contract

An adapter must support two different fidelity levels:

| Direction | Required result |
| --- | --- |
| acme -> acme | Preserve the native session structure and content, but assign a fresh task identity and target path. |
| acme -> another agent | Convert visible conversation, tool evidence, images, and metadata into the neutral transcript. |
| another agent -> acme | Restore the neutral transcript as a queryable native Acme task. |

Do not copy source-specific internal fields into another agent's state. Do not
claim native fidelity for a semantic cross-agent conversion.

Adding an agent does not require a bundle version bump when it only adds a new
per-agent native session directory. Changing the neutral schema, manifest
meaning, integrity rules, or another incompatible contract does require a
format decision and a backward-compatible reader.

## 1. Register the agent name

Update `internal/bundle/format.go`:

1. Add an `AgentAcme` constant.
2. Add it to `SupportedAgents`.
3. Ensure `IsSupportedAgent` accepts it.

Keep the canonical name lowercase and stable. It becomes part of manifests,
JSON output, native-session paths inside bundles, flags, and import ledgers.

## 2. Implement the native adapter

Create `internal/acme/`. Follow the Codex and Claude packages, but model the
actual Acme storage contract rather than copying either implementation.

The package must provide enough behavior for the CLI to:

- resolve the Acme home from an explicit flag, an Acme-specific environment
  variable, and the platform default;
- discover the current session and resolve an explicit session ID;
- load the raw native session plus title, working directory, timestamps, model,
  git metadata, and referenced images where available;
- convert the raw session to `neutral.Transcript` without executing or fetching
  session content;
- restore a same-agent native session with a fresh identity;
- restore a neutral transcript as a native Acme task for cross-agent imports;
- update only the minimum append-only index/state needed for the task to appear;
- verify that the imported task is queryable through Acme's real state.

Every write path must create the same class of backups as the existing
adapters. It must not modify an existing task to simulate an import.

## 3. Wire the CLI

Update the explicit routing points in `internal/cli`:

- `cli.go`: `--source`, `--target`, usage text, source detection, and Acme's
  current-host environment markers;
- `share.go`: export routing, native-session collection, neutral conversion,
  and secret scanning when Acme uses a different event shape;
- `import.go`: target-home resolution, same-agent detection, and native restore;
- `preview.go`, `detail.go`, and `verify.go`: any source-specific display or
  verification branches.

Keep explicit switches while there are only a few adapters. Extract a shared
adapter registry only when the third implementation demonstrates a stable
common interface and the registry removes meaningful duplication.

Host detection is a convenience, not a hidden requirement. Explicit
`--source acme` and `--target acme` must always work and take precedence.

## 4. Update the agent-facing workflow

Update `skills/agent-handoff/SKILL.md` and plugin metadata where necessary:

- list Acme in the supported directions;
- document how the skill detects that it is running inside Acme;
- map interactive questions to Acme's native question tool if it has one;
- add platform-specific binary invocation only if Acme resolves plugin files
  differently;
- preserve the existing preview-before-import and explicit-confirmation flow.

Do not make the skill parse or rewrite session storage itself. Native state
knowledge belongs in the Go adapter.

## 5. Test the full matrix

Adapter unit tests must use synthetic sessions under temporary homes. Add
focused coverage for:

- home and current-session resolution on macOS, Linux, and Windows paths;
- raw session parsing, metadata, images, and malformed records;
- same-agent identity rewriting and native-event fidelity;
- cross-agent neutral conversion in both directions;
- append-only index/state updates, backups, duplicate handling, and verify;
- secret scanning and untrusted-content behavior;
- explicit source/target overrides and current-host auto-detection;
- bundle write/read round trips with the new native session directory.

Extend `internal/integration_test` from the current four directions to the
relevant N x N matrix. At minimum, test `acme -> acme`, every existing source
into Acme, and Acme into every existing target. If Acme has a plugin host, add a
real isolated-home plugin-install smoke test to CI.

Run:

```sh
go test ./... -count=1 -race
go vet ./...
make cross VERSION=v<version>

cd deploy/worker
npm ci
npm test
```

The pull request is complete only when the platform CI matrix passes and the
README, Chinese documentation, command help, skill behavior, and changelog all
describe the same support boundary.

