#!/usr/bin/env bash
# scripts/build-hook-binary.sh — produce desktop/hookinstall/atterm-hook
# for go:embed. -trimpath + -s -w make the output reproducible so the
# embedded sha8 only changes when cmd/atterm-hook source changes.
set -euo pipefail
cd "$(dirname "$0")/.."

OUT="desktop/hookinstall/atterm-hook"
mkdir -p "$(dirname "$OUT")"
go build -trimpath -ldflags='-s -w' -o "$OUT" ./cmd/atterm-hook
echo "built $OUT ($(wc -c < "$OUT") bytes)"
