# 贡献指南（agent-handoff）

[English](../CONTRIBUTING.md) | 简体中文

感谢你有兴趣改进 agent-handoff。本文档涵盖工作流程、贡献预期和架构地图。

改代码前先读 [AGENTS.md](../AGENTS.md)。它是编码智能体与贡献者共同遵守的仓库级契约。

## 基本原则

- **对用户数据保守。** agent-handoff 读写真实的智能体状态（`~/.codex`、`~/.claude`）。导入必须保持只追加；已有线程、索引、数据库行一律不碰。每条写入路径都要留备份。
- **密钥永远是敏感问题。** 新导出特性必须经过密钥扫描；新扫描规则必须高置信（我们宁可漏报，也不阻断正当分享）。
- **JSON 输出是契约。** 智能体解析 CLI 输出；字段名与结构非必要不变。
- **统一品牌名 `agent-handoff`。**

## 开发流程

```sh
git clone <你的 fork>
cd agent-handoff
make test      # 单元 + 集成测试，做任何事之前先跑
make lint      # golangci-lint（errcheck、govet、staticcheck、unused、misspell、copyloopvar）
make build     # bin/agent-handoff
```

1. Fork，从 `main` 拉主题分支。功能使用 `feat/<名称>`，修复使用 `fix/<名称>`，文档使用 `docs/<名称>`。
2. 完成改动，补测试 —— 集成测试套件（`internal/integration_test`）覆盖四象限往返与链接交接；跨智能体或格式改动必须同步更新。
3. 本地确保 `make test lint` 全绿。CI 会跑完整矩阵（Ubuntu/macOS/Windows，`-race`）加 lint 和 worker 语法检查。
4. 提交 PR，写清楚：改了什么、为什么、怎么验证的。

提交信息沿用仓库历史格式：`type: summary`（feat / fix / test / build / docs / refactor），主题行 72 字符以内。

## 分支与发布策略

- `main` 是唯一长期分支，必须始终处于可发布状态。
- 日常改动从短期 `feat/*`、`fix/*` 或 `docs/*` 分支通过 Pull Request 合入。
- 合并前必须通过全部必需 CI。项目只有一名活跃维护者时，不强制第二人批准，避免维护者被锁死。
- `main` 禁止强推和删除。
- 发布从已通过 CI 的 `main` 创建不可变 `v*` 标签，不维护长期 `release` 分支。

## 架构地图

```
main.go                     入口
internal/cli/               命令面：share、preview、import、verify
internal/bundle/            .agent-handoff.zip 容器（v2 + v1 读取兼容）
internal/codex/             Codex 适配器：读 rollout、规范化、还原、校验
internal/claude/            Claude 适配器：读会话/index、还原、校验
internal/neutral/           智能体中立转录（跨智能体桥梁）
internal/session/           共享的 jsonl 迭代/分析工具
internal/safety/            导出前密钥扫描
internal/link/              AES-256-GCM + 兼容链接服务、中继、配置化 Provider
internal/images/            图片资源收集
internal/ledger/            导入去重账本
internal/idgen/             UUIDv7/v4、标题、路径
deploy/worker/              Cloudflare Worker 链接服务（单文件 JS）
skills/agent-handoff/         智能体技能（SKILL.md），随插件分发
```

### 扩展点

- **新增源/目标智能体**：按 [新增其他智能体](adding-agent.zh-CN.md) 执行，其中明确了原生读取/恢复、中立转换、CLI 与宿主接入、捆绑包兼容性和必须覆盖的 N x N 测试矩阵。
- **自定义文件 Provider**：当前 schema 能表达的 multipart/raw 服务只需写 `config.json`，无需改 Go。新增通用字段必须补测试并经过安全审查，详见[扩展与定制](extensions.zh-CN.md)和[链接服务](link-service.zh-CN.md#自定义-provider)。
- **新扫描规则**：加到 `internal/safety/scan.go` 的 `rules`（注意顺序 —— 特定模式放通用模式前面）并补测试。
- **捆绑包格式变更**：增量字段算次版本；不兼容变更必须升 `format_version`，并为旧版本保留读取路径（参考 `internal/bundle/zip.go` 的 v1 兼容）。

### 测试约定

- 单元测试与包同目录（`foo_test.go`）。
- 跨包行为放进 `internal/integration_test`，用临时目录构造合成智能体主目录 —— 绝不碰开发者真实的 `~/.codex` 或 `~/.claude`。
- 加密/worker 测试用进程内 fake worker（`httptest`），不走网络。

## 发布流程（维护者）

推送 `v*` 标签触发发布工作流：先跑测试，再由 goreleaser 构建全平台矩阵（darwin/linux amd64/arm64、windows amd64），带校验和打包并附到 GitHub Release。`install.sh` 消费这些产物。

## 提交 issue

请附上：agent-handoff 版本（`agent-handoff version`）、操作系统、执行的命令、JSON 输出（敏感内容自行脱敏）。安全问题见 [SECURITY.zh-CN.md](SECURITY.zh-CN.md) —— 漏洞不要开公开 issue。

功能优先级和后续 Agent 接入计划见[路线图](ROADMAP.zh-CN.md)。
