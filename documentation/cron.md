# Scheduled Work

| Job | Schedule | Entry point | Secrets / bindings | Limits | Retry and observability |
| --- | --- | --- | --- | --- | --- |
| Expired share cleanup | Hourly (`0 * * * *`) | Worker `scheduled()` handler | `SHARE_BUCKET` or `SHARE_KV`, `SHARE_BUDGET_GATE` | Paginated storage scan; releases live-byte quota | Cloudflare Worker cron logs; next hourly run retries remaining records |

KV expiration is native and does not depend on the cron for blob deletion. R2 uses `expires_at` metadata plus scheduled cleanup. Cleanup is idempotent: deleting an absent object is harmless, and quota release is tied to stored share metadata so repeated scans do not continually subtract live bytes.

Operators should verify the cron trigger after deployment and inspect Worker logs after storage-binding changes. A failed run does not make expired links importable because the API also checks logical expiry; it only delays physical ciphertext deletion.
