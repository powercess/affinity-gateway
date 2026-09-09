#!/usr/bin/env bash
set -euo pipefail

mode_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
export TEST_PORT="${TEST_PORT:-18243}"
export CONSOLE_PORT="${CONSOLE_PORT:-18242}"
export CONSOLE_BIND="${CONSOLE_BIND:-0.0.0.0}"
export AFFINITY_CONSOLE_PASSWORD="${AFFINITY_CONSOLE_PASSWORD:-affinity-local-console-development}"

compose() {
    (cd -- "$mode_root" && docker compose -p caddy-affinity-console \
        -f deploy/compose.test.yaml -f deploy/compose.console.yaml "$@")
}

fail() { echo "$*" >&2; return 1; }

main() {
    local action="${1:-status}"
    if (($#)); then shift; fi
    local arg
    local -a services=()
    case "$action" in
        status)
            (($# == 0)) || { fail 'status 不接受参数'; return 1; }
            compose ps -a
            ;;
        up) compose up -d "$@" ;;
        down)
            (($# == 0)) || { fail 'down 不接受参数，不删除数据卷'; return 1; }
            compose down
            ;;
        reload)
            if (($# == 1)) && [[ "$1" == config ]]; then
                compose exec -T affinity-gateway caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile || return
                compose exec -T affinity-gateway caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
                return
            fi
            if (($# == 0)); then set -- web gateway; fi
            for arg in "$@"; do
                case "$arg" in
                    web) services+=(console) ;;
                    gateway) services+=(affinity-gateway) ;;
                    new-api) services+=(new-api) ;;
                    *) fail 'reload 支持 web、gateway、new-api，或单独 config'; return 1 ;;
                esac
            done
            compose build "${services[@]}" || return
            compose up -d --no-deps --no-build "${services[@]}"
            ;;
        test)
            (($# == 0)) || { fail 'test 不接受参数'; return 1; }
            compose run --rm --no-deps -e AFFINITY_CONSOLE_PASSWORD probe python /test/console_check.py
            ;;
        logs)
            for arg in "$@"; do
                case "$arg" in
                    web) services+=(console) ;;
                    gateway) services+=(affinity-gateway) ;;
                    *) services+=("$arg") ;;
                esac
            done
            compose logs --tail 100 -f "${services[@]}"
            ;;
        docker)
            (($# > 0)) || { fail '需要 Compose 子命令，例如 just docker up'; return 1; }
            [[ "$1" != -* ]] || { fail '不接受项目级参数'; return 1; }
            for arg in "$@"; do
                case "$arg" in
                    --project-name|--project-name=*|--file|--file=*|--project-directory|--project-directory=*|--env-file|--env-file=*)
                        fail '项目与配置文件由脚本固定'; return 1 ;;
                esac
                if [[ "$1" == down && ( "$arg" == --volumes || "$arg" =~ ^-[^-]*v ) ]]; then
                    fail '脚本不删除数据卷，请核对卷名后单独操作'; return 1
                fi
            done
            if [[ "$1" == up ]]; then shift; main up "$@"; else compose "$@"; fi
            ;;
        *) fail "未知命令：$action；运行 just 查看列表" ;;
    esac
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then main "$@"; fi
