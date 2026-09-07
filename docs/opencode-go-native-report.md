# 官方 OpenCode Go 原生请求实测

日期：2026-09-07。OpenCode 1.18.29，原生 `opencode-go/deepseek-v4-flash`。

## 方法与范围

使用现有测试镜像，在独立、运行后自动删除的容器内执行真实 `opencode run`。认证读取现有 OpenCode Go 渠道的密钥，通过 stdin 传入，不打印、不写入代码或报告。未经过 new-api 或亲和网关，未修改现有渠道配置。

仅覆盖原生 provider 的 baseURL 为容器内捕获转发器地址，以及通过环境变量配置 apiKey；没有自定义 provider npm、模型定义或会话头。关闭分享、禁止工具执行以隔离测试。原生系统提示词和模型参数保留。捕获器将相同请求体和应用层请求头转发到 `https://opencode.ai/zen/go/v1/chat/completions`，Host 由 HTTP 客户端改为真实域名，连接层头不透传。它不是 TLS 原始线缆抓包。

脚本：`deploy/test/opencode_go_native.py`。脱敏请求体、请求头、SSE 响应、CLI 输出在忽略 Git 的 `artifacts/native-go/native-go.json`。

## 结果

| CLI 操作 | 实际 HTTP 请求 | 正文回复 | 结果 |
| --- | --- | --- | --- |
| 新会话，记住 ORCHID | 标题 + 正文 | OK | 2 × 200 |
| 用 --session 续聊，询问记忆词 | 正文 | ORCHID | 200 |
| 独立新会话 | 标题 + 正文 | HELLO | 2 × 200 |

五个请求都收到 HTTP 200 和 `text/event-stream`。三个正文流完整包含 `[DONE]`；两个标题流在转发期间记录 ConnectionResetError，未保存完整响应，不能声称标题生成完整成功，也未定位断开是哪一端触发。转发器测得耗时 1.701–2.569 秒（包含提前断开的标题请求）。断言验证续聊 session 不变、新会话 session 不同、没有 X-Session-Affinity。续聊 CLI 报告 cache.read=1920；这是供应商报告的缓存计数，不足以单独证明某个头必然导致缓存命中。

## 请求头

| 字段 | 实测值 / 生命周期 |
| --- | --- |
| Authorization | Bearer 认证（产物已脱敏） |
| User-Agent | `opencode/1.18.29 ai-sdk/provider-utils/4.0.23 runtime/bun/1.3.14` |
| x-opencode-client | `cli` |
| x-opencode-project | `global`（本次在无 Git 项目的 /work 下） |
| x-opencode-session | `ses_…`；标题与正文相同，跨 CLI 进程续聊不变，新会话改变 |
| x-opencode-request | `msg_…`；同一轮标题与正文相同，下一轮改变，不是 HTTP 请求唯一标识 |
| Content-Type | `application/json` |
| Accept | `*/*` |

原生请求未出现 X-Session-Affinity、X-Session-Id 或 Session-Id。

## 请求体

正文请求包含 `model=deepseek-v4-flash`、`max_tokens=32000`、`top_p=0.95`、`stream=true`、`stream_options.include_usage=true` 以及 messages。首轮为 system/user，续聊为 system/user/assistant/user，客户端重发历史。模型能回答记忆词不代表服务端只凭 session 保存了对话内容。

标题请求使用相同模型，另带 `temperature=0.5` 和 `reasoning_effort=low`，system 明确要求生成标题。本次未见 metadata、prompt_cache_key、previous_response_id。工具权限为 deny，因此没有 tools；不能推广为正常编码请求没有工具字段。

## 对出站插件的启示

- 强亲和应以可信上下文派生稳定的供应商会话标识，写入 x-opencode-session；不要每轮生成新值。
- project 和 client 不是会话标识；request 也不能替代 session，且不能用于 HTTP 请求去重。
- 标题等辅助请求属于同一个会话，统计和审计需要区分“用户轮次”与“上游 HTTP 次数”。
- 保留协议、消息、流式响应和正常认证；最终强制剥离内部 X-Session-Affinity。
- 不能从五次成功请求推断全部观察到的头都必需。本次没有删头对照实验，也没有测试 Anthropic/Responses 模型、工具循环、重试、压缩和 Git 项目场景。
- 本测试验证原生 harness 的成功请求形态，不证明任意 harness 加几个头即可兼容所有 OpenCode Go 模型；也不建议伪造 OpenCode 版本。

## 第一版适配策略

`opencode-go-session@1.0.0` 只根据可信的 `X-Session-Affinity` 和 binding ID
派生稳定的 `x-opencode-session`。Go 核心应用修改后强制删除内部亲和头。
`User-Agent`、`x-opencode-client`、`x-opencode-project` 和
`x-opencode-request` 只透传已有值；本插件不生成、不替换，也不把其他 harness
伪装成官方 OpenCode。身份伪造若未来确有需要，必须作为单独显式插件开发。
