# 部署

## 发布镜像

| 组件 | 镜像 |
| --- | --- |
| 亲和网关 | `ghcr.io/powercess/caddy-session-affinity-gateway:0.1.6` |
| 定制 new-api | `ghcr.io/powercess/caddy-session-affinity-new-api:0.1.6` |
| 控制台 | `ghcr.io/powercess/caddy-session-affinity-console:0.1.6` |

## 启动

```bash
cp deploy/release.env.example deploy/release.env
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml up -d
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml ps
```

必须先替换三个独立随机 secret。`AFFINITY_TEST=false` 时还必须设置至少 32 字节的
控制台密码。模型入口默认为 `0.0.0.0:18343`，控制台默认为 `0.0.0.0:18342`。

只有 gateway 和 console 发布宿主机端口。gateway 的 `8237` 出口、`8240` 控制 API，
以及 new-api、PostgreSQL、Redis 均只在 Compose 网络中访问。

## 开发

```bash
just up
just status
just reload web gateway new-api
just down
```

`just up` 使用源码构建的隔离开发环境；发布版 Compose 只拉取已发布镜像。

## 数据

- `affinity_config`：真实出口 binding 和插件链。
- `new_api_db`：new-api 数据及强亲和渠道绑定。
- Redis：缓存，不是强亲和的持久真相。

停止服务使用 `docker compose down`，不要附加 `-v`，否则会删除上述数据卷。

## 升级至 0.1.6（含 macmini）

发布镜像包含 linux/amd64 和 linux/arm64，Docker 会按主机架构选择镜像。
Intel 与 Apple Silicon macmini 均可通过 Linux 容器运行；不需要宿主机 Go 或 Bun。
本版本没有新增数据库迁移，也没有修改出口插件或亲和 HMAC 算法。

升级前备份正在使用的 Caddyfile、环境文件、网关配置卷和 new-api 数据库，记录旧版本。
保持原 Compose 项目名、数据卷、出口 ID 与 SESSION_AFFINITY_SECRET，不要重新生成密钥。
将现有环境文件中的 AFFINITY_VERSION 改为 0.1.6，然后使用原来的部署路径和项目名执行：

```bash
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml pull
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml up -d
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml ps
```

- **保留原 Caddyfile**：未配置 identity_source 时仍为原来的混合识别策略。
- **采用新版示例 Caddyfile**：默认 identity_source metadata，只带请求头的请求会被拒绝。
  示例同时匹配 Messages、Chat Completions 和 Responses；不要将这个默认策略直接用于
  未验证的 OpenAI 协议客户端。仅推荐已验证的 Claude Code / omp Messages 专用入口。
- **控制台已有保存规则**：继续优先于 Caddyfile 默认值；不会被升级自动覆盖。
  需要适配 omp 旁请求时，在对应入口选择“仅使用 metadata 会话”并保存。
- 同时升级 gateway 和 console，以获得完整的规则来源提示。网关重启会清空内存观测记录，
  并可能中断进行中的流式请求；持久化渠道绑定保留。
- 验证普通请求、/btw 和 recap，以及所使用的其他客户端；真实供应商验收仍需在部署现场完成。

回滚时恢复旧镜像版本和旧 Caddyfile；旧网关不认识 identity_source 指令，不能直接加载
新版示例配置。控制台保存的规则也应按备份恢复，不能仅靠回退镜像恢复原策略。
不要删除配置卷或数据库卷。这里是仓库层面的兼容性判断，不代替对实际 macmini 配置的检查。
