# caddy-session-affinity

基于 **Caddy** 的轻量会话亲和网关,置于 **new-api** 中转站前后,用插件方式接管入站/出站请求头,实现:

- **入站**:把来自任意源的请求归一化为统一的会话标识(`x-opencode-session` / `X-Session-Id`),供 new-api 原生"渠道亲和"按会话锁定渠道;
- **出站**:按目标供应商(`opencode.ai` / `DeepSeek` / 火山方舟等)适配请求头,降封号概率、提升缓存命中率与中转体验。

纯文档仓库:本仓库当前只包含**设计、架构、开发指南与实验记录**,不包含 Go 实现代码(实现计划见 [docs/development.md](docs/development.md))。

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

因此会话 ID 的**生成**与**按供应商适配**职责,落在本网关的插件上;new-api 保持零 fork。

---

## 架构一览

```
客户端(omp / Hermes / RikkaHub / Claude Code / ...)
        │  https://new-api.powercess.com
        ▼
┌───────────────────────────────┐
│  Caddy 入站代理 (:8236)       │  会话识别 → UUIDv7 生成/映射 → 注入双头
│  caddy-session-affinity 插件  │  (x-opencode-session + X-Session-Id)
└───────────────┬───────────────┘
                ▼
        new-api (:8235)         ← 原生渠道亲和(按 header 锁渠道),零配置
        │  出站(默认剥客户端头)
        ▼
┌───────────────────────────────┐
│  Caddy 出站代理                │  按供应商适配/剥离请求头(可选增强)
│  caddy-session-affinity 插件  │
└───────────────┬───────────────┘
                ▼
  opencode.ai / DeepSeek / 火山方舟
```

三种部署模式见 [docs/architecture.md](docs/architecture.md)。

---

## 文档索引

| 文档 | 内容 |
|---|---|
| [docs/architecture.md](docs/architecture.md) | 架构设计、外部代理如何托管 new-api 入站/出站、出站接管机制、三种部署模式、供应商适配策略 |
| [docs/testing.md](docs/testing.md) | **测试环境与亲和验证方案**:拓扑、harness 矩阵、断言口径、docker-compose、CI |
| [docs/development.md](docs/development.md) | 开发指南:模块划分、Caddy 插件规范(xcaddy)、Caddyfile 写法、实现路线 |
| [docs/experiments.md](docs/experiments.md) | 实验与测试记录:new-api 亲和、透传、会话稳定性的实测结论 |

---

## 快速上手(规划)

> 以下为规划中的用法,当前仓库未包含实现。

```bash
# 构建带插件的 Caddy(示例)
xcaddy build --with github.com/powercess/caddy-session-affinity

# 用 Caddyfile 启动入站代理
caddy run --config Caddyfile
```

示例配置见 [docs/development.md](docs/development.md) 的 Caddyfile 章节。

---

## 状态

- [x] GitHub 仓库创建(`powercess/caddy-session-affinity`)
- [x] 架构/开发/测试/实验文档
- [ ] 插件实现(规划中)
- [ ] docker-compose.yaml(规划中,测试编排见 [docs/testing.md](docs/testing.md))
