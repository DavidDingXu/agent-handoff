#!/usr/bin/env python3
"""Build single-plugin and local marketplace ZIPs for one Codex platform."""

import argparse
import json
import os
from pathlib import Path
import subprocess
from tempfile import TemporaryDirectory
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


PLUGIN = Path(__file__).resolve().parent
REPO = PLUGIN.parent.parent
TARGETS = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64")
MANIFEST = ".codex-plugin/plugin.json"


def package(target: str, destination: Path) -> tuple[Path, Path]:
    manifest_path = PLUGIN / MANIFEST
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    marketplace_path = PLUGIN / "marketplace.json"
    marketplace = json.loads(marketplace_path.read_text(encoding="utf-8"))
    version = manifest["version"]
    platform, architecture = target.split("-")
    executable = manifest["name"] + (".exe" if platform == "windows" else "")
    resource = f"bin/{executable}"
    root = marketplace["name"]
    plugin_root = f"{root}/plugins/{manifest['name']}"
    portable = {
        "$schema": "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json",
        **{key: manifest[key] for key in ("name", "version", "description", "author", "license")},
        "extensions": {
            "com.openai": {
                "interface": manifest["interface"],
                "requires_local_executor": manifest["requires_local_executor"],
            }
        },
    }
    mcp = {
        "$schema": "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json",
        "mcpServers": {
            manifest["name"]: {
                "type": "stdio",
                "command": "./" + resource,
                "args": ["--host", "codex"],
                "cwd": "${PLUGIN_ROOT}",
            }
        }
    }

    destination.mkdir(parents=True, exist_ok=True)
    single_archive = destination / f"agent-handoff-codex-ui-v{version}-{target}.zip"
    marketplace_archive = destination / f"{root}-v{version}-{target}.zip"
    with TemporaryDirectory(prefix="agent-handoff-codex-build-", dir=destination) as temp:
        binary = Path(temp) / executable
        subprocess.run(
            [
                "go", "build", "-trimpath", "-buildvcs=false",
                "-ldflags", f"-s -w -X github.com/DavidDingXu/agent-handoff/internal/cli.Version=v{version}",
                "-o", str(binary), "./plugins/qoder",
            ],
            cwd=REPO,
            env={**os.environ, "CGO_ENABLED": "0", "GOOS": platform, "GOARCH": architecture},
            check=True,
        )
        binary_bytes = binary.read_bytes()
        for archive, prefix in ((single_archive, ""), (marketplace_archive, f"{plugin_root}/")):
            staging = Path(temp) / archive.name
            with ZipFile(staging, "w", ZIP_DEFLATED) as output:
                if prefix:
                    output.write(marketplace_path, f"{root}/.agents/plugins/marketplace.json")
                    output.write(manifest_path, prefix + MANIFEST)
                output.writestr(prefix + "plugin.json", json.dumps(portable, ensure_ascii=False))
                output.writestr(prefix + "mcp.json", json.dumps(mcp))
                output.write(REPO / "LICENSE", prefix + "LICENSE")
                output.write(REPO / "THIRD_PARTY_NOTICES.md", prefix + "THIRD_PARTY_NOTICES.md")
                for license_path in sorted((REPO / "third_party_licenses").rglob("*")):
                    if license_path.is_file():
                        output.write(license_path, prefix + license_path.relative_to(REPO).as_posix())
                info = ZipInfo(prefix + resource)
                info.create_system = 3
                info.external_attr = 0o100755 << 16
                output.writestr(info, binary_bytes, compress_type=ZIP_DEFLATED)
            with ZipFile(staging) as output:
                if (bad_entry := output.testzip()) is not None:
                    raise ValueError(f"Corrupt package entry {bad_entry}")
            staging.replace(archive)
    return single_archive, marketplace_archive


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=TARGETS)
    parser.add_argument("--out", type=Path, default=REPO / "dist")
    args = parser.parse_args()
    for archive in package(args.target, args.out.resolve()):
        print(archive)


if __name__ == "__main__":
    main()
