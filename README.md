# caddy-session-affinity

Docker Compose 已提供：[部署指南](deploy/README.md)。生产编排接入已有 new-api；
隔离测试编排包含独立 new-api、Postgres、Redis 和 mock 上游，并提供自动初始化与探针。

当前实施 [强亲和方案](docs/strict-affinity.md)：显式身份、SQL 原子持久绑定、禁止自动渠道迁移；[软亲和](docs/soft-affinity-deferred.md) 只保留设计，不实现。
最新 [6 种 Harness 强亲和测试报告](docs/strict-harness-report.md)：含 pi、Qwen Code、Kimi CLI；直连 92/92，严格入口明确区分成功/拒绝/恢复阻断，专用契约入口 24/24，160 个并发请求无渠道漂移。
原版行为与缺陷记录见 [初次 E2E](docs/harness-e2e-report.md)、[第一轮协议矩阵](docs/provider-matrix-report.md)，不能把历史结果当作当前保证。

基于 **Caddy** 的轻量会话亲和网关,置于 **new-api** 中转站前后,用插件方式接管入站/出站请求头,实现:

- **入站**：将可靠的原生会话标识派生为内部 `X-Session-Affinity`；缺失、冲突或无法检查时明确拒绝。
- **调度**：配套 new-api 补丁在发送前原子确定渠道，固定到单 key 渠道，存储或绑定渠道异常时失败关闭。
- **出站**：按供应商配置派生会话头和已声明的 body 会话/缓存字段；不改写供应商资源引用，不承诺真实缓存命中率。

当前配置与保证边界以 [强亲和契约](docs/strict-affinity.md) 和 [配置示例](configs/Caddyfile.example) 为准。以下动机保留早期讨论背景；不推断任意客户端都具有可用会话标识。

---

## 动机

| 问题 | 影响 |
|---|---|
| opencode.ai 自 2026-09-06 强制要求 `x-opencode-session`(每个会话一个稳定 ID),缺失将报错 | 客户端(omp / Hermes / AI SDK 系)普遍不发送该头,直连与经中转均缺 |
| new-api 渠道亲和粒度不足 | 无会话标识的客户端(Hermes 等)落到 `token_id` 兜底,会话级亲和失效 |
| new-api 转发时默认剥离客户端请求头 | 即使入站带上了会话头,出站也带不到上游 |

核心结论(经实测,详见 [docs/experiments.md](docs/experiments.md)):

1. **new-api 原生支持 `request_header` 亲和键**(`x-opencode-session` / `X-Session-Id` 作为 key_source 真实生效),也支持 `gjson` 从 body 提取 `metadata.user_id` / `user`;
2. **new-api 不能"生成"会话 ID**——它只能透传已有值(header_override / pass_headers),会话 ID 只能由客户端或中间代理生成;
3. **new-api 默认不透传客户端头**——出站头需要渠道显式配置 `header_override` / `param_override_template.pass_headers`,或由外部代理接管。

因此标识规范化与供应商适配由 Caddy 负责，强渠道绑定由固定版本 new-api 补丁负责；不再以零 fork 为目标。

---

## 架构一览

```
客户端(omp / Hermes / RikkaHub / Claude Code / ...)
        │  https://new-api.powercess.com
        ▼
┌───────────────────────────────┐
│  Caddy 入站代理 (:8236)       │  显式身份 → 租户作用域 HMAC
│  caddy-session-affinity 插件  │  X-Session-Affinity；缺失则拒绝
└───────────────┬───────────────┘
                ▼
        new-api (:8235)         ← 严格补丁：原子 SQL 绑定，不自动迁移
        │  出站(默认剥客户端头)
        ▼
┌───────────────────────────────┐
│  Caddy 出站代理                │  按供应商适配/剥离请求头(可选增强)
│  caddy-session-affinity 插件  │
└───────────────┬───────────────┘
                ▼
  opencode.ai / DeepSeek / 火山方舟
```

部署约束见 [docs/strict-affinity.md](docs/strict-affinity.md)。

---

## 文档索引

| 文档 | 内容 |
|---|---|
| [强亲和契约](docs/strict-affinity.md) | 当前实现、字段、原子绑定、失败策略、部署边界 |
| [软亲和备忘](docs/soft-affinity-deferred.md) | 暂缓实施的内容指纹方案 |
| [docs/architecture.md](docs/architecture.md) | 架构设计、外部代理如何托管 new-api 入站/出站、出站接管机制、三种部署模式、供应商适配策略 |
| [docs/testing.md](docs/testing.md) | **测试环境与亲和验证方案**:拓扑、harness 矩阵、断言口径、docker-compose、CI |
| [docs/development.md](docs/development.md) | 开发指南:模块划分、Caddy 插件规范(xcaddy)、Caddyfile 写法、实现路线 |
| [docs/experiments.md](docs/experiments.md) | 实验与测试记录:new-api 亲和、透传、会话稳定性的实测结论 |

---

## 快速上手

> 配套构建与测试使用 deploy/compose.test.yaml；接入现有 new-api 见 deploy/README.md。仅构建下面的 Caddy 不等于部署了强绑定。

```bash
# 构建带插件的 Caddy(示例)
xcaddy build v2.11.4 --with github.com/powercess/caddy-session-affinity=.

# 用 Caddyfile 启动入站代理
./caddy run --config configs/Caddyfile.example
```

示例配置见 [docs/development.md](docs/development.md) 的 Caddyfile 章节。

---

## 状态

- [x] GitHub 仓库创建(`powercess/caddy-session-affinity`)
- [x] 架构/开发/测试/实验文档
- [x] 第一版插件实现（显式会话 + 出站策略；真实 new-api/供应商联调待完成）
- [x] Docker 镜像与部署/隔离测试 Compose（见 deploy/README.md）
