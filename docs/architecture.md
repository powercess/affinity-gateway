# 架构设计

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
