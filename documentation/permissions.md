# Permissions

agent-handoff has no user accounts or application roles. Authorization comes from local OS access, explicit CLI flags, and optional Worker upload authentication.

| Resource | Operation | User / local agent | Anonymous provider | Worker operator |
| --- | --- | --- | --- | --- |
| Local Codex/Claude history | Read selected session | Allowed after explicit share request | No access | No access |
| Existing local sessions | Modify/delete | Never allowed | No access | No access |
| New imported session | Append | Allowed only with `import --execute` after preview confirmation | No access | No access |
| Plaintext zip | Create/read | User-controlled; created `0600` | No access unless user sends the file | No access unless user sends the file |
| Link ciphertext | Upload/download | Allowed for the requested share/import | Stores and serves fallback ciphertext | Stores and serves ciphertext for the project or a self-hosted Worker |
| Link decryption key | Possess/use | Anyone holding the full URL | Not present in provider requests | Not present in storage API requests |
| Worker upload | Create share | Anonymous unless `SHARE_UPLOAD_TOKEN` is set | Not applicable | Policy configured by operator |
| Worker cleanup | Delete expired ciphertext | No direct access | Provider-owned retention | Scheduled Worker handler only |

Local scope is derived from OS filesystem permissions and the resolved agent home (`CODEX_HOME`, `CLAUDE_CONFIG_DIR`, or defaults). There is no database row-level security layer. Code-enforced invariants are append-only restore, backup-before-write, duplicate detection, checksum verification, and bounded archive extraction.
