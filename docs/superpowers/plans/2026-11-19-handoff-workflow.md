# Task handoff workflow

The user authorized all four capabilities with user experience first: local Agent switching, an automatic brief, browser-readable encrypted shares, and optional code changes. This plan is the implementation record, not an additional approval gate.

## User flow and contracts

- Switching exports the explicitly selected, already persisted task and creates a new native target task without asking either model to respond. An unknown current identity opens a local picker; it never silently selects the newest task. Starting another model turn remains the user's action.
- Every new bundle includes a bounded, deterministic brief quoting the first request, latest request, latest response and recent tool evidence with source entry indices. Users can add notes. No inferred completion claims or mandatory model/API dependency.
- Browser shares decrypt and verify locally, then show the brief, conversation, expandable tool evidence and optional code inventory. Keys stay in fragments; imported content is text, never HTML or remote resources. Hosted and anonymous relay links share the same reader.
- Code is off by default. Export previews eligible changed text files, sizes and exclusions; new files require explicit selection. The bundle records a Git base commit, a text patch and selected new files, with bounded size. Receiving users can keep just the conversation, or explicitly apply the reviewed code into a new independent Git worktree. Existing tasks and working directories remain intact. Missing bases and conflicts give actionable recovery instead of a partial silent import.
- Optional v2 entries: `agent/brief.json`, `agent/brief.md`, `code/manifest.json`, `code/changes.patch`, `code/files/*`. All are covered by checksums. Old v1/v2 packages still import. New optional payloads are validated before side effects; import fingerprints distinguish different code and user notes.

## Implementation

1. Core payload: deterministic brief and safe code inspection/capture/application, bundle validation and tests.
2. Browser reader: shared hosted/relay reader, local bounded ZIP/DEFLATE and integrity validation, accessible bilingual UI and adverse tests.
3. Local switch: exact identity, local task picker/catalog, new native task, resume command and supported host entry helpers, with synthetic native-state tests.
4. Integration: CLI flags and bridge commands; DSH brief/notes, optional file selection, switch target action, receiving preview and explicit isolated code application. Preserve one searchable task dropdown and separate import/export views.
5. Verify: Go race/vet, Worker tests, DSH component + host + both SDK checks, all nine source/target paths with synthetic homes, no-model switch path, fresh installed DSH UI and real browser reader. Reuse checks until a relevant change invalidates them. Record exact native continuation limits.
6. Update concise English/Chinese installation and workflow docs, skill and generated bundle guidance. Submit a PR, wait for required CI, merge and publish an immutable release/npm package from green main under existing authorization. Never rebuild an already published version.

## Ownership

Core payload, browser reader and local switch are independent parallel tasks after the shared contract. The primary agent owns CLI/bridge integration, DSH host/client, documentation, final review and release. All tests use synthetic sessions and temporary homes.
