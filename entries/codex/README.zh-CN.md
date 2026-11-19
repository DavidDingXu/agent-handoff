# Codex 项目操作

安装支持 `switch` 的 `agent-handoff`，并让它与 `claude` 都能在集成终端的
PATH 中找到。在项目的本地环境设置中添加一个名为 **交接到 Claude** 的
Action，粘贴 [handoff-to-claude.txt](handoff-to-claude.txt) 中的命令。

操作在集成终端中执行，不向模型发送提示。宿主提供准确的 Codex 会话 ID 时
直接使用；未提供时，在终端按编号选择本地任务。工作目录只筛选候选列表，
不会替你选择任务；取消选择不会创建任务。目标 CLI 打开新历史，不自动发送
续做消息。

这是项目工具栏操作，不是 Codex 插件的全局按钮或全局快捷键。官方文档说明了
在应用中配置 Actions 的方法，但未定义可移植的配置文件格式，也未保证 Action
环境中一定存在当前任务 ID。这些文件不会自动写入用户配置。

仅创建历史并取得继续命令时，删掉 `--open`。已知准确源任务时，可加上
`--thread EXACT_ID`。

依据：[官方本地环境文档](https://learn.chatgpt.com/docs/environments/local-environment)。
