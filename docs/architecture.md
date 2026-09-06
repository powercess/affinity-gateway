# 架构设计

> 历史方案，不再作为实施规范。当前以 [强亲和契约](strict-affinity.md) 为准：Caddy 确定性 HMAC＋new-api 持久原子绑定；软亲和与内容哈希暂缓。以下零 fork、凭据兜底、进程内映射等描述均是旧讨论，不代表当前行为。

## 1. 总体拓扑

```
客户端(omp / Hermes / RikkaHub / Claude Code / 任意 OpenAI/Anthropic 兼容源)
        │  https://new-api.powercess.com
        ▼
┌───────────────────────────────────────────────┐
│ Caddy 入站代理(:8236)                          │
│  ① 会话识别(优先级见下)                        │
│  ② UUIDv7 生成 / 映射复用                      │
│  ③ 注入 x-opencode-session + X-Session-Id     │
│  ④ 原样转发给 new-api(body 逐字节透传,流式)     │
└───────────────────┬───────────────────────────┘
                    ▼
        new-api (:8235)   ← 零 fork、零配置改造
        │  原生渠道亲和:按 header/gjson 会话键锁渠道
        │  出站:默认只带 Content-Type / Accept(剥客户端头)
        ▼
┌───────────────────────────────────────────────┐
│ Caddy 出站代理(可选)                           │
│  按目标供应商适配/剥离请求头                   │
└───────────────────┬───────────────────────────┘
                    ▼
   opencode.ai / DeepSeek / 火山方舟
```

## 2. 为什么外部代理"托管"入站与出站

### 2.1 new-api 的事实边界(实测结论,详见 experiments.md)

| 能力 | new-api 行为 | 结论 |
|---|---|---|
| 会话亲和 | 原生支持 `request_header` 与 `gjson` 亲和键 | 入站只需注入稳定会话头即可锁渠道 |
| 会话 ID 生成 | **不能生成**;只能透传已有值 | 生成职责必须外置(客户端或代理) |
| 出站头透传 | **默认剥离**;需渠道显式配 `header_override` / `pass_headers` | 出站头由配置层或代理接管 |

因此"托管"意味着:

- **入站托管(生成)**:new-api 不产会话 ID,由 Caddy 入站插件负责识别会话、生成/复用 UUIDv7、注入请求头与 body 标识;
- **出站托管(适配)**:new-api 默认把会话头剥掉,若要按供应商补头/改头,由 Caddy 出站插件接管——按"会话 ID + 目标站点"决策,这是 new-api 配置层做不到的"动态"逻辑。

### 2.2 入站与出站的职责切分

| 方向 | 职责 | 谁做 | 为什么 |
|---|---|---|---|
| 入站 | 会话识别优先级 → UUIDv7 生成/映射 → 注入双头 | Caddy 入站插件 | new-api 不生成 |
| 入站 | (可选)改写 body `metadata.user_id` 兜底 | Caddy 入站插件 | 兼容只认 gjson 的规则 |
| 中转 | 渠道亲和锁渠道 | new-api 原生 | 零改造 |
| 中转 | 出站透传已有会话头 | new-api 渠道配置(`header_override`/`pass_headers`) | 配置层可做原样搬运 |
| 出站 | 按供应商动态变换/剥离头 | Caddy 出站插件(可选) | new-api 配置层只能静态透传 |

## 3. 会话识别优先级(入站)

```
1. body metadata.user_id        (Anthropic 协议,omp / Claude Code)
2. header x-opencode-session    (opencode 生态)
3. header X-Session-Id          (通用会话头)
4. body user                    (OpenAI 兼容的 user 字段)
5. 兜底:Authorization token 指纹 → 映射表查稳定 ID
```

- 命中已有标识 → **复用**(同一会话永不漂移)
- 无标识 → 查映射表:`<token|指纹>` → UUIDv7(存在则复用,不存在则生成)
- 每次请求随机生成 → **禁止**(破坏会话连续性与供应商缓存)

## 4. 会话映射表

- 存储:进程内存 `map[token|指纹 → UUIDv7]`
- 生成:UUIDv7(时间有序)
- 生命周期:进程重启可丢失(旧会话缓存过期无害)
- 扩展点:可选 Redis / SQLite 持久化

## 5. 出站供应商适配(规划)

出站插件按"目标站点"决定请求头策略,每个供应商一份适配模板:

| 供应商 | 需要的头 | 说明 |
|---|---|---|
| opencode.ai | `x-opencode-session`(会话稳定 ID) | 强制,缺则 2026-09-06 起报错 |
| DeepSeek | 一般无需会话头 | 仅透传必要头,避免多余头 |
| 火山方舟 | 按渠道要求 | 预留扩展模板 |

关键约束:

- **只有目标渠道需要会话头时才注入**——避免把会话 ID 泄漏给不需要的供应商;
- 出站代理可兜底**剥离**非目标渠道的会话头(白名单制),对冲"宽松透传"风险;
- 若仅需原样透传,优先用 new-api 渠道配置(`header_override: {"x-opencode-session": "{client_header:x-opencode-session}"}` 或 `pass_headers`),无需出站代理。

## 5.1 出站接管机制:Caddy 怎么截获 new-api 的出站流量

### 原理(源码确认)

new-api 出站请求 URL 完全由**渠道配置的 `base_url`** 决定:

- `relay/common/relay_utils.go:26` — `GetFullRequestURL(baseURL, requestURL)` = `base_url + 请求路径`
- 各适配器 `GetRequestURL`(openai/claude/...)均调用它拼接

因此"接管出站" = **把渠道 `base_url` 从真实供应商改为本机 Caddy 出站代理地址**,new-api 的所有出站流量先经 Caddy,再由 Caddy 转发真实供应商。全程不改 new-api 代码。

### 供应商识别:每渠道一个 path 前缀(推荐)

new-api 一个渠道只能配一个 `base_url`,但出站代理要区分多个供应商。用 **path 前缀分流**:

```caddyfile
:8237 {
    handle /opencode/* {
        uri strip_prefix /opencode          # 剥掉标记前缀,保留 /v1/...
        session_affinity outbound { vendor opencode ... }
        reverse_proxy https://api.opencode.ai
    }
    handle /deepseek/* {
        uri strip_prefix /deepseek
        session_affinity outbound { vendor deepseek ... }
        reverse_proxy https://api.deepseek.com
    }
}
```

对应 new-api 渠道配置:

| 渠道 | 原 base_url | 改为 |
|---|---|---|
| opencode | `https://api.opencode.ai` | `http://127.0.0.1:8237/opencode` |
| deepseek | `https://api.deepseek.com` | `http://127.0.0.1:8237/deepseek` |

每渠道一条 handle,供应商识别零歧义;亲和已在 new-api 锁渠道,出站代理只做头适配、不重新路由。(备选:单端口动态上游按 body model 猜供应商,会与 new-api 渠道映射重复,不推荐。)

### 会话 ID 如何到达出站代理

new-api 默认剥离客户端头(实测+源码),入站注入的 `x-opencode-session` 到出站 Caddy 时默认已丢失。两条恢复路径:

| 方式 | 做法 | 适用 |
|---|---|---|
| **A. 渠道透传 header(推荐)** | opencode 渠道配 `header_override: {"x-opencode-session": "{client_header:x-opencode-session}"}`,new-api 出站时"还原"入站会话头 | 会话头需到达供应商,零解析成本 |
| **B. body 透传** | `metadata.user_id` 跟随 body 透传(new-api 转发默认透传 body),出站代理从 body 提取 | 需要解析流式 body,有成本;仅当方式 A 不可行 |

### 完整链路与要点

```
客户端
  → 入站 Caddy(:8236):识别 → UUIDv7 生成/映射 → 注入 x-opencode-session / X-Session-Id
  → new-api(:8235):亲和按 header 锁渠道
      渠道 base_url = http://127.0.0.1:8237/<vendor>
      渠道 header_override 还原会话头(方式 A)
  → 出站 Caddy(:8237):按 path 识别供应商 → 适配/剥离头 → 转发真实供应商
  → opencode.ai / DeepSeek / 火山方舟
```

要点与坑:

1. **`Authorization` 头**:new-api 出站带的供应商 key,Caddy `reverse_proxy` 默认透传,不得改写;
2. **path 前缀必须剥离**:否则真实供应商收到 `/opencode/v1/...` 报 404,`uri strip_prefix` 解决;
3. **流式 SSE**:Caddy reverse_proxy 原生逐字节透传、不缓冲;
4. **TLS**:new-api → 出站 Caddy 本地 http;出站 Caddy → 供应商 https;
5. **职责边界**:仅"原样透传会话头" → 模式 A(new-api 渠道配置,无需出站代理);要"会话 ID → 供应商专属格式 / 差异化策略 / 剥离多余头" → 模式 B(出站代理)。


## 6. 三种部署模式

### 模式 A(推荐,最简):仅入站代理 + new-api 配置透传

```
客户端 → Caddy入站(:8236) → new-api(:8235) → 供应商
```

- 入站插件:识别/生成/注入
- new-api:亲和锁渠道 + 渠道配 `header_override` 透传会话头
- 无出站代理;最轻、最稳

### 模式 B(完整):入站 + 出站双代理

```
客户端 → Caddy入站(:8236) → new-api(:8235) → Caddy出站 → 供应商
```

- 出站插件:按供应商动态变换/剥离头
- 适合"会话 ID → 供应商专属格式"等配置层做不到的场景

### 模式 C(直连场景):客户端直连 opencode 的兜底

客户端直连 `api.opencode.ai` 时,由入站代理统一注入会话头再转发——覆盖不经 new-api 的流量。

## 7. 非目标

- 不改 new-api 源码(不 fork)
- 不做多租户计费、鉴权管理(new-api 已具备)
- 不替代 llmgateway(仅借鉴其会话亲和思路)
