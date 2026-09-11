# 测试环境与亲和验证方案

> 可复现的本地测试台（harness 容器 + 分层抓包）见 [testkit/README.md](../testkit/README.md)。
>
> 当前执行口径以 [强亲和扩展 Harness 报告](strict-harness-report.md) 为准，包含 pi / Qwen Code / Kimi CLI、SQL 原子绑定和故障验证。以下保留早期方案与历史记录，不代表当前全部能力。

> 已提供 deploy/compose.test.yaml、自动 seed.py、HTTP smoke 和完整链路 probe。
> OpenAI Chat 基础链路已通过；零间隔多会话复验发现首次绑定漂移，见 integration-results.md。
> 以下完整 harness 矩阵仍是规划，不代表所有客户端/协议都已验证。

> 更新：真实 OpenCode 1.18.29、Claude Code 2.1.263、omp 18.1.11 已完成两轮 E2E，含四段捕获、独立校验和 600 请求压力回归。具体通过/失败范围见 [实测报告](harness-e2e-report.md)，下文未覆盖项仍是规划。

> 目标:搭建**可复现、可自动化的测试环境**,用多种客户端(harness)验证:
> ① 入站会话归一化是否正确;② new-api 渠道亲和是否按会话锁渠道;③ 出站供应商适配头是否正确。
>
> 原则:测试环境**完全隔离**(独立 new-api + 独立 postgres/redis + mock 供应商),不触碰生产;全部 docker-compose 编排,一条命令起、一条命令测、一条命令清。

---

## 1. 拓扑:生产镜像 + mock 供应商

```
┌─────────────────────────────── 测试环境(docker compose) ───────────────────────────────┐
│                                                                                          │
│  harness 层(多种客户端)                                                                  │
│   ┌──────────┬──────────┬──────────┬──────────┬──────────────┐                          │
│   │ curl     │ openai-  │ anthropic│ opencode│ Claude Code  │                          │
│   │ (手工头) │ python   │ -sdk    │ CLI    │ / Codex CLI  │                          │
│   └────┬─────┴────┬─────┴────┬─────┴────┬────┴──────┬───────┘                          │
│        ▼          ▼          ▼          ▼           ▼                                   │
│  ┌─────────────────────────────────────────────────────────────────────────┐            │
│  │ Caddy 入站代理(:8236)+ session_affinity 插件(被测)                       │            │
│  │   识别 → UUIDv7 生成/映射 → 注入 x-opencode-session / X-Session-Id      │            │
│  └────────────────────────────────────┬────────────────────────────────────┘            │
│                                       ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────────┐            │
│  │ new-api(测试实例,:8235)                                                 │            │
│  │   渠道亲和规则(4 源:request_header ×2 + gjson ×2)                       │            │
│  │   渠道 base_url → http://caddy-out:8237/<vendor>                        │            │
│  │   header_override 还原会话头(方式 A)                                    │            │
│  └────────────────────────────────────┬────────────────────────────────────┘            │
│                                       ▼                                                  │
│  ┌─────────────────────────────────────────────────────────────────────────┐            │
│  │ Caddy 出站代理(:8237)+ session_affinity outbound 插件(被测)             │            │
│  │   按 path 识别供应商 → 适配/剥离头 → 转发                               │            │
│  └──────┬──────────────┬──────────────┬───────────────────────────────────┘            │
│         ▼              ▼              ▼                                                   │
│   ┌───────────┐ ┌───────────┐ ┌───────────────┐                                         │
│   │ mock-open │ │ mock-ds   │ │ mock-volc     │  每个供应商一个 echo 服务器:             │
│   │ (echo)    │ │ (echo)    │ │ (echo)        │  · 返回收到的完整 headers(断言出站适配)   │
│   └───────────┘ └───────────┘ └───────────────┘  · 返回唯一 vendor 标识(断言亲和锁渠道)   │
│                                                                                          │
│  ┌───────────────┐  ┌───────────────┐                                                     │
│  │ postgres(隔离)│  │ redis(隔离)   │   new-api 的独立数据源                               │
│  └───────────────┘  └───────────────┘                                                     │
└──────────────────────────────────────────────────────────────────────────────────────────┘
```

### 为什么用 mock echo 供应商

- **断言亲和**:每个 mock 供应商在响应体里塞唯一 `vendor` 标识(如 `"mock_vendor":"opencode"`)。同一会话两次请求,若两次响应 `vendor` 相同 → 亲和锁住了同一渠道;不同会话分布到不同 `vendor` → 亲和按会话隔离。
- **断言出站适配**:mock 把收到的请求 headers 原样回显(含 `x-opencode-session`),测试断言"目标供应商收到会话头、非目标供应商没收到"。
- **零外部依赖**:不真连 opencode.ai/DeepSeek,测试离线可复现、无成本、无封号风险。

### mock 供应商行为契约

```jsonc
// POST /v1/chat/completions 响应(非流式)
{
  "id": "...", "object": "chat.completion",
  "model": "deepseek-v4-flash",
  "choices": [{ "index": 0, "message": { "role": "assistant",
               "content": "ok from mock vendor" }, "finish_reason": "stop" }],
  "usage": { "prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15 },
  "mock_vendor": "opencode",                 // ← 亲和断言键
  "mock_echo_headers": {                     // ← 出站适配断言键(收到的请求头)
    "x-opencode-session": "sess-...",
    "authorization": "Bearer sk-...",
    "user-agent": "Go-http-client/1.1"
  }
}
```

流式请求返回 `text/event-stream` 逐块 SSE,验证 Caddy 流式透传不缓冲、不截断。

---

## 2. harness 矩阵(各种客户端)

每个 harness 代表一类真实客户端,覆盖不同协议、会话标识携带方式、UA。

| # | harness | 协议 | 会话标识 | 验证点 | 落地方式 |
|---|---|---|---|---|---|
| H1 | curl 手工 | OpenAI `/v1/chat/completions` | `X-Session-Id` 头 | 亲和命中 `request_header`;优先级 | shell 脚本 |
| H2 | curl 手工 | OpenAI | `x-opencode-session` 头 | opencode 生态会话头亲和 | shell 脚本 |
| H3 | curl 手工 | Anthropic `/v1/messages` | body `metadata.user_id` | gjson 亲和 | shell 脚本 |
| H4 | curl 手工 | OpenAI | body `user` 顶层字段 | gjson `user` 亲和 | shell 脚本 |
| H5 | openai-python | OpenAI | 无 + 自定义 header | SDK 兼容 + token 兜底 | python |
| H6 | anthropic-sdk | Anthropic | `metadata.user_id` | SDK 兼容 | python |
| H7 | opencode CLI(真实) | opencode 生态 | `x-opencode-session` | 真实客户端闭环 | node |
| H8 | Codex CLI(真实) | `/v1/responses` | `Session_id`/`Thread_id` | codex 头透传 | node |
| H9 | Claude Code(真实) | Anthropic | `metadata.user_id` | 真实客户端 | node |
| H10 | Hermes 模拟 | OpenAI | 无 | **token 级兜底亲和**(邮件点名场景) | node |
| H11 | Bun fetch / ai-sdk UA | OpenAI | 无 | 复现邮件点名 UA 缺失场景 | node |
| H12 | 并发乱序 | 混合 | 混合 | 亲和稳定性(压力) | python/go |

### 断言统一口径

每个 harness 跑完后,产出三组断言:

1. **亲和命中**:查 new-api logs 表 `admin_info.channel_affinity`:
   - `key_source` = `request_header` / `gjson`(正确识别)
   - `key_fp` 同会话一致(会话稳定)
   - `channel_id` 同会话一致(锁同一渠道)
2. **会话稳定性**:同会话连发 N 次,注入的 `x-opencode-session`/`X-Session-Id` 值不变、`key_fp` 相同。
3. **出站适配**:mock 回显的 `mock_echo_headers` 断言——目标供应商 path 收到会话头,非目标供应商没有(泄漏检测)。

---

## 3. 亲和效果判定标准

| 场景 | 期望 | 判定 |
|---|---|---|
| 同会话连续请求(带会话头) | 锁同一渠道 | 两次 `mock_vendor` 相同 + `channel_id` 相同 |
| 同会话连续请求(带 body user_id) | 锁同一渠道 | 同上,gjson 命中 |
| 无任何标识(裸 token) | token 级兜底 | 同 token 锁同一渠道(客户端级,非会话级) |
| 不同会话 | 可分布不同渠道 | `mock_vendor` 可能不同(加权路由) |
| 会话 TTL 过期 | 重新锁渠道 | TTL 后首请求 `channel_id` 可能变化 |
| 渠道禁用 | 亲和失效回落 | 按 `keep_on_channel_disabled` 配置:保留/清缓存重选 |
| header vs body 同值 | header 优先 | 日志 `key_source=request_header` |

---

## 4. 技术实现方案

### 4.1 docker-compose 服务清单(规划)

```yaml
# docker-compose.test.yaml(规划)
services:
  caddy-inbound:        # 被测:入站插件
    build: .
    ports: ["8236:8236"]
    volumes: ["./Caddyfile.test:/etc/caddy/Caddyfile:ro"]
    depends_on: [new-api]
  caddy-outbound:       # 被测:出站插件
    build: .
    ports: ["8237:8237"]
    volumes: ["./Caddyfile.outbound.test:/etc/caddy/Caddyfile:ro"]
  new-api:
    image: calciumion/new-api:v1.0.0-rc.25
    environment:
      SQL_DSN: postgresql://root:123456@postgres:5432/new-api
      REDIS_CONN_STRING: redis://redis:6379
      SELF_USE_MODE: "true"
    ports: ["8235:3000"]
    depends_on: [postgres, redis]
  mock-opencode:        # echo 供应商 A
    build: ./test/mocks
    environment: { MOCK_VENDOR: opencode }
  mock-deepseek:        # echo 供应商 B
    build: ./test/mocks
    environment: { MOCK_VENDOR: deepseek }
  mock-volc:            # echo 供应商 C
    build: ./test/mocks
    environment: { MOCK_VENDOR: volc }
  postgres: { image: postgres:16-alpine }
  redis:    { image: redis:8-alpine }
  test-runner:          # 跑 harness 矩阵 + 断言
    build: ./test/harness
    depends_on: [caddy-inbound, caddy-outbound, new-api]
    command: ["./run-matrix.sh"]
```

> 注:受 rootless Docker 网络限制(容器到宿主任意端口不可达),测试环境内**所有服务走 compose 内部网络互访**,不依赖宿主端口。new-api 渠道 `base_url` 指向 compose 服务名 `http://caddy-outbound:8237/<vendor>`。

### 4.2 new-api 测试实例初始化(seed)

测试启动时自动完成:

1. 注册/登录 admin(`root`),提升 role,设 quota;
2. 写渠道亲和规则(4 源,同生产);
3. 建 3 个渠道:
   - opencode → `http://caddy-outbound:8237/opencode`
   - deepseek → `http://caddy-outbound:8237/deepseek`
   - volc    → `http://caddy-outbound:8237/volc`
   - 各渠道 `header_override` 还原 `x-opencode-session`(方式 A);
4. 建测试 token。

### 4.3 测试执行与断言工具

- **runner**:`test/run-matrix.sh` 顺序跑 H1-H12,每个 harness 产出 JSON 结果;
- **断言库**:`test/assert.js`(node)统一校验三组断言,失败即非零退出;
- **日志探针**:直连 compose 内 postgres,`SELECT other::jsonb->'admin_info'->>'channel_affinity' FROM logs`,提取 `key_source`/`key_fp`/`channel_id`。

### 4.4 CI(GitHub Actions,规划)

```yaml
# .github/workflows/test.yml(规划)
on: [push, pull_request]
jobs:
  affinity-test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: docker compose -f docker-compose.test.yaml up -d --build
      - run: docker compose -f docker-compose.test.yaml run --rm test-runner
      - run: docker compose -f docker-compose.test.yaml down -v   # 不留痕
```

CI 与本地同一套 compose,保证可复现。

---

## 5. 分层测试策略

| 层 | 范围 | 工具 | 何时跑 |
|---|---|---|---|
| 单元 | session 识别优先级、UUIDv7 映射、body 改写 | `go test ./...` | 每次 push |
| 集成 | 入站 Caddy → new-api → mock,单会话/多会话 | compose + runner | CI |
| harness 矩阵 | 多客户端 × 协议 × 标识 | run-matrix.sh | CI(可并行) |
| 压力 | 并发乱序会话稳定性 | H12 | 手动/定时 |

## 6. 验证与实验记录的对应

生产只读验证已确认的事实(见 experiments.md)在测试环境里**变成可重复断言**:

- `request_header` 亲和生效 → H1/H2 断言 `key_source=request_header`
- gjson `metadata.user_id`/`user` 亲和 → H3/H4 断言 `key_source=gjson`
- 同会话 `key_fp` 稳定 → 所有 harness 的会话稳定性断言
- 出站会话头恢复 → mock 回显断言(方式 A header_override)
- 无标识 → token 兜底 → H10/H11 断言

## 7. 里程碑

| 阶段 | 内容 |
|---|---|
| T1 | compose 骨架 + mock echo + new-api seed 脚本 + H1-H4(curl 基础) |
| T2 | 断言库 + 亲和判定标准落地(H1-H4 全绿) |
| T3 | 真实 SDK harness(H5/H6)+ opencode/Codex CLI(H7/H8) |
| T4 | token 兜底场景(H10/H11)+ 并发压力(H12) |
| T5 | CI 接入 + README 测试章节 |
