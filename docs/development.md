# 开发指南

> 以下为早期规划，第一版现已实现。实际构建、配置与功能边界见 [implementation.md](implementation.md)。

## 1. 技术选型

| 项 | 选择 | 理由 |
|---|---|---|
| 网关 | **Caddy v2** | 静态二进制、自动 HTTPS、反向代理一流、插件体系成熟 |
| 插件语言 | **Go** | Caddy 原生生态,Caddyfile 模块化 |
| 构建 | **xcaddy** | `xcaddy build --with <module>` 一行构建带插件的 Caddy |
| 运行 | Docker 容器 | macmini 无本地 Go 运行时,容器隔离、随 compose 管理 |

## 2. 项目结构(规划)

```
caddy-session-affinity/
├── go.mod
├── session_affinity.go      # 插件主模块(caddy.RegisterModule + http.handlers)
├── session/
│   └── session.go           # 会话识别优先级 + UUIDv7 生成/映射表
├── body/
│   └── body.go              # body 改写(metadata.user_id 注入)+ 流式透传
├── outbound/
│   └── outbound.go          # 出站供应商适配(按目标站点)
├── rules/
│   └── rules.go             # 规则引擎(拒绝/改写/审计,预留)
├── Caddyfile.example
├── docker-compose.yaml
└── docs/
```

## 3. Caddy 插件规范

- 模块注册:`caddy.RegisterModule(SessionAffinity{})`
- 命名空间:`http.handlers`(实现 `caddyhttp.MiddlewareHandler`)
- Caddyfile 指令:`RegisterHandlerDirective("session_affinity", parseCaddyfile)`
- 依赖注入:`caddy.AppID` / `Provision(ctx)` 中初始化映射表

```go
// 示意(非实现)
func (s SessionAffinity) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
    sid := session.Resolve(r)          // 识别(header → body gjson → token 指纹)
    if sid == "" { sid = session.New() } // UUIDv7 生成/映射
    r.Header.Set("x-opencode-session", sid)
    r.Header.Set("X-Session-Id", sid)
    return next.ServeHTTP(w, r)
}
```

## 4. 构建

```bash
# 本地开发
go mod init github.com/powercess/caddy-session-affinity
go get github.com/caddyserver/caddy/v2@latest

# 构建带插件的 Caddy
xcaddy build --with github.com/powercess/caddy-session-affinity
```

## 5. Caddyfile 示例(规划)

```caddyfile
# 入站代理:接管客户端流量,注入会话头后转发给 new-api
:8236 {
    reverse_proxy 192.168.1.123:8235

    session_affinity {
        # 会话识别优先级:header > body gjson > token 指纹
        source_header  x-opencode-session X-Session-Id
        source_body    metadata.user_id user
        inject_headers x-opencode-session X-Session-Id
        # 可选:改写 body metadata.user_id
        # inject_body_metadata user_id
        # 可选:规则引擎(拒绝/审计)
        # rules /etc/caddy/rules.json
    }
}

# 出站代理:按目标供应商适配(模式 B)
:8237 {
    reverse_proxy {
        dynamic { ... }   # 目标站点由请求决定
    }
    session_affinity {
        mode outbound
        vendor opencode  { header x-opencode-session {session_id} }
        vendor deepseek  { strip x-opencode-session }
        vendor volcengine { header X-Ark-... {session_id} }
    }
}
```

## 6. 实现路线(里程碑)

| 阶段 | 内容 | 验收 |
|---|---|---|
| M1 | 插件骨架:注册 + 会话识别 + UUIDv7 + 注入双头 + 流式反代 | 入站带稳定会话头,new-api 亲和日志 `key_source=request_header` |
| M2 | new-api 渠道透传配置验证 | 出站到供应商带会话头 |
| M3 | 出站适配插件(按供应商模板) | 目标站点收到正确头,非目标站点剥离 |
| M4 | 规则引擎(拒绝/审计) | 规则命中返回 4xx |
| M5 | docker-compose.yaml + 文档 + 开源发布 | 一键部署 |

## 7. 验证方法

- **亲和生效**:入站打带 `X-Session-Id` 的请求,查 new-api logs 表 `admin_info.channel_affinity.key_source`,应为 `request_header`;
- **会话稳定**:同会话连发 3 次,注入值不变、`key_fp` 相同;
- **出站透传**:用上游回显/抓包确认目标站点收到 `x-opencode-session`;
- **流式**:SSE 逐字节透传,无缓冲/截断。

## 8. docker-compose.yaml(规划)

```yaml
# 示意:入站代理 + new-api + (可选)出站代理
services:
  caddy-inbound:
    build: .
    ports: ["8236:8236"]
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile
    network_mode: host   # 或 bridge + extra_hosts,视宿主机可达性

  new-api:
    image: calciumion/new-api:latest
    ports: ["8235:3000"]
    # ...(沿用现有 compose)
```
