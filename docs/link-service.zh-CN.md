# 链接分享

[English](link-service.md) | 简体中文

`agent-handoff share --format link` 把捆绑包变成一条可以随处粘贴的 URL。捆绑包在**你的机器上**用 AES-256-GCM 加密。零配置时先使用项目运营的 Worker，不可用时自动尝试匿名临时文件服务；传 `--endpoint` 时只走你自建的 Cloudflare Worker。

## 威胁模型

- 存储供应商和自建存储 API 无法读取分享的任务：256 位密钥在客户端生成，**只**随 URL fragment 传输（匿名中继链接用 `#h=…`，自建链接用 `#k=…`）。浏览器和 HTTP 客户端不会在请求中发送 fragment。resolver 页面在浏览器中打开后能够读取自身 fragment，因此应使用内置的已审查页面或你信任的 resolver；CLI 导入不会请求 resolver。
- 密文完整性双重保障：解密时 GCM 认证；下载时核对链接 manifest 里记录的 SHA-256。
- 机密性损失仅限于持有**完整链接**（含 fragment）的人。链接本身就是能力凭证 —— 请通过你信任的渠道发送。
- 供应商能看到 IP、上传/下载时间和密文大小；它无法在不被发现的情况下伪造或篡改内容。

## 零配置模式

未配置 endpoint 时，CLI 先把密文上传到项目运营的 `agent-handoff-link.798148655.workers.dev`。它不需要账号或 token，默认 10 分钟失效，每条链接最多下载 10 次；公共池每天最多 800 次分享、每月 2 万次，同时存活密文不超过 512 MiB。额度耗尽时 CLI 会自动切到后备池。

项目服务不可用时，CLI 只加密一次，并发上传到最多两家供应商；候选池包括 [Filebin](https://filebin.net/api)、[tmpfiles.org](https://tmpfiles.org/api)、[Uguu](https://uguu.se/api) 和 [temp.sh](https://temp.sh/)。单家失败不会阻断分享；导入时依次尝试链接里记录的副本，直到大小、SHA-256 和 AES-GCM 校验全部通过。如果全部上传失败，CLI 会保留本地 zip 并返回 `fallback_zip`。

匿名后备链接默认 24 小时，`--ttl` 限制为 60 秒–7 天。各供应商可能采用更短的保留策略，因此某个副本可能早于链接的逻辑有效期消失；CLI 会继续尝试其他副本，并在逻辑到期后拒绝整条链接。这些免费服务都属于尽力而为，限制和可用性可能随时变化；必须保证交付时应使用 zip 或自建 endpoint。

生成的 URL 指向静态 `/r` resolver，并在 `#h=` 中携带紧凑 manifest：

```json
{
  "v": 1,
  "r": [
    { "p": "tmpfiles.org", "u": "https://tmpfiles.org/dl/…/bundle.enc" },
    { "p": "uguu.se", "u": "https://d.uguu.se/….enc" }
  ],
  "k": "base64url AES 密钥",
  "n": "base64url GCM nonce",
  "s": "密文 sha256",
  "b": 48213,
  "e": "2026-08-24T03:20:00Z"
}
```

CLI 只接受受支持供应商精确域名下的 HTTPS URL，并在下载前拒绝重复供应商、过期或超限 manifest；它不会请求 resolver URL。可以用 `AGENT_HANDOFF_RESOLVER` 替换内置解析页，但必须是 HTTPS，并提供同样只在浏览器本地读取 fragment 的静态页面。

## 自建模式

设置 `AGENT_HANDOFF_ENDPOINT` 或传 `--endpoint` 后，CLI 会绕过匿名供应商，使用下面的 Worker 协议。

### 分享生命周期

```
发送方                                    worker（Cloudflare）
──────                                    ──────────────────
1. 构建 zip 捆绑包
2. AES-256-GCM 加密    ── 密文 ────────> POST /v1/shares（multipart）
3. 收到 share_url                        密文存 R2/KV，manifest 存 DO
4. 把 #k=<key> 拼到 share_url            （服务端永远见不到密钥）

接收方
──────
5. GET /v1/shares/:id          → 链接 manifest（JSON）
6. GET /v1/shares/:id/blob     → 密文（校验大小 + sha256）
7. 用 fragment 里的密钥解密      → zip → 走正常导入路径
```

worker 强制的限制：每个 blob 最大 32 MiB（KV 模式 25 MiB）、链接默认有效期 10 分钟（可通过 `ttl_seconds` 请求 60 秒 – 24 小时；发送方 CLI 参数为 `--ttl <秒>`）、每个分享最多 10 次下载、在线字节总量 4 GiB，外加 BudgetGate Durable Object 控制的月度上传/下载配额。KV 模式下 TTL 由 KV 原生强制执行（密文到点自动消失）；R2 模式下通过 `expires_at` 加每小时一次的 cron 清理过期分享。

### HTTP API

| 路由 | 方法 | 用途 |
| --- | --- | --- |
| `/v1/capabilities` | GET | 服务限制，以及上传是否需要 token |
| `/v1/shares` | POST | 上传（multipart 字段：`share_id`、`manifest`、文件 `blob`）→ `201` 返回 `share_url`、`manifest_url`、`expires_at` |
| `/v1/shares/:id` | GET | 链接 manifest JSON |
| `/v1/shares/:id/blob` | GET | 加密捆绑包字节 |
| `/s/:id` | GET | 人类可读的分享页（导入指引） |
| `/s/:id.agent.md` | GET | 智能体交接 markdown |
| `/s/:id.agent.json` | GET | 智能体交接 JSON |
| `/r` | GET | 匿名 `#h=` 链接的静态解析页；不使用存储绑定 |

是否要求上传 bearer token（`Authorization: Bearer …`）取决于部署配置；CLI 从 `--token` 或 `AGENT_HANDOFF_TOKEN` 读取。

### 自建链接 manifest

```json
{
  "schema": "agent-handoff.link.v1",
  "thread": { "id": "0192…", "title": "fix flaky retry test" },
  "bundle": { "url": "https://share.example.com/v1/shares/Wi5x…/blob", "sha256": "…", "bytes": 48213 },
  "crypto": { "alg": "AES-256-GCM", "nonce": "…", "key_ref": "url-fragment:k" },
  "ttl_seconds": 600,
  "expires_at": "2026-08-21T09:51:12Z"
}
```

客户端的 `Validate` 会强制校验 schema、算法、nonce 存在性与 `key_ref` —— manifest 本身永远不含密钥。

### 部署你自己的实例

worker 是单文件（`deploy/worker/src/index.js`，无构建步骤），使用一个 Durable Object 加一个 blob 存储：**Workers KV**（无需绑卡；免费额度 1 GB 存储 / 每天 1k 写 / 100k 读 —— 对 10 分钟链接绰绰有余）或 **R2**（需要绑卡；blob 最大 32 MiB）。运行时自动探测绑定的是哪个。

方案 A —— KV，不用绑卡：

```sh
cd deploy/worker
npm ci
npx wrangler login
cp wrangler.toml.example wrangler.toml
npx wrangler kv namespace create SHARE_KV   # 把返回的 id 填进 wrangler.toml
npx wrangler secret put SHARE_UPLOAD_TOKEN  # 可选：要求上传 token
npx wrangler deploy
```

如果该 Cloudflare 账号从未使用过 Workers，执行 `wrangler login` 后要先在 Cloudflare Dashboard 打开一次 **Workers & Pages**，创建或确认该账号的 `workers.dev` 子域名。这是 Dashboard 中的一次性账号初始化；未完成时，`wrangler deploy` 会报 API 错误 `10063`。可以运行 `npx wrangler whoami`，确认 Dashboard 与 Wrangler 使用的是同一个账号。

方案 B —— R2，需绑卡：

```sh
npx wrangler r2 bucket create agent-handoff   # 把 wrangler.toml 里的 KV 块换成 R2 块
npx wrangler deploy
```

默认地址是 `<worker-name>.<your-subdomain>.workers.dev`，不需要自有域名。然后让客户端指向你的实例：

```sh
export AGENT_HANDOFF_ENDPOINT=https://agent-handoff-link.<your-subdomain>.workers.dev
export AGENT_HANDOFF_TOKEN=<与-SHARE_UPLOAD_TOKEN-相同的值>
curl -fsS "$AGENT_HANDOFF_ENDPOINT/v1/capabilities"
agent-handoff share --format link
```

如果没有设置可选的 Worker secret，就不要配置 `AGENT_HANDOFF_TOKEN`。`wrangler.toml` 含账号专属 namespace id，应只保留在本机；仓库已将它加入 Git 忽略列表。

默认配置按团队规模 + 免费额度设计；公开实例请先调整 `index.js` 里的 `LIMITS`。这个 worker 刻意保持简单可审计（约 600 行，零依赖）。

## CLI 集成

- 未配置 endpoint 时，CLI 使用项目运营的 Worker 并生成 `#k=` 链接；不可用时再由匿名供应商池生成 `#h=` 链接。
- `--endpoint URL` / `AGENT_HANDOFF_ENDPOINT` —— 使用自建 worker 并生成 `#k=` 链接。
- `--token TOKEN` / `AGENT_HANDOFF_TOKEN` —— 上传 bearer token。
- `AGENT_HANDOFF_RESOLVER` —— 匿名链接的可选 HTTPS 解析页。
- 当前模式下所有上传都失败时，CLI 会返回本地 zip（`status: "fallback_zip"`），分享不会凭空消失。
- `import <url>` 接受完整链接（含 fragment），下载、校验、解密后走正常导入路径 —— 包括干跑/`--execute` 区分与重复检测。
