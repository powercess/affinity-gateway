# Docker Compose 部署

> 当前强亲和需要 Caddy 插件与 [new-api 固定版本补丁](../docs/strict-affinity.md) 配套部署；仅接入未修改的 new-api 不提供原子、不迁移的强绑定保证。旧测试失败记录仍保留。

最新结果见 [6 种 Harness 强亲和报告](../docs/strict-harness-report.md)。

测试模拟器支持预设回复池、三轮历史恢复校验和可配置首包/分片/结束延迟，见 [模拟器配置与协议说明](../docs/mock-simulator.md)。

## 接入已有 new-api

本目录 compose.yaml 只启动一个双向 Caddy，不启动或修改已有 new-api、数据库。
已有 new-api 必须单独切换到下面构建的镜像，并设置 `STRICT_SESSION_AFFINITY=true`、保留原 SQL 数据卷：

```sh
docker build -t new-api-affinity-strict:local deploy/new-api
```

补丁增加 `strict_affinity_bindings` 表，不自动清理、不设置 TTL。先备份；不要对生产执行 test seed。当前仅接受明确分组、single-key 的 type 1/14 渠道。

需要 Docker Compose 和已有 new-api 的专用 Docker 网络。出站 8237 不发布到宿主机，
但网络内其他容器可访问；不要连接不可信容器。网络需要有公网出口。

从仓库根目录执行：

```sh
cp deploy/.env.example deploy/.env
mkdir -p deploy/secrets
umask 077
openssl rand -hex 32 > deploy/secrets/affinity_secret
```

仅首次创建 secret；后续部署保留它。生成命令会覆盖旧文件，勿重复执行。
编辑 deploy/.env，将 NEW_API_NETWORK 和 NEW_API_UPSTREAM 改成现有网络名和服务地址。
默认使用 new-api_default / new-api:3000；不自动创建该外部网络。
修改 deploy/caddy/routes 中的示例供应商域名，确认其接受派生后的 64 字符 hex ID。
不要求会话头的站点使用 policy strip。供应商凭证仍配置在 new-api。

```sh
docker compose --env-file deploy/.env -f deploy/compose.yaml config --quiet
docker compose --env-file deploy/.env -f deploy/compose.yaml build
docker compose --env-file deploy/.env -f deploy/compose.yaml run --rm --no-deps affinity-gateway caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker compose --env-file deploy/.env -f deploy/compose.yaml up -d
```

入站默认仅宿主机 127.0.0.1:8236 可访问，可接到原有 HTTPS 入口。若 HTTPS 入口也在
容器中，让它加入专用网络并访问 affinity-gateway:8236；它的 localhost 不是宿主机。
目前推理会话处理覆盖 /v1/messages、/v1/chat/completions、/v1/responses；其他路径
（含 UI、登录、模型列表）直接转发。无可靠会话标识的请求拒绝并提示补充 X-Session-Id；
不再支持 fallback credential。cache_key_as_session 仅适用于已约定缓存键就是会话 ID 的独立客户端入口，默认关闭。

new-api 每个渠道设置：

| 渠道 | base_url |
|---|---|
| OpenCode A | http://affinity-gateway:8237/r/opencode-a |
| OpenCode B | http://affinity-gateway:8237/r/opencode-b |
| 无会话头上游 | http://affinity-gateway:8237/r/deepseek |

严格补丁使用独立持久绑定，不依赖原版 TTL 亲和规则；未启用补丁时原版规则不能替代强保证。
每个需要会话适配的渠道的 header_override 设置为：

```json
{"X-Session-Affinity":"{client_header:X-Session-Affinity}"}
```

根据具体 new-api 版本在渠道设置中填入对象；这不是完整渠道 JSON。
确认渠道 URL 拼接保留 /v1/...。严格补丁拒绝多 Key 渠道，不自动迁移已绑定渠道。

## 更新站点配置

配置目录整体只读挂载，新增 routes/*.caddy 后先校验再 reload。exec 不执行镜像入口，
因此调用入口脚本读取 secret 文件：

```sh
docker compose --env-file deploy/.env -f deploy/compose.yaml exec affinity-gateway /bin/sh /usr/local/bin/affinity-entrypoint caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
docker compose --env-file deploy/.env -f deploy/compose.yaml exec affinity-gateway /bin/sh /usr/local/bin/affinity-entrypoint caddy reload --config /etc/caddy/Caddyfile --adapter caddyfile
docker compose --env-file deploy/.env -f deploy/compose.yaml ps
```

healthcheck 只检查 Caddy 本地存活，不承诺 new-api 或供应商可用。日志有大小限制；
没有开启记录会话头的 access log。/data、/config 持久化，当前为内网 HTTP，HTTPS
仍由原有入口负责。不要发布出站 8237、管理 2019 或健康检查 8238。

## 隔离测试栈

现在可自动初始化固定版本的隔离实例并运行测试：

```sh
docker compose -f deploy/compose.test.yaml up -d --build
docker compose -f deploy/compose.test.yaml run --rm probe python /test/seed.py
```

seed.py 的目标固定为测试网络内的 new-api:3000；使用测试专用管理员 affinitytest，
密码为 Isolated-Affinity-Test-Only-2026（仅限本地隔离环境）。它创建缺失的测试渠道、
复用测试 Token，并写入测试模型价格和亲和规则；不要在生产实例运行。
默认 Token 仅在进程内传给探针；`SEED_ONLY=1` 模式写入隔离测试卷的 0600 文件供真实 CLI 使用，不打印或导出。已初始化为其他管理员的实例需走下面的
手动流程。初始化时最多等待约 70 秒让渠道缓存同步，实际探针无失败重试。
探针还覆盖 8 个独立会话、每会话 3 轮、4 个并发 worker 的身份与渠道稳定性。
当前零间隔并发复验发现 new-api 首次绑定窗口的间歇漂移，探针会严格报错，详见
../docs/integration-results.md。可用 `run --rm -e TEST_TURN_DELAY=0.2 probe python /test/seed.py`
做轮间 200ms 对照；这不是修复，默认仍为零间隔。

compose.test.yaml 使用独立项目名、数据库和网络，不连接任何真实供应商。
它启动同一插件镜像、new-api v1.0.0-rc.25、Postgres、Redis、四段捕获和 Chat/Messages mock。
真实 CLI 的安装、执行、校验和证据导出命令见 [详细测试报告](../docs/harness-e2e-report.md#7-文件证据与复现)。
仅测试入站发布为 127.0.0.1:18236，可通过 TEST_PORT 修改。

```sh
docker compose -f deploy/compose.test.yaml up -d --build
```

首次访问 http://127.0.0.1:18236 完成 new-api 初始化，然后在测试实例内：

1. 创建两个 OpenAI 兼容渠道，模型均为 affinity-test，分组、优先级和权重相同，
   base_url 分别是 http://affinity-gateway:8237/r/opencode-a 和 /r/opencode-b。
   使用任意测试 Key，例如 mock-only，配置上面的 header_override。
2. 创建第三个渠道，模型 strip-test，base_url 为
   http://affinity-gateway:8237/r/no-session，同样透传内部头以验证出站剥离。
3. 配置渠道亲和规则：模型 ^affinity-test$，路径 /v1/chat/completions，
   key_sources 为 [{"type":"request_header","key":"X-Session-Affinity"}]，TTL 300 秒。
4. 创建有这两个模型访问权及足够测试额度的测试 Token。

以上为手动流程。自动 seed 已适配本项目固定镜像版本；其他版本需另行验证接口。
然后将 Token 放入当前 shell 的 TEST_API_KEY 环境变量：

```sh
docker compose -f deploy/compose.test.yaml run --rm probe
```

probe 验证重复请求的站点与会话头稳定、内部头剥离、SSE 内容完整和无会话头策略。
无需初始化账号即可执行容器级 smoke 检查（不验证 new-api 亲和）：

```sh
docker compose -f deploy/compose.test.yaml run --rm probe python /test/smoke.py
```

mock 仅把非敏感路由标记与会话标识放进 assistant.content，避免 new-api 剥离顶层扩展字段；
不回显 Authorization 或 prompt。它支持 Chat Completions，不是完整 Anthropic/Responses 模拟器。
同渠道多次命中有随机巧合可能，最终仍需结合 new-api 日志中的亲和命中来源与 channel_id
确认。当前 probe 不测 TTFT、统计分布、账号故障转移或所有客户端矩阵。

停止测试栈（保留测试数据，便于重复验证）：

```sh
docker compose -f deploy/compose.test.yaml down
```

只有明确不再需要测试初始化数据时才使用 down -v，删除的是该测试项目的数据卷。
