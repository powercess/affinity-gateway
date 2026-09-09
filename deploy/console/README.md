# Affinity Gateway · 容器联调环境

## 日常命令

仓库根目录使用 just + Bash 管理 Docker Compose，不依赖宿主机 Node 或 Bun：

```bash
just docker up        # 复用已有镜像并后台启动，也可用 just up
just up --build       # 需要时检查并重建全部开发镜像
just status           # 状态，初始化完成时 seed 应为 Exited (0)
just reload           # 重建前端和网关；不重启 new-api、数据库
just reload web       # 仅前端
just reload gateway   # 仅网关；内存观测记录清空
just reload new-api   # 更新 new-api 补丁后重建
just reload config    # 验证后热加载 Caddy 配置
just test             # seed 成功结束后，执行完整容器链路检查
just logs gateway     # 跟随日志，Ctrl-C 退出
just down             # 停止容器，保留数据卷
```

运行 `just` 列出命令。`just docker` 传递 Compose 子命令，但固定项目与配置文件，拒绝 `down -v`，避免误删绑定与数据库。`just test` 会产生真实的模拟供应商请求和消费日志，不是只读检查。前端自己的工具链保留在 `web/`，容器内完成 Node 构建。

`just up` 复用本地已有镜像；首次缺少镜像时仍需下载或构建。修改源码后用
`just reload web`、`just reload gateway` 或 `just reload new-api` 更新对应服务，
也可用 `just up --build` 构建全部服务。前端日常开发用 `just web-dev` 热更新。

网关和 new-api 的 Docker 构建使用 BuildKit 持久缓存保存 Go 模块与编译结果；
前端已有 Bun 下载缓存。源码改变后重建可复用这些缓存，首次启用仍需预热。
缓存保存在当前 Docker builder，`just down` 不会清除；切换 builder 或清理
构建缓存后需要重新下载。缓存不进入最终运行镜像。

`AFFINITY_TEST`、`CONSOLE_BIND`、`CONSOLE_PORT`、`TEST_PORT`、`AFFINITY_CONSOLE_PASSWORD` 可通过环境变量覆盖，脚本默认值与下面的容器环境一致；自定义后应在各次调用中保持一致。不自动保存密码到文件。更改这些环境变量应执行 `just up` 重建容器配置，`just reload config` 不能更新容器环境变量。

脚本测试：`just test-scripts`。命令失败返回非零退出码，构建失败不会继续替换运行中的容器。

## 原始 Compose 命令

在仓库根目录启动（独立项目名与数据卷，不影响旧测试环境）：

```bash
TEST_PORT=18243 docker compose -p caddy-affinity-console \
  -f deploy/compose.test.yaml -f deploy/compose.console.yaml up -d --build
```

- 网关控制台：`http://127.0.0.1:18242/`，开发 Compose 默认 `AFFINITY_TEST=true`，无需输入访问密码。
- 使用 `AFFINITY_TEST=false just up` 恢复控制台鉴权，密码由 `AFFINITY_CONSOLE_PASSWORD` 设置。仅精确值 `true` 免鉴权；网关进程未设置该变量时仍要求密码。模型 API 的 new-api Token 鉴权保持原有行为。
- new-api 后台：`http://127.0.0.1:18243/`，用户名 `affinitytest`，密码 `Isolated-Affinity-Test-Only-2026`。
- 模型 API：`http://127.0.0.1:18243/v1/`。自动生成的测试 token 保存在此项目的 `test_artifacts` 卷 `/artifacts/token`，不输出到日志。
- `CONSOLE_PORT` 和 `TEST_PORT` 可调整端口；`AFFINITY_CONSOLE_PASSWORD` 可覆盖控制台密码。默认监听 `0.0.0.0`，其他机器使用服务器 IP 访问；设置 `CONSOLE_BIND=127.0.0.1` 可恢复仅本机访问。Compose 需支持 `!override`（2.24.4 或更新版本）。

包含前端静态容器、带采集模块的 Caddy、打过强亲和补丁的真实 new-api、Postgres、Redis、测试捕捉器、供应商模拟器和一次性 seed 初始化服务。seed 成功退出（exit 0）是正常状态。只有前端和入口端口发布至主机；数据库、Redis、出口和采集 API 不单独发布。

链路：客户端 → 网关 → capture → new-api → capture → 网关出口 → 模拟供应商；响应沿原路经过 new-api。前端通过同源 `/api` 访问网关采集接口。

这是一套开发联调环境：供应商回复为模拟数据，凭证是公开的测试值，不要暴露到公网或填入生产密钥。new-api 数据保存在独立数据库卷；观测记录仍为网关进程内最近 1000 条，网关重启后清空。

验证：

```bash
TEST_PORT=18243 docker compose -p caddy-affinity-console \
  -f deploy/compose.test.yaml -f deploy/compose.console.yaml \
  run --rm --no-deps probe python /test/console_check.py
```

自定义控制台密码时，验证命令添加 `-e AFFINITY_CONSOLE_PASSWORD` 传入同名环境变量。

停止时使用相同的项目名和两个配置文件执行 `down`；不加 `-v`，保留数据库与测试产物。

## 本次验证结果

2026-09-06：前端和新网关镜像构建成功；new-api 使用此前已构建的固定版本强亲和镜像。7 个常驻容器运行，seed 以 0 退出。前端与 new-api 页面 HTTP 200。

`console_check.py` 通过：同会话三次请求（含一次模型 SSE）保持同一出口和供应商标识；内部头不泄露；缺标识请求拒绝；控制台 API 认证、入出站会话指纹关联、脱敏与 SSE 均正常。快照当时共 36 条事件，其中包含初始化就绪探测产生的记录。

隔离 Postgres 的 new-api `logs` 回读：4 条消费日志（type 2），合计 40 prompt tokens / 20 completion tokens，证明包括模型流式请求在内的回复通过 new-api 统计。初始化期间还产生 9 条错误日志（type 3），来自就绪探测等待渠道缓存，不作为模型链路成功请求计数。未运行完整 harness 矩阵重测。
