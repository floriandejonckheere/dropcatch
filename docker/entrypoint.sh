#!/bin/sh
# Runs Dropcatch as the user given by PUID/PGID (default 1000:1000).
#
# When started as root, the data directory is handed to that user and
# privileges are dropped with su-exec. When started as a non-root user
# (compose "user:" or docker --user), Dropcatch simply runs as that user.
set -e

if [ "$(id -u)" != "0" ]; then
  exec dropcatch "$@"
fi

PUID="${PUID:-1000}"
PGID="${PGID:-1000}"
case "$PUID:$PGID" in
  *[!0-9:]*|:*|*:)
    echo "dropcatch: PUID and PGID must be numeric (got PUID=$PUID PGID=$PGID)" >&2
    exit 1
    ;;
esac
if [ "$PUID" = "0" ]; then
  echo "dropcatch: warning: running as root (PUID=0) is not recommended" >&2
fi

data="${DROPCATCH_DATA_DIR:-/data}"
mkdir -p "$data"
if [ "$(stat -c %u:%g "$data")" != "$PUID:$PGID" ]; then
  chown -R "$PUID:$PGID" "$data"
fi

exec su-exec "$PUID:$PGID" dropcatch "$@"
