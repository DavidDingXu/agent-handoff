# DSH Agent Handoff

[English](README.md) | 简体中文

在 **DeepSeek Harness 原生界面**中，将任务分享为 ZIP 或加密链接；收到的 DSH、Codex
或 Claude Code 任务可导入为新的 DSH 会话。

## 兼容范围

实验性首版固定支持 **DSH 0.2.2-rc.5**、Node **>=24.9.0**。其他 DSH 版本需验证
会话和插件 API 后再开放。随包提供 macOS/Linux x64、arm64 和 Windows x64 传输核心。

DSH → DSH 仅对经过审查的被动事件提供原生恢复。普通 DSH 日志常含系统提示词、
provider 配置、权限或插件状态，因此需要显式选择**对话与工具记录**模式。
Codex/Claude → DSH 始终使用此模式。不会恢复来源权限、预设、待执行任务、凭据、
媒体文件或 provider 状态；原始日志仅作为私有审计文件保存，不会执行。

## 构建与安装

目前尚未发布到 npm。先在仓库中构建：

```sh
make cross VERSION=v0.4.6
cd plugins/dsh
npm ci
npm run check
npm run package
npm run check:package
```

将生成的 `dist/dsh-agent-handoff-0.1.0.tgz` 通过绝对路径安装到指定 DSH profile
（需要 pnpm）：

```sh
dsh plugin --profile web add /absolute/path/dsh-agent-handoff-0.1.0.tgz
```

重启 DSH。桌面版先完全退出，再使用该桌面版自带的命令，选择 `--profile desktop`。
安装时不要用 `--from-default-profile` 重置已有配置。

## 使用

- 打开会话，点击标题栏的**分享任务**，选择明文 ZIP 或加密链接。检测到密钥时会阻止
  分享，必须明确确认后才能继续。上传不可用时，提供可下载的 ZIP 兜底。
- 点击侧栏的**导入任务**或打开**任务交接**面板。选择 ZIP、加密链接，或 DSH 主机上
  的绝对文件路径，并选择本地工作区。
- 预览标题、来源、消息数、完整性检查和提示，选择可用导入方式，点击**确认为新任务
  导入**，再点击**打开任务**。

操作只允许 DSH owner 连接执行。已有任务不会覆盖；重复导入返回之前创建的会话。
部分失败会返回已创建会话供恢复，不会悄悄重建。预览、密钥确认和下载票据十分钟过期。

ZIP 上传／下载上限为 16 MiB。预览绑定已验证内容及目标工作区，修改后必须重新预览。
打开导入任务不会自动发送提示词或执行历史工具。

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
配置或环境变量中，不写入交接内容。详见仓库的 [provider 文档](../../docs/link-service.zh-CN.md)。

## 验证

`npm test` 覆盖原生控件与插槽、认证路由边界、预览绑定、重复／部分失败恢复、密钥确认、
子进程限制，以及固定 SDK 的持久化／续聊生命周期，使用合成会话和本地模拟模型，无需
模型凭据。Go 测试覆盖原有四条方向及 DSH → Codex/Claude，插件测试覆盖另外三条。
Web 界面和桌面传输合同分别验证；不等于做过 Electron 或实体 Windows 桌面的验收。
