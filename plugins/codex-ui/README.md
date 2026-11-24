# Agent Handoff UI for Codex

[简体中文](README.zh-CN.md)

Build both packages for your desktop platform. This example targets Apple Silicon.

```sh
python3 plugins/codex-ui/package.py darwin-arm64
```

Open **Customize → Plugins → Add → Create plugin** in Codex Desktop. Choose `agent-handoff-codex-ui-v<version>-<platform>.zip` directly and click **Add plugin**. In a new local chat, mention **Agent Handoff UI** and ask it to open the panel.

Install the chat Skill from the repository marketplace with the commands in the main README. It handles “share the current task” and “import <file/link>”. For the visual panel, use the platform ZIP from GitHub Releases or the local marketplace instructions below. [Install the chat Skill](../../README.md#codex--claude-code) · [Release downloads](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0)

To import a task, choose or drop a handoff ZIP up to 256 MiB, or paste its encrypted share link. Select a destination from the workspace list. Use **Add workspace** to enter an existing folder if it is missing from the list.

## Install from a local marketplace

Extract `agent-handoff-codex-preview-v<version>-<platform>.zip` into a directory you plan to keep. Open that directory as a Codex project, choose **Agent Handoff Preview** in Plugins, and install **Agent Handoff UI**. Restart the desktop app if the marketplace picker has not refreshed.

You can also register and install it with Codex CLI. Replace the example path with the extracted marketplace directory.

```sh
codex plugin marketplace add /path/to/agent-handoff-codex-preview
codex plugin add agent-handoff-ui@agent-handoff-codex-preview
```

The package includes its local MCP executable for the selected platform. The panel appears when the Codex host supports MCP Apps rendering.

To open the same panel in a browser, run the executable from the extracted marketplace directory. Open the private localhost URL it prints and leave the process running while using the panel.

```sh
./plugins/agent-handoff-ui/bin/agent-handoff-ui --host codex --browser
```

On Windows, run `.\plugins\agent-handoff-ui\bin\agent-handoff-ui.exe --host codex --browser`.
