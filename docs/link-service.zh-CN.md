# 链接分享

[English](link-service.md) | 简体中文

`agent-handoff share --format link` 把捆绑包变成一条可以随处粘贴的 URL。捆绑包在**你的机器上**用 AES-256-GCM 加密。零配置时先使用项目运营的服务，不可用时自动尝试匿名临时文件服务。核心不绑定 Cloudflare：用户既可以指定任意兼容 HTTP endpoint，也可以通过声明式 provider 配置接入已有文件服务。

## 威胁模型

- 存储供应商和自建存储 API 无法读取分享的任务：256 位密钥在客户端生成，**只**随 URL fragment 传输（匿名中继链接用 `#h=…`，自建链接用 `#k=…`）。浏览器和 HTTP 客户端不会在请求中发送 fragment。resolver 页面在浏览器中打开后能够读取自身 fragment，因此应使用内置的已审查页面或你信任的 resolver；CLI 导入不会请求 resolver。
- 密文完整性双重保障：解密时 GCM 认证；下载时核对链接 manifest 里记录的 SHA-256。
- 机密性损失仅限于持有**完整链接**（含 fragment）的人。链接本身就是能力凭证 —— 请通过你信任的渠道发送。
- 供应商能看到 IP、上传/下载时间和密文大小；它无法在不被发现的情况下伪造或篡改内容。

## 零配置模式

未配置 endpoint 时，CLI 先把密文上传到项目可选运营的 `agent-handoff-link.798148655.workers.dev`。它不需要账号或 token，默认 10 分钟失效。Worker 只执行配置中的保留时间和密文大小限制，账户与平台额度直接由 Cloudflare 自身负责；任何失败都会让 CLI 自动切到后备池。

项目服务不可用时，CLI 只加密一次，并发上传到最多两家供应商；候选池包括 [Filebin](https://filebin.net/api)、[tmpfiles.org](https://tmpfiles.org/api)、[Uguu](https://uguu.se/api) 和 [temp.sh](https://temp.sh/)。单家失败不会阻断分享；导入时依次尝试链接里记录的副本，直到大小、SHA-256 和 AES-GCM 校验全部通过。如果全部上传失败，CLI 会保留本地 zip 并返回 `fallback_zip`。

匿名后备链接默认 24 小时，`--ttl` 限制为 60 秒–7 天。各供应商可能采用更短的保留策略，因此某个副本可能早于链接的逻辑有效期消失；CLI 会继续尝试其他副本，并在逻辑到期后拒绝整条链接。这些免费服务都属于尽力而为，限制和可用性可能随时变化；必须保证交付时应使用 zip 或自建 endpoint。

### 内置路由配置

发布版 CLI 自带下面这条零配置路由。它属于产品默认配置，不是自定义 provider 必须遵守的厂商绑定：

1. 先尝试项目运营的兼容 endpoint。
2. 默认 endpoint 失败时，并发启动四家匿名服务的上传。
3. 最多记录两个成功副本；有一个有效副本就可以返回链接。
4. 全部失败时返回本地 zip。

显式传入 `--endpoint` 或 `AGENT_HANDOFF_ENDPOINT` 时，只使用用户指定的兼容 hosted service，不会把数据再发给匿名 provider。存在 `config.json` 时，则只使用下文的声明式 provider，同样替换本次分享的整条内置路由。

| 内置匿名 provider | 上传形式 | 下载 URL 处理 | 本地账号/token |
| --- | --- | --- | --- |
| Filebin | 直接上传密文字节 | 使用生成的对象 URL | 不需要 |
| tmpfiles.org | multipart 字段 `file` | 从 JSON 取 URL 并规范成 `/dl/...` | 不需要 |
| Uguu | multipart 字段 `files[]` | 读取 JSON 文件列表里的第一个 URL | 不需要 |
| temp.sh | multipart 字段 `file` | 读取纯文本 URL | 不需要 |

这些内置 adapter 只用于消化公共 API 的差异，不会向声明式 schema 注入厂商字段。实际保留时间和可用性由外部服务决定，可能独立于 CLI 版本发生变化。

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

CLI 对内置供应商只接受精确受支持域名；对用户配置的 provider 只接受公共 HTTPS URL。下载前会拒绝非法 provider 标识、重复供应商、过期或超限 manifest；它不会请求 resolver URL。可以用 `AGENT_HANDOFF_RESOLVER` 替换内置解析页，但必须是 HTTPS，并提供同样只在浏览器本地读取 fragment 的静态页面。

## 自定义 Provider

已有国内对象存储、企业文件平台或普通文件上传 API 时，可以用一个声明式 JSON 文件接入，不执行第三方程序。默认配置路径：

- macOS：`~/Library/Application Support/agent-handoff/config.json`
- Linux：`${XDG_CONFIG_HOME:-~/.config}/agent-handoff/config.json`
- Windows：`%AppData%\agent-handoff\config.json`

也可以用 `--config <file>` 为单次分享指定其他配置。multipart 示例：

```json
{
  "providers": [{
    "name": "my-service",
    "upload_url": "https://files.example.com/api/upload",
    "upload_type": "multipart",
    "file_field": "file",
    "headers": { "Authorization": "Bearer ${MY_FILE_TOKEN}" },
    "form_fields": { "expire": "{ttl_seconds}" },
    "response_type": "json",
    "url_json_pointer": "/data/url"
  }]
}
```

直接上传二进制并从纯文本响应读取 URL：

```json
{
  "providers": [{
    "name": "raw-store",
    "upload_url": "https://files.example.com/upload/{filename}",
    "upload_type": "raw",
    "response_type": "text"
  }]
}
```

schema 只包含通用 HTTP 概念：

| 字段 | 是否必填 | 合同 |
| --- | --- | --- |
| `name` | 是 | 稳定的小写 provider ID，只能使用字母、数字、`.`、`_`、`-` |
| `upload_url` | 是 | 公共 HTTPS 上传 URL，可以使用模板变量 |
| `upload_type` | 是 | `multipart` 或 `raw`，两者都使用 HTTP `POST` |
| `file_field` | 仅 multipart | 接收加密文件的 multipart 字段名 |
| `headers` | 否 | HTTP header；危险的传输层 header 会被拒绝 |
| `form_fields` | 否 | 额外的 multipart 文本字段 |
| `response_type` | 是 | `text` 或 `json` |
| `url_json_pointer` | 仅 JSON | 指向公共下载 URL 的 RFC 6901 Pointer，数组路径例如 `/files/0/url` |

URL、header 和表单值可引用 `{filename}`、`{bytes}`、`{sha256}`、`{ttl_seconds}`，也可用 `${ENV_NAME}` 读取本机环境变量。token 不应直接写进配置。`providers` 可以配置多项，CLI 会并发上传并最多保留两个成功副本。

provider 返回的下载 URL 必须是公共 HTTPS，并允许接收方无需鉴权用 `GET` 取得完全相同的密文字节。配置文件采用严格 JSON，未知字段会报错。需要多步登录、动态签名或自定义下载请求的服务暂不属于第一版声明式合同，可通过 Issue 提议新的通用字段，不能注入 shell 命令。

配置文件存在时即表示显式选择自定义 provider：CLI 不再尝试项目 Worker 或四家匿名供应商。多个 provider 并发上传，最多记录两个成功副本；全部失败时返回本地 zip。若希望分享链接本身也不使用默认 `workers.dev` resolver，可同时配置你信任的 `AGENT_HANDOFF_RESOLVER`；CLI 导入本来就不会请求 resolver。

## 可选 hosted-service 实现

`deploy/worker` 是项目维护的一种兼容 hosted-service 实现；它不属于 provider schema，也不是 CLI 的必需组件。自建该实现时，从 `wrangler.toml.example` 复制出 `deploy/worker/wrangler.toml`，行为参数都在配置文件中管理：

| 变量 | 默认值 | 含义 |
| --- | ---: | --- |
| `SHARE_DEFAULT_TTL_SECONDS` | `600` | 请求未指定 TTL 时采用的有效期 |
| `SHARE_MAX_TTL_SECONDS` | `86400` | 可接受的最长有效期 |
| `SHARE_MAX_BLOB_BYTES` | `26214400` | 最大密文大小，仍不会超过所选存储平台的硬限制 |

该实现不再定义项目级下载次数、上传额度或存储额度，直接由托管账号和存储平台执行自身限制。其他兼容 hosted service 可以使用完全不同的部署文件和基础设施，只需保持面向客户端的链接合同。

## CLI 集成

- 默认使用项目运营的 Worker 并生成 `#k=` 链接；不可用时由 Filebin、tmpfiles、Uguu、temp.sh 生成 `#h=` 链接。
- 默认配置文件或 `--config FILE` —— 显式使用一个或多个自定义 HTTP provider 并生成 `#h=` 链接。
- `AGENT_HANDOFF_RESOLVER` —— 中继链接的可选 HTTPS 解析页。
- 当前模式下所有上传都失败时，CLI 会返回本地 zip（`status: "fallback_zip"`），分享不会凭空消失。
- `import <url>` 接受完整链接（含 fragment），下载、校验、解密后走正常导入路径 —— 包括干跑/`--execute` 区分与重复检测。
