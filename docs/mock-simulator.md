# 可配置供应商模拟器

仅用于隔离测试。支持 OpenAI Chat Completions、Responses 与 Anthropic Messages 的**文本回复子集**，包括 JSON 和 SSE。
不是完整模型服务，不支持工具调用输出、推理签名、多模态输出、OAuth 或真实缓存计费。
Responses 仅支持 `store=false`、完整 input 历史；不实现 `previous_response_id`、`conversation` 服务端持久化。
Responses 事件使用官方事件名称与字段，并由真实客户端解析验证；新增的注册矩阵与限制见 [协议矩阵报告](provider-matrix-report.md)。

## 回复池与恢复

编辑 [deploy/test/replies.json](../deploy/test/replies.json)。全局默认值可被每条回复覆盖。
同一场景的数组定义第 1、2、3……轮回复，选择方式是最新用户消息中的标记：

```text
[mock:resume:1] 开始测试
[mock:resume:2] 恢复之前的会话
[mock:resume:3] 继续第三轮
```

第二轮必须提交第一轮的用户消息和完整助手回复；第三轮必须按顺序带回前两轮历史。
支持字符串 content 与 `[{"type":"text","text":"…"}]` 内容块。
缺少历史、助手文本不一致、未知场景、超出池范围、一个用户消息有多个标记，返回对应协议的 JSON 400 错误。

示例恢复请求（Chat Completions）：

```json
{
  "model": "affinity-test",
  "stream": true,
  "stream_options": {"include_usage": true},
  "messages": [
    {"role": "user", "content": "[mock:resume:1] 开始测试"},
    {"role": "assistant", "content": "已记录测试口令：蓝鲸-42。"},
    {"role": "user", "content": "[mock:resume:2] 恢复之前的会话"}
  ]
}
```

它返回 `会话已恢复，之前的测试口令是蓝鲸-42。`。
Messages 使用 `/v1/messages`、模型 `claude-sonnet-4-6`，并添加正整数 `max_tokens`。
经 Caddy/new-api 发请求仍需有效测试令牌及可识别会话身份；场景标记**不参与亲和 ID 派生**。

回复选择无服务端递增计数器：同一历史重试返回相同文本；并发请求互不消耗池；模拟器重启后仍可复现。
这模拟的是客户端重放历史的恢复语义，不是供应商持有会话数据库；渠道/身份连续性由独立亲和校验器检查。
无场景标记时保留原有诊断 JSON 文本，兼容已有 affinity probe 和 smoke。

## 延迟设置

| 字段 | 含义 |
|---|---|
| `initial_delay_ms` | 开始返回 HTTP 响应前等待；JSON/SSE 均生效 |
| `chunk_interval_ms` | SSE 相邻文本分片间等待，结构事件不额外等待 |
| `end_delay_ms` | 最后一个文本分片后，到终止事件前等待；仅 SSE |
| `chunk_chars` | 每个文本分片 Unicode 字符数，不是 token 数 |
| `input_tokens` / `output_tokens` | 固定测试 usage 值，不是实际分词计数或供应商收费 |

例如：

```json
{
  "defaults": {"initial_delay_ms": 100, "chunk_interval_ms": 50, "end_delay_ms": 100, "chunk_chars": 4},
  "scenarios": {
    "demo": [
      {"text": "第一条预设回复"},
      {"text": "已恢复上一轮消息", "initial_delay_ms": 500}
    ]
  }
}
```

所有数值须为整数；延迟范围 0–10000ms，chunk_chars 为 1–100000。场景总模拟时长上限 30 秒。
配置中的未知字段、空回复池和无效值会在启动时报错。配置在启动时加载，不做请求中热更新，避免同一测试运行混用回复池。

```sh
docker compose -f deploy/compose.test.yaml restart mock
```

Compose 使用 `MOCK_REPLY_CONFIG=/test/replies.json`，可修改此环境变量选择另一份只读挂载的 JSON 文件。
修改 Compose 环境变量后使用 `docker compose -f deploy/compose.test.yaml up -d mock` 重建容器配置。
不要同时执行多个会重建 mock 的 Compose 命令。

## 响应字段与协议约束

按 OpenAI Docs 技能核对的 [Chat Completions 官方定义](https://developers.openai.com/api/reference/python/resources/chat/subresources/completions/methods/create)：

- JSON 包含独立 `chatcmpl-…` ID、`object=chat.completion`、整数 created、model、choices、usage。
- SSE 同一次响应 ID/created/model 保持一致，object 为 `chat.completion.chunk`。
- 首分片含 assistant role，随后 content 增量，结束 choice 为 `delta={}`、`finish_reason=stop`。
- `include_usage=true` 时普通分片 usage 为 null，额外最后用量分片 choices 为空；最终 `[DONE]`。
- 每个请求生成独立响应 ID，不再复用固定 `chatcmpl-mock`。

按 [Anthropic 流式规范](https://platform.claude.com/docs/en/build-with-claude/streaming)：

- JSON 为 Message 对象，包含独立 `msg_…` ID、role、content 文本块、model、usage、stop_reason、stop_sequence。
- SSE 顺序为 message_start → content_block_start → text_delta 分片 → content_block_stop → message_delta → message_stop。
- 每个 event 名称与 JSON type 对应，文本块 index 为 0；结束原因是 end_turn，不发送 OpenAI 的 `[DONE]`。
- 起始 Message 内容为空、结束原因 null；最后 message_delta 输出累计用量。

两者响应标识头分别为 `x-request-id` / `request-id`。错误为各自 JSON 包装，不再返回 HTML send_error 页面。
场景名、轮次和延迟配置只写入 `mock_reply` 测试捕获事件，不添加到供应商响应 JSON/SSE 的私有字段中。

边界：这里只验证正常文本终止，不模拟 max_tokens 截断、stop sequence、工具强制调用或推理事件。
usage 为可配置 fixture，不能用来验证真实 token 预算；不宣称完整供应商 API 一致性。
HTTP 请求需未压缩且具有 Content-Length，正文上限 2MiB。

## 测试及本次结果（2026-09-06）

```sh
python3 -m unittest discover -s deploy/test -p 'test_*.py' -v
docker compose -f deploy/compose.test.yaml run --rm probe python /test/smoke.py
docker compose -f deploy/compose.test.yaml run --rm -e HARNESS_RESUME_SCENARIO=1 harness
docker compose -f deploy/compose.test.yaml run --rm probe python /test/validate.py
docker compose -f deploy/compose.test.yaml run --rm probe python /test/export.py
```

本地 17/17 测试通过（10 个模拟器测试 + 7 个原有校验器测试）；包含真实回环 HTTP 测试：
设定 100ms 首包、40ms 分片、80ms 结束等待，检查两种协议实际到达时序，且测试容许少量计时误差。
Docker smoke 通过。模拟器已经在测试栈启动。

真实客户端运行 ID：`9f8e8e23-4d60-4147-b455-39c5fedc7201`。

| 配置 | 首次 / 恢复 / 第三轮 / 新会话预设回复 |
|---|---|
| OpenCode | 4/4 文本精确匹配 |
| Claude Code | 4/4 文本精确匹配 |
| omp Anthropic | 4/4 文本精确匹配 |
| omp Chat，显式每会话头 | 4/4 文本精确匹配 |
| omp Chat，无会话头 | 原有 3 个 400 失败保留，未触发第三轮 |

总计 19 个回复用例，16 个匹配。文本比较从 CLI 结构化结果/标准输出提取，不仅检查退出码。
本轮完整亲和校验仍复现 OpenCode 首轮跨站点，并发现 8 条 Messages 正文身份透传；18 条成功请求的分段 HMAC 校验通过。
恢复成功不代表原有首轮渠道竞态和正文身份透传问题已解决；完整 validate.py 仍会对这些问题返回非零。
本轮没有重跑原报告的 600 请求压力测试，原耗时/比例不可用于描述新的模拟延迟配置。
export.py 默认只导出当前 CLI 运行证据，避免附带旧的压力报告；需要附带明确对应的协议测试结果时显式设置 `EXPORT_PROTOCOL_RESULTS=1`。

[本轮回复校验结果](../artifacts/9f8e8e23-4d60-4147-b455-39c5fedc7201/recovery.json)；
[本轮完整请求与场景事件](../artifacts/9f8e8e23-4d60-4147-b455-39c5fedc7201/capture.jsonl)。
