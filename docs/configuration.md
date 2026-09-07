# 配置

## OpenCode Go

在控制台创建或选择 binding：

```text
ID: opencode-cn059
Origin: https://opencode.ai
插件: opencode-go-session@1.1.0
```

然后在 new-api 创建单 Key 渠道：

```text
Base URL: http://affinity-gateway:8237/r/opencode-cn059/zen/go
Key: OpenCode Go API Key
Models: 实际启用的 OpenCode Go 模型
```

不要配置 `X-Session-Affinity` 的渠道 header override。定制 new-api 在严格模式下原样
透传该内部标识，出口网关将它转换为 `x-opencode-session` 后删除。

插件不会生成或替换 `User-Agent`、`x-opencode-client`、`x-opencode-project`、
`x-opencode-request`；已有值只按 HTTP 转发链正常透传。

客户端必须为每个对话提供受支持的稳定会话标识，例如：

```http
X-Session-Id: one-stable-id-per-conversation
```

新对话必须使用不同值；同一对话和重试保持不变。缺失或冲突会失败关闭。
