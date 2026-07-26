#!/bin/sh
set -e

PUID=${PUID:-1000}
PGID=${PGID:-1000}

# --- Group ---
EXISTING_GROUP=$(getent group "$PGID" | cut -d: -f1 || true)

if [ -n "$EXISTING_GROUP" ]; then
    GROUP_NAME="$EXISTING_GROUP"
else
    addgroup -g "$PGID" app
    GROUP_NAME="app"
fi

# --- User ---
EXISTING_USER=$(getent passwd "$PUID" | cut -d: -f1 || true)

if [ -n "$EXISTING_USER" ]; then
    USER_NAME="$EXISTING_USER"
else
    adduser -u "$PUID" -G "$GROUP_NAME" -D -s /bin/sh app
    USER_NAME="app"
fi

# --- TUN permission ---
if [ -c /dev/net/tun ]; then
    chmod 0666 /dev/net/tun || true
fi

echo "Running as: $(whoami)"
echo "Target user: $USER_NAME:$GROUP_NAME"

# Check NET_ADMIN
CAP_NET_ADMIN=0
CAP=$(awk '/CapEff/ {print $2}' /proc/self/status)

if [ -n "$CAP" ] && [ $((0x$CAP & (1 << 12))) -ne 0 ]; then
    CAP_NET_ADMIN=1
fi

if [ "$CAP_NET_ADMIN" = "1" ]; then
    echo "CAP_NET_ADMIN available, keeping capability"

    exec setpriv \
        --reuid="$USER_NAME" \
        --regid="$GROUP_NAME" \
        --init-groups \
        --inh-caps=+net_admin \
        --ambient-caps=+net_admin \
        "$@"
else
    echo "CAP_NET_ADMIN unavailable, running without it"

    exec setpriv \
        --reuid="$USER_NAME" \
        --regid="$GROUP_NAME" \
        --init-groups \
        "$@"
fi
