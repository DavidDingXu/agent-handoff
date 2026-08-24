# agent-handoff

[English](README.md) | 简体中文

[![CI](https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml/badge.svg)](https://github.com/DavidDingXu/agent-handoff/actions/workflows/ci.yml)
[![CodeQL](https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml/badge.svg)](https://github.com/DavidDingXu/agent-handoff/actions/workflows/codeql.yml)
[![Release](https://img.shields.io/github/v/release/DavidDingXu/agent-handoff)](https://github.com/DavidDingXu/agent-handoff/releases/latest)
[![License](https://img.shields.io/github/license/DavidDingXu/agent-handoff)](LICENSE)

**传递真实编码会话，不是手写一份交接摘要。** Codex 或 Claude Code 任务可以分享为文件或端到端加密链接；接收方导入后得到一个原生新任务，直接继续对话。

对话、工具证据、图片和任务元数据一起传递。同智能体保留原生事件结构，跨智能体使用中立转录。无需截图、复制粘贴、注册账号或依赖服务端会话历史。

## 30 秒安装

```sh
# Claude Code
claude plugin marketplace add https://github.com/DavidDingXu/agent-handoff
claude plugin install agent-handoff@agent-handoff

# Codex
codex plugin marketplace add https://github.com/DavidDingXu/agent-handoff
codex plugin add agent-handoff@agent-handoff
```

重启智能体后直接说**「分享当前任务」**。接收方安装同一个插件后说**「导入 &lt;文件/链接&gt;」**。发送方和接收方都需要安装插件；不需要 Go 环境，也不需要 Cloudflare 账号。

## 特性

- **四象限交接** —— codex→codex、codex→claude、claude→codex、claude→claude。同智能体导入保留原生事件结构与内容，只重写创建新任务所需的身份字段；跨智能体导入走中立转录语义路径，保留可见对话与工具证据。
- **零配置加密链接** —— `--format link` 在本机用 AES-256-GCM 加密，再把密文上传到项目运营的免费服务。该服务不可用时，CLI 自动尝试匿名多供应商中继；全部上传失败则保留本地 zip。团队也可用 `--endpoint` 切到自己的 Worker。详见 [docs/link-service.zh-CN.md](docs/link-service.zh-CN.md)。
- **导出前密钥扫描** —— 六条高置信规则（OpenAI/Anthropic 密钥、AWS 密钥、GitHub token、私钥块、bearer JWT），命中即阻断导出，除非你显式确认。发现项总是上报，展示时已脱敏。
- **绝不乱动你的数据** —— 导入只追加一个新任务；已有线程、索引、数据库行一律不碰；每次写入前自动备份。
- **单一静态二进制** —— 纯 Go（无 CGO 的 sqlite），覆盖 macOS/Linux amd64/arm64 与 Windows amd64；输出 JSON，人和智能体都好读。

## 为什么不直接写一份交接摘要？

| 方式 | 对话与工具证据 | 作为原生任务打开 | 文件或加密链接 | Codex ↔ Claude Code |
| --- | --- | --- | --- | --- |
| 复制粘贴或手写摘要 | 不完整 | 否 | 手工发送 | 手工改写 |
| 通用交接提示词 | 摘要 | 否 | 通常是文本 | 摘要 |
| **agent-handoff** | **完整包含** | **是** | **两者都支持** | **内置支持** |

分享文件和链接都会带上安装入口。接收方只需安装一次，就能继续原任务，不必再让发送方重新整理格式。

## 其他安装方式

**独立 CLI — macOS/Linux：**

```sh
curl -fsSL https://raw.githubusercontent.com/DavidDingXu/agent-handoff/main/install.sh | sh
```

**独立 CLI — Windows PowerShell：**

```powershell
irm https://raw.githubusercontent.com/DavidDingXu/agent-handoff/main/install.ps1 | iex
```

也可以从 [Releases](https://github.com/DavidDingXu/agent-handoff/releases/latest) 下载压缩包。两套安装器都会强制下载并验证发布的 SHA-256 校验值。

**源码编译（Go 1.25+）：**

```sh
git clone https://github.com/DavidDingXu/agent-handoff && cd agent-handoff
make build                    # → bin/agent-handoff
make install                  # → $(go env GOPATH)/bin/agent-handoff
```

## 快速上手

装好插件后，所有操作都是自然语言 —— 智能体会调用内置的 agent-handoff 技能完成。

### 分享当前任务

> 你：「分享当前任务」

智能体生成 `fix-flaky-retry-test.agent-handoff.zip` 并展示卡片：标题、大小、27 条消息、2 张图片。把文件通过 IM/邮件发给对方即可。

### 分享为链接

> 你：「生成分享链接」

无需账号、token 或部署，智能体会直接返回一条端到端加密的 HTTPS 链接。默认先使用项目运营的免费服务，不可用时自动尝试匿名供应商；解密能力始终只在 URL fragment 里，发送时**必须带完整链接**。链接默认约 10 分钟失效，托管服务属于尽力而为。团队需要自主管控或最长 24 小时时，可配置 `AGENT_HANDOFF_ENDPOINT`（或传 `--endpoint`）切到自建 Worker，见 [docs/link-service.zh-CN.md](docs/link-service.zh-CN.md)。

### 接收方导入

对方在他的智能体（Codex 或 Claude Code 均可，装了本插件就行）里说：

> 「导入 /path/to/fix-flaky-retry-test.agent-handoff.zip」
> 「导入 https://share.example.com/s/Wi5x…#k=Qm9…」

智能体先展示预览卡片（标题、来源、时间范围、首末消息），确认后导入为一个**原生新任务**，出现在任务列表顶部，可以直接继续对话。重复导入同一分享会提示 `duplicate` 而不是悄悄分叉。

### 跨智能体

来源和目标智能体自动识别（比如在 Claude Code 里导入 codex 导出的包），无需手动指定。同智能体 → 原生结构高保真恢复并生成新任务身份；跨智能体 → 语义转换（可见对话与工具证据保留，模型、git 分支、时间戳等元数据继承）。

想直接用 CLI？底层命令见下方「命令」一节；上面所有对话背后跑的就是它。

## 命令

| 命令 | 用途 |
| --- | --- |
| `share` | 把线程/会话导出为 `.agent-handoff.zip`（或加密链接） |
| `preview` | 预览一个捆绑包（文件或 URL），不导入 |
| `import` | 干跑或 `--execute` 导入到目标智能体 |
| `verify` | 确认导入的任务在目标智能体状态中可查询 |

主要参数（运行 `agent-handoff` 不带参数查看完整列表）：

- `share`：`--source codex|claude`、`--thread <id>|current`、`--format zip|link`、`--out FILE`、`--endpoint URL`、`--token TOKEN`、`--ttl 秒数`、`--include-secrets`
- `import`：`--target codex|claude`、`--cwd DIR`、`--execute`、`--allow-duplicate`、`--home DIR`
- `verify`：`--thread ID`、`--source codex|claude`、`--cwd DIR`

源/目标智能体从 `CODEX_THREAD_ID` / `CLAUDE_SESSION_ID` 之类的环境变量自动探测，默认 codex。智能体主目录从 `CODEX_HOME` / `CLAUDE_CONFIG_DIR` 或 `~/.codex` / `~/.claude` 解析。

环境变量：`AGENT_HANDOFF_ENDPOINT`（可选的自建 worker 地址）、`AGENT_HANDOFF_TOKEN`（对应的上传 bearer token）、`AGENT_HANDOFF_RESOLVER`（匿名链接的可选静态解析页）。

## 工作原理

```
发送方                                        接收方
──────                                        ──────
~/.codex/sessions/…/rollout.jsonl   ─┐        ┌─> 新的 rollout.jsonl（全新 UUIDv7 id）
~/.codex/state_5.sqlite (threads 行) │  zip   │   追加 threads 行（按 schema 自适应）
        中立转录格式        ←────────┼──────> │   ~/.claude/projects/…/<uuid>.jsonl
        图片、manifest、校验和       ─┘  或   │   index 条目 + ledger 记录
                                           link（AES-256-GCM，能力凭证在 URL fragment）
```

捆绑包是一个确定性 zip：`manifest.json`、原始源会话、中立转录、智能体元数据、图片、密钥扫描报告、逐文件 SHA-256 校验和，以及一份写给接收智能体看的 `AGENT_README.md`。完整规范：[docs/bundle-format.zh-CN.md](docs/bundle-format.zh-CN.md)。

导入刻意保守：

1. **先校验和** —— 任何写入之前先验证完整性。
2. **去重 ledger** —— 每个主目录一份导入账本；重复导入同一分享返回 `status: duplicate`，而不是悄悄分叉。
3. **只追加写入** —— 备份落在原文件旁边；失败时错误原样上报。
4. **不可信内容只是数据** —— agent-handoff 绝不执行、也不主动抓取分享会话里的任何内容。

## 安全

- 链接载荷在本机 AES-256-GCM 加密；服务端只存密文，无法解密。
- 高置信密钥命中会阻断导出，除非显式传 `--include-secrets`。
- 捆绑包逐文件校验和，篡改可检测；`preview` 会报告 `checksum_status`。
- 漏洞请私下报告，见 [SECURITY.zh-CN.md](docs/SECURITY.zh-CN.md)。

## 开发

```sh
make test      # 单元 + 集成测试（四象限往返、链接加密）
make lint      # golangci-lint
make build     # bin/agent-handoff
```

CI 在 Ubuntu/macOS/Windows 上带 `-race` 跑测试。架构：`internal/bundle`（容器格式）、`internal/{codex,claude}`（智能体适配器）、`internal/neutral`（跨智能体转录）、`internal/link`（端到端加密 + worker 客户端）、`internal/safety`（密钥扫描）、`internal/cli`。新增一个智能体 = 一个适配器包 + `bundle.SupportedAgents` 里一个条目。

欢迎贡献，见 [CONTRIBUTING.zh-CN.md](docs/CONTRIBUTING.zh-CN.md)（[English](CONTRIBUTING.md)）。License：Apache-2.0。
