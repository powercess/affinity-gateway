# 强亲和改造与扩展 Harness 测试报告

日期：2026-09-06。结论：已实施强身份准入、new-api 原子持久绑定和出站声明字段隔离。最终独立校验未发现同会话同模型渠道漂移或本次检查的会话字段原值泄漏。缺少可靠标识的客户端仍被拒绝，不把拒绝计为“客户端兼容成功”。软亲和仅有[暂缓文档](soft-affinity-deferred.md)，未实现。

## 1. 实现变化

- Caddy：新增 X-Claude-Code-Session-Id；请求头/结构化 metadata 冲突检测；即使有头也检查 JSON、重复 key、体积和编码；缺标识返回协议对应的 JSON 错误与 X-Affinity-Error，提示补 X-Session-Id。
- 删除可执行的 credential fallback / missing strip 配置。prompt_cache_key 默认不充当会话身份；只允许操作员在独立入口显式约定 `cache_key_as_session true`。
- 出站 derive：除了头，对 metadata.user_id JSON 字符串内的 session_id 和 prompt_cache_key 做站点隔离；保留 conversation / previous_response_id 资源引用及无关字段。
- new-api：固定 v1.0.0-rc.25 提交 f116414284162ad15d8925f7bca494c109b83e93，增加 strict_affinity_bindings SQL 表；唯一主键原子确定首次绑定，竞争请求读同一胜出渠道；无 TTL；禁止自动跨渠道重试；验证当前渠道状态和凭据/路由配置摘要。

保证限定为认证 token、会话、明确分组、请求模型作用域内的单 key type 1/14 渠道。不同模型可分别绑定；供应商内部账号调度不受我们控制。详见[强亲和契约](strict-affinity.md)。

## 2. Harness 搜集与版本

| Harness | 安装版本 | 此次覆盖 |
|---|---|---|
| OpenCode | 1.18.29 | 内置 OpenAI/Anthropic；三个自定义 SDK adapter |
| omp | 18.1.11 | 内置 OpenAI 两种模型、Anthropic；三个自定义 API |
| Claude Code | 2.1.263 | 原生 Anthropic |
| pi | 0.73.1 | 内置 OpenAI/Anthropic；三个自定义 API |
| Qwen Code（国内） | 0.23.0 | OpenAI、Anthropic |
| Kimi CLI（国内） | 1.50.0 | kimi、openai_legacy、openai_responses、anthropic |

配置依据：[pi 模型配置](https://github.com/badlogic/pi-mono/blob/main/packages/coding-agent/docs/models.md)、[Qwen Code provider 配置](https://github.com/QwenLM/qwen-code/blob/main/docs/users/configuration/model-providers.md)、[Kimi provider 文档](https://www.kimi.com/code/docs/en/kimi-code-cli/configuration/providers)。实际安装包与 --help 优先用于核对具体版本。

搜集时也查看了 [iFlow 配置](https://github.com/iflow-ai/iflow-cli/blob/main/docs_en/configuration/settings.md)、[腾讯 CodeBuddy 安装入口](https://github.com/Tencent/awesome-devbuddy)。它们是后续候选，本轮没有安装和执行，不能列入已验证矩阵。GUI/IDE 产品及依赖生产 OAuth 的注册方式不在本轮范围。

pi 使用 @mariozechner/pi-coding-agent 的固定 0.73.1 包；npm 安装提示该名称已弃用、后续迁往 @earendil-works。本轮没有偷偷更换包，结论仅针对所列版本。

Kimi 在线文档提到 openai，但 1.50.0 安装包 ProviderType 实际要求 openai_legacy。初次配置验证失败后，按真实类型修正并重跑；没有给客户端添加不存在的协议类型。Claude Code 无原生 Chat/Responses 注册的组合不构造假配置；Qwen 本轮未配置不存在于已核实 authType 列表中的 Responses 类型。

## 3. 环境和配置原则

真实二进制运行在 Docker 测试镜像。每种注册、每条线路使用独立 HOME/workspace；运行时客户端仅连接 internal harness_net，不接触宿主项目、生产配置、真实凭据或真实模型额度。

配置只设置必要的 provider 类型、base URL、测试 key、模型及 CLI 非交互输出。内置 provider 不覆盖模型 API/能力元数据；不注入会话头，不修改 harness 源码，不通过关闭标题任务使结果变绿。

OpenCode 禁用工具执行/分享；Kimi --quiet 是原生非交互选项，它会自动批准工具，但测试提示词要求纯文本，模拟器不产生工具调用，且运行环境为空工作区、内部网络。Kimi 自定义模型 schema 要求 max_context_size，本次设置 262144；它不是我们测量得到的模型能力。其他工具声明保留，仍只验证文本收发子集。

模拟器维持 Chat / Messages / Responses 对应的文本 JSON/SSE 格式；请求中不植入自定义协议字段。测试标记只是用户消息中的合成文本。恢复要求客户端真实提交前轮用户消息和助手回复，不靠模拟器全局计数装作恢复。OpenAI 字段依据通过 OpenAI Docs 核对，[官方 Responses API](https://developers.openai.com/api/reference/cli/resources/responses/methods/create) 与已安装 SDK 类型用于限定模拟器范围。

## 4. 主矩阵结果

每种注册执行首次、--continue 恢复、第三轮、新会话，共四个 CLI 进程。

| 注册 | 实际协议 | 直连回复 | 默认严格入口回复 | 拒绝说明 |
|---|---|---:|---:|---|
| OpenCode 内置 openai / gpt-5.2 | Responses | 4/4 | 4/4 | — |
| OpenCode 内置 anthropic | Messages | 4/4 | 4/4 | — |
| OpenCode @ai-sdk/openai-compatible | Chat | 4/4 | 4/4 | — |
| OpenCode @ai-sdk/openai | Responses | 4/4 | 4/4 | — |
| OpenCode @ai-sdk/anthropic | Messages | 4/4 | 4/4 | — |
| omp 内置 openai / gpt-4.1 | Responses | 4/4 | 0/4 | 下划线头丢失，缓存键默认不采用 |
| omp 内置 openai / gpt-5.2 | Responses | 4/4 | 0/4 | 同上 |
| omp 内置 anthropic | Messages | 4/4 | 4/4 | — |
| omp openai-completions | Chat | 4/4 | 0/4 | 无可靠会话标识 |
| omp openai-responses | Responses | 4/4 | 0/4 | 只有缓存键 |
| omp anthropic-messages | Messages | 4/4 | 4/4 | — |
| Claude Code 原生 | Messages | 4/4 | 4/4 | — |
| pi 内置 openai | Responses | 4/4 | 0/4 | 下划线头丢失，缓存键默认不采用 |
| pi 内置 anthropic | Messages | 4/4 | 0/4 | 无可靠会话标识 |
| pi openai-completions | Chat | 4/4 | 0/4 | 无可靠会话标识 |
| pi openai-responses | Responses | 4/4 | 0/4 | 下划线头丢失，缓存键默认不采用 |
| pi anthropic-messages | Messages | 4/4 | 0/4 | 无可靠会话标识 |
| Qwen Code openai | Chat | 4/4 | 0/4 | 未观察到可用会话字段 |
| Qwen Code anthropic | Messages | 4/4 | 0/4 | 未观察到可用会话字段 |
| Kimi openai_legacy | Chat | 4/4 | 0/4* | 无可靠会话标识 |
| Kimi openai_responses | Responses | 4/4 | 0/4* | 无可靠会话标识 |
| Kimi anthropic | Messages | 4/4 | 0/4* | metadata.user_id 是普通字符串；默认不推断为 session |
| Kimi kimi | Chat | 4/4 | 0/4* | 只有缓存键 |

*Kimi 首次和新会话收到 HTTP 400 后，不保留可恢复会话；随后两个 --continue 进程在本地退出，没有发出推理请求。这 8 个阶段标为“被首次拒绝阻断”，不能算作 HTTP 拒绝测试，也不能算恢复成功。

汇总：

- 直连：92/92 CLI 回复成功，110 条推理请求全部 HTTP 200（包括客户端辅助请求）。
- 默认严格入口：92 次 CLI 尝试；32 次回复成功、52 次请求按缺身份策略拒绝、8 次恢复在本地被阻断。
- 默认严格链路实际推理请求：42 条 HTTP 200，52 条 HTTP 400。42 条完整链路的入站 HMAC/字节保持、出站 HMAC/限定字段改写全部通过；52 条拒绝均未进入 canonical/egress/supplier，且有 affinity_identity_required 提示。
- 接纳注册的首次/恢复/第三轮 canonical 保持一致，新会话不同。同一 canonical、同一模型没有跨渠道漂移。

这里“严格校验 PASS”包含正确拒绝负例，不表示 23 种注册都能无配置直接使用。

## 5. 已声明缓存键语义的专用入口

对已核实把原生 session ID 放入 prompt_cache_key 的客户端，另外测试内部 8239 入口，操作员配置 cache_key_as_session true。客户端仅更换 endpoint，没有补头。该选项不默认打开，不能凭 User-Agent 自动启用，也不能推广到任意缓存分组键。

| 注册 | 回复与恢复 | 完整链路校验 |
|---|---:|---:|
| omp 内置 openai / gpt-4.1 | 4/4 | 4/4 |
| omp 内置 openai / gpt-5.2 | 4/4 | 4/4 |
| omp 自定义 Responses | 4/4 | 4/4 |
| pi 内置 openai | 4/4 | 4/4 |
| pi 自定义 Responses | 4/4 | 4/4 |
| Kimi 原生 kimi | 4/4 | 4/4 |

合计 24/24，续聊稳定、新会话分离、渠道稳定，出站 cache key 均已隔离。Kimi 安装源码 kimi_cli/llm.py 明确将 session_id 赋给 kimi provider 的 prompt_cache_key；OMP/pi 也通过其 provider 源码与原始抓包交叉核对。本轮没有对普通 metadata.user_id 开启类似宽松解释。

主结果口径为 92 直连＋92 默认严格＋24 专用契约，共 208 个 CLI 用例；不把探索性失败和重复运行混入通过率。

## 6. 强绑定与故障测试

真实 Caddy → 真实补丁 new-api → PostgreSQL → 出站 → 模拟供应商：

| 检查 | 实测 |
|---|---|
| 20 个新会话，各 8 个并发且消息内容不同 | 160/160 成功；每组只有一个 route 和一个出站 ID |
| 缺 ID、缓存键未声明、头冲突、头/body 冲突、普通 user 字符串 | 5/5 返回指定 400 错误 |
| new-api 重启前后使用同一会话 | 渠道与出站身份不变 |
| 上游模拟 503 | 只向原渠道发送一次，不跨渠道重试 |
| 已绑定渠道禁用 | 503 affinity_upstream_unavailable；供应商请求数 0 |
| 已绑定渠道凭据改变 | 503 affinity_binding_changed；供应商请求数 0 |
| 绑定表暂时不可查询 | 503 affinity_store_unavailable；供应商请求数 0 |
| 故障恢复 | 原渠道/身份恢复，不创建新绑定 |

存储故障通过临时重命名测试表实现，不删除记录；渠道状态/测试 key 修改后恢复。某次直接 SQL 恢复状态后，new-api 的内存能力缓存仍保留不可用状态，重启刷新后复验。初次重启探针也遇到服务尚未就绪，健康检查通过后才进行“重启后绑定不变”的断言。这些属于故障编排过程，不掩盖为无停机保证。

故障期间没有删除任何会话记录；最终表名、渠道状态和测试 key 已恢复。故障脚本的机器断言和每次供应商请求数见 faults.jsonl。

补丁单元测试另外验证 SQLite 下并发候选只能产生一个胜者、后续候选不可覆盖、不同绑定键独立、取消上下文不创建记录。PostgreSQL 的真实并发已在上述链路执行；MySQL 未实测，不能声称跨数据库全部验证。

## 7. 单元、构建与校验器

- Go 插件：go test -race ./...、go vet ./... 通过；9 个顶层测试包含严格准入表驱动子用例。
- Python 模拟器/独立 oracle：21/21 通过，包含拒绝 body 原值泄漏、改写资源 ID、改写无关字段等负例。
- new-api 固定提交与补丁完整镜像构建成功，保留原版前端；专用模型绑定单元测试通过。
- Caddy 新镜像运行成功；Compose 配置及补丁格式检查通过。

校验器没有调用生产 Go 的 HMAC 实现；独立重算入站/出站值，并比较允许改写字段之外的 JSON 语义。只检查本次声明的身份字段，不宣称隐藏了所有用户元数据。未覆盖工具执行、推理签名、图片音频、OAuth、真实模型能力或真实缓存计费。

## 8. 证据与复现

- [直连 validation-direct.json](../artifacts/matrix-7b4c8ad3-4a6f-4d6c-9743-46980003678f/validation-direct.json)、同目录 capture-direct.jsonl / runs.json / 配置 / CLI 日志。
- [最终串行严格矩阵](../artifacts/matrix-83780105-0dde-4168-9fcb-8e60c396be4c/validation.json)。
- [专用契约入口矩阵](../artifacts/matrix-b06e5e9c-38de-48a4-9d52-b53fed49979e/validation.json)。
- [并发校验](../artifacts/strict-83fed8f9-cfe0-4e63-ad1f-c9d0c5ba7bce/validation.json)、同目录 faults.jsonl 和 capture.jsonl。

最初混合运行 matrix-7b4... 的 affinity 时间窗曾与独立并发探针重叠，导致归属校验混入探针请求；该目录原始失败 validation.json 保留，**不用于最终严格结果**。最终默认严格矩阵另行串行重跑，避免筛掉异常让旧运行变绿。直连 lane 不受干扰，单独生成有明确命名的投影报告。抓包凭据落盘前脱敏，artifacts 被 gitignore，需单独归档。

```sh
docker compose -f deploy/compose.test.yaml build
docker compose -f deploy/compose.test.yaml --profile tools build harness
docker compose -f deploy/compose.test.yaml up -d affinity-gateway new-api postgres redis capture mock
docker compose -f deploy/compose.test.yaml run --rm --no-deps -e SEED_ONLY=1 probe python /test/seed.py
docker compose -f deploy/compose.test.yaml run --rm --no-deps harness python3 /test/provider_matrix.py
docker compose -f deploy/compose.test.yaml exec -T -e STRICT_MATRIX=1 capture python /test/validate_matrix.py <RUN_ID>
docker compose -f deploy/compose.test.yaml run --rm --no-deps probe python /test/strict_e2e.py
docker build --target unit -f deploy/new-api/Dockerfile deploy/new-api
```

不要并行运行 matrix 与其他推理探针。driver 收集全部结果后退出，并不以其退出码代表兼容通过；必须运行 oracle。STRICT_MATRIX=1 将“正确拒绝”作为策略验证成功；恢复成功数仍单独列出。专用入口通过 MATRIX_LANES=cache-contract 与 MATRIX_CASES 筛选上述六种注册，默认入口保持严格默认值。
