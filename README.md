<h1 align="center">Affinity Gateway</h1>

<p align="center">模型 API 亲和网关 · 强会话亲和 · 入站与出站定制</p>

<p align="center">
  <a href="https://github.com/powercess/caddy-session-affinity/actions/workflows/ci.yml"><img src="https://github.com/powercess/caddy-session-affinity/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <img src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white" alt="Go 1.26">
  <img src="https://img.shields.io/badge/Caddy-2.11.4-1F88C0" alt="Caddy 2.11.4">
  <img src="https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black" alt="React 19">
</p>

<p align="center">
  <a href="docs/deployment.md">部署</a> ·
  <a href="docs/configuration.md">配置</a> ·
  <a href="docs/observability.md">控制台与 API</a> ·
  <a href="docs/development.md">开发</a>
</p>

## 核心功能

- **强亲和**：在相同 Token、分组和模型下，同一会话固定使用同一渠道。渠道异常时返回错误，不自动切换。
- **入站定制**：配置从哪些请求头和 body 字段识别会话、移除哪些会话头，以及如何校验请求。支持预览规则，保存后立即生效。
- **出站定制**：配置供应商地址和适配插件，转换供应商需要的会话头及支持的 body 字段。
- **Web 控制台**：管理入站规则与出口供应商，查看请求记录、会话和脱敏后的请求头。

## 实现

```text
客户端 → Caddy 入站 → new-api → Caddy 出站 → 模型供应商
```

Caddy 入站校验客户端会话标识，用 HMAC 生成内部标识；定制 new-api 在发送请求前通过 SQL 原子写入渠道绑定；Caddy 出站按供应商插件转换会话字段，并移除内部标识。

强亲和需要配套的 new-api 补丁，目前支持单 Key 的 OpenAI / Anthropic 渠道。客户端需提供稳定会话标识（如 `X-Session-Id`），缺失或冲突时拒绝请求。详见 [强亲和说明](docs/strict-affinity.md)。

控制台使用 React，通过 API 管理配置，通过 SSE 接收观测更新。请求记录保留最近 1000 条，重启后清空。

## 启动

```bash
cp deploy/release.env.example deploy/release.env
# 编辑 deploy/release.env，设置密钥和控制台密码
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml up -d
```

模型 API：`:18343` · 控制台：`:18342`

在控制台添加供应商后，将生成的内部 Base URL 填入 new-api 渠道。完整步骤见 [部署指南](docs/deployment.md) 和 [供应商配置](docs/configuration.md)。
