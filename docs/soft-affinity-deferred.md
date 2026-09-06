# 软亲和：设计备忘（暂缓，不实现）

状态：DEFERRED。当前产品只实施强亲和；不提供内容哈希、用户/IP/凭据兜底或历史关联路由。本文不是已实现配置指南。

未来候选：租户隔离的首条用户消息 HMAC 形成路由分组；后续可研究请求/回复历史前缀关联。内容分组不是已证明的会话身份，不能直接写为供应商有状态 conversation ID。

可能误判：相同开场白、复制聊天、分支、上下文压缩、动态 system/tools、并发首轮、重试。哈希长度不能解决语义歧义。若重启此方案，需要独立配置身份来源、绑定迁移策略与失败行为，并在日志中明确 source=content_hash / mode=soft。

参考：

- [sub2api OpenAI 调度](https://github.com/Wei-Shaw/sub2api/blob/main/backend/internal/service/openai_gateway_scheduling.go)：显式来源与内容 seed 分层。
- [CLIProxyAPI 选择器](https://github.com/router-for-me/CLIProxyAPI/blob/main/sdk/cliproxy/auth/selector.go)：显式身份、别名及初始消息指纹。

这些是可借鉴的软亲和策略，不构成强亲和保证。当前缺少可靠标识必须返回可操作的拒绝错误。
