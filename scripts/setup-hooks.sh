#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
REPO_ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

if ! git -C "$REPO_ROOT" rev-parse --git-dir >/dev/null 2>&1; then
  echo "ERROR: $REPO_ROOT is not a git repository."
  exit 1
fi

SOURCE_HOOK="$REPO_ROOT/scripts/git/commit-msg"
if [ ! -f "$SOURCE_HOOK" ]; then
  echo "ERROR: Source hook not found: $SOURCE_HOOK"
  exit 1
fi

HOOKS_DIR="$(git -C "$REPO_ROOT" rev-parse --git-path hooks)"
TARGET_HOOK="$HOOKS_DIR/commit-msg"

mkdir -p "$HOOKS_DIR"
cp "$SOURCE_HOOK" "$TARGET_HOOK"
chmod +x "$TARGET_HOOK"

echo "Installed commit-msg hook:"
echo "  source: $SOURCE_HOOK"
echo "  target: $TARGET_HOOK"
