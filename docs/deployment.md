# 部署

## 发布镜像

| 组件 | 镜像 |
| --- | --- |
| 亲和网关 | `ghcr.io/powercess/caddy-session-affinity-gateway:0.1.2` |
| 定制 new-api | `ghcr.io/powercess/caddy-session-affinity-new-api:0.1.2` |
| 控制台 | `ghcr.io/powercess/caddy-session-affinity-console:0.1.2` |

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
