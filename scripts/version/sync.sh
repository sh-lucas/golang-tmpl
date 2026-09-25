#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CHANGELOG_FILE="$ROOT_DIR/specs/changelog.md"
VERSION_FILE="$ROOT_DIR/VERSION"
MODE=sync
FORCE=0

for arg in "$@"; do
  case "$arg" in
    --sync) MODE=sync ;;
    --check) MODE=check ;;
    --check-new) MODE=check-new ;;
    --force) FORCE=1 ;;
    *) echo "Unknown argument: $arg"; exit 1 ;;
  esac
done

if [[ ! -f "$CHANGELOG_FILE" || ! -f "$VERSION_FILE" ]]; then
  echo "Changelog ($CHANGELOG_FILE) or VERSION ($VERSION_FILE) not found." >&2
  exit 1
fi

CHANGELOG_VERSION=$(sed -nE 's/^## v?([0-9]+\.[0-9]+\.[0-9]+)$/\1/p' "$CHANGELOG_FILE" | head -n 1)
CURRENT_VERSION=$(tr -d '[:space:]' < "$VERSION_FILE")

if [[ -z "$CHANGELOG_VERSION" || -z "$CURRENT_VERSION" ]]; then
  echo "Could not read a stable version from changelog.md and VERSION." >&2
  exit 1
fi

if [[ "$MODE" == check ]]; then
  [[ "$CHANGELOG_VERSION" == "$CURRENT_VERSION" ]] || { echo "Version mismatch: changelog $CHANGELOG_VERSION, VERSION $CURRENT_VERSION" >&2; exit 1; }
  exit 0
fi

if [[ "$MODE" == check-new ]]; then
  [[ "$CHANGELOG_VERSION" != "$CURRENT_VERSION" ]] && [[ "$(printf '%s\n' "$CURRENT_VERSION" "$CHANGELOG_VERSION" | sort -V | tail -n1)" == "$CHANGELOG_VERSION" ]] || {
    echo "Changelog must declare a version newer than VERSION ($CURRENT_VERSION); found $CHANGELOG_VERSION." >&2
    exit 1
  }
  exit 0
fi

if [[ "$FORCE" -eq 0 ]]; then
  CURRENT_BRANCH=$(git -C "$ROOT_DIR" branch --show-current 2>/dev/null || true)
  [[ "$CURRENT_BRANCH" == develop ]] || exit 0
fi

[[ "$CHANGELOG_VERSION" == "$CURRENT_VERSION" ]] && exit 0

echo "$CHANGELOG_VERSION" > "$VERSION_FILE"
git -C "$ROOT_DIR" add "$VERSION_FILE"
echo "Synchronized release version to $CHANGELOG_VERSION."
