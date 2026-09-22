#!/usr/bin/env bash
# Install pitago without sudo: script/install.sh [asset-url-or-tag]
# Defaults to ~/.local/bin (user-writable, so `pitago --update` needs no sudo).
set -euo pipefail
cd "$(dirname "$0")/.."
DEST="${DEST:-$HOME/.local/bin/pitago}"
mkdir -p "$(dirname "$DEST")"
if [[ "${1:-}" == http* ]]; then
  URL="$1"
elif [[ "${1:-}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"; ARCH="$(uname -m)"
  [[ "$ARCH" == x86_64 ]] && ARCH=amd64; [[ "$ARCH" == aarch64 ]] && ARCH=arm64
  [[ "$OS" == darwin ]] || OS=linux
  URL="https://github.com/cavaldos/pitago/releases/download/$1/pitago-$OS-$ARCH"
else
  OS="$(uname -s | tr '[:upper:]' '[:lower:]')"; ARCH="$(uname -m)"
  [[ "$ARCH" == x86_64 ]] && ARCH=amd64; [[ "$ARCH" == aarch64 ]] && ARCH=arm64
  [[ "$OS" == darwin ]] || OS=linux
  URL="https://github.com/cavaldos/pitago/releases/latest/download/pitago-$OS-$ARCH"
fi
curl -L -o "$DEST" "$URL"
chmod +x "$DEST"
case ":$PATH:" in *":$HOME/.local/bin:"*) ;; *)
  echo "note: add to PATH: export PATH=\"\$HOME/.local/bin:\$PATH\"" >&2 ;;
esac
echo "installed $DEST"
"$DEST" --version
