#!/usr/bin/env bash
# scripts/dev.sh — start atterm-relay in the foreground for development.
#
# Usage:
#   ATTERM_TOKEN=dev ./scripts/dev.sh
#
# In another terminal:
#   ATTERM_TOKEN=dev go run ./cmd/atterm-agent -- bash
#
# Then open http://localhost:8080?token=dev
set -euo pipefail
cd "$(dirname "$0")/.."

: "${ATTERM_TOKEN:=dev}"
export ATTERM_TOKEN

ADDR="${ATTERM_ADDR:-:8080}"

echo "atterm-relay: addr=$ADDR token=$ATTERM_TOKEN"
exec go run ./cmd/atterm-relay --addr "$ADDR" --web web
