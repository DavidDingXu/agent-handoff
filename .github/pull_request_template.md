## What changed

Describe the user-visible behavior and why this is the smallest appropriate change.

## Verification

- [ ] `go test ./... -count=1`
- [ ] `go vet ./...`
- [ ] `npm test` in `deploy/worker` when Worker behavior changes
- [ ] Codex and Claude Code behavior considered
- [ ] macOS, Windows, and Linux impact considered
- [ ] No real transcripts, share links, credentials, or account-specific configuration committed

## Security boundary

Describe any change to local agent state, bundle contents, external requests, encryption, or secret handling. Write `None` when it does not apply.
