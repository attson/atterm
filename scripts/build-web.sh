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
