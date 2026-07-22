#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

if ! getent group app >/dev/null 2>&1; then
    addgroup -S -g "$PGID" app
fi

if ! id app >/dev/null 2>&1; then
    adduser -S -u "$PUID" -G app app
fi

exec su-exec app "$@"