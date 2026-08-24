# 链接分享

[English](link-service.md) | 简体中文

`agent-handoff share --format link` 把捆绑包变成一条可以随处粘贴的 URL。捆绑包在**你的机器上**用 AES-256-GCM 加密。零配置时先使用项目运营的服务，不可用时自动尝试匿名临时文件服务；高级用户可以通过声明式配置接入已有 HTTP 文件服务，不绑定 Cloudflare。

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

`upload_type` 只接受 `multipart` 或 `raw`。响应可以是 `text`，也可以是 `json` 并用 RFC 6901 JSON Pointer 指向下载 URL；数组路径例如 `/files/0/url`。URL、header 和表单值可引用 `{filename}`、`{bytes}`、`{sha256}`、`{ttl_seconds}`，也可用 `${ENV_NAME}` 读取本机环境变量。token 不应直接写进配置。

provider 返回的下载 URL 必须是公共 HTTPS，并允许接收方无需鉴权用 `GET` 取得完全相同的密文字节。配置文件采用严格 JSON，未知字段会报错。需要多步登录、动态签名或自定义下载请求的服务暂不属于第一版声明式合同，可通过 Issue 提议新的通用字段，不能注入 shell 命令。

配置文件存在时即表示显式选择自定义 provider：CLI 不再尝试项目 Worker 或四家匿名供应商。多个 provider 并发上传，最多记录两个成功副本；全部失败时返回本地 zip。若希望分享链接本身也不使用默认 `workers.dev` resolver，可同时配置你信任的 `AGENT_HANDOFF_RESOLVER`；CLI 导入本来就不会请求 resolver。

## CLI 集成

- 默认使用项目运营的 Worker 并生成 `#k=` 链接；不可用时由 Filebin、tmpfiles、Uguu、temp.sh 生成 `#h=` 链接。
- 默认配置文件或 `--config FILE` —— 显式使用一个或多个自定义 HTTP provider 并生成 `#h=` 链接。
- `AGENT_HANDOFF_RESOLVER` —— 中继链接的可选 HTTPS 解析页。
- 当前模式下所有上传都失败时，CLI 会返回本地 zip（`status: "fallback_zip"`），分享不会凭空消失。
- `import <url>` 接受完整链接（含 fragment），下载、校验、解密后走正常导入路径 —— 包括干跑/`--execute` 区分与重复检测。
