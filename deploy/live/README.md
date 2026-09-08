# Affinity Gateway · 真实供应商环境

本环境没有 mock、capture 和自动 seed，不修改现有测试环境和数据库。

## 配置与启动

1. 复制 `.env.example` 为 `.env`，填入四个独立随机密钥（建议各 64 位十六进制，控制台密码至少 32 字符）。保护文件权限。不要复用开发环境的公开密码。
2. 运行 `just live up`。控制台默认 `http://服务器:18342`，new-api 管理和模型入口默认 `http://服务器:18343`，绑定 `0.0.0.0`。公网使用前应加 TLS 和访问限制；当前 HTTP 不能保护密码和 API Key。
3. 登录亲和控制台，在“配置”中添加出口 ID 和真实供应商 Origin。Origin 例如 `https://relay.example.com`，不带 `/v1`。复制页面生成的内部 Base URL。
4. 在 new-api 页面初始化管理员，创建单 Key 的 OpenAI 或 Anthropic 渠道，选择明确的分组，配置模型和计费。

每个 new-api 渠道填写：

- Base URL：亲和控制台显示的内部地址，不能填写供应商域名，也不能使用测试环境的 capture 地址。
- Key：真实供应商 Key。网关不另存 Key，转发 new-api 按渠道协议生成的认证头。
- 严格模式会把已校验的 `X-Session-Affinity` 原样透传到网关出口；无需渠道请求头覆盖。

客户端使用 new-api 颁发的 Token，访问 18343 入口，并携带支持的明确会话标识（例如稳定的 `X-Session-Id`）。真实供应商响应经出口网关返回 new-api，再经入口网关返回客户端，统计仍由 new-api 完成。

## 出口规则

出口代理不保存供应商 Key，也不解析或改写供应商协议字段。它保留 new-api 发出的路径、查询、方法、请求头和请求体，只把内部出口地址换成真实 Origin。供应商 Key 由 new-api 渠道管理。

出口 ID 和 Origin 创建后不可修改。更换域名时新增一个出口 ID，并在 new-api 新建渠道；删除旧出口后，旧地址会明确失败，不会迁移到新域名。修改 DNS 指向或供应商内部路由不在此保证范围内。不要更换亲和密钥或丢失 new-api 数据库，否则无法延续原绑定。

出口增删即时生效，不需要 reload。`just live reload` 用于修改 Caddyfile 后验证并热加载。`just live status` 查看状态，`just live logs` 查看日志，`just live down` 停止但保留数据库卷。

宿主机需要 Go、just、Docker Compose。真实环境变量文件已被 Git 忽略，出口映射保存在 Docker 数据卷。尚未填写供应商凭证并实际发起请求前，不代表真实供应商验收完成。
