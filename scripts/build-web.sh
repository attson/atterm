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
  # Build the browser OPAQUE client (github.com/bytemare/opaque -> WASM) so the
  # web speaks the SAME protocol bytes as the desktop + relay (cross-client
  # interop). The web imports opaque.wasm + Go's wasm_exec.js via Vite ?url;
  # both are gitignored — only the Vite-emitted hashed copies in
  # web-dist/assets/ land in git. The mobile/capacitor bundle builds the SAME
  # wasm into desktop/frontend/src/lib via this same script. Reproducibility
  # flags (-trimpath -buildvcs=false) are documented in build-opaque-wasm.sh.
  ./scripts/build-opaque-wasm.sh web/src/shared/lib

  # Clear Vite's dep cache so the build is reproducible. A WARM cache (left by a
  # prior `vite build`/`vite dev`) produces byte-different chunks than a COLD
  # one — that divergence broke the "verify embed has no drift" gate (the
  # committed web-dist must match a fresh CI build). Always build cold.
  rm -rf web/node_modules/.vite
  (cd web && npm run build)
  if [ -d web/dist ] && [ -n "$(ls -A web/dist 2>/dev/null)" ]; then
    rsync -a web/dist/ "$dist/"
    find "$dist" -type f \( \
      -name '*.html' -o \
      -name '*.js' -o \
      -name '*.css' -o \
      -name '*.webmanifest' \
    \) -exec perl -pi -e 's/[ \t]+$//' {} +
  fi
fi

echo "web-dist synced from web/dist/ ($(find "$dist" -type f | wc -l | tr -d ' ') files)"
