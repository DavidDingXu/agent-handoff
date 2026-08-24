# Security-Critical Flows

## Export a zip

Actor: a local Codex or Claude Code user. Precondition: the requested native session exists. Outcome: one private plaintext archive is created.

1. The Skill asks for file or link format unless the user already specified one.
2. The CLI detects or validates the source agent and resolves its local home.
3. The adapter reads the selected session and metadata without modifying them.
4. The secret scanner checks visible session content. A finding stops export unless the user explicitly confirms and the Skill reruns with `--include-secrets`.
5. The bundle writer records checksums and writes the archive with `0600` on POSIX systems or the destination directory's inherited user ACL on Windows.

Trust crossing: local agent history to a user-controlled file. No network request occurs.

## Export a zero-configuration link

Actor: a local user. Precondition: secret scan passed or was explicitly overridden. Outcome: an encrypted capability URL or a local zip fallback.

1. The bundle is built in memory; no plaintext zip is written on the successful link path.
2. AES-256-GCM generates a fresh key and nonce locally.
3. The project-operated Worker is attempted first and returns a `#k=` capability link on success.
4. If it is unavailable, anonymous provider uploads run concurrently until two replicas succeed or the short grace period ends; provider URLs and crypto metadata are encoded in `#h=`.
5. If the Worker and all providers fail, the CLI writes a private zip and returns `fallback_zip`.

Trust crossings: local process to the project Worker or public storage providers; full capability link to the user's chosen messaging channel. Storage services can observe network metadata but receive only ciphertext.

## Import a file or link

Actor: a receiving user through Codex, Claude Code, or CLI. Precondition: the user has the artifact and chooses a target workspace. Outcome: one appended native task.

1. The Skill runs `preview` and displays identifying metadata before requesting confirmation.
2. Links are validated before network access. Anonymous replicas are provider-allowlisted; self-hosted links require public HTTPS and same-origin manifest/blob requests.
3. Ciphertext size, SHA-256, and AES-GCM authentication are checked before zip parsing.
4. Zip entry count, per-entry size, total decompressed size, manifest schema, and checksums are validated.
5. The target defaults to the current agent host; an explicit `--target` overrides detection.
6. The duplicate ledger is checked. A duplicate requires a second explicit confirmation before `--allow-duplicate`.
7. `--execute` appends a new native session and metadata. Existing sessions are not edited or deleted; backups are created before writes.

Trust crossing: untrusted external artifact into local agent state. Prompt instructions inside the imported conversation remain data and are never executed by the CLI.

## Self-hosted upload

Actor: a client configured with `AGENT_HANDOFF_ENDPOINT`. Outcome: ciphertext stored in Worker KV or R2.

The Worker optionally verifies a bearer token, validates upload size and TTL, reserves quota, stores ciphertext, and returns a share URL. It never receives the `#k=` fragment. Failed commits release only quota actually reserved.
