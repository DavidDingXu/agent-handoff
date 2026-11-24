# Qoder 版 Agent Handoff

将本机 Qoder 任务导出为 ZIP 或端到端加密链接。接收来自 Qoder、Codex、Claude Code 和 DeepSeek Harness 的交接包，在本机创建新的 Qoder 任务。

[English](README.md)

## 安装和使用

1. 在 Qoder 桌面端打开 **扩展 → 插件 → 添加插件 → 导入插件**，选择适合当前系统和处理器的 Agent Handoff ZIP。
2. 启用插件，从应用入口打开 **Agent Handoff**。也可以在对话中请 Qoder 打开 Agent Handoff。
3. 在 **导出任务** 页搜索并选中任务，选择 **ZIP 文件** 或 **加密链接**，再点击 **生成交接内容**。ZIP 保存在下载目录，下载目录不可用时保存在用户主目录。ZIP 内的对话内容为明文。链接服务不可用时，面板会显示已保存的 ZIP 路径。
4. 在 **导入任务** 页选择或拖入 ZIP，也可以粘贴完整的加密链接。从列表选择目标工作区，或通过 **添加工作区** 输入其他本地目录。预览交接内容后，点击 **确认为新任务导入**。从 Qoder 历史打开导入的任务。历史中暂未出现时，退出并重新打开 Qoder。

插件运行随包提供的本地连接器。分享时读取本机 Qoder 历史，导入时追加新任务。生成链接沿用 Agent Handoff 的分享服务和上传配置。连接器可读取用户选中或提供绝对路径的 ZIP 文件。客户端可能会提示启用或信任本地连接器。

## 兼容范围

当前存储适配针对 Qoder Desktop 0.5.17。Apple Silicon 构建用合成会话核对了桌面插件流程和自带 SDK 的历史读取。其余安装包已验证能够构建。Codex、Claude Code 或 DSH 作为接收方时，需要使用支持 Qoder 交接包的新版核心。

交接保留可见文本和历史工具记录。图片和附件暂未纳入。工具记录以历史文本写入 Qoder。插件要求交接包带校验和。Qoder 使用自定义存储目录时，可在本地连接器环境中配置 `AGENT_HANDOFF_QODER_DATABASE` 和 `AGENT_HANDOFF_QODER_RUNTIME`。

## macOS 市场上传包

在发布中心提交 macOS 版时，上传 `agent-handoff-qoder-v<version>-darwin-universal.zip`。包内的本地 MCP 连接器可在 Apple Silicon 和 Intel Mac 上运行。构建脚本按发布表单显示的 20 MB 上限检查文件大小。

市场页面的使用条件可填写如下内容。

> 适用于搭载 Apple Silicon 或 Intel 芯片的 Qoder macOS 桌面版。插件运行随包提供的本地 MCP 连接器，读取本机任务历史以分享选中的任务，并在确认导入后创建新的本地 Qoder 任务。生成链接时使用已配置的 Agent Handoff 分享服务。

Qoder 桌面端支持从本地安装该 ZIP。使用本地 stdio 连接器的插件能否通过公开市场审核仍待确认。 2026-11-24 在 Apple Silicon Mac 上验证了包内两个架构的工作区列表、ZIP 上传、导出、预览和导入。Intel 架构通过 Rosetta 运行。两个架构都只链接 macOS 系统库。 [Qoder 插件发布指南](https://docs.qoder.com/zh/developers/publishing/plugins)

## 构建和验证

在仓库根目录运行以下命令。

```sh
python3 plugins/qoder/package.py darwin-arm64
python3 plugins/qoder/tests/package_smoke.py dist/agent-handoff-qoder-v0.6.1-darwin-arm64.zip
go test ./plugins/qoder -race -count=1
```

在装有 `lipo` 和 `codesign` 的 Mac 上运行 `python3 plugins/qoder/package.py darwin-universal` 可生成单个 macOS 市场包。冒烟测试可通过 `--arch arm64` 或 `--arch x86_64` 分别启动对应架构。

构建脚本也支持 `darwin-amd64`、`linux-amd64`、`linux-arm64` 和 `windows-amd64`。安装包包含对应平台的 Go 连接器。构建需要 Go 和 Python 3.10 或更高版本。安装包冒烟测试使用隔离的合成桌面数据，需在与安装包一致的平台上运行。

[Qoder 插件打包规格](https://docs.qoder.com/developers/plugins/specification) · [Qoder 应用入口](https://docs.qoder.com/qoder/plugins)
