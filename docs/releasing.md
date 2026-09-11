# 发布

分支模型见[分支模型](branching.md)：`dev` 是主干，`main` 是发布分支，tag 只能打在
`main` 上。

## 流程

1. 确认要发布的改动都已合入 `dev` 且 CI 通过。
2. 开 `dev → main` 晋升 PR，用 **merge commit** 合并。
3. 切到 `main` 拉取最新代码，在晋升 merge commit 上打语义化版本 tag：

   ```bash
   git switch main && git pull
   git tag -a v0.1.8 -m "v0.1.8"
   git push origin v0.1.8
   ```

`.github/workflows/release.yml` 先校验该 tag 是 `main` 的祖先（否则直接失败），再为
gateway、new-api、console 构建 `linux/amd64` 和 `linux/arm64` 镜像并推送到 GHCR，
同时生成 SBOM 和 provenance。

发布标签包括完整版本、major/minor、major、commit SHA 和 `latest`；tag 名含 `-`
（如 `v0.2.0-rc.1`）时不会打 `latest`。真实供应商 Key 永远不进入 GitHub Actions。

## CI

`.github/workflows/ci.yml` 在 PR 以及 `dev`/`main` 的 push 上运行 Go 测试和前端构建；
在 PR 和 `main` 上额外执行三个容器构建与发布 Compose 校验。CI 不调用真实模型供应商。
