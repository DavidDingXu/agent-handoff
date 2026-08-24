# Verification Map

## Existing coverage

| Use case | Rule and negative case | Evidence | Status |
| --- | --- | --- | --- |
| Four-direction handoff | Same-agent restores raw sessions; cross-agent synthesizes from neutral transcript | `internal/integration_test`, agent adapter tests | Existing integration tests |
| Current-host import | No `--target` uses Codex/Claude host markers; explicit target wins | `internal/cli/cli_test.go` | Existing unit tests |
| Secret gate | Supported patterns block export and hints do not leak values | `internal/safety/scan_test.go` | Existing unit tests |
| Bundle integrity | Tampered checksums, malformed manifests, entry bombs, and decompression limits fail before writes | `internal/bundle/zip_test.go` | Existing unit tests |
| Link confidentiality/integrity | AES-GCM round trip, wrong key, checksum mismatch, URL fragment parsing | `internal/link/link_test.go` | Existing unit tests |
| Anonymous provider failover | Failed/corrupt replicas fall through; unsafe hosts and expired links fail | `internal/link/relay_test.go` | Existing unit tests |
| Link plaintext handling | In-memory zip path and private fallback permissions | `internal/bundle/zip_test.go`, `internal/cli/share_test.go` | Existing unit tests |
| Import safety | Duplicate ledger, backups, append-only state, and verification | adapter, ledger, integration tests | Existing unit/integration tests |
| Worker quotas and cleanup | Commit-time limits, rollover, download counts, expiry cleanup | `deploy/worker/src/index.test.js` | Existing Node tests |
| Public provider availability | At least one anonymous provider completes an encrypted round trip | Scheduled `provider-health.yml` guarded live test | Existing operational test |
| Plugin packaging | Fresh Codex and Claude Code homes install the local marketplace at the manifest version | CI plugin smoke job | Existing integration test |
| Dependency vulnerabilities | Go vulnerability and npm audit checks | Release audit; Dependabot | Manual release check plus automation |

CI requires Go vet, race-enabled tests and builds on Ubuntu, macOS, and Windows; golangci-lint; Worker tests; and CodeQL analysis.

## Proposed tests

| Use case | Expected behavior | Type |
| --- | --- | --- |
| Real Codex and Claude releases | Export/import remains compatible with the latest stable native storage schemas | Guarded live matrix |
| GitHub plugin install | Fresh Codex and Claude config directories install the tagged marketplace and load the Skill | Post-release smoke test |
| Release installers | macOS/Linux installer downloads the tagged asset and verifies checksum; Windows archive launches | Release smoke test on each OS |
| macOS/Windows trust prompts | Document actual unsigned-binary first-run behavior | Manual release review |

## Gaps

| Gap | Exposure | Priority |
| --- | --- | --- |
| Native agent storage formats can change without notice | Imported tasks may not appear or resume | High |
| Anonymous provider APIs have no contract controlled by this project | Link creation can fall back to zip | High, operational |
| Prebuilt binaries are not signed or notarized | Installation warnings and reduced trust | Medium |
| Resolver monitoring detects outages but does not provide an SLA | Browser helper page can be unavailable while CLI import still works | Medium |
| Linux ARM and Windows are CI-built but not exercised against real desktop agent installations | Platform-specific native-state drift | Medium |
