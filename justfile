set positional-arguments

# 列出开发命令
default:
    @just --list

# 构建并后台启动完整开发环境
up *args:
    @bash scripts/mode.sh up "$@"

# 查看容器状态
status:
    @bash scripts/mode.sh status

# 重建 web/gateway/new-api；config 仅验证并热加载配置
reload *targets:
    @bash scripts/mode.sh reload "$@"

# 在已启动环境执行端到端测试
test:
    @bash scripts/mode.sh test

# 跟随日志，可指定 web、gateway、new-api
logs *services:
    @bash scripts/mode.sh logs "$@"

# 停止开发环境，保留数据卷
down:
    @bash scripts/mode.sh down

# 调用本套 Docker Compose，例如 just docker up
docker +args:
    @bash scripts/mode.sh docker "$@"

# 测试管理脚本，不访问 Docker
test-scripts:
    @bash scripts/mode_test.sh

# 管理真实供应商环境（独立于模拟测试环境）
live action="status":
    @bash scripts/live.sh "$1"
