# 安全边界

- `X-Session-Affinity` 是内部可信标识，不是公网客户端可直接选择的供应商会话 ID。
- 定制 new-api 只在严格路由成功后透传它；所有严格渠道必须指向网关内部出口。
- 网关在公网出站前无条件删除该头。
- gateway `8237/8240`、new-api、PostgreSQL 和 Redis 不应映射到宿主机。
- 供应商 Key 由 new-api 保存，网关 binding 只保存 Origin 和插件链。
- `SESSION_AFFINITY_SECRET` 变更会改变派生身份；应备份并在所有实例保持一致。
- 不要将 `.env`、抓包、数据库备份或真实 API Key 提交到仓库。

`AFFINITY_TEST=true` 会完全关闭控制台认证。只有明确接受监听地址上所有访问者都能查看
和修改 binding 时才启用。模型 API 的 new-api Token 鉴权不受该开关影响。
