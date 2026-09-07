# 网关观测接口（第一阶段）

`session_affinity` 增加可选 `observe_id`。配置后记录该入口/出口处理完成的事件：时间、配置 ID、方向、模型、选中的会话字段、会话指纹、策略、状态、总耗时、处理前后头部。默认关闭，不改变既有配置行为。

当前存储为进程内最近 1000 条事件，重启清空，没有 Redis/SQL 依赖。前端配置页展示保留范围。它不是持久审计存储，也不是渠道绑定的权威数据。每条事件是一个网关处理阶段；同一会话用凭证隔离后的统一标识再作 keyed fingerprint 关联，不以时间邻近推测单次请求配对。

## 配置

在需要观察的插件块中加入 `observe_id harness-main` 或 `observe_id opencode-a`。入出站需使用相同的亲和 secret 才能关联会话指纹。出口 strip 策略若没有配置 secret，不产生可关联指纹。

在独立内部管理监听器配置：

```caddyfile
route /api/* {
    affinity_console AFFINITY_CONSOLE_PASSWORD SUPPLIER_CONFIG_FILE
}
```

环境变量必须至少 32 字节；用户名固定 `admin`，HTTP Basic 认证覆盖全部 API。不要在公网使用明文 HTTP；在管理入口提供 HTTPS 或经受保护的本地隧道访问。接口不开放 CORS，也不接受查询参数中的凭证。

- `GET /api/observations`：最新快照 `{items, revision, capacity, retention}`，最新事件在前。
- `GET /api/events`：`event: snapshot` 更新通知，每 20 秒保活。每次连接立即通知；客户端收到通知重新查询整个有界快照。因此断线恢复不依赖丢失的增量事件，不会重复追加记录。慢客户端写入设超时，不阻塞请求处理。
- `GET /api/suppliers`、`POST /api/suppliers`、`DELETE /api/suppliers/{id}`：查询、添加和删除持久化出口映射。写操作检查同源，出口 ID 和 Origin 不允许原位修改。
- 不支持的方法返回 405；未认证返回 401。

采集使用 Caddy 非缓冲 ResponseRecorder，不读取或重组响应流。记录亲和相关头的 HMAC 指纹，只记录 Authorization / X-Api-Key / Cookie 是否存在（值固定为 `[redacted]`），其余任意请求头和提示词不入库。不记录 upstream error 的原始字符串。页面显示的是插件交给下一处理器前的头部快照，不是 reverse_proxy 完成供应商认证重写后的抓包。

## 本地验证

使用 `go run ./cmd/affinity-caddy run --config configs/Caddyfile.console-local --adapter caddyfile`，预先设置 `AFFINITY_CONSOLE_PASSWORD`、`SESSION_AFFINITY_SECRET` 和 `SUPPLIER_CONFIG_FILE`。示例仅绑定本机；18241 的 `/v1/*` 为采集探针，18239 为动态出口。生产不要复制探针 respond 路由。

前端开发服务器默认代理 `/api` 至 Docker 控制台的 18242 端口，可用 `AFFINITY_CONSOLE_UPSTREAM` 指定其他管理入口。默认打开真实数据界面；原型仍可通过 `?demo=1#/` 查看。登录密码仅保留在当前页面内存，退出清除；事件流通过带认证头的 fetch 读取。

## 尚未实现

- 持久历史、多实例汇总。
- 经 new-api 透传并验证的单次请求追踪 ID、真实供应商最终目标和最终出站头部。
- 首字节耗时、WebSocket 消息级采集、进行中事件。

上述能力不会用假数据或推断结果补齐。接入真实流量前应在测试 Compose 中逐项验证，再决定启用范围。
