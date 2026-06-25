#!/bin/sh
#
# Entrypoint for the atterm-relay docker image. Runs as root, takes
# ownership of the bind-mounted persistence directory so the relay can
# create users.db (the SQLite backend) inside it, then drops
# privileges to the unprivileged 'atterm' user before exec'ing the
# relay binary.
#
# The relay's persistence dir is host-mounted from docker-compose, and
# when host_dir is auto-created by Docker it lands owned by host root.
# Without the chown step, SQLite open fails with SQLITE_CANTOPEN
# (modernc.org/sqlite reports this as "unable to open database file:
# out of memory (14)"). See PR that added this script for details.
#
# If the image is started as a non-root user (docker run -u 1000), we
# skip the chown and just exec — the caller is responsible for
# ensuring the bind-mount is writable by that user.

set -e

if [ "$(id -u)" = "0" ]; then
    dir="${ATTERM_RELAY_CONFIG_DIR:-/etc/atterm}"
    if [ -d "$dir" ]; then
        chown -R atterm:atterm "$dir"
    fi
    exec su-exec atterm:atterm /usr/local/bin/atterm-relay "$@"
fi

exec /usr/local/bin/atterm-relay "$@"
