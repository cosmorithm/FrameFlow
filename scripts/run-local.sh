#!/bin/sh
# Run both native binaries in the foreground. Ctrl-C stops children started here.
set -eu
cd "$(dirname "$0")/.."
state=.frameflow
if [ ! -r "$state/local.env" ]; then
    echo "请按 readme.md 配置 .frameflow/local.env，或运行 ./scripts/compose-up.sh。" >&2
    exit 1
fi
set -a
. "$state/local.env"
set +a
for name in feed-api interaction-service; do
    if [ ! -x "$state/bin/$name" ]; then
        mkdir -p "$state/bin"
        go build -tags nomsgpack -o "$state/bin/$name" "./cmd/$name"
    fi
done
if [ ! -f web/dist/index.html ]; then (cd web && npm ci && npm run build); fi
mkdir -p "$state/logs" "$state/redis"
started=""
cleanup() {
    for pid in $started; do kill "$pid" 2>/dev/null || true; done
    for pid in $started; do wait "$pid" 2>/dev/null || true; done
}
trap cleanup EXIT
trap 'exit 130' INT TERM
if [ -n "${LOCAL_REDIS_PORT:-}" ] && [ -x "$state/bin/redis-server" ]; then
    if ! "$state/bin/redis-cli" -h 127.0.0.1 -p "$LOCAL_REDIS_PORT" ping >/dev/null 2>&1; then
        LC_ALL=C LANG=C "$state/bin/redis-server" --bind 127.0.0.1 --port "$LOCAL_REDIS_PORT" --save '' --appendonly no --dir "$PWD/$state/redis" >"$state/logs/redis.log" 2>&1 &
        started="$!"
    fi
fi
"$state/bin/interaction-service" >"$state/logs/interaction.log" 2>&1 &
interaction_pid=$!
started="$started $interaction_pid"
"$state/bin/feed-api" &
feed_pid=$!
started="$started $feed_pid"
sleep 1
if ! kill -0 "$interaction_pid" 2>/dev/null; then cat "$state/logs/interaction.log" >&2; exit 1; fi
wait "$feed_pid"
