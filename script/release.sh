#!/usr/bin/env bash
# Cut a release: script/release.sh v0.0.1
# Creates + pushes the tag; GitHub Actions (release.yml) builds
# binaries and publishes the GitHub Release.
set -euo pipefail
cd "$(dirname "$0")/.."

TAG="${1:-}"
[[ "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || {
  echo "usage: script/release.sh vX.Y.Z  (e.g. script/release.sh v0.0.1)" >&2
  exit 1
}
[[ -z "$(git status --porcelain)" ]] || {
  echo "error: working tree is dirty — commit or stash first" >&2
  exit 1
}
git rev-parse "$TAG" >/dev/null 2>&1 && {
  echo "error: tag $TAG already exists" >&2
  exit 1
}

git tag -a "$TAG" -m "Release $TAG"
git push origin "$TAG"
echo "pushed $TAG — watch the release build under GitHub Actions"
