# testkit — 可复现的 harness 容器与分层抓包

本地/CI 端到端验证台：**一组薄 harness 镜像 + 一个 L7 tap + 一个 L4 pcap sidecar**。
当前是 PoC：opencode / kimi 两个 harness，末端为离线 mock。接真实链路的方式见文末。

```
harness-*  ──(harness_net, internal)──▶  tap :8001  ──(backend)──▶  mock :8000
                                            ▲
                                      pcap sidecar
                                   (共享 tap 的 network namespace)
```

工具链：**TS 系列用 bun（安装 + 运行 + node 兼容），Python 系列用 uv（解释器 + 安装）**。

## 文件

| 路径 | 作用 |
|---|---|
| `base/Dockerfile` | 共享基础镜像：Debian + bun + uv，全部 digest/sha256 钉死 |
| `base/contract.sh` | 所有 harness 共用的入口契约 |
| `harnesses/matrix.json` | harness 矩阵唯一来源：版本、分发、lockfile、协议 |
| `harnesses/<name>/` | 每个 harness 的 `Dockerfile` + `run.sh` + `extract.py` + lockfile |
| `capture/` | mitmproxy L7 tap（`addon.py`）与多 hop 启动器（`run_hops.py`） |
| `pcap/Dockerfile` | alpine + tcpdump，L4 原始抓包 |
| `compose.poc.yaml` | PoC 编排 |
| `cases.json` | 用例：prompt、conversation、`expect_kind` 与对应判据 |
| `scripts/` | `build` / `check-matrix` / `smoke` / `enter` |

## 锁定

- 基础镜像：`debian:bookworm-slim`；bun 与 uv 分别从官方镜像 `oven/bun:1.4.2-debian`、`ghcr.io/astral-sh/uv:0.12.12` 按版本 tag 取二进制（多 arch 由官方 manifest 处理）；Python 由 `uv python install 3.13.15` 提供到 `/opt/venv`。构建期安装，运行期不联网装任何东西。
- 依赖完整性由 lockfile 负责（`bun.lock` 的 `sha512-`、`uv.lock` 的 `hash`），工具链完整性由官方镜像 tag 负责；仓库里不出现手抄的校验和。
- `/usr/local/bin/node` 软链到 bun，带 `#!/usr/bin/env node` 的 CLI 和 npm 包的 `postinstall` 不需要额外 Node。
- 每个 harness 版本写在 `matrix.json` 一处，`lib.sh` 导出给 compose 插值；`check-matrix.sh` 校验它与 `package.json`/`bun.lock`/`pyproject.toml`/`uv.lock` 一致。
- 升级工具链 = 改 Dockerfile 里那一行 tag（`oven/bun:<version>-debian` / `ghcr.io/astral-sh/uv:<version>`）。

| harness | 分发 | 锁定 |
|---|---|---|
| `opencode-ai` | npm，bun 安装 | `package.json` + `bun.lock` + `bun install --frozen-lockfile`；`trustedDependencies` 放行 postinstall |
| `kimi-cli` | PyPI，uv 安装 | `pyproject.toml` + `uv.lock` + `uv sync --frozen` |

## harness 契约

`run.sh` 对上层只暴露一个接口，runner 不需要知道任何 CLI 语法：

```
in : BASE_URL API_KEY PROMPT MODEL PROTOCOL RESUME HARNESS_HOME
out: stdout = 助手回复文本（逐字节）
     stderr = CLI 原始输出，同时落 $HARNESS_LOG_DIR/<case>.stdout|.stderr
```

- `RESUME` 接受 `1 / true / yes / on`。
- 会话状态落在 `HARNESS_HOME` 下（`HOME`/`XDG_*` 全部重定向）；**同一 conversation 的 first 与 resume 必须挂同一个 home**，由 `cases.json` 的 `conversation` 字段表达。
- 适配器负责把 `BASE_URL` 转成各客户端要求的形状：opencode 的 provider `baseURL` 带 `/v1`；kimi 的 `openai_legacy`/`openai_responses`/`kimi` 带 `/v1`，`anthropic` 不带。

## 用例判据

`cases.json` 的每条用例用 `expect_kind` 声明"什么算通过"：

| kind | 判据 |
|---|---|
| `reply` | stdout 与 `expect` 逐字节一致 |
| `probe` | harness **实际提供的会话身份**与 `matrix.json` 的 `session_identity` 一致，并落 `identity/<harness>.json` |
| `reject` | harness 非零退出，且该用例窗口内的抓包出现带 `expect_error` 的 4xx |

`reject` 已实现但当前未启用——PoC 链路里没有会拒绝请求的组件，接上网关后第一个"缺身份必须 400"的用例会用到它。

`probe` 是防"声明与实测漂移"的机制：`session_identity` 有 `supply`（`header` / `declared_body` / `none`）和 `fields` 两个字段，由探针用例每次运行核对。例如 opencode 是 `header` + `x-session-affinity`/`x-session-id`，kimi 在 `openai_legacy` 下是 `none`。

断言按 kind 分组统计，**"正确拒绝"不与"亲和通过"混算**。

## 抓包

| 层 | 组件 | 输出 |
|---|---|---|
| L7 | `capture/`（mitmproxy reverse + addon） | 每 hop 一个 in-path tap，脱敏 NDJSON：`hop / phase / trace_id / time_ns / method / path / headers / body_sha256 / status / streaming` |
| L4 | `pcap/`（tcpdump，`network_mode: service:tap`） | 共享 tap netns 抓原始字节，`tcpdump -r` 汇总 flows |

- in-path 而非嗅探：harness 的 base URL 直接指向 tap，"是否被抓到"无歧义。
- addon 在 `responseheaders` 对 `text/event-stream` 设 `stream=True`，避免 tap 自身缓冲改变被观测行为。
- `authorization / x-api-key / cookie` 等落盘前替换为 `[REDACTED]`。
- tap 注入 `X-Test-Trace-Id`，断言要求同一 trace 同时出现在 tap 与供应商侧 capture。
- 每次运行一个目录：`artifacts/testkit/runs/<run-id>/`，含 `capture.jsonl`（L7）、`pcap/`、`supplier/`、`replies/`、`logs/`、`identity/`、`home/`、`toolchains.txt`，以及 `case-runs.jsonl`——它记录每个用例的退出码和它在 `capture.jsonl` 中的行区间，让抓包可以按用例切片。

## 用法

前置：`docker`（含 compose 插件）、`jq`。`check-matrix.sh` 只需要 `jq`；`build.sh` 和 `smoke.sh` 需要 Docker。

```bash
TESTKIT_APT_MIRROR=http://mirror.rackspace.com bash testkit/scripts/build.sh   # 构建
bash testkit/scripts/check-matrix.sh                                          # 版本一致性（不需要 Docker）
bash testkit/scripts/smoke.sh                                                 # 端到端 + 断言
```

`just testkit-build` / `testkit-check` / `testkit-poc` / `testkit-shell` / `testkit-exec`。

`APT_MIRROR` 接受完整 URI，默认 `http://deb.debian.org`。若代理改写镜像导致 apt 哈希校验失败，换一个可达镜像即可；该步骤已内置 5 次重试与最终二进制校验。

## 手动测试

批量用例之外，还可以直接进一个干净的 harness 手动折腾：

```bash
just testkit-shell opencode                      # 交互式 shell，整段会话录制
just testkit-exec opencode 'opencode run --format json --model affinity/gpt-5.2 "hi"'
MODEL=gpt-4.1 just testkit-shell kimi            # 透传 MODEL / PROTOCOL
TESTKIT_RUN_ID=trial just testkit-shell opencode # 用独立的 run 目录
```

`enter.sh` 会把 `run.sh` 的前置准备全部做完（HOME/XDG 重定向、provider 配置生成、API key 环境变量），再把控制权交出去——所以进去的不是空容器，而是**已经指向 tap 的配置好的 harness**。banner 里会打印可以直接粘贴的 CLI 命令。

- **容器一次性**：`compose run --rm`，退出即销毁。跨会话保留的是 run 目录。
- **run 目录稳定**在 `artifacts/testkit/runs/shell/`，每个 harness 有独立的 `home/<harness>/`，所以 CLI 自己的会话状态能跨次延续（opencode 可以直接 `--continue`）。
- **退出后不拆栈**，方便再进去；停止用 `docker compose -f testkit/compose.poc.yaml down`。
- `enter.sh <harness> -- "<command>"` 是非交互版，给 agent 用：不需要 TTY，管道喂进去也能跑。

会话期间实时产出，agent 可以在进行中就读：

```
artifacts/testkit/runs/shell/
  logs/terminal.log   ← 敲了什么、CLI 回了什么（script -f，实时 flush）
  capture.jsonl       ← 每个 HTTP 请求/响应，脱敏、带 trace
  pcap/entry.pcap     ← 原始字节
  supplier/           ← mock 侧收到的
  case-runs.jsonl     ← 每次会话在 capture.jsonl 里的行区间（用 from/to 切片）
```

注意：TUI 类 CLI 的全屏重绘会让 `terminal.log` 充满 ANSI 转义，raw 录制保真但读之前建议剥掉；机器可读的权威记录是 `capture.jsonl`。

## 验证状态

`smoke.sh` 在 rootless Docker 上 24/24 通过，按 kind 分组：`reply` 4/4（opencode 与 kimi 各 first → resume，回复逐字节匹配，resume 由 mock 的严格历史校验背书）、`probe` 7/7（两个 harness 的身份供给与 `matrix.json` 一致）、`capture` 13/13（NDJSON schema 完整、凭据全脱敏、SSE 识别为 streaming、tap 与供应商侧 trace 一致、pcap 可被 tcpdump 解析）。`case-runs.jsonl` 的窗口覆盖全部抓包行，无遗漏无重叠。镜像：base 613MB / opencode 860MB / kimi 900MB / capture 333MB / pcap 15MB。

## 修改前必读

1. bun 默认拦截生命周期脚本；npm 类 harness 必须在 `package.json` 声明 `trustedDependencies`，否则 postinstall 不生成平台二进制。
2. bun 会安装所有平台的 optional 包（opencode 约 725MB）。必须在**同一个 `RUN` 内**安装后立即删除，否则体积留在层里；删除后需 `opencode --version` 验证仍可运行。
3. mitmproxy 12 用 `mitmdump` 控制台脚本启动；`python -m mitmproxy.tools.main mitmdump` 会静默退出 0。
4. `contract.sh` 位于 base 镜像 `/opt/testkit/contract.sh`；harness 构建上下文之外的路径不会被复制进镜像。
5. 新增 harness：加 `harnesses/<name>/{Dockerfile,run.sh,extract.py,lockfile}`，在 `matrix.json` 登记，`compose.poc.yaml` 加 service，`cases.json` 加用例；`check-matrix.sh` 会校验这几处是否齐全。

## 下一步

- 接真实链路：`CAPTURE_HOPS` 扩成四条，复用 `deploy/compose.test.yaml` 的 affinity-gateway / new-api / postgres / redis / mock。`cases.json` 的 `[mock:resume:N]` 已与 `deploy/test/replies.json` 对齐。
- 接真实供应商时再加 TLS 解密（mitmproxy 测试 CA，或 eBPF `ecapture` 兜底）。
- flows 汇总从 `tcpdump -r` 升级为 `tshark -z conv,tcp`。
- 接入 CI；harness 构建可用 matrix 并行。
