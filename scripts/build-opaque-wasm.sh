#!/usr/bin/env bash
# Build the browser/mobile OPAQUE client (github.com/bytemare/opaque -> WASM)
# into a destination dir, alongside Go's wasm_exec.js runtime loader.
#
# Usage: [ATTERM_PINNED_GO=1] scripts/build-opaque-wasm.sh <out_dir>
#   <out_dir> receives opaque.wasm + wasm_exec.js (both gitignored; only the
#   Vite-emitted content-hashed copies under each bundle's assets/ land in git).
#   ATTERM_PINNED_GO=1 additionally requires the exact CI Go version — set it
#   only when the output feeds the drift-gated web-dist embed (build-web.sh
#   does; the mobile prebuild deliberately does not, per AGENTS.md).
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
# .github/workflows/build.yml) — enforced below, not merely assumed.
# wasm_exec.js MUST come from the toolchain that built the .wasm (its path
# moved misc/ -> lib/ across Go versions; try both).

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

# The compiled .wasm differs between Go PATCH releases (1.23.8 vs 1.23.12
# produce different bytes from identical sources), its content hash lands in
# the asset filename, and that name cascades into every chunk that ?url-imports
# it — so a build on the wrong patch rewrites all of web-dist/ and fails CI's
# "verify embed has no drift" gate. go.mod's `go 1.23.0` is only a FLOOR and
# does not pin this; adding a `toolchain` directive would not fix it either,
# because GOTOOLCHAIN-downloaded toolchain modules ship without
# misc/wasm/wasm_exec.js. So check the version the same way build-web.sh
# checks Node: against the one value CI pins.
#
# Opt-in, because only the web-dist embed is drift-gated. The mobile bundle's
# wasm never lands in git (only vite's hashed copy under mobile/www does), so
# demanding the exact patch there would break `npm run build:capacitor` for
# every contributor without buying any reproducibility.
if [ "${ATTERM_PINNED_GO:-0}" = "1" ]; then
  workflow=.github/workflows/build.yml
  expected_go=$(sed -nE "s/^[[:space:]]*GO_VERSION:[[:space:]]*'?([0-9][0-9.]*)'?[[:space:]]*$/\1/p" "$workflow" | head -1)
  if [ -z "$expected_go" ]; then
    echo "build-opaque-wasm.sh: could not read GO_VERSION from $workflow." >&2
    echo "Reproducibility cannot be verified; refusing to build a wasm that may drift." >&2
    exit 1
  fi
  actual_go=$(go env GOVERSION | sed -E 's/^go//')
  if [ "$expected_go" != "$actual_go" ]; then
    echo "build-opaque-wasm.sh: Go version mismatch." >&2
    echo "  Expected: go${expected_go} (per GO_VERSION in $workflow)" >&2
    echo "  Actual:   go${actual_go}" >&2
    echo "The embedded web-dist assets are content-hashed from this binary, so a" >&2
    echo "different patch release rewrites all of web-dist/ and fails CI's" >&2
    echo "\"verify embed has no drift\" gate. Install the pinned SDK:" >&2
    echo "  go install golang.org/dl/go${expected_go}@latest && go${expected_go} download" >&2
    echo "  export PATH=\"\$HOME/sdk/go${expected_go}/bin:\$PATH\"" >&2
    exit 1
  fi
fi

wasm_exec_src="$(go env GOROOT)/misc/wasm/wasm_exec.js"
[ -f "$wasm_exec_src" ] || wasm_exec_src="$(go env GOROOT)/lib/wasm/wasm_exec.js"

GOOS=js GOARCH=wasm go build -trimpath -buildvcs=false -ldflags="-s -w" \
  -o "$out_dir/opaque.wasm" ./cmd/opaque-wasm
# rm -f first: an existing wasm_exec.js may lack the owner write bit
# (older Go SDKs shipped it read-only, and plain `cp` inherits that on
# first copy), which would make subsequent `cp` fail with EACCES.
rm -f "$out_dir/wasm_exec.js"
cp "$wasm_exec_src" "$out_dir/wasm_exec.js"

echo "built opaque.wasm + wasm_exec.js -> $out_dir"
