# 隔离 Compose 验证：2026-09-06

> 本文为早期协议探针记录。后续已运行真实 OpenCode、Claude Code、omp、四段捕获和 600 请求压力回归，见 [详细 E2E 报告](harness-e2e-report.md)。本文末尾的未测范围只适用于早期这一轮。

环境：本地 rootless Docker，Caddy 2.11.4 + 本仓库插件，new-api v1.0.0-rc.25，
独立 Postgres/Redis，Python OpenAI Chat mock。无真实供应商请求，无生产配置修改。

执行 `docker compose -f deploy/compose.test.yaml run --rm probe python /test/seed.py`。
seed 自动创建测试管理员、两条 affinity-test 模型渠道和一条 strip-test 渠道，
通过渠道 header_override 透传 X-Session-Affinity，亲和规则读取该请求头。

基础验证已通过：

- 同会话 3 次普通请求及 1 次 SSE，站点和派生标识一致。
- 内部权威头、通用会话头未泄漏到 mock。
- no-session 路由剥离所有被识别的会话头。
- 8 个独立会话，每会话 3 轮，4 worker 并发：曾通过，但零轮间间隔复验发现间歇漂移，见下文。
- 本次并发请求实际覆盖 opencode-a、opencode-b 两个站点。
- 查询 new-api logs：已命中亲和的记录中，各指纹对应 count(distinct channel_id)=1，
  key_source 为 request_header；非流式与流式请求使用同一个指纹。
  注意首次未命中亲和的请求可能没有 channel_affinity 字段，不能只统计非空指纹来宣称全程不漂移。

## 未通过：首次绑定窗口

零轮间间隔并发测试连续两次失败，捕获同会话路由序列：
`opencode-a → opencode-b → opencode-b`。增加 TEST_TURN_DELAY=0.2 的对照运行通过。
默认 probe 保留零间隔和严格断言，未加自动重试或默认 sleep 掩盖问题。

源码证据：固定版本 middleware/distributor.go 的 Distribute 在 c.Next() 返回后才
调用 RecordChannelAffinity；它在下游处理完成后写入缓存。结合极快 mock 响应和上述
对照结果，首次响应已到客户端、绑定尚未可见的时序窗口是目前最可能的原因，未通过
缓存写入级追踪最终确认。此项需要 new-api 调度层进一步处理；未修改第三方镜像或
给代理增加任意等待。不能宣称当前链路在紧邻请求下严格锁定第一轮渠道。

对照命令：
`docker compose -f deploy/compose.test.yaml run --rm -e TEST_TURN_DELAY=0.2 probe python /test/seed.py`

源码：https://github.com/QuantumNous/new-api/blob/v1.0.0-rc.25/middleware/distributor.go

首次创建渠道后曾返回 503（数据库 abilities 已有记录，但内存渠道缓存未同步）。
seed 已增加只针对初始化的有界 readiness 等待，probe 本身不重试。
该版本登录 data 包含 user 和 access_token，seed 已适配，且不输出凭证。
规则显式清空旧的 path 和 param_override_template，避免配置反序列化保留默认模板。

边界：未测试真实供应商缓存、账号故障切换、TTL 过期、Anthropic/Responses 推理、
多 Key 账号固定和流式延迟阈值；本次 SSE 断言为完整性与身份连续性。
