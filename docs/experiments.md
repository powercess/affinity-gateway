# 实验与测试记录

> 以下结论来自针对 new-api 的实测(含隔离实例复现与生产只读验证),是本项目设计的依据。

## 1. 结论速览

| # | 结论 | 依据 |
|---|---|---|
| 1 | new-api **原生支持 `request_header` 亲和键** | 实测:带 `X-Session-Id` / `x-opencode-session` 的请求命中亲和,日志 `key_source=request_header` |
| 2 | new-api 也支持 `gjson` 从 body 提取 `metadata.user_id` / `user` 作亲和键 | 实测:仅 body 携带时 `key_source=gjson` |
| 3 | header 亲和优先级高于 body | 实测:header 与 body 同时存在时取 header |
| 4 | new-api **不能生成**会话 ID | 源码:header_override 占位符仅 `{api_key}` / `{client_header:}`;无会话级生成 |
| 5 | new-api **默认剥离客户端头**出站 | 源码:`SetupApiRequestHeader` 只带 Content-Type/Accept;需渠道显式透传 |
| 6 | 同一会话(同 header 值)亲和稳定不漂移 | 实测:同 `X-Session-Id` 两次请求 `key_fp` 相同、锁同一渠道 |
| 7 | 无任何会话标识时亲和不命中,落到 token 兜底 | 实测:裸请求日志无 `channel_affinity` |

## 2. 实测方法

### 2.1 环境

- 镜像:`calciumion/new-api:v1.0.0-rc.25`(与生产同版本)
- 隔离实例:独立 postgres/redis,渠道指向生产 `new-api.powercess.com`(只读验证,未改生产任何配置)
- 亲和规则(测试用,4 源):
  ```json
  {
    "name": "test hs",
    "model_regex": ["^deepseek.*"],
    "path_regex": ["/v1/chat/completions"],
    "key_sources": [
      {"type": "request_header", "key": "X-Session-Id"},
      {"type": "request_header", "key": "x-opencode-session"},
      {"type": "gjson", "path": "metadata.user_id"},
      {"type": "gjson", "path": "user"}
    ],
    "ttl_seconds": 300
  }
  ```

### 2.2 关键证据(日志 `admin_info.channel_affinity`)

| 请求 | `key_source` | `key_key` / `key_path` | `key_fp`(会话指纹) |
|---|---|---|---|
| 仅 `X-Session-Id` 头 | `request_header` | `X-Session-Id` | `1dc6c12b` |
| 仅 `x-opencode-session` 头 | `request_header` | `x-opencode-session` | `0ac6b919` |
| 仅 body `metadata.user_id` | `gjson` | `metadata.user_id` | `2cc65cda` |
| 仅 body `user` | `gjson` | `user` | `152fa200` |
| `X-Session-Id` + body 同值 | `request_header` | `X-Session-Id` | `9a6b0a4c`(取 header) |
| 无任何标识 | (不命中) | — | — |

同一会话两次请求 `key_fp` 完全一致 → 锁同一渠道,亲和稳定。

## 3. 源码依据

### 3.1 入站亲和键提取(`service/channel_affinity.go:290-315`)

```go
switch src.Type {
case "request_header":
    return strings.TrimSpace(c.Request.Header.Get(src.Key))
case "gjson":
    // gjson.GetBytes(body, src.Path) → metadata.user_id / user
}
```

### 3.2 出站默认剥头(`relay/channel/api_request.go:51-64`)

```go
func SetupApiRequestHeader(...) {
    req.Set("Content-Type", c.Request.Header.Get("Content-Type"))
    req.Set("Accept", c.Request.Header.Get("Accept"))
    // 其它客户端头一律不复制
}
```

### 3.3 header_override 占位符全集(`relay/channel/api_request.go:154-188`)

- `{api_key}`:渠道 key
- `{client_header:<name>}`:搬入站头
- **无会话级生成占位** → new-api 不能自己造会话 ID

## 4. 对设计的约束

1. **入站必须注入稳定会话头**(生成),不能指望 new-api 生成;
2. **出站会话头需渠道显式透传**(`header_override` 或 `pass_headers`),否则被剥;
3. 亲和按 header 生效(已验证),入站插件只需注入双头,不必改写 body(改写 body 仅为兼容只认 gjson 的场景);
4. 出站适配要"按供应商隔离"——非目标渠道不注入会话头,避免泄漏。

## 5. 踩坑记录

### 5.1 rootless Docker 网络限制

本机为 rootless Docker,容器到宿主任意端口不可达(slirp 仅转发已发布端口,容器网关非宿主真实地址);容器可出公网(可访问 `new-api.powercess.com`)。影响:隔离实例验证入站转发可行,但无法直接观察"生产出站到供应商"的头。出站透传结论主要依据源码 + 生产日志 `key_source` 佐证。

### 5.2 亲和规则命中条件

规则需同时满足 `model_regex` 与 `path_regex`;实测若规则漏配 `gjson` 源,仅 body 请求不命中(测试制品问题,补源后正常)。

## 6. 待验证(生产侧,需授权)

| 项 | 方法 |
|---|---|
| 生产 opencode 渠道当前 `header_override` / `pass_headers` 是否透传 `x-opencode-session` | 只读查生产渠道配置 |
| 生产实际出站到供应商的 UA / 会话头 | 上游回显或抓包(生产侧) |
