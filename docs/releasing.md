# 发布

普通 push 和 pull request 运行 `.github/workflows/ci.yml`：Go、前端、三个容器构建及
发布 Compose 校验。CI 不调用真实模型供应商。

发布使用语义版本 tag：

```bash
git tag v0.1.0
git push origin v0.1.0
```

`.github/workflows/release.yml` 会为 gateway、new-api、console 构建
`linux/amd64` 和 `linux/arm64` 镜像并推送到 GHCR，同时生成 SBOM 和 provenance。

发布标签包括完整版本、major/minor、major、commit SHA 和 `latest`。发布前必须保证
工作树已提交且 CI 通过；真实供应商 Key 永远不进入 GitHub Actions。
