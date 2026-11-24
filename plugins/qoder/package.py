#!/usr/bin/env python3
"""Build a self-contained Qoder plugin ZIP for a desktop platform or both Mac architectures."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from tempfile import TemporaryDirectory
from zipfile import ZIP_DEFLATED, ZipFile, ZipInfo


PLUGIN = Path(__file__).resolve().parent
REPO = PLUGIN.parent.parent
MACOS_UNIVERSAL = "darwin-universal"
MARKETPLACE_ZIP_LIMIT = 20_000_000
TARGETS = ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64", "windows-amd64", MACOS_UNIVERSAL)


def build_connector(binary: Path, platform: str, architecture: str, version: str) -> None:
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


def build_macos_universal(binary: Path, version: str) -> None:
    slices = []
    for architecture in ("arm64", "amd64"):
        slice_path = binary.with_name(f"{binary.name}-{architecture}")
        build_connector(slice_path, "darwin", architecture, version)
        slices.append(slice_path)
    subprocess.run(["/usr/bin/lipo", "-create", *slices, "-output", binary], check=True)
    subprocess.run(["/usr/bin/lipo", binary, "-verify_arch", "arm64", "x86_64"], check=True)
    subprocess.run(["/usr/bin/codesign", "--force", "--sign", "-", "--timestamp=none", binary], check=True)
    subprocess.run(["/usr/bin/codesign", "--verify", "--strict", "--all-architectures", binary], check=True)


def package(target: str, destination: Path, database: str | None = None, runtime: str | None = None) -> Path:
    manifest = json.loads((PLUGIN / ".qoder-plugin" / "plugin.json").read_text(encoding="utf-8"))
    version = manifest["version"]
    if target == MACOS_UNIVERSAL:
        manifest["description"] += (
            " 适用于搭载 Apple Silicon 或 Intel 芯片的 Qoder macOS 桌面版。"
            " For Qoder Desktop on Apple Silicon and Intel Macs."
        )
    if database or runtime:
        manifest["name"] += "-smoke"
        manifest["displayName"] += " Smoke Test"
    platform, architecture = target.split("-")
    executable = "agent-handoff-qoder.exe" if platform == "windows" else "agent-handoff-qoder"
    resource = f"bin/{executable}"
    connector = {
        "type": "stdio",
        "command": "${QODER_PLUGIN_ROOT}/" + resource,
        "timeout": 100000,
    }
    if database or runtime:
        connector["env"] = {
            "AGENT_HANDOFF_QODER_DATABASE": database or "",
            "AGENT_HANDOFF_QODER_RUNTIME": runtime or "",
        }

    destination.mkdir(parents=True, exist_ok=True)
    archive = destination / f"{manifest['name']}-qoder-v{version}-{target}.zip"
    with TemporaryDirectory(prefix="agent-handoff-qoder-build-") as temp:
        binary = Path(temp) / executable
        if target == MACOS_UNIVERSAL:
            build_macos_universal(binary, version)
        else:
            build_connector(binary, platform, architecture, version)
        binary.chmod(0o755)
        staging = Path(temp) / archive.name
        with ZipFile(staging, "w", ZIP_DEFLATED) as output:
            output.writestr(".qoder-plugin/plugin.json", json.dumps(manifest, ensure_ascii=False))
            output.writestr("mcp.json", json.dumps({"mcpServers": {manifest["name"]: connector}}))
            output.write(REPO / "LICENSE", "LICENSE")
            output.write(REPO / "THIRD_PARTY_NOTICES.md", "THIRD_PARTY_NOTICES.md")
            for license_path in sorted((REPO / "third_party_licenses").rglob("*")):
                if license_path.is_file():
                    output.write(license_path, license_path.relative_to(REPO).as_posix())
            skill = "skills/agent-handoff/SKILL.md"
            output.write(PLUGIN / skill, skill)
            info = ZipInfo(resource)
            info.create_system = 3
            info.external_attr = 0o100755 << 16
            output.writestr(info, binary.read_bytes(), compress_type=ZIP_DEFLATED)
        with ZipFile(staging) as output:
            if (bad_entry := output.testzip()) is not None:
                raise ValueError(f"Corrupt package entry {bad_entry}")
        if target == MACOS_UNIVERSAL and staging.stat().st_size > MARKETPLACE_ZIP_LIMIT:
            raise ValueError(f"macOS universal ZIP exceeds the {MARKETPLACE_ZIP_LIMIT:,}-byte marketplace upload limit")
        archive.write_bytes(staging.read_bytes())
    return archive


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("target", choices=TARGETS)
    parser.add_argument("--out", type=Path, default=REPO / "dist")
    parser.add_argument("--smoke-database", help="Absolute path to an isolated synthetic Qoder database")
    parser.add_argument("--smoke-runtime", help="Absolute path to the matching synthetic Qoder runtime")
    args = parser.parse_args()
    if args.target == MACOS_UNIVERSAL and sys.platform != "darwin":
        parser.error("macOS universal packaging requires macOS with lipo and codesign")
    if bool(args.smoke_database) != bool(args.smoke_runtime):
        parser.error("the smoke-test database and runtime must be supplied together")
    for path in [args.smoke_database, args.smoke_runtime]:
        if path and not Path(path).is_absolute():
            parser.error("smoke-test paths must be absolute")
    print(package(args.target, args.out.resolve(), args.smoke_database, args.smoke_runtime))


if __name__ == "__main__":
    main()
