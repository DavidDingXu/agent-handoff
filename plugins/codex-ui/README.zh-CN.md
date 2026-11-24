# Codex 任务交接面板

[English](README.md)

按当前平台构建两个安装包。以下命令适用于 Apple 芯片的 Mac。

```sh
python3 plugins/codex-ui/package.py darwin-arm64
```

在 Codex 桌面端打开 **自定义 → 插件 → 添加 → 创建插件**，直接选择 `agent-handoff-codex-ui-v<version>-<platform>.zip`，然后点击 **添加插件**。新开一个本地聊天，引用 **Agent Handoff UI** 后输入「打开 Agent Handoff 任务交接面板」。

对话 Skill 可按主 README 的命令从 GitHub 在线安装，用「分享当前任务」和「导入 <文件/链接>」调用。可视化面板使用对应平台的 Release ZIP，也可通过下文的本地插件市场安装。[安装对话 Skill](../../README.zh-CN.md#codex--claude-code) · [Release 下载](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0)

导入任务时，可以选择或拖入 256 MiB 以内的交接 ZIP，或粘贴加密分享链接。从工作区列表选择目标。如果目录还没出现在列表中，点「添加工作区」并输入现有目录的路径。

## 从本地插件市场安装

将 `agent-handoff-codex-preview-v<version>-<platform>.zip` 解压到长期保留的目录。你可以在 Codex 中打开该目录，在插件目录中选择 **Agent Handoff Preview** 并安装 **Agent Handoff UI**。如果插件来源列表尚未刷新，重启客户端。

也可以使用 Codex CLI 安装。将命令中的示例路径替换为解压后的插件市场目录。

```sh
codex plugin marketplace add /path/to/agent-handoff-codex-preview
codex plugin add agent-handoff-ui@agent-handoff-codex-preview
```

安装包内有对应平台的本地 MCP 可执行文件。支持 MCP Apps 渲染的 Codex 客户端会显示交接面板。

也可以从解压后的插件市场目录运行下列命令，在浏览器打开同一个面板。打开命令输出的本机访问地址，使用期间保持进程运行。

```sh
./plugins/agent-handoff-ui/bin/agent-handoff-ui --host codex --browser
```

Windows 上运行 `.\plugins\agent-handoff-ui\bin\agent-handoff-ui.exe --host codex --browser`。
