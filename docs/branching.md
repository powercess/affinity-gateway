# 分支模型

仓库使用「双分支晋升」模型：`dev` 是主干，`main` 是发布账本。

```text
feature/* ──PR(squash)──▶ dev ──晋升 PR(merge commit)──▶ main ──tag──▶ 镜像发布
```

## 分支职责

| 分支 | 角色 | 变更方式 |
| --- | --- | --- |
| `dev` | 默认分支、主干。所有开发都合入这里。 | 只能通过 PR（squash） |
| `main` | 发布分支。只接受来自 `dev` 的晋升 PR，tag 只打在这里。 | 只能通过 PR（merge commit） |

`dev` 是仓库默认分支，因此新 clone 和新建 PR 的 base 默认都落在它上面。

## 日常开发

```bash
git switch dev && git pull
git switch -c feat/my-change
# ... 提交 ...
git push -u origin feat/my-change
gh pr create --base dev
```

PR 合并后源分支会被自动删除。这是仓库设置 `delete_branch_on_merge` 的行为，不是
workflow：fork 来的 PR 源分支在对方仓库，不会被删。

合入 `dev` 使用 **squash**，一个 PR 对应 `dev` 上的一个提交。

## 发布：先晋升，再打 tag

1. 开一个 `dev → main` 的晋升 PR，用 **merge commit** 合并（不要 squash）。
   于是 `main` 上「一个提交 = 一次发布」，可审计。
2. 在该 merge commit 上打 annotated tag，见[发布](releasing.md)。

> `main` 会因为晋升产生 merge commit，所以分支保护**不要**开启
> "Require linear history"，否则晋升 PR 永远合不进去。

## 紧急修复

走正常路径（`dev` → 晋升）需要两个 PR，会拉长恢复时间。允许直接向 `main` 提 hotfix
PR，但**合入后必须立刻把 `main` 合回 `dev`**，否则下一次晋升会把这个修复覆盖掉：

```bash
git switch dev && git pull
git merge --no-ff origin/main -m "chore: back-merge hotfix from main"
git push
```

## CI 与保护规则

- `.github/workflows/ci.yml`：`test` 在 PR 及 `dev`/`main` 的 push 上运行；
  `containers` 只在 PR 和 `main` 上运行（三个镜像构建最贵，`dev` 的日常 push 不需要）。
- `.github/workflows/release.yml`：仅由 `v*` tag 触发，且第一步校验该 tag 是 `main`
  的祖先，避免误发布未经晋升的代码。
- `dev` 与 `main` 均配置了 ruleset：禁止删除、禁止 force push、必须走 PR、必须通过
  `test` + `containers` 状态检查。仓库管理员保留 bypass 权限，用于规则配错时自救。
