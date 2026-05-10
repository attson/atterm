#!/bin/bash
# atterm auto-update install helper for macOS.
# Args: <pid> <src-archive> <dst-bundle>
set -e
pid=$1
src=$2
dst=$3

log_dir="${HOME}/Library/Logs/atterm"
log="${log_dir}/install-${pid}.log"
mkdir -p "$log_dir"
exec 2>>"$log"

# Wait for parent atterm to exit (cap 30s, 0.5s poll).
for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
unzip -q "$src" -d "$tmp"
new=$(find "$tmp" -maxdepth 1 -name "*.app" | head -1)
[ -d "$new" ] || { echo "no .app bundle in archive"; exit 1; }

trash="${dst}.old.$$"
mv "$dst" "$trash"
mv "$new" "$dst"
rm -rf "$trash"

# Strip macOS quarantine xattr so Gatekeeper doesn't re-prompt the user.
xattr -dr com.apple.quarantine "$dst" 2>/dev/null || true

open "$dst"

rm -f "$src"
rm -rf "$tmp"
