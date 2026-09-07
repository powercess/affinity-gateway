#!/usr/bin/env bash
set -euo pipefail
live_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$live_root"
if (($# > 1)); then
    echo '只接受一个操作参数，不支持删除数据卷' >&2
    exit 1
fi
live_compose() {
    docker compose --env-file deploy/live/.env -p caddy-affinity-live -f deploy/compose.live.yaml "$@"
}
case "${1:-status}" in
    up)
        live_compose up -d --build
        ;;
    reload)
        live_compose exec -T affinity-gateway caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
        live_compose exec -T affinity-gateway caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
        ;;
    status) live_compose ps -a ;;
    logs) live_compose logs --tail 100 -f ;;
    down) live_compose down ;;
    *) echo '支持：just live up|reload|status|logs|down' >&2; exit 1 ;;
esac
