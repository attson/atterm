#!/usr/bin/env bash
# scripts/dev.sh — start atterm-relay in the foreground for development.
#
# Usage:
#   ./scripts/dev.sh
#
# Then open http://localhost:8080/login.html and sign in with a bootstrap
# admin account (set ATTERM_BOOTSTRAP_ADMIN_EMAIL / _PASSWORD or use
# --dev-insecure on loopback to skip the strength + Origin checks).
set -euo pipefail
cd "$(dirname "$0")/.."

ADDR="${ATTERM_ADDR:-:8080}"

echo "atterm-relay: addr=$ADDR"
exec go run ./cmd/atterm-relay --addr "$ADDR" --web web --dev-insecure
