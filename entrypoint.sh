#!/bin/sh
set -e

mkdir -p /app/data

if [ "$(id -u)" = "0" ]; then
    chown -R 1000:1000 /app/data 2>/dev/null || true
    chmod 775 /app/data 2>/dev/null || true
    exec su-exec 1000:1000 /bin/telegram-monitor "$@"
fi

exec /bin/telegram-monitor "$@"
