# Affinity Gateway

**亲和网关 · 强会话亲和，可定制的入站与出站。**

Affinity Gateway 是面向模型 API 中转的双向网关，基于 Caddy，配合定制 new-api 使用。从客户端会话识别、渠道持久绑定，到供应商会话字段适配，在请求进入和离开中转站时提供可配置的控制，并通过 Web 控制台查看处理结果。

## 核心能力

| 能力 | 当前实现 |
|---|---|
| **强会话亲和** | 从明确的客户端会话身份派生租户作用域标识；配套 new-api 补丁在发送前通过 SQL 原子确定渠道，持久绑定，不自动迁移 |
| **入站定制** | 按入口配置会话头的识别与移除、metadata 来源、严格 JSON 校验或仅请求头校验；支持草稿预览，保存后对后续请求生效 |
| **出站适配** | 管理“出口 ID → HTTPS Origin”映射，为 new-api 生成内部 Base URL；选择已支持的供应商插件，派生会话头及已声明的 body 会话／缓存字段 |
| **可观测控制台** | 查看入口与出口处理事件、会话指纹、规则版本和脱敏头部，通过 SSE 获取更新；管理入站规则与出口配置 |

定制能力的具体字段与范围见 [入站规则及管理 API](docs/observability.md)、[供应商配置](docs/configuration.md) 和 [Caddyfile 示例](configs/Caddyfile.example)。

## 请求如何经过网关

```text
客户端：稳定、明确的会话标识
    │
    ▼
Affinity Gateway · 入站
    识别与校验 → 租户作用域 HMAC → X-Session-Affinity
    │
    ▼
定制 new-api
    鉴权与计费 → SQL 原子渠道绑定 → 协议适配
    │
    ▼
Affinity Gateway · 出站
    出口映射 → 供应商插件 → 移除内部亲和标识
    │
    ▼
模型供应商
```

响应沿原链路返回。控制台连接网关管理 API，配置规则并观察各处理阶段；渠道持久绑定由 new-api 的 SQL 存储负责。

### 亲和保证的边界

- 强绑定需要网关与 **[固定版本 new-api 补丁](docs/strict-affinity.md)** 配套部署；仅运行 Caddy 插件不足以提供完整保证。
- 绑定限定在认证 token、会话、明确分组、请求模型的作用域内，当前支持单 key 的 type 1/14 渠道。不同模型可以分别绑定。
- 缺失、非法或冲突的会话身份会被拒绝；存储或绑定渠道异常时失败关闭，不自动切换渠道。不提供内容哈希或凭据兜底的[软亲和](docs/soft-affinity-deferred.md)。
- 出站只改写已声明支持的会话／缓存字段，不改写供应商资源引用，也不承诺供应商真实缓存命中率。
- 观测保留进程内最近 1000 条已完成事件，重启清空；每条记录对应一个处理阶段。持久历史、完整跨阶段请求追踪及 WebSocket 采集尚未实现。

## 快速部署

发布编排包含网关、定制 new-api、控制台、PostgreSQL 和 Redis。

```bash
cp deploy/release.env.example deploy/release.env
# 编辑 deploy/release.env，替换所有 secret 并设置控制台密码
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml up -d
```

默认模型 API 入口为 `:18343`，控制台为 `:18342`。new-api、PostgreSQL、Redis、网关出口与管理 API 只在 Compose 内网访问。公网部署的 HTTPS 与访问限制见 [部署说明](docs/deployment.md) 和 [安全边界](docs/security.md)。

启动后：

1. 登录控制台，在“配置”中添加出口 ID、真实供应商 HTTPS Origin，并选择需要的供应商插件。
2. 在 new-api 创建单 key 渠道，填写控制台生成的内部 Base URL、供应商 Key、明确分组及模型。
3. 客户端使用 new-api 颁发的 Token，并为每个会话携带稳定标识，例如 `X-Session-Id`；新会话使用新值，同一会话与重试保持不变。

完整步骤见 [真实供应商环境](deploy/live/README.md)。接入已有 new-api 见 [Docker Compose 部署](deploy/README.md)。

## 本地开发

整套隔离联调环境包含控制台、网关、定制 new-api、数据库和模拟供应商：

```bash
just up           # 构建并启动
just status       # 查看状态
just reload       # 重建前端与网关
just test         # 运行容器链路检查
just down         # 停止并保留数据卷
```

详见 [容器联调说明](deploy/console/README.md)。运行 `just` 查看全部命令。

只开发控制台：

```bash
cd web
bun install --frozen-lockfile
bun run dev
```

默认访问 `http://127.0.0.1:18240/`，开发服务器代理网关管理 API。`bun run check` 运行测试、类型检查与生产构建。真实界面为默认入口，`?demo=1#/` 保留演示原型。详见 [控制台开发](web/README.md)。

单独构建 Caddy 插件：

```bash
xcaddy build v2.11.4 --with github.com/powercess/caddy-session-affinity=.
./caddy run --config configs/Caddyfile.example
```

## 文档

| 文档 | 内容 |
|---|---|
| [强亲和契约](docs/strict-affinity.md) | 身份、原子绑定、失败策略与部署边界 |
| [配置](docs/configuration.md) | 供应商插件、渠道 Base URL 与客户端会话标识 |
| [观测与规则 API](docs/observability.md) | 入站规则、草稿预览、出口管理、事件与数据保留 |
| [部署](docs/deployment.md) | 发布镜像、启动方式与持久数据 |
| [安全边界](docs/security.md) | 网络、凭证与管理访问 |
| [架构](docs/architecture.md) | 双向代理、供应商适配与部署模式 |
| [开发指南](docs/development.md) | Caddy 模块与配置开发 |
| [强亲和测试报告](docs/strict-harness-report.md) | 六种 Harness 的验证结果与测试范围 |
| [测试方案](docs/testing.md) | 隔离环境、协议矩阵与验证方法 |

早期动机与实验保留在 [实验记录](docs/experiments.md)、[初次 E2E](docs/harness-e2e-report.md) 和 [协议矩阵](docs/provider-matrix-report.md)。历史结果不代表当前保证；运行行为以当前实现、配置与强亲和契约为准。

## 项目名称与兼容性

项目名称为 **Affinity Gateway（亲和网关）**，原名 `caddy-session-affinity`。Caddy 是网关的底层实现。

现有仓库地址与 Go 模块路径 `github.com/powercess/caddy-session-affinity`、已发布镜像前缀 `ghcr.io/powercess/caddy-session-affinity-*` 继续沿用。Caddy 指令 `session_affinity`、`affinity_console`、配置环境变量和协议头名称保持兼容，现有构建与部署命令无需因产品更名而调整。
