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

# --web points at the served static root. web/index.html imports /src/main-web.ts
# which only exists after vite build; use the built assets under web/dist.
if [[ ! -d web/dist ]]; then
	echo "web/dist not found — run 'cd web && npm install && npm run build' first," >&2
	echo "or drop --web from the command below to use the embedded fs (production default)." >&2
	exit 1
fi

echo "atterm-relay: addr=$ADDR"
exec go run ./cmd/atterm-relay --addr "$ADDR" --web web/dist --dev-insecure
