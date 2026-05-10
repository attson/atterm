#!/bin/bash
# atterm auto-update install helper for Linux.
# Args: <pid> <src-archive> <dst-binary>
set -e
pid=$1
src=$2
dst=$3

log_dir="${HOME}/.local/share/atterm"
log="${log_dir}/install-${pid}.log"
mkdir -p "$log_dir"
exec 2>>"$log"

for i in {1..60}; do
  kill -0 "$pid" 2>/dev/null || break
  sleep 0.5
done

tmp=$(mktemp -d)
tar -xzf "$src" -C "$tmp"
[ -f "$tmp/atterm-desktop" ] || { echo "atterm-desktop not in archive"; exit 1; }

mv "$tmp/atterm-desktop" "$dst"
chmod +x "$dst"

# Detach the relaunched process from this script.
setsid "$dst" >/dev/null 2>&1 < /dev/null &

rm -f "$src"
rm -rf "$tmp"
