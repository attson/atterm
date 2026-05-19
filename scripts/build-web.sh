#!/usr/bin/env bash
# Build the embedded web assets for atterm-relay.
#
# Output: internal/relay/web-dist/ is overwritten to match web/dist/
# (Vite multi-entry MPA output, including the service worker, manifest,
# and content-hashed assets emitted by vite-plugin-pwa).
#
# CI relies on this script being deterministic. Do not introduce
# timestamps, environment-dependent paths, or unpinned tooling.

set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$here"

# Vite content-hash output is sensitive to the Node version (the same
# sources produce different chunk filenames on Node 20 vs 24). CI pins
# Node 20 in .github/workflows/build.yml; the repo's .nvmrc also pins 20.
# If the local Node major doesn't match, abort early — otherwise the
# resulting web-dist/ will drift from what CI produces and the CI
# "verify embed has no drift" job will fail on the next PR.
expected_node_major=$(cat .nvmrc | tr -d '[:space:]')
actual_node_major=$(node -v | sed -E 's/^v([0-9]+).*/\1/')
if [ "$expected_node_major" != "$actual_node_major" ]; then
  echo "build-web.sh: Node version mismatch." >&2
  echo "  Expected: v${expected_node_major}.x (per .nvmrc and CI build.yml NODE_VERSION)" >&2
  echo "  Actual:   $(node -v)" >&2
  echo "Use 'nvm use' (or 'nvm install ${expected_node_major}') before running this script." >&2
  exit 1
fi

dist=internal/relay/web-dist

# Wipe the embed dir without removing the .gitkeep anchor.
mkdir -p "$dist"
find "$dist" -mindepth 1 -not -name '.gitkeep' -delete

if [ -f web/package.json ] && [ -d web/node_modules ]; then
  (cd web && npm run build)
  if [ -d web/dist ] && [ -n "$(ls -A web/dist 2>/dev/null)" ]; then
    rsync -a web/dist/ "$dist/"
  fi
fi

echo "web-dist synced from web/dist/ ($(find "$dist" -type f | wc -l | tr -d ' ') files)"
