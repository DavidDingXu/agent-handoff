# 捆绑包格式（`*.agent-handoff.zip`）

[English](bundle-format.md) | 简体中文

本文档定义 agent-handoff v2 捆绑包容器格式：磁盘布局、每个条目、manifest schema、校验文件与兼容规则。它既是 `internal/bundle` 里 Go 实现的规范依据，也适用于任何第三方读取方。

## 设计目标

- **自描述**：不依赖任何网络访问或外部服务，就能检查、预览、导入一个捆绑包。
- **原生与语义共存于一个工件**：原生事件和内容与中立转录一同携带。分页 Codex rollout 会连同其引用的历史物化为一个独立会话，不保留发送方本地 lineage。同智能体导入复用原生数据并重写新任务所需的身份与路径字段；跨智能体导入使用中立表示。
- **防篡改可检测**：每个条目都有 SHA-256 校验，导入写入前先验证。
- **确定性**：相同输入产出字节级相同的 zip（条目排序、固定时间戳），捆绑包可以 diff、可以复验。

## 布局

```
manifest.json            捆绑包 manifest（schema、id、计数、智能体信息）
AGENT_README.md          给接收智能体的操作说明
checksums.json           其余每个文件的 sha256
codex/session.jsonl      可移植原生会话            （目录名 = 源智能体）
codex/meta.json          发送方元数据             （threads 表行）
codex/images.json        图片清单                 （仅 codex 源）
codex/images/*           图片资源                 （仅 codex 源）
agent/neutral.json       智能体中立转录
agent/restore.md         导入后的续接上下文
safety/scan.json         分享时的密钥扫描结果
```

`codex/` 目录按源智能体命名：Claude 来源的捆绑包用 `claude/session.jsonl` 和 `claude/meta.json`（Claude 的 index 条目），不带图片。智能体 `a` 的条目名定义在 `internal/bundle/format.go`：`a/session.jsonl`、`a/meta.json`、`a/images.json`、`a/images/*`。

`manifest.json` 与 `checksums.json` 永远在压缩包根目录。无论产出平台，所有条目统一用正斜杠存储。

## manifest.json

```json
{
  "format_version": 2,
  "artifact_type": "agent-handoff",
  "source_agent": "codex",
  "target_support": ["codex", "claude"],
  "source_thread_id": "0192c0de-…",
  "title": "fix flaky retry test",
  "source_cwd": "/Users/alice/repo",
  "created_at": "2026-08-21T09:41:12.345Z",
  "message_count": 27,
  "image_count": 2,
  "source_cli": "codex-cli 0.45.0",
  "model_provider": "openai",
  "git_branch": "main",
  "git_origin_url": "git@github.com:acme/repo.git",
  "files": ["agent/neutral.json", "agent/restore.md", "AGENT_README.md", "checksums.json", "codex/images.json", "codex/images/img-0.png", "codex/meta.json", "codex/session.jsonl", "manifest.json", "safety/scan.json"]
}
```

字段语义：

| 字段 | 含义 |
| --- | --- |
| `format_version` | 本规范为 `2`。读取方必须拒绝未知的**主**版本，但可接受更新的次版本（为增量字段保留）。 |
| `artifact_type` | 恒为 `"agent-handoff"`，读取方的快速健全性检查。 |
| `source_agent` | `"codex"` 或 `"claude"` —— 产出原始会话的智能体。 |
| `target_support` | 本捆绑包可导入的智能体，目前恒为两者。 |
| `source_thread_id` | 发送方的线程（Codex UUIDv7）或会话（Claude UUIDv4）id。 |
| `source_cwd` | 发送方的工作目录。仅作信息展示，绝不用作导入目标。 |
| `message_count` | 规范化后会话中 user+assistant 消息数（发送方视角）。 |
| `image_count` | 成功拷入捆绑包的图片数。 |
| `files` | 完整条目列表，含 `manifest.json` 自身；`checksums.json` 在列表中但不被自身哈希覆盖（见下）。 |

## checksums.json

一个 JSON 对象：条目名 → 小写十六进制 SHA-256，覆盖**除 `checksums.json` 自身外的每个条目**（自引用排除）。最后写入。`ReadZip` 会重算每个摘要；`import --execute` 在任何条目不匹配时拒绝写入，`preview` 报告 `checksum_status`。

参考读取方在解压前还会执行资源上限：最多 1024 个条目、单条目最大 128 MiB、总解压内容最大 256 MiB。第三方读取方也应在校验 checksum 之前执行等价限制，避免恶意压缩包先耗尽内存。

## agent/neutral.json

跨智能体表示（schema `agent-handoff.neutral.v1`）：

```json
{
  "schema": "agent-handoff.neutral.v1",
  "source_agent": "codex",
  "source_id": "0192c0de-…",
  "title": "fix flaky retry test",
  "source_cwd": "/Users/alice/repo",
  "entries": [
    { "kind": "message", "role": "user",      "text": "the retry test flakes on CI", "timestamp": "…" },
    { "kind": "tool",    "tool": "shell",     "status": "completed", "input": "go test ./…", "output": "ok  …" },
    { "kind": "message", "role": "assistant", "text": "Fixed: …" }
  ]
}
```

- `kind` 为 `message`（带 `role` user/assistant 和 `text`）或 `tool`（带 `tool`、`status` called/completed/failed、`input`、`output`）。
- 刻意有损：可见消息与工具证据保留；推理轨迹与智能体专属的事件结构不保留。原始会话始终随包同行以备审计。
- Codex rollout 会把每条消息存两遍（`event_msg` 行 + `response_item` 副本），转换器做了去重，接收方看到每条消息只出现一次。
- 隐藏内容会被过滤：隐藏的用户消息、环境/上下文块、发送方自己的导出工具调用轮次都被剔除，接收方看到的与发送方看到的完全一致。

## codex/meta.json

Codex 来源时：发送方 `threads` 表行（来自 `state_5.sqlite`），扁平 JSON 对象。同智能体导入时，该行克隆进接收方数据库并叠加导入专属字段（新 id、时间戳、cwd、独立会话 `history_mode`），模型、effort、git 元数据、沙箱/审批设置等全部保留 —— 且只写接收方 schema 里实际存在的列，旧/新版 Codex 都能工作。发送方本地的 `history_base` 与 `context_window` lineage 会被主动移除，因为接收方没有这些 rollout 文件。接收方完全没有 `state_5.sqlite` 时，agent-handoff 会引导建出一张最小 `threads` 表，导入的任务立刻出现在列表里。

## claude/meta.json

Claude 来源时：发送方的会话 index 条目（存于 `~/.claude/projects/<dir>/index.json`），同智能体导入时用于重建列表元数据。

## safety/scan.json

分享时抓取的密钥扫描结果（规则与发现项，提示已脱敏）。这是审计记录；导入方不重新扫描，但报告随包同行，接收侧（人或智能体）可以复查。

## Agent README 与续接说明

- `AGENT_README.md` —— 写给接收智能体的直白说明：包里有什么、精确的 `agent-handoff preview/import` 命令、安全规则（绝不自动执行内容、写入前先确认）。
- `agent/restore.md` —— 导入时重新生成的续接上下文：任务从哪来、如何继续。

## v1 兼容

v1 捆绑包（仅 codex、扁平布局：压缩包根下的 `session.jsonl`、`meta.json`、`images.json`、`images/`，无中立转录、无按智能体的目录）可读取。读取方在智能体校验之前先应用 v1 回退（源智能体 = codex，条目名按扁平规则解析）。不再支持写出 v1。

## 版本演进

- 只有**不兼容变更**才升 `format_version`；增量字段算次版本修订，在本文档记录。
- 新增源智能体 = 新增一个智能体目录 + manifest 字段；`target_support` 与 `SupportedAgents` 是 `internal/bundle/format.go` 中的扩展点。
