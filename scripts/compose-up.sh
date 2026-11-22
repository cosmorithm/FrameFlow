#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
if [ ! -f .env ]; then
    umask 077
    tmp=$(mktemp ./.env.XXXXXX)
    trap 'rm -f "$tmp"' EXIT
    for key in MYSQL_ROOT_PASSWORD FEED_DB_PASSWORD INTERACTION_DB_PASSWORD PLAY_SECRET; do
        value=$(openssl rand -hex 32)
        printf '%s=%s\n' "$key" "$value" >> "$tmp"
    done
    ln "$tmp" .env || test -f .env
    rm -f "$tmp"
    trap - EXIT
fi
exec docker compose up --build -d
