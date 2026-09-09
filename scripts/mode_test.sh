#!/usr/bin/env bash
set -euo pipefail
source "$(dirname -- "${BASH_SOURCE[0]}")/mode.sh"

calls=()
compose() {
    local command
    printf -v command '%q ' "$@"
    calls+=("${command% }")
}
expect() { [[ "$1" == "$2" ]] || { echo "Expected: $2; actual: $1" >&2; exit 1; }; }

main status
expect "${calls[0]}" 'ps -a'
calls=(); main docker up
expect "${calls[0]}" 'up -d'
calls=(); main up --build
expect "${calls[0]}" 'up -d --build'
calls=(); main reload
expect "${calls[0]}" 'build console affinity-gateway'
expect "${calls[1]}" 'up -d --no-deps --no-build console affinity-gateway'
calls=(); main reload web
expect "${calls[0]}" 'build console'
calls=(); main reload config
expect "${calls[0]}" 'exec -T affinity-gateway caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile'
expect "${calls[1]}" 'exec -T affinity-gateway caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile'
calls=(); main test
expect "${calls[0]}" 'run --rm --no-deps -e AFFINITY_CONSOLE_PASSWORD probe python /test/console_check.py'
calls=(); main docker logs --since '10 minutes ago'
expect "${calls[0]}" 'logs --since 10\ minutes\ ago'
calls=(); main down
expect "${calls[0]}" 'down'
for arg in -v --volumes; do
    if (main docker down "$arg" >/dev/null 2>&1); then echo 'volume guard failed' >&2; exit 1; fi
done
if (main reload postgres >/dev/null 2>&1); then exit 1; fi
if (main docker up --project-name=other >/dev/null 2>&1); then exit 1; fi
calls=()
compose() { calls+=("$1"); return 7; }
if main reload web; then echo 'build error not propagated' >&2; exit 1; else expect "$?" 7; fi
expect "${#calls[@]}" 1
echo 'PASS: dispatch, argument boundaries, reload scope, volume guards, failure propagation'
