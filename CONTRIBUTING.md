# 贡献指南

## 分支模型

`dev` 是主干，`main` 是发布分支。**所有代码改动都向 `dev` 提 PR**，`dev` 和 `main`
都不能直接 push（已由 ruleset 强制，见[分支模型](docs/branching.md)）。

一个 PR 只做一件事。PR 合入 `dev` 使用 squash，合并后源分支会自动删除。

## 环境要求

| 依赖 | 版本 | 说明 |
| --- | --- | --- |
| Go | 1.26 | 见 `go.mod` |
| Bun | 1.4.0 | 见 `.bun-version`，前端在 `web/` |
| Docker + Compose | — | 构建镜像与端到端验证；只改纯逻辑时可跳过 |

仓库用 `justfile` 封装常用命令，`just` 是可选依赖；也可以用 `just --list` 查看全部命令。

## 本地校验

提交前请自行跑通与 CI 一致的检查：

```bash
bash scripts/mode_test.sh          # 管理脚本单测（等价于 just test-scripts）
go test ./...                      # Go 单元测试

cd web && bun install --frozen-lockfile
cd web && bun run check            # vitest + tsc + vite build
```

CI 还会额外构建三个镜像并校验发布 Compose，本地可选执行：

```bash
docker build -f deploy/Dockerfile -t affinity-gateway:dev .
docker build -f deploy/console/Dockerfile -t affinity-console:dev .
docker compose -f deploy/compose.release.yaml config --quiet
```

需要完整开发环境（热更新、模拟供应商、端到端用例）时：

```bash
just up            # 后台启动开发环境
just status        # 查看容器状态
just test          # 在已启动环境跑端到端测试
just down          # 停止并保留数据卷
```

## 提交 PR

```bash
git switch dev && git pull
git switch -c feat/my-change
# ... 修改并提交 ...
git push -u origin feat/my-change
gh pr create --base dev
```

- 目标分支必须是 `dev`。**不要**向 `main` 提功能 PR；`main` 只接受维护者的晋升 PR。
- 状态检查 `test` 和 `containers` 必须全绿才能合并。
- 纯文档 PR 也会跑 `containers`（约 3 分钟），这是让必需检查恒定回报的代价。

## 提交信息

仓库使用 Conventional Commits，常用前缀：`feat`、`fix`、`docs`、`ci`、`build`、
`test`、`chore`、`refactor`。正文说明**为什么**这么改，而不是复述改了什么。

```text
fix: recognize DSH official session header

DSH 的新版客户端改用官方 session 头，旧的识别顺序匹配不到，
导致同一会话被拆成多个 key。
```

## 不要提交

- **真实供应商 Key、token、`.env`**。CI 有一道检查会在命中
  `sk-[A-Za-z0-9_-]{20,}` 时直接失败；文档和测试里请用明显的假值。
  真实凭据只放在本地 `deploy/secrets/`（已 gitignore）或环境变量中。
- 构建产物与本地数据：`/bin/`、`/build/`、`/artifacts/`、`/web/dist/`、
  `/web/node_modules/`、`*.local` 等，完整列表见 `.gitignore`。

## 发布

发布由维护者负责：`dev → main` 晋升 PR（merge commit）→ 在 merge commit 上打语义化版本
tag。贡献者不需要关心版本号，详见[发布](docs/releasing.md)。
