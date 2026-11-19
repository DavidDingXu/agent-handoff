# Claude 本地交接入口

根插件通过 `hooks/hooks.json` 自动加载这个 Mod，无需再安装第二个插件。
Claude Code v2.1.287 及以上可使用 `/handoff codex` 或 `/handoff claude`；
加上 `--include-code` 可携带符合条件的已跟踪文本改动。普通交接 Skill 仍可用于分享。

这个即时命令从原生 `$.session.id()` 和 `$.session.cwd()` 取得当前身份与目录，
调用本地 `agent-handoff switch`，不调用模型、不提交提示，因此无需源模型额度。
它创建目标原生历史并显示准确的继续命令。打开另一个应用界面仍由用户操作；
Mod 没有可依据的终端移交或 Codex 窗口 API。

插件自动使用内置核心程序，无需另装 CLI。开发时可将 `AGENT_HANDOFF_BIN` 设为
核心程序的绝对路径以覆盖默认值。
当前会话尚未落盘时，命令会明确说明，不会换成附近任务；核心程序的敏感信息
扫描仍然生效。

使用已安装的 Claude Code CLI 做隔离验证：

```sh
claude plugin validate plugins/claude --strict
claude plugin test plugins/claude
```

测试使用 Claude 原生 Mod 测试框架，无需登录、真实会话、网络或模型调用。
目前在 macOS 的 v2.1.322 验证通过；Windows/Linux 和完整交互界面仍需各自的
宿主验证。子目录清单用于限定测试范围，正常安装使用根清单与同一个模块。

默认程序路径依据原生 `$.plugin.root`。Windows 使用内置 amd64 程序；
macOS/Linux 通过参数数组运行 `uname -s -m` 选择平台包，不依赖 Node 或 Shell。

官方依据：[Mods](https://code.claude.com/docs/en/plugins/mods)、
[命令与进程 API](https://code.claude.com/docs/en/plugins/mods/api)、
[原生测试](https://code.claude.com/docs/en/plugins/mods/test)。
