#!/usr/bin/env bash
# Build the embedded web assets for atterm-relay.
#
# Output: internal/relay/web-dist/ is overwritten to match
#   - web/legacy/ (verbatim copy; the existing vanilla site)
#   - web/dist/   (Vite output; added in PR-B and later)
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

# Layer 1: copy the legacy vanilla site so the relay keeps serving the
# UI users have today, byte-identical, while later PRs replace entries
# one at a time.
if [ -d web/legacy ]; then
  rsync -a --exclude='*.test.mjs' web/legacy/ "$dist/"
fi

# Layer 2 (PR-B onward): overlay Vite output on top of legacy. The
# build is skipped when node_modules is absent (caller forgot npm ci)
# or when web/dist is empty (PR-A placeholder). The _placeholder*
# artifact from PR-A is filtered out so it never lands in the embed.
if [ -f web/package.json ] && [ -d web/node_modules ]; then
  (cd web && npm run build)
  if [ -d web/dist ] && [ -n "$(ls -A web/dist 2>/dev/null)" ]; then
    rsync -a --exclude='_placeholder*' web/dist/ "$dist/"
  fi
fi

echo "web-dist synced from web/legacy/ ($(find "$dist" -type f | wc -l | tr -d ' ') files)"
