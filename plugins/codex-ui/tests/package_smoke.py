#!/usr/bin/env python3
"""Check the single-plugin ZIP and start its packaged MCP server."""

import argparse
import json
import os
from pathlib import Path
import stat
import subprocess
from tempfile import TemporaryDirectory
from zipfile import ZipFile


def check_package(archive: Path, marketplace_archive: Path) -> None:
    with TemporaryDirectory(prefix="agent-handoff-codex-smoke-") as temp:
        root = Path(temp)
        plugin = root / "plugin"
        codex_home = root / "codex"
        codex_home.mkdir()
        with ZipFile(archive) as package, ZipFile(marketplace_archive) as marketplace_package:
            manifest = json.loads(package.read("plugin.json"))
            assert manifest["$schema"] == "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
            assert manifest["extensions"]["com.openai"]["requires_local_executor"]
            name = manifest["name"]
            mcp = json.loads(package.read("mcp.json"))["mcpServers"][name]
            assert mcp["command"] in ("./bin/agent-handoff-ui", "./bin/agent-handoff-ui.exe")
            assert mcp["cwd"] == "${PLUGIN_ROOT}" and mcp["args"] == ["--host", "codex"]
            executable = mcp["command"].removeprefix("./")
            entries = set(package.namelist())
            licenses = {path for path in entries if path.startswith("third_party_licenses/")}
            assert licenses
            assert entries - licenses == {"plugin.json", "mcp.json", "LICENSE", "THIRD_PARTY_NOTICES.md", executable}
            assert stat.S_IMODE(package.getinfo(executable).external_attr >> 16) == 0o755

            marketplace_path, = [path for path in marketplace_package.namelist()
                                 if path.endswith("/.agents/plugins/marketplace.json")]
            marketplace_root = marketplace_path.removesuffix("/.agents/plugins/marketplace.json")
            entry, = json.loads(marketplace_package.read(marketplace_path))["plugins"]
            assert entry["name"] == name
            prefix = marketplace_root + "/" + entry["source"]["path"].removeprefix("./") + "/"
            assert json.loads(marketplace_package.read(prefix + ".codex-plugin/plugin.json"))["name"] == name
            for path in package.namelist():
                assert package.read(path) == marketplace_package.read(prefix + path), path
            package.extractall(plugin)

        binary = plugin / executable
        binary.chmod(0o755)
        env = {
            **os.environ, "HOME": str(root), "USERPROFILE": str(root),
            "CODEX_HOME": str(codex_home), "XDG_STATE_HOME": str(root / "state"),
        }
        requests = [
            {"method": "initialize", "params": {"protocolVersion": "2025-11-25"}},
            {"method": "tools/list"},
            {"method": "tools/call", "params": {"name": "list_workspaces", "arguments": {}}},
        ]
        result = subprocess.run(
            [str(binary), *mcp["args"]], cwd=plugin, env=env, text=True, capture_output=True,
            input="".join(json.dumps({"jsonrpc": "2.0", "id": index, **request}) + "\n"
                          for index, request in enumerate(requests, 1)),
            check=True, timeout=30,
        )
        responses = {response["id"]: response["result"]
                     for response in map(json.loads, result.stdout.splitlines())}
        assert responses[1]["serverInfo"]["version"] == "v" + manifest["version"]
        assert "open_handoff" in {tool["name"] for tool in responses[2]["tools"]}
        assert isinstance(responses[3]["structuredContent"]["workspaces"], list)
        print("Codex single-plugin ZIP, marketplace ZIP and local MCP server passed")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("marketplace_archive", type=Path)
    args = parser.parse_args()
    check_package(args.archive, args.marketplace_archive)
