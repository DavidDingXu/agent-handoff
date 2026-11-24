#!/usr/bin/env python3
"""Exercise a Qoder package's stdio connector using a temporary desktop profile."""

import argparse
import base64
import json
import os
from pathlib import Path
import sqlite3
import stat
import subprocess
from tempfile import TemporaryDirectory
from zipfile import ZipFile


REPO = Path(__file__).resolve().parents[3]
SOURCE = "86739315-cb6c-4e78-824c-2f19ae099ec2"
BLOCKED = "451246e8-4fb9-4005-958b-ec24f7c0b620"


def init_fixture(root: Path) -> tuple[Path, Path, Path]:
    workspace = root / "workspace with spaces"
    workspace.mkdir()
    (root / "Downloads").mkdir()
    database = root / "main.sqlite"
    runtime = root / "runtime"
    project = runtime / "projects" / "synthetic"
    project.mkdir(parents=True)
    with sqlite3.connect(database) as connection:
        connection.executescript((REPO / "internal/integration_test/testdata/qoder-desktop.sql").read_text())
        for task, title in ((SOURCE, "Qoder MCP synthetic task"), (BLOCKED, "Qoder blocked fixture")):
            connection.execute(
                """INSERT INTO chat_sessions (session_id, session_kind, conversation_mode, product_mode,
                title, cwd, execution_kind, model, permission_mode, extra_json, created_at, updated_at,
                execution_target_json, archived)
                VALUES (?, 'standard', 'agent', 'work', ?, ?, 'local', 'auto', 'default',
                '{}', 1767139200000, 1767225600000, '{}', 0)""",
                (task, title, str(workspace)),
            )
            text = "Test the packaged Qoder app." if task == SOURCE else "Synthetic scanner case " + "ghp_" + "F" * 36
            records = [
                {"type": "user", "uuid": "u", "parentUuid": None, "sessionId": task,
                 "message": {"role": "user", "content": text}},
                {"type": "assistant", "uuid": "a", "parentUuid": "u", "sessionId": task,
                 "message": {"role": "assistant", "content": "Verified fixture evidence."}},
            ]
            (project / (task + ".jsonl")).write_text(
                "".join(json.dumps(record) + "\n" for record in records), encoding="utf-8"
            )
    return database, runtime, workspace


class MCP:
    def __init__(self, process: subprocess.Popen[str]):
        self.process = process
        self.request_id = 0

    def call(self, method: str, **params):
        self.request_id += 1
        self.process.stdin.write(json.dumps({
            "jsonrpc": "2.0", "id": self.request_id, "method": method, "params": params,
        }) + "\n")
        self.process.stdin.flush()
        response = json.loads(self.process.stdout.readline())
        assert response["id"] == self.request_id and "result" in response, response
        return response["result"]

    def tool(self, name: str, **args):
        result = self.call("tools/call", name=name, arguments=args)
        assert not result.get("isError"), result
        return result["structuredContent"]


def check_package(archive: Path, root: Path, architecture: str | None = None) -> None:
    database, runtime, workspace = init_fixture(root)
    plugin = root / "plugin"
    with ZipFile(archive) as zipped:
        assert zipped.testzip() is None
        assert "THIRD_PARTY_NOTICES.md" in zipped.namelist()
        assert any(path.startswith("third_party_licenses/") for path in zipped.namelist())
        manifest = json.loads(zipped.read(".qoder-plugin/plugin.json"))
        skill = manifest["skills"].removeprefix("./") + "agent-handoff/SKILL.md"
        assert b"name: agent-handoff" in zipped.read(skill)
        configuration = json.loads(zipped.read("mcp.json"))["mcpServers"][manifest["name"]]
        resource = configuration["command"].removeprefix("${QODER_PLUGIN_ROOT}/")
        mode = zipped.getinfo(resource).external_attr >> 16
        assert stat.S_IMODE(mode) == 0o755
        zipped.extractall(plugin)
    binary = plugin / resource
    binary.chmod(0o755)
    if archive.stem.endswith("-darwin-universal"):
        assert archive.stat().st_size <= 20_000_000
        assert "Apple Silicon and Intel Macs" in manifest["description"]
        subprocess.run(
            ["/usr/bin/lipo", str(binary), "-verify_arch", "arm64", "x86_64"], check=True
        )
        subprocess.run(
            ["/usr/bin/codesign", "--verify", "--strict", "--all-architectures", str(binary)], check=True
        )
    command = [str(binary)] if architecture is None else ["/usr/bin/arch", "-" + architecture, str(binary)]
    env = {
        **os.environ,
        "HOME": str(root),
        "USERPROFILE": str(root),
        "XDG_STATE_HOME": str(root / "state"),
        "AGENT_HANDOFF_QODER_DATABASE": str(database),
        "AGENT_HANDOFF_QODER_RUNTIME": str(runtime),
        "AGENT_HANDOFF_ENDPOINT": "https://127.0.0.1:1",
    }
    with subprocess.Popen(
        command, stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, env=env
    ) as process:
        client = MCP(process)
        try:
            initialized = client.call("initialize", protocolVersion="2025-11-25")
            assert initialized["serverInfo"]["version"] == "v" + manifest["version"]
            tools = client.call("tools/list")["tools"]
            assert {tool["name"] for tool in tools} == {
                "open_handoff", "list_workspaces", "upload_handoff_zip", "list_qoder_tasks",
                "share_qoder_task", "preview_handoff", "import_qoder_task",
            }
            assert tools[0]["_meta"]["qoder/ui"]["entrypoints"][0] == {"type": "global"}
            assert all(tool["_meta"]["ui"]["visibility"] == ["app"] for tool in tools[1:])
            ui = client.call("resources/read", uri=tools[0]["_meta"]["ui"]["resourceUri"])["contents"][0]
            assert ui["mimeType"] == "text/html;profile=mcp-app" and "ui/initialize" in ui["text"]

            assert client.tool("open_handoff", mode="import") == {"mode": "import"}
            assert client.tool("open_handoff", mode="export") == {"mode": "export"}
            workspaces = client.tool("list_workspaces")["workspaces"]
            assert {Path(item["path"]) for item in workspaces} == {workspace.resolve()}
            assert {item["id"] for item in client.tool("list_qoder_tasks")["tasks"]} == {SOURCE, BLOCKED}
            assert [item["id"] for item in client.tool("list_qoder_tasks", query="SYNTHETIC TASK")["tasks"]] == [SOURCE]
            blocked = client.tool("share_qoder_task", task_id=BLOCKED, format="zip")
            assert blocked["status"] == "blocked" and not blocked.get("path")
            share = client.tool("share_qoder_task", task_id=SOURCE, format="zip")
            bundle = Path(share["path"])
            assert bundle.is_file()
            if os.name != "nt":
                assert stat.S_IMODE(bundle.stat().st_mode) == 0o600
            before = bundle.read_bytes()
            download = client.call("resources/read", uri=share["download_uri"])["contents"][0]
            assert base64.b64decode(download["blob"]) == before

            preview = client.tool("preview_handoff", source=share["path"], cwd=str(workspace))
            assert preview["checksum_status"] == "ok" and preview["title"] == "Qoder MCP synthetic task"
            bundle.write_bytes(b"changed after preview")
            imported = client.tool("import_qoder_task", ticket=preview["ticket"])
            assert imported["status"] == "ok" and imported["thread_id"] != SOURCE
            replay = client.call("tools/call", name="import_qoder_task", arguments={"ticket": preview["ticket"]})
            assert replay["isError"] is True
            upload = client.tool("upload_handoff_zip", size=str(len(before)))
            uploaded = client.tool(
                "upload_handoff_zip", upload_id=upload["upload_id"],
                data=base64.b64encode(before).decode("ascii"),
            )
            second_preview = client.tool("preview_handoff", source=uploaded["source"], cwd=str(workspace))
            duplicate = client.tool("import_qoder_task", ticket=second_preview["ticket"])
            assert duplicate["status"] == "duplicate" and duplicate["thread_id"] == imported["thread_id"]
            assert len(client.tool("list_qoder_tasks")["tasks"]) == 3

            fallback = client.tool("share_qoder_task", task_id=SOURCE, format="link")
            assert fallback["status"] == "fallback_zip" and Path(fallback["path"]).is_file()
        finally:
            process.stdin.close()
            assert process.wait(timeout=5) == 0, process.stderr.read()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--arch", choices=("arm64", "x86_64"), help="Launch the selected macOS binary slice")
    args = parser.parse_args()
    with TemporaryDirectory(prefix="handoff-qoder-package-test-") as directory:
        check_package(args.archive, Path(directory), args.arch)
    print("Packaged Qoder connector smoke checks passed")
