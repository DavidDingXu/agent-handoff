# DSH Agent Handoff

[English](README.md) | 简体中文

在 **DeepSeek Harness 原生界面**中，将任务分享为 ZIP 或加密链接；收到的 DSH、Codex
或 Claude Code 任务可导入为新的 DSH 会话。

## 安装

桌面版打开**插件 → 添加插件**，填写 `dsh-agent-handoff`，安装后启用。
新入口未出现时重启 DSH。[查看 npm 包](https://www.npmjs.com/package/dsh-agent-handoff)。

升级时先卸载旧版第三方插件，再按上述步骤安装并启用新版。若镜像源暂时找不到新版本，
在安装窗口切换为 npm 官方源。

离线安装可从 [GitHub Releases](https://github.com/DavidDingXu/agent-handoff/releases) 下载
`dsh-agent-handoff-0.5.1.tgz` 和 `checksums.txt`，核对文件的 SHA-256 后，在同一安装入口
填写安装包的绝对路径。

Web profile 使用以下命令（需要 pnpm）：

```sh
dsh plugin --profile web add dsh-agent-handoff
```

Web 命令也接受安装包的绝对路径。rc.1 的 npm CLI 不能管理 Desktop profile。
安装时不要用 `--from-default-profile` 重置已有配置。

### 从源码构建

仓库已包含匹配的 Go 核心，无需 Go 环境：

```sh
git clone https://github.com/DavidDingXu/agent-handoff.git
cd agent-handoff/plugins/dsh
npm ci
npm run package
npm run check:package
```

按上方步骤安装生成的 `dist/dsh-agent-handoff-0.5.1.tgz`。开发时在打包前运行
`npm run check` 和 `npm run check:compat`。

## 使用

- 打开**任务交接 → 导出任务**，在任务下拉框中按标题或工作区路径搜索并选择任务，再选择明文 ZIP 或加密链接。密钥检测命中后需
  显式确认；上传失败时提供 ZIP 兜底。会话标题栏的分享入口作为快捷方式保留。
- 打开**任务交接 → 导入任务**，选择 ZIP 或加密链接，再选择本地工作区。
- 核对预览与可用导入方式，确认创建新任务后打开。

操作只允许 DSH owner 连接执行。已有任务不会覆盖；重复导入返回之前创建的会话。
部分失败会返回已创建会话供恢复，不会悄悄重建。预览、密钥确认和下载票据十分钟过期。

ZIP 上传／下载上限为 16 MiB。预览绑定已验证内容及目标工作区，修改后必须重新预览。
打开导入任务不会自动发送提示词或执行历史工具。

## 兼容范围

已验证 **DSH 0.2.2-rc.1 / rc.5**、Node **>=24.9.0**。其他 DSH 版本需验证
会话和插件 API 后再开放。随包提供 macOS/Linux x64、arm64 和 Windows x64 传输核心。
涉及 DSH 的交接，两端都需要 agent-handoff 0.5.0 或更新版本。

DSH → DSH 仅对经过审查的被动事件提供原生恢复。普通 DSH 日志常含系统提示词、
provider 配置、权限或插件状态，因此需要显式选择**对话与工具记录**模式。
Codex/Claude → DSH 始终使用此模式。不会恢复来源权限、预设、待执行任务、凭据、
媒体文件或 provider 状态；原始日志仅作为私有审计文件保存，不会执行。

## 宿主配置

托管 profile 自动提供私有状态目录。需要自定义时，在 profile 的
`cordis.patch.yml` 中覆盖 `agent-handoff`：

```yaml
- id: agent-handoff
  config:
    stateDir: /absolute/private/path/agent-handoff
    # corePath: /absolute/path/agent-handoff
    # endpoint: https://your-share-service.example
    # configPath: /absolute/path/provider-config.json
    ttlSeconds: 3600
    timeoutMs: 60000
```

Windows 使用本机绝对路径；非托管运行时必须配置 `stateDir`。Token 放在既有 provider
配置或环境变量中，不写入交接内容。详见仓库的 [provider 文档](https://github.com/DavidDingXu/agent-handoff/blob/main/docs/link-service.zh-CN.md)。

## 验证

`npm test` 覆盖原生控件与插槽、认证路由边界、预览绑定、重复／部分失败恢复、密钥确认、
子进程限制，以及固定 SDK 的持久化／续聊生命周期，使用合成会话和本地模拟模型，无需
模型凭据。Go 测试覆盖原有四条方向及 DSH → Codex/Claude，插件测试覆盖另外三条。
CI 覆盖两个受支持的 SDK 版本。0.5.0 已在 macOS Desktop rc.1 验收安装、三种来源导入、
打开与 ZIP 导出；导出的 ZIP 也在当前 Codex、Claude 客户端通过本地模拟模型完成续聊。
不代表已验收付费模型或实体 Windows 桌面。详见[完整验证记录](https://github.com/DavidDingXu/agent-handoff/blob/main/plugins/dsh/VERIFICATION.md)。
