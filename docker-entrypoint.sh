#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

# --- Group ---
# Check if the specified GID is already in use by another group
EXISTING_GROUP=$(getent group "$PGID" | cut -d: -f1)
if [ -n "$EXISTING_GROUP" ]; then
    # GID already taken, reuse the existing group
    GROUP_NAME="$EXISTING_GROUP"
else
    # GID is free, create app group
    addgroup -g "$PGID" app
    GROUP_NAME="app"
fi

# --- User ---
# Check if the specified UID is already in use by another user
EXISTING_USER=$(getent passwd "$PUID" | cut -d: -f1)
if [ -n "$EXISTING_USER" ]; then
    # UID already taken, reuse the existing user
    USER_NAME="$EXISTING_USER"
else
    # UID is free, create app user
    adduser -u "$PUID" -G "$GROUP_NAME" -D -s /bin/sh app
    USER_NAME="app"
fi

# Fix TUN device permissions so non-root users can access it
if [ -c /dev/net/tun ]; then
    chmod 0666 /dev/net/tun || true
fi

exec su-exec "$USER_NAME" "$@"
