#!/usr/bin/env bash
# Build the browser/mobile OPAQUE client (github.com/bytemare/opaque -> WASM)
# into a destination dir, alongside Go's wasm_exec.js runtime loader.
#
# Usage: scripts/build-opaque-wasm.sh <out_dir>
#   <out_dir> receives opaque.wasm + wasm_exec.js (both gitignored; only the
#   Vite-emitted content-hashed copies under each bundle's assets/ land in git).
#
# Both the web bundle (web/src/shared/lib) and the mobile/capacitor bundle
# (desktop/frontend/src/lib) ship the SAME wasm so every client speaks the
# identical OPAQUE protocol bytes as the Go desktop + relay (cross-client
# interop). Keeping the build in one script keeps those bytes in lock-step.
#
# Reproducibility (the committed web-dist embed must survive CI's
# "verify embed has no drift" gate, and the .wasm content hash cascades into
# every JS chunk that ?url-imports it):
#   -trimpath       strips the build machine's absolute module-cache paths.
#   -buildvcs=false strips Go's automatic VCS stamp. Without it the binary
#                   embeds vcs.revision = git HEAD; on pull_request CI that
#                   HEAD is the EPHEMERAL refs/pull/N/merge commit whose SHA
#                   changes every run -> the wasm (and its dependents) drift.
# Both require the SAME Go version everywhere (GO_VERSION pinned exactly in
# .github/workflows/build.yml). wasm_exec.js MUST come from the toolchain that
# built the .wasm (its path moved misc/ -> lib/ across Go versions; try both).

set -euo pipefail

out_dir="${1:?usage: build-opaque-wasm.sh <out_dir>}"

# Resolve out_dir to an absolute path relative to the CALLER's cwd before we
# cd to the repo root — so callers pass a path natural to wherever they run
# (build-web.sh from root: "web/src/shared/lib"; the desktop/frontend npm
# prebuild from that dir: "src/lib").
mkdir -p "$out_dir"
out_dir=$(cd "$out_dir" && pwd)

here=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$here"

wasm_exec_src="$(go env GOROOT)/misc/wasm/wasm_exec.js"
[ -f "$wasm_exec_src" ] || wasm_exec_src="$(go env GOROOT)/lib/wasm/wasm_exec.js"

GOOS=js GOARCH=wasm go build -trimpath -buildvcs=false -ldflags="-s -w" \
  -o "$out_dir/opaque.wasm" ./cmd/opaque-wasm
cp "$wasm_exec_src" "$out_dir/wasm_exec.js"

echo "built opaque.wasm + wasm_exec.js -> $out_dir"
