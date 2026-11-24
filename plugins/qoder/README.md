# Agent Handoff for Qoder

Share a local Qoder task as a ZIP or an end-to-end encrypted link. Import a handoff from Qoder, Codex, Claude Code, or DeepSeek Harness as a new local Qoder task.

[简体中文](README.zh-CN.md)

## Install and use

Qoder can install published marketplace plugins and eligible shared plugins by link. For Agent Handoff, download the platform ZIP from GitHub Releases and import it locally. [Qoder online installation](https://docs.qoder.com/qoder/plugins) · [Release downloads](https://github.com/DavidDingXu/agent-handoff/releases/tag/v0.7.0)

In Qoder chat, type `/` and choose the Agent Handoff skill, or ask to share the current task or import a handoff. The skill opens the matching tab in the panel.

1. In Qoder Desktop, open **Extensions → Plugins → Add Plugins → Import plugin** and select the Agent Handoff ZIP for your operating system and CPU.
2. Enable the plugin, then open **Agent Handoff** from its app entry. You can also ask Qoder to open Agent Handoff in a conversation.
3. In the export tab, search for and select the task. Select **ZIP file** or **Encrypted link**, then create the handoff. ZIPs are saved to Downloads, or your home directory if Downloads is unavailable. ZIPs contain plaintext conversation data. If the link service is unavailable, the app shows the saved ZIP path.
4. In the import tab, choose or drop a ZIP, or paste the complete encrypted link. Select an existing destination workspace, or use **Add workspace** to enter another local directory. Preview the handoff and select **Import as a new task**. Open the imported task from Qoder history. If the task is missing from the running client, quit and reopen Qoder.

The plugin runs a bundled local connector. It reads local Qoder history to share tasks and appends a new task on import. Creating links uses the core Agent Handoff link service and provider configuration. The connector can open ZIP files the user selects or supplies by absolute path. The Qoder desktop app may ask you to enable or trust the local connector.

## Compatibility

The current desktop storage adapter targets Qoder Desktop 0.5.17. The Apple Silicon build was exercised against the desktop plugin host and the bundled SDK using synthetic records. The remaining packages are build-verified. Receivers in Codex, Claude Code, or DSH need a handoff core that recognizes Qoder bundles.

Handoffs preserve visible text and historical tool evidence. Images and attachments are omitted. Tool evidence restored to Qoder is passive conversation text. The plugin requires a bundle with checksums. When Qoder uses a custom storage directory, set `AGENT_HANDOFF_QODER_DATABASE` and `AGENT_HANDOFF_QODER_RUNTIME` in the local connector environment.

## Marketplace ZIP for macOS

For a macOS marketplace listing, upload `agent-handoff-qoder-v<version>-darwin-universal.zip`. It contains one local MCP connector that runs on Apple Silicon and Intel Macs. The build enforces the 20 MB upload limit displayed in the publishing form.

You can use this prerequisites text in the listing.

> Qoder Desktop for macOS on an Apple Silicon or Intel Mac. The plugin runs a bundled local MCP connector. It accesses local task history to export selected tasks and creates a new local Qoder task when you confirm an import. Link sharing uses the configured Agent Handoff sharing service.

The Qoder desktop client can install the ZIP locally. Public marketplace review support for a plugin using a local stdio connector is unconfirmed. On 2026-11-24, both connector slices passed the packaged workspace listing, ZIP upload, export, preview, and import checks on an Apple Silicon Mac, with Rosetta running the Intel slice. Both link only macOS system libraries. [Qoder plugin publishing guide](https://docs.qoder.com/developers/publishing/plugins)

## Build and check

Run these commands at the repository root:

```sh
python3 plugins/qoder/package.py darwin-arm64
python3 plugins/qoder/tests/package_smoke.py dist/agent-handoff-qoder-v0.7.0-darwin-arm64.zip
go test ./plugins/qoder -race -count=1
```

To build the single macOS marketplace ZIP, run `python3 plugins/qoder/package.py darwin-universal` on a Mac with `lipo` and `codesign`. The smoke test accepts `--arch arm64` or `--arch x86_64` to launch a specified slice.

The build script also accepts `darwin-amd64`, `linux-amd64`, `linux-arm64`, and `windows-amd64`. The installer bundles the Go connector for the selected platform. Building requires Go and Python 3.10 or newer. Package smoke tests use an isolated synthetic desktop profile and run on the same platform as the packaged connector.

[Qoder plugin packaging specification](https://docs.qoder.com/developers/plugins/specification) · [Qoder app entry points](https://docs.qoder.com/qoder/plugins)
