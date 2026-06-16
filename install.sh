#!/bin/bash
# Usage: curl -fsSL https://raw.githubusercontent.com/AIEngineering26/promptvm-cli/main/install.sh | bash
set -euo pipefail

REPO="AIEngineering26/promptvm-cli"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Detect OS and architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)
case $ARCH in
  x86_64)        ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

if [ "$OS" != "linux" ] && [ "$OS" != "darwin" ]; then
  echo "Unsupported OS: $OS (use Homebrew on macOS or download Windows binaries from GitHub Releases)" >&2
  exit 1
fi

# Get latest version
VERSION=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": "v[^"]*' | cut -d'"' -f4)
if [ -z "$VERSION" ]; then
  echo "Failed to fetch latest version" >&2
  exit 1
fi

echo "Installing promptvm $VERSION for ${OS}/${ARCH}..."

# Download and install
FILENAME="promptvm_${VERSION#v}_${OS}_${ARCH}.tar.gz"
TMP_DIR=$(mktemp -d)
trap 'rm -rf "$TMP_DIR"' EXIT

curl -fsSL "https://github.com/$REPO/releases/download/$VERSION/$FILENAME" -o "$TMP_DIR/$FILENAME"
tar xzf "$TMP_DIR/$FILENAME" -C "$TMP_DIR" promptvm

# Install binary
if [ -w "$INSTALL_DIR" ]; then
  mv "$TMP_DIR/promptvm" "$INSTALL_DIR/promptvm"
else
  sudo mv "$TMP_DIR/promptvm" "$INSTALL_DIR/promptvm"
fi

echo "promptvm $VERSION installed to $INSTALL_DIR/promptvm"

# Best-effort: install the bundled "promptvm" agent skill for Claude Code / Codex
# so any agent session already knows how to drive PromptVM. Non-fatal; it also
# happens automatically on first run. Opt out with PROMPTVM_NO_AGENT_SKILL=1.
if [ -z "${PROMPTVM_NO_AGENT_SKILL:-}" ]; then
  "$INSTALL_DIR/promptvm" agent install >/dev/null 2>&1 \
    && echo "Installed the promptvm agent skill (manage with 'promptvm agent'; disable with PROMPTVM_NO_AGENT_SKILL=1)." \
    || true
fi
