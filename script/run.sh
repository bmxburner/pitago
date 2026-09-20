#!/usr/bin/env bash
# Run openpi from source: script/run.sh [flags...]
set -euo pipefail
cd "$(dirname "$0")/.."
exec go run ./src "$@"
