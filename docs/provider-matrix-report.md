# 原生 Harness / Provider 注册矩阵测试报告

测试日期：2026-09-06。结论：三种协议的原生客户端文本收发均已验证，但当前亲和链路不能宣布兼容性、渠道稳定性和标识隔离全部通过。

## 范围和方法

真实安装版本：OpenCode 1.18.29、omp 18.1.11、Claude Code 2.1.263；Caddy 2.11.4、new-api v1.0.0-rc.25。使用现有 Docker Compose 隔离环境，测试客户端运行时仅能访问内部网络，不使用生产账号、项目或真实模型额度。

每种注册运行四个独立 CLI 进程：首次提问、`--continue` 恢复、继续第三轮、新会话。恢复依赖客户端自己的会话存储；模拟器检查请求确实带回前轮用户消息及完整助手回复，再返回对应回复池文本。每次 CLI 输出必须和预期文本精确相等。

两条线路分别使用独立 HOME 和工作目录：

- 直连对照：真实 CLI → 抓包 → 模拟供应商，检验原生协议与回复解析。
- 亲和链路：真实 CLI → 入站抓包 → Caddy 入站 → 规范标识抓包 → new-api → 出站抓包 → Caddy 出站 → 模拟供应商抓包。

仅更换 endpoint、测试凭据和所选模型。内置 provider 不覆盖 API 类型、模型能力、上下文或成本元数据；自定义 provider 使用客户端实际支持的 npm adapter / api 枚举。没有注入会话头、固定 session ID、修改安装包或强制关闭标题请求。OpenCode 禁止工具执行和分享；Claude Code 限制单轮；提示词要求仅文本回复。自动更新和外部模型抓取关闭，工作区为空。这些属于隔离措施，不宣称与日常配置逐字相同。

抓包增加 `X-Test-Request-Id` 仅用于关联各跳，亲和逻辑不使用它。凭据在落盘前脱敏；请求体保留合成测试数据。启动探测请求不计入推理请求数。

## 注册矩阵（以实际请求路径判定协议）

| 客户端 / 注册方式 | 模型 | 实际协议 | 直连回复 | 亲和链路回复 |
|---|---|---|---:|---:|
| OpenCode 内置 openai | gpt-5.2 | Responses | 4/4 | 4/4* |
| OpenCode 内置 anthropic | claude-sonnet-4-6 | Messages | 4/4 | 4/4 |
| OpenCode 自定义 @ai-sdk/openai-compatible | gpt-4.1 | Chat Completions | 4/4 | 4/4 |
| OpenCode 自定义 @ai-sdk/openai | gpt-5.2 | Responses | 4/4 | 4/4 |
| OpenCode 自定义 @ai-sdk/anthropic | claude-sonnet-4-6 | Messages | 4/4 | 4/4 |
| omp 内置 openai | gpt-4.1 | Responses | 4/4 | 0/4，HTTP 400 |
| omp 内置 openai | gpt-5.2 | Responses | 4/4 | 0/4，HTTP 400 |
| omp 内置 anthropic | claude-sonnet-4-6 | Messages | 4/4 | 4/4 |
| omp 自定义 openai-completions | gpt-4.1 | Chat Completions | 4/4 | 0/4，HTTP 400 |
| omp 自定义 openai-responses | gpt-5.2 | Responses | 4/4 | 0/4，HTTP 400 |
| omp 自定义 anthropic-messages | claude-sonnet-4-6 | Messages | 4/4 | 4/4 |
| Claude Code 原生 Anthropic | claude-sonnet-4-6 | Messages | 4/4 | 4/4 |

特别注意：本版本 omp 内置 gpt-4.1 实际也是 Responses，不能仅根据模型名称猜测使用 Chat Completions。

Claude Code 原生 OpenAI Chat / Responses 注册不属于此版本公开支持的网关协议，标记“不适用”，没有通过编造配置或额外协议转换伪造支持。其 Bedrock、Vertex 等云认证方式不在本次三协议范围内。参见 [Claude Code 官方网关协议](https://code.claude.com/docs/en/llm-gateway-protocol)。OpenCode 内置 openai 的 Responses 选择可对照 [固定版本源码](https://github.com/anomalyco/opencode/blob/v1.18.29/packages/opencode/src/provider/provider.ts)。

## 实测缺陷与定位

### 1. omp 的问题必须按注册方式区分

| 注册方式 | 入站抓到的原生会话信号 | 当前结果 |
|---|---|---|
| 内置 openai，两种模型 | 请求头 `session_id`，请求体 `prompt_cache_key` | 带下划线头在 Caddy 接入层丢失；插件不提取 prompt_cache_key，入站 400 |
| 自定义 openai-responses | 无上述会话头；请求体有 `prompt_cache_key` | 插件不提取该字段，入站 400 |
| 自定义 openai-completions | 未发现当前支持的头或 body 会话字段 | 无可识别标识，入站 400 |
| 内置/自定义 anthropic | `x-claude-code-session-id`，以及 metadata.user_id JSON 字符串内的 session_id | 通过 body 标识成功建立亲和 |

因此，不能继续将所有 omp 情况笼统描述为“没有会话头”。抓包证明部分请求有标识，但传输层或提取规则不兼容。失败请求没有进入 canonical / egress / supplier 阶段。直连 4/4 与链路 0/4 的对照排除了这些用例中回复模拟器不被客户端接受的原因。失败后的恢复用例不等同于成功会话恢复，报告保留失败，不补假历史。

### 2. CLI 成功不等于渠道亲和成功

完整亲和运行共 58 条推理请求：40 条 HTTP 200 且通过独立入站/出站校验，16 条入站 400，2 条标题请求 503。

成功链路按客户端注册、canonical ID、模型分组，而不是把不同模型错误归并到一个渠道要求。捕获到 4 个同会话同模型跨 A/B 渠道组：OpenCode 自定义 Chat 两个会话、OpenCode 自定义 Responses 一个会话、自定义 Anthropic 一个会话。

这些请求的入站 HMAC 和出站站点 HMAC 都正确；问题在冷会话的并行标题/主请求与 new-api 成功后才写入绑定的时序。原始请求时间、route 和 canonical 均在 validation.json / capture.jsonl。该单次矩阵证明存在漂移，不用于估计生产故障率；本轮没有重跑原报告的 600 请求压力测试。

成功注册的首次、恢复、第三轮 canonical 连续，新会话 canonical 不同。此检查与渠道漂移检查独立，因此不会以“ID 相同”替代“渠道相同”。

### 3. 请求体标识隔离仍不完整

完整亲和运行发现 20 条原始标识暴露观察：12 条 Anthropic 请求保留 body metadata 中的会话 ID；8 条 OpenCode Responses 请求保留 prompt_cache_key。当前出站只重写已支持的请求头，未统一处理这些 body 字段。

这不妨碍当前文本回复，但意味着不能声称供应商只能看到站点隔离后的会话标识。新校验器对此返回非零，不将它算作全绿。

### 4. 保留原生标题模型，而不是修改 harness 绕开它

*首次完整运行中，OpenCode 内置 openai 自行选择 `gpt-5.4-nano` 做标题；测试渠道未登记该模型，产生两条 503，主回复仍 4/4。只在测试 seed 中补充该实际观察到的模型、定价占位和规则，不修改 OpenCode 的 small_model 或模型路由。

补配置后针对该注册重跑四阶段：4/4 回复、6/6 推理请求 HTTP 200、6/6 入站出站校验通过，没有同模型渠道漂移；仍有 4 条 prompt_cache_key 原值暴露。因此复测也不是隔离全通过。旧的 503 证据完整保留。

## 模拟器协议依据与边界

新增 Responses 文本 SSE：response.created / in_progress、output_item.added、content_part.added、output_text.delta / done、content_part.done、output_item.done、response.completed；使用 sequence_number、item_id、output_index、content_index 和对应 usage。不是给 Chat chunk 换一个路径，也不在 Responses SSE 末尾添加 Chat 的 `[DONE]`。

字段参考 [OpenAI 官方 Responses API](https://developers.openai.com/api/reference/python/resources/responses/methods/create) 与已安装 omp 所携带、注明来源为 openai-node 6.42.0 的 ResponseStreamEvent 类型。OpenAI 文档技能用于核对该协议依据，而非自行设计新协议。真实客户端 48/48 的解析和恢复成功是互操作证据，但不是完整供应商一致性认证。

模拟器只覆盖本次文本路径：不执行 tools，不输出推理签名，不实现图片、音频、OAuth、服务端持久化或真实缓存。Responses 要求 `store=false` 和完整 input；明确拒绝存储及 previous_response_id / conversation 用法，不假装实现。请求中原有工具声明及参数照常保留，没有为了模拟器能处理而从客户端删字段。

单元测试 20/20：回复池、Chat / Messages、错误与延迟、原有校验器负例，以及新增 Responses 历史和事件生命周期检查。

## 实现、复现与证据

新增主驱动 `deploy/test/provider_matrix.py`，独立校验 `deploy/test/validate_matrix.py`，Responses 模拟 `deploy/test/mock_responses.py`，无副作用 CLI 输出解析 `deploy/test/cli_output.py`。生产 Go 插件未修改。

在已有隔离 Compose 环境完成 build/up 和 seed 后：

```bash
docker compose -f deploy/compose.test.yaml run --rm --no-deps -e SEED_ONLY=1 probe python /test/seed.py
docker compose -f deploy/compose.test.yaml run --rm --no-deps harness python3 /test/provider_matrix.py
docker compose -f deploy/compose.test.yaml exec -T capture python /test/validate_matrix.py <RUN_ID>
docker compose -f deploy/compose.test.yaml exec -T capture python -m unittest discover -s /test -p 'test_*.py'
```

默认 driver 同时执行两条线路；可设 `MATRIX_LANES=direct` 或 `affinity`，用 `MATRIX_CASES=oc-native-openai` 等筛选注册。driver 为收集全部失败不中途停止；必须运行 validator，后者遇到兼容性、漂移或标识暴露返回 1。不要只把 driver 的退出码当作测试通过。

本地导出的三轮目录（artifacts 被 gitignore，需单独保存）：

- [直连完整矩阵](../artifacts/matrix-c6226f02-4388-4dd0-be6e-9970d0760d1f/validation.json)：48 个 CLI 用例，58 条推理请求全部 HTTP 200。
- [亲和完整矩阵](../artifacts/matrix-1b1d2602-fc13-49f1-a053-59f752778df4/validation.json)：48 个 CLI 用例，回复 32 成功 / 16 失败，严格校验失败。
- [标题模型补配置复测](../artifacts/matrix-d238a377-0487-4005-83df-8150e1c5ae93/validation.json)：4 个 CLI 用例、6 条推理请求，传输成功但标识隔离失败。

每个目录都有 runs.json、capture.jsonl、每次 CLI 日志、每种注册的脱敏原始配置快照和 validation.json。总计执行 100 个 CLI 用例、122 条推理请求。运行中发现的配置缺口、真实兼容性问题和生产设计缺陷分别记录，没有用补头或隐藏辅助请求使结果变绿。
