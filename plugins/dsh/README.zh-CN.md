# DSH Agent Handoff

[English](README.md) | 简体中文

在 **DeepSeek Harness 原生界面**中，将任务分享为 ZIP 或加密链接；收到的 DSH、Codex
或 Claude Code 任务可导入为新的 DSH 会话。

## 安装

桌面版打开**插件 → 添加插件**，填写 `dsh-agent-handoff`，安装后启用。
新入口未出现时重启 DSH。[查看 npm 包](https://www.npmjs.com/package/dsh-agent-handoff)。

升级时先卸载旧版第三方插件，再按上述步骤安装并启用新版。若镜像源暂时找不到新版本，
在安装窗口切换为 npm 官方源。

安装本次发布可从 [GitHub Release v0.7.0](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0) 下载
`dsh-agent-handoff-0.7.0.tgz` 和 `checksums.txt`，核对文件的 SHA-256 后，在同一安装入口
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

按上方步骤安装生成的 `dist/dsh-agent-handoff-0.7.0.tgz`。开发时在打包前运行
`npm run check` 和 `npm run check:compat`。

## 使用

在 DSH 对话里输入 `/agent-handoff`，说明要分享还是导入，再点击结果中的**分享任务**或**导入任务**，进入对应的原生界面。

- 打开**任务交接 → 导出任务**，在任务下拉框中按标题或工作区路径搜索并选择任务，再选择明文 ZIP 或加密链接。密钥检测命中后需
  显式确认。生成后在弹窗中复制链接或下载 ZIP；关闭后可点**查看结果**，不会重复上传。
  链接生成失败时会提供 ZIP。会话标题栏的分享入口作为快捷方式保留。
- 打开**任务交接 → 导入任务**，选择 ZIP 或加密链接，再选择本地工作区。
- 核对预览与可用导入方式，确认创建新任务后打开。
- **本机切换：** 导出页选择**切换 Agent**与目标应用，创建 Codex／Claude 原生任务。
  完成后弹出继续命令，点击**复制命令**。关闭后可用**查看继续命令**重新打开，不会重复创建任务。
  到运行 DSH 的电脑，在显示的目录执行继续命令。无需来源模型响应，不会自动发送提示。
- **简报与备注：** 任务概览可展开已有请求、回复与工具记录；备注放在**备注与代码（可选）**中。加密链接也能
  在浏览器本地解密阅读，接收方无需先安装。
- **携带代码：** 展开**备注与代码（可选）**后勾选，默认关闭。按文件选择文本改动，新文件默认不选。接收时预览文件和新目录，
  主动勾选**在新工作区应用代码**才会创建独立 Git worktree，保留原工作区。

操作只允许 DSH owner 连接执行。已有任务不会覆盖；重复导入返回之前创建的会话。
部分失败会返回已创建会话供恢复，不会悄悄重建。预览、密钥确认和下载票据十分钟过期。

ZIP 上传／下载上限为 16 MiB。预览绑定已验证内容及目标工作区，修改后必须重新预览。
打开导入任务不会自动发送提示词或执行历史工具。
代码最多 256 个文件，每个 1 MiB，补丁与新文件合计 8 MiB；接收仓库需已有基准提交。
缺少提交或补丁冲突时，可改为仅导入任务。[完整用法与恢复说明](../../docs/usage.zh-CN.md)。

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
