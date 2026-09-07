# 升级与回滚

升级前备份 PostgreSQL 和 `affinity_config` 数据卷，并记录当前
`SESSION_AFFINITY_SECRET`。修改 `AFFINITY_VERSION` 后执行：

```bash
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml pull
docker compose --env-file deploy/release.env -f deploy/compose.release.yaml up -d
```

回滚时将 `AFFINITY_VERSION` 改回原 tag 并再次启动。不要在升级或回滚时删除数据卷，
也不要更换亲和 secret。binding 的插件版本会保存在 `affinity_config` 中。
