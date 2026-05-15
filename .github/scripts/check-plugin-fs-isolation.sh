#!/usr/bin/env bash
set -euo pipefail

# Red line #11: PluginFS bindings must not be reachable via the uplink/relay
# path. Any reference outside desktop/ (excluding test files) fails CI.

hits=$(grep -rn 'PluginFS\b' \
    --include='*.go' \
    desktop/uplink.go \
    desktop/uplink_*.go \
    internal/ 2>/dev/null || true)

if [ -n "$hits" ]; then
    echo "ERROR: PluginFS referenced outside the desktop/local binding surface:"
    echo "$hits"
    exit 1
fi

echo "ok: PluginFS isolation preserved"
