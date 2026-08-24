#!/bin/sh
# agent-handoff installer — downloads a release binary from GitHub Releases.
# Usage: curl -fsSL https://raw.githubusercontent.com/DavidDingXu/agent-handoff/main/install.sh | sh
# Override with REPO=owner/repo and VERSION=v0.3.0. Installs to ~/.local/bin
# by default; override with INSTALL_DIR.

set -eu

REPO="${REPO:-DavidDingXu/agent-handoff}"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-$HOME/.local/bin}"

# --- detect platform ---
OS="$(uname -s)"
ARCH="$(uname -m)"
case "$OS" in
  Darwin) OS="darwin" ;;
  Linux) OS="linux" ;;
  *) echo "error: unsupported OS $OS" >&2; exit 1 ;;
esac
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "error: unsupported architecture $ARCH" >&2; exit 1 ;;
esac

# --- resolve version ---
if [ "$VERSION" = "latest" ]; then
  VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
  if [ -z "$VERSION" ]; then
    echo "error: could not resolve the latest release of $REPO" >&2
    exit 1
  fi
fi

# --- download ---
ASSET_VERSION="${VERSION#v}"
ASSET="agent-handoff-$ASSET_VERSION-$OS-$ARCH.tar.gz"
URL="https://github.com/$REPO/releases/download/$VERSION/$ASSET"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "==> downloading agent-handoff $VERSION ($OS/$ARCH)"
curl -fsSL "$URL" -o "$TMP/agent-handoff.tar.gz"

echo "==> verifying checksum"
CHECKSUMS_URL="https://github.com/$REPO/releases/download/$VERSION/checksums.txt"
curl -fsSL "$CHECKSUMS_URL" -o "$TMP/checksums.txt"
WANT="$(awk -v f="$ASSET" '$2 == f {print $1}' "$TMP/checksums.txt")"
if [ -z "$WANT" ]; then
  echo "error: checksum entry missing for $ASSET" >&2
  exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
  GOT="$(sha256sum "$TMP/agent-handoff.tar.gz" | awk '{print $1}')"
else
  GOT="$(shasum -a 256 "$TMP/agent-handoff.tar.gz" | awk '{print $1}')"
fi
if [ "$GOT" != "$WANT" ]; then
  echo "error: checksum mismatch (want $WANT, got $GOT)" >&2
  exit 1
fi
echo "    checksum ok"

echo "==> installing to $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
tar -xzf "$TMP/agent-handoff.tar.gz" -C "$TMP"
mv "$TMP/agent-handoff" "$INSTALL_DIR/agent-handoff"
chmod +x "$INSTALL_DIR/agent-handoff"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *) echo "note: $INSTALL_DIR is not on your PATH" ;;
esac

echo "==> done: $("$INSTALL_DIR/agent-handoff" version)"
