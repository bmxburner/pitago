#!/usr/bin/env bash
# Build pitago: script/build.sh
# Env: VERSION (default: git tag/commit), OUT (default: bin/pitago),
#      GOOS/GOARCH (default: host).
set -euo pipefail
cd "$(dirname "$0")/.."
VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
OUT="${OUT:-bin/pitago}"
mkdir -p "$(dirname "$OUT")"
go vet ./...
CGO_ENABLED=0 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$OUT" ./src
echo "built $OUT ($VERSION)"
