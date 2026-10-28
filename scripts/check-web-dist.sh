#!/usr/bin/env bash
# Verify that the committed relay embed exactly matches a fresh deterministic
# browser build. This is deliberately side-effect free so release tooling and
# hooks can run it without rewriting the worktree.

set -euo pipefail

repo_root=$(git rev-parse --show-toplevel)
cd "$repo_root"

expected_node_major=$(tr -d '[:space:]' < .nvmrc)
actual_node_major=$(node -v 2>/dev/null | sed -E 's/^v([0-9]+).*/\1/' || true)
if [ "$actual_node_major" != "$expected_node_major" ]; then
  echo "[check-web-dist] Node ${expected_node_major}.x is required (current: ${actual_node_major:-not installed})." >&2
  echo "[check-web-dist] Run: nvm use ${expected_node_major}" >&2
  exit 1
fi

tmpdir=$(mktemp -d -t atterm-webdist-check-XXXX)
trap 'rm -rf "$tmpdir"' EXIT

current_dist="$tmpdir/current"
fresh_dist="$tmpdir/fresh"
cp -R internal/relay/web-dist "$current_dist"
mkdir -p "$fresh_dist"
touch "$fresh_dist/.gitkeep"

if ! ATTERM_WEB_DIST_DIR="$fresh_dist" ./scripts/build-web.sh; then
  echo "[check-web-dist] deterministic web build failed." >&2
  exit 1
fi

if ! diff -qr "$current_dist" "$fresh_dist" >/dev/null 2>&1; then
  echo "[check-web-dist] internal/relay/web-dist is stale." >&2
  echo "[check-web-dist] Run ./scripts/build-web.sh, commit the generated files, then retry the release." >&2
  diff -qr "$current_dist" "$fresh_dist" || true
  exit 1
fi

echo "[check-web-dist] embedded web assets match a fresh build."
