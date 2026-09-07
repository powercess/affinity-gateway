# 强亲和契约与部署

当前只实现强亲和；[软亲和备忘](soft-affinity-deferred.md) 已搁置。

new-api 管理端通过只读接口 `GET /api/option/strict_affinity` 暴露强亲和的有效状态、持久绑定数量和存储类型。系统设置的渠道亲和页面会单独展示该状态；它不等同于 new-api 原生的 TTL 缓存亲和，也不能通过普通亲和开关关闭。

## 保证范围

固定到本部署控制的 single-key OpenAI(type 1) / Anthropic(type 14) new-api 渠道。绑定作用域是认证 token ID、规范会话 ID、明确分组、请求模型。同一会话不同模型可以分别绑定（例如主模型与标题模型）；这不是跨模型全局固定同一站点。供应商内部账号调度不在本项目控制范围。

强绑定使用主 SQL 数据库持久记录，不使用 TTL 缓存，不因 Redis 清空、请求失败或服务重启重新选取。首次候选通过唯一主键 insert-on-conflict 原子确定，竞争失败者读取胜出绑定；发往上游前验证当前渠道状态和路由/凭据配置摘要。失败不自动重试其他渠道，旧记录不删除。

当前不支持 auto 分组、显式渠道选择 token、多 key 轮换、其他渠道类型；这些场景拒绝。数据库不可读写、已绑定渠道禁用/删除、凭据/路由配置改变均拒绝。绑定记录不能在会话存续期间删除或恢复到丢失记录的旧备份。密钥与规范化算法升级必须有迁移方案，不得直接更换后重启。

## 身份提取

接收 Session-Id、X-Session-Id、X-Session-Affinity、X-Opencode-Session、Thread-Id、Conversation-Id、X-Claude-Code-Session-Id，以及已支持的结构化 metadata.user_id.session_id 或 conversation 资源 ID。

会话头重复、为空、超限、冲突，或与结构化 metadata 会话 ID 冲突时拒绝。始终检查 JSON，包括显式头已存在的情况；重复 JSON key、不可检查的压缩体、超出 body_limit 都拒绝，不在解析失败时改用凭据。入站成功不改写 body 字节。

Caddy 2.11.4 接入层为防止 CGI 头别名安全问题会丢弃含下划线的头。插件仍能在单元级识别 Session_id，但部署后不能依赖它；本项目不删除上游安全补丁。

`prompt_cache_key` 默认不是会话身份。操作员明确约定该字段是客户端持久化会话 ID 时，可在独立入口配置 `cache_key_as_session true`。这不是内容哈希，不是根据 User-Agent 自动猜测，也不应在任意公共客户端入口全局开启。需用真实客户端验证首次/恢复/新会话，并承担客户端遵守约定的前提。

`fallback credential`、`missing strip` 已不允许；旧配置将校验失败，而不是继续静默降级。

## 出站

`policy derive` 按站点派生头标识，并隔离 body 内的结构化 metadata session_id 和 prompt_cache_key。cache key 使用独立 HMAC 域并保留原键作为输入，不将不同缓存分组折叠成一个。只在需要重写时重新编码 JSON，其余字段语义不变；更新 Content-Length。不会改写 conversation / previous_response_id 资源引用，也不承诺隐藏所有用户元数据。

`policy strip` / `passthrough` 是显式供应商输出策略，不代表允许缺少入站会话身份；若需要站点标识隔离必须使用 derive。

## 错误

- 缺少身份：400，`affinity_identity_required`，提示提供 X-Session-Id；同会话/重试不变，新会话不同。
- 身份冲突、非法 JSON：400；body 超限：413；不支持的压缩：415。
- 绑定存储/渠道不可用、配置变化：503，不建议通过更换 session ID 规避。

网关错误通过 `X-Affinity-Error` 给出自定义机器码。Messages 使用 Anthropic error 外壳，Chat/Responses 使用 error 对象；不是供应商原生新增错误码。

## new-api 补丁与信任边界

[deploy/new-api](../deploy/new-api) 包含固定提交 f116414284162ad15d8925f7bca494c109b83e93（v1.0.0-rc.25）的补丁、源文件模板和完整镜像构建。设置 `STRICT_SESSION_AFFINITY=true` 启用；未设置保持原版调度，不提供上述强绑定保证。原版 UI/品牌和许可文件保留。

必须同时部署该补丁与 Caddy；仅替换 Caddy 不足以保证渠道强绑定。new-api 入站与 Caddy 出站端口必须只对可信中转网络开放，不允许客户端绕开网关。内部 canonical 头不是可在公网信任的签名凭据；new-api 仍执行原有 token 认证和模型授权。

测试 Compose 已使用补丁镜像；现有外部 new-api 部署需自行切换到该镜像并保留 SQL 数据卷。先备份数据库，禁止直接对生产运行测试 seed。
