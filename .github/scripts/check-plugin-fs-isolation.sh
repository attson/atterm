#!/usr/bin/env bash
set -euo pipefail

# Red line #11: the desktop's *local* filesystem bindings must not be reachable
# via the uplink/relay path.
#
# There are two such binding surfaces, and both are here because in each case a
# safety property is carried by a comment rather than by code:
#
#   PluginFS      — the local filesystem. Its gate (fsAccess) bounds a relay
#                   driver to allowRoots.
#
#   the SFTP      — a saved SSH host browsed over SFTP (desktop/sftp_source.go).
#   source          It deliberately does *not* apply allowRoots, because those
#                   roots are seeded from $HOME and local session cwds and mean
#                   nothing on a remote POSIX filesystem. That reasoning holds
#                   only while the caller is this machine's own webview. The
#                   comment on sftpResolvePath says a roots restriction has to
#                   come back if this source is ever exposed over the relay FS
#                   channel; this script is what makes that a guard rather than
#                   a sentence, by failing the build at the moment somebody
#                   wires it into an uplink file.
#
# Any reference from the uplink/relay side fails CI.

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$repo_root"

# The uplink/relay side. desktop/uplink*.go is the desktop's half of the relay
# protocol; desktop/remote_fs.go holds the relay FS channel's executor and its
# op switch, which is the most natural place somebody would wire a new source
# into the relay path; internal/ is everything that runs on, or speaks to, the
# relay.
search_paths=(
    desktop/uplink.go
    desktop/uplink_*.go
    desktop/remote_fs.go
    internal/
)

# The SFTP source's identifiers. Listed rather than matched by prefix so that a
# rename cannot silently empty the guard — see the staleness check below.
sftp_symbols=(
    sftpBrowser
    sftpResolvePath
    sftpDo
    dialSFTP
    ListSFTPHosts
    SFTPListDir
    SFTPFileMeta
    SFTPReadFile
    SFTPWriteFile
    SFTPCreateFile
    SFTPMkdir
    SFTPRename
    SFTPRemove
    SFTPDisconnect
)

source_file=desktop/sftp_source.go
missing=()
for sym in "${sftp_symbols[@]}"; do
    grep -qE "\b${sym}\b" "$source_file" || missing+=("$sym")
done
if [ ${#missing[@]} -ne 0 ]; then
    echo "ERROR: this guard has gone stale — these symbols are no longer in ${source_file}:"
    printf '  %s\n' "${missing[@]}"
    echo "A renamed symbol that nothing here matches is a guard that passes by accident."
    echo "Update sftp_symbols in $0 to the current names."
    exit 1
fi

sftp_pattern=$(IFS='|'; echo "\\b(${sftp_symbols[*]})\\b")

fail=0

plugin_hits=$(grep -rnE '\bPluginFS\b' \
    --include='*.go' \
    "${search_paths[@]}" 2>/dev/null || true)

if [ -n "$plugin_hits" ]; then
    echo "ERROR: PluginFS referenced outside the desktop/local binding surface:"
    echo "$plugin_hits"
    fail=1
fi

sftp_hits=$(grep -rnE "$sftp_pattern" \
    --include='*.go' \
    "${search_paths[@]}" 2>/dev/null || true)

if [ -n "$sftp_hits" ]; then
    echo "ERROR: the SFTP file source is referenced from the uplink/relay side:"
    echo "$sftp_hits"
    echo
    echo "That source applies no allowRoots restriction, on the stated grounds that"
    echo "its only caller is this machine's own webview (desktop/sftp_source.go,"
    echo "the comment on sftpResolvePath). Exposing it to a relay driver breaks that"
    echo "premise: bring the roots restriction back before wiring it up."
    fail=1
fi

if [ "$fail" -ne 0 ]; then
    exit 1
fi

echo "ok: PluginFS and the SFTP file source stay on the local binding surface"
