# 真实 Harness 端到端测试报告

日期：2026-09-06。结论：**当前方案尚不能承诺严格的首轮渠道亲和或完整供应商身份隔离，不建议以这两项保证上线。**

后续模拟器已扩展为可配置回复池，并用真实 CLI 校验三轮回复恢复；见 [新增模拟器测试](mock-simulator.md)。下文压力数字来自修改前的模拟器，不应混用。

本轮只修改测试设施和文档，未修改生产插件逻辑，也未修改第三方 new-api 镜像。

## 1. 测试范围与环境

不是手写 HTTP 请求冒充客户端：实际安装并运行以下 CLI，使用各自的会话持久化/恢复功能。

| 组件 | 固定版本 / 配置 |
|---|---|
| OpenCode | npm `opencode-ai@1.18.29` |
| Claude Code | npm `@anthropic-ai/claude-code@2.1.263` |
| omp（oh-my-pi） | npm `@oh-my-pi/pi-coding-agent@18.1.11` |
| 安装/运行基础镜像 | `node:22-bookworm-slim`，安装 Bun 1.4.2 |
| Caddy | 2.11.4 + 当前工作区插件；重新 build 确认镜像构建缓存匹配源码 |
| new-api | `calciumion/new-api:v1.0.0-rc.25` |
| 数据服务 | PostgreSQL 16、Redis 8，独立测试卷 |
| 协议上游 | Python 本地 Chat Completions / Anthropic Messages + SSE 模拟器 |
| 调度配置 | 两个 Chat 渠道、两个 Anthropic 渠道、一个 strip 渠道；同协议 A/B 等权重 |
| 亲和规则 | 请求头 `X-Session-Affinity`；TTL 300 秒；包含 group/model/rule |

供应商是模拟器，**不代表真实供应商认证、缓存命中、配额或特殊 ID 格式已通过**。
客户端真实，new-api 真实，Caddy 真实；只有最后的模型服务是模拟的。

完整回归运行两次，每次 15 个 CLI 用例 / 17 个推理请求。另有一次早期 9 用例探索运行，不混入最终统计。

- 基线运行：`5256ff5d-d579-469f-8e2c-9cab253d781e`，保留实际 OpenCode 漂移。
- 隔离复测：`3e7a7047-b61e-4852-a346-c29a14e022db`，harness 只连接 Docker `internal: true` 网络，无直接公网出口。
- 无宿主代码、用户配置、生产密钥挂载；工作目录 `/work` 为一次性容器目录。
- 禁用工具及扩展/技能发现（按客户端支持配置）；Claude 禁用非必要流量，OpenCode 禁用自动更新/模型目录抓取。
- 测试用户名/令牌均为隔离实例专用。令牌仅存测试卷 `token`，权限 0600，未导出。

## 2. 捕获与断言设计

```text
真实 CLI
  → native tap :8001          原始请求头、正文
  → Caddy 入站 :8236
  → canonical tap :8002       规范化身份、入站正文完整性
  → new-api :3000             真实鉴权、渠道选择、亲和缓存、协议适配
  → egress tap :8003          选中站点的路由、内部头透传结果
  → Caddy 出站 :8237
  → supplier mock :8000       供应商实际收到的头、正文
```

native tap 只补测试追踪头 `X-Test-Request-Id`，**不补亲和 ID**。new-api 显式透传追踪头与内部亲和头。
追踪头不参与 HMAC 或渠道亲和规则。四段以同一追踪 ID 关联，避免只查“缓存命中日志”遗漏首次请求。

记录内容：纳秒时间戳、路径、脱敏请求头、完整测试正文、正文 SHA-256、响应状态。
Authorization、X-Api-Key、Cookie、Proxy-Authorization 持久化前脱敏。导出再次扫描测试 token。
不会记录生产请求；记录器仅适用于本测试栈，不是生产日志组件。

独立 Python 校验器不调用 Go 插件派生函数，而是重算长度前缀编码和 HMAC-SHA256：

```text
internal = "sa:v1:" + HMAC(secret, frame("inbound:v1", credential, "conversation", original_sid))
supplier = HMAC(secret, frame("outbound:v1", site_scope, internal))
frame(part) = UTF-8 字节长度 + ":" + UTF-8 字节
```

逐请求检查：

1. 原生身份是否可提取；凭证命名空间是否正确。
2. canonical 头是否等于独立计算值；已识别原始会话头是否被清理。
3. native → canonical 正文 SHA-256 是否完全一致。
4. new-api 是否将同一 internal 传至选中的出站路径。
5. supplier 头是否等于该站点独立计算值；内部/通用头是否泄漏。
6. egress → supplier 正文是否保持一致。**不要求 new-api 前后字节相等**，它会正常重新序列化正文。
7. 首轮所有子请求和恢复轮是否同身份、同站点；新会话是否不合并。
8. 额外检查正文原始身份是否仍暴露：这是独立隔离指标，不能被“头校验通过”覆盖。

校验器另有 7 个自测试：正常链路、错误命名空间、入站正文篡改、丢失透传、错误站点、内部头泄漏、出站正文篡改，全部通过。

## 3. 真实客户端结果

每组包含首次、恢复、新会话三次 CLI 调用。

| 客户端 / 模式 | 真实捕获的原生会话字段 | CLI 结果 | 严格亲和 / 隔离结果 |
|---|---|---|---|
| OpenCode，自定义 Chat provider | `X-Session-Affinity` 和 `X-Session-Id`，值相同 `ses_…` | 两轮均 3/3 成功 | 基线首轮标题与主请求分流；隔离复测未漂移；问题未解决 |
| Claude Code，Messages | `X-Claude-Code-Session-Id`；正文 JSON 字符串 `metadata.user_id.session_id` | 两轮均 3/3 成功 | 恢复身份/站点稳定，新会话隔离；正文原始身份仍传出 |
| omp，通用 Chat，无补头 | 无本插件可识别的会话头或正文会话字段 | 两轮均 0/3，HTTP 400 | 原生配置不兼容，不能验证其经本链路恢复对话 |
| omp，Chat，显式每会话补头 | 测试配置 `X-Session-Id: UUID` | 两轮均 3/3 成功 | 同配置恢复稳定、换 UUID 新会话隔离；这是配置对照，不是原生能力 |
| omp，Messages，无补头 | `X-Claude-Code-Session-Id`；正文 `metadata.user_id.session_id` | 两轮均 3/3 成功 | 恢复身份/站点稳定；正文原始身份仍传出 |

每轮实际 17 个请求：OpenCode 5 个（两次新建各有标题请求 + 主请求，恢复 1 个），其余四组各 3 个。
其中 14 个完成整个链路，14/14 入站 HMAC、出站 HMAC、分段正文完整性通过；3 个原生 omp Chat 请求在入站被拒绝。
每轮 6 个 Messages 请求仍含原始正文会话身份。隔离复测的校验器因此仍返回非零退出码。

### OpenCode 请求样本（节选）

```http
POST /v1/chat/completions
User-Agent: opencode/1.18.29 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14
Authorization: [REDACTED]
X-Session-Affinity: ses_f8a4aa400ffep27QiBTwLHyRkU
X-Session-Id: ses_f8a4aa400ffep27QiBTwLHyRkU
```

```json
{"model":"affinity-test","max_tokens":1024,"stream":true,"stream_options":{"include_usage":true}}
```

这里只展示非 messages 字段；完整正文见捕获文件。
该样本没有 `X-Opencode-Session` 原生头，不能按客户端名字猜测请求头。

### Claude Code / omp Messages 样本

```json
{"metadata":{"user_id":"{\"session_id\":\"01a075b5-8b71-744c-a3c1-7c7dd70dd795\"}"}}
```

这是 omp 的实际正文格式。Claude Code 相同位置另有 `device_id` 和 `account_uuid`。
当前插件从此 JSON 字符串提取 session_id，而不是从 `X-Claude-Code-Session-Id` 提取。
该新头也不在当前清理列表；本测试 new-api 默认不透传它，不能把下游没收到该头归功于插件。

omp 的 Messages 请求实际 User-Agent 是 `claude-cli/2.1.257 (external, cli)`，不是 `omp/18.1.11`。
客户端归属依据运行清单和时间段关联，不仅依赖 UA。后续实现不能只凭 UA 判断客户端类型。

## 4. 首轮竞态：真实客户端证据

基线 OpenCode 首轮两请求使用同一 canonical ID：
`sa:v1:9c4d995740adc8bf6639c8d6c0fc6b81a10953d825b79091675f5d90b6d8766b`。

以标题请求入站时刻为 0，捕获时间线如下（约数）：

| 时间 | 事件 | 站点 |
|---|---|---|
| 0 ms | 标题生成入站，trace `d0cffef7-50fe-4e06-9a63-cf74f9bd9fb7` | — |
| 12 ms | 标题请求到供应商 | B |
| 285 ms | 主回答入站，trace `c4fed706-6cf9-4c9f-8d1e-fb5e11b74b4d` | — |
| 292 ms | 主回答到供应商 | A |
| 423 ms | 标题响应转发完成 | B |
| 701 ms | 主回答响应转发完成 | A |

主请求到达时标题请求尚未完成；相同会话已经被分到不同站点。两次 HMAC 都正确，所以不能靠修改派生算法解决。
复测未漂移仅说明等权选择有可能恰好选中同一站点。

固定 new-api 版本在 `c.Next()` 返回且响应成功后才 `RecordChannelAffinity`，不是在首次选路时原子占位。
这与上述并发首轮证据一致。非流式紧邻请求的具体缓存可见性仍未做 Redis 写入级追踪，不能宣称所有时序细节均已证明。
[new-api 调度源码](https://github.com/QuantumNous/new-api/blob/v1.0.0-rc.25/middleware/distributor.go)。

## 5. 合成协议边界与压力测试

这些是 stdlib `unittest` / Python HTTP 测试，**不是额外的真实 CLI 并发测试**。

### 边界结果

12 个测试方法，其中 11 个通过，`test_header_aliases` 有 2 个失败子用例。

| 检查项 | 结果 |
|---|---|
| 缺凭证 / 缺会话 | 分别 401 / 400，符合预期 |
| 冲突会话头 / 重复同名头 / 超长 ID | 400，符合预期 |
| 超过 2MiB 的正文会话提取 | 400，符合默认限制 |
| conversation 字符串、对象；metadata JSON 字符串；Unicode 字节长度派生 | 通过 |
| 六种连字符头别名 | 通过 |
| `Session_id`、`Conversation_id` | 失败，HTTP 400 |
| 同站点稳定、跨站点派生不同、strip 头清理 | 通过 |
| 未知路由、缺失/非法出站 internal | 404 / 400 / 400，通过 |
| 凭证命名空间隔离 | 不同凭证生成不同 canonical；无效凭证仍被 new-api 拒绝 |
| SSE 增量及终止标记 | 首条 data 到结束仍 >150ms，且存在 `[DONE]`，通过 |

下划线失败不是旧镜像：重新构建确认源码对应。Caddy 2.11.4 在插件执行前删除带 `_` 的头，
用于防止与 CGI/FastCGI 等头映射产生安全别名碰撞。**不建议关闭这项安全保护**；客户端应改用连字符头。
源码位于 Caddy `modules/caddyhttp/server.go:497`，注释引用 GHSA-f59h-q822-g45g。
目前测试保留失败，以揭示“插件函数支持”与“整个服务器支持”的差异。

### 600 次请求压力回归

8 个 worker；每组 100 个独立会话，每会话 3 轮；新会话 UUID；不重试、不用等待修饰默认结果。

| 轮间等待 | 请求数 | 完整成功但渠道/身份漂移的会话 | HTTP 错误会话 | 耗时 |
|---|---:|---:|---:|---:|
| 0 | 300 | **73 / 100** | 0 | 1.209 秒 |
| 200ms（对照） | 300 | 0 / 100 | 0 | 5.634 秒 |

该比例只描述这次本地 mock、该版本和负载，不能外推为生产漂移概率；200ms 不是可靠修复。
代理记录器会影响时序，这些数字不是性能基准。压力路径跳过 native tap，但仍经过 canonical/egress tap。

初次压力尝试另发现环境配置错误：new-api 默认连接池上限 1000，超过 PostgreSQL 默认连接容量，出现 500 / SQLSTATE 53300。
已仅在测试 Compose 设置 `SQL_MAX_OPEN_CONNS=20`、`SQL_MAX_IDLE_CONNS=5` 后重跑，得到上表无 HTTP 错误的完整结果。
测试程序也已改为记录每个会话错误，不因第一个错误丢失后续统计。
[new-api 连接池默认值](https://github.com/QuantumNous/new-api/blob/v1.0.0-rc.25/model/main.go)。

### 本地回归

- `go test -race -count=1 -v ./...`：7 个测试全部通过，非缓存执行。
- `go vet ./...`：通过。
- 校验器 mutation tests：7/7 通过。
- `git diff --check`：通过。

## 6. 应借此修正的设计

| 优先级 | 问题 | 建议 |
|---|---|---|
| P1 | 首次选路无原子绑定，真实客户端辅助请求也会竞争 | 在 new-api 调度层设计会话首选渠道的原子占位/复用；区分 pending、成功和故障释放；多实例需共享仲裁。不要把锁搬到无法决定渠道的入站头处理器 |
| P1 | 只重写头，正文原始 session/device 身份仍透传 | 增加供应商级正文策略：明确允许保留什么，重写什么；Messages 的 metadata JSON 字符串需结构化处理并验证。若不做，应明确只承诺头级适配 |
| P1 | omp 通用 Chat 缺少可用会话信号 | 客户端扩展/包装器按实际会话注入 ID，恢复沿用，新建换 ID；不得用固定全局头合并所有会话，也不能靠 prompt 哈希猜测 |
| P2 | 新的 Claude 会话头不被识别/清理 | 在定义优先级与冲突策略后纳入适配，同时验证头/正文不一致及超大正文场景 |
| P2 | 下划线兼容性被 Caddy 安全层阻断 | 修正对外支持矩阵，推荐标准连字符头；如必须迁移，在明确可信边界独立设计，不绕过安全过滤 |
| P2 | 渠道固定不等于账号固定 | 本轮每渠道单 key；生产多 key 轮换需要另做账号级亲和测试 |

## 7. 文件、证据与复现

代码：

- [测试编排](../deploy/compose.test.yaml)、[CLI 镜像](../deploy/test/Dockerfile.harness)、[真实 CLI 测试驱动](../deploy/test/harness.py)。
- [四段捕获](../deploy/test/capture.py)、[协议上游](../deploy/test/mock.py)、[初始化](../deploy/test/seed.py)。
- [入站/出站校验器](../deploy/test/validate.py)、[校验器自测试](../deploy/test/test_validate.py)。
- [边界和压力测试](../deploy/test/e2e.py)、[脱敏证据导出](../deploy/test/export.py)。

本次本地证据（`artifacts/` 已忽略，不提交测试正文到 Git）：

- [基线完整请求](../artifacts/capture.jsonl)、[基线严格校验](../artifacts/validation.json)、[CLI 命令与日志索引](../artifacts/runs.json)。
- [边界测试输出](../artifacts/boundaries.log)、[全部 200 会话压力明细](../artifacts/stress.json)。
- [无公网复测校验](../artifacts/3e7a7047-b61e-4852-a346-c29a14e022db/validation.json)、[无公网复测请求](../artifacts/3e7a7047-b61e-4852-a346-c29a14e022db/capture.jsonl)。

从仓库根目录运行：

```sh
docker compose -f deploy/compose.test.yaml build affinity-gateway harness
docker compose -f deploy/compose.test.yaml up -d
docker compose -f deploy/compose.test.yaml run --rm -e SEED_ONLY=1 probe python /test/seed.py
docker compose -f deploy/compose.test.yaml run --rm harness
docker compose -f deploy/compose.test.yaml run --rm probe python /test/validate.py
docker compose -f deploy/compose.test.yaml run --rm probe python /test/e2e.py
docker compose -f deploy/compose.test.yaml run --rm probe python /test/export.py
python3 -m unittest discover -s deploy/test -p test_validate.py -v
```

注意：harness.py 是采集驱动，会完成所有用例后退出；最终门禁是 validate.py 和 e2e.py。
当前它们应返回非零，因为发现的问题没有被产品修复；不要加 `|| true` 当作测试通过。
因此在 fail-fast CI 中，证据导出需要使用框架的 always/finally 阶段，而非依赖前序退出码为零。
安装顶层 CLI 固定版本，但基础镜像 tag 和间接 npm 依赖并非完整供应链锁定；跨时间复验应同时保存镜像 digest。

## 8. 未覆盖和保留状态

没有测试：真实供应商缓存命中/账号额度、OAuth 订阅、Responses/WebSocket/Realtime、TLS/H2/H3、
工具调用多轮、取消/断线、429/5xx 故障切换、TTL 到期、Redis 故障、多 new-api 实例、多 key、长时间负载。
采集器只支持 Content-Length 请求，不支持 chunked request；响应增量转发支持本次 SSE。
恢复测试通过 CLI 重新启动实现，不等价于 TUI 全操作覆盖。

测试栈仍运行，入站为 `http://127.0.0.1:18236`；出站和数据库未发布到宿主机。
测试原始资料保存在独立卷和本地已脱敏 artifacts。没有删除测试卷、没有启动生产 Compose。
