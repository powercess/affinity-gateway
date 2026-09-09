# 旁会话复用主会话渠道：实现与实测

日期：2026-09-09。用户选择：`/btw`、recap 与主会话走同一个出口渠道。
已实现 metadata 唯一来源预设，真实 omp 和 Claude Code TUI 经完整测试链路通过。
**供应商端为协议模拟器；未部署生产、未调用真实供应商。**

## 原因与功能影响

实际 omp 18.1.11 / 18.1.15 请求形状：

```text
主请求：X-Claude-Code-Session-Id = P
        metadata.user_id = "{\"session_id\":\"P\"}"
旁请求：X-Claude-Code-Session-Id = P:side:<新ID>
        metadata.user_id = "{\"session_id\":\"P\"}"
```

这里观察到的头是 `X-Claude-Code-Session-Id`，不是推测的 `X-Session-Id`。
这是隔离环境的真实客户端抓包；不声称已经读取用户生产失败请求的原始值。

安装包的 `runEphemeralTurn` 显式设置临时 `sessionId`，同时通过
`prepareSimpleStreamOptions` 继承主 agent 的 provider metadata。
入站把这两种不同层次的 ID 当成必须相等的身份，因此正常旁请求被拒绝。

| 功能 | 证据与影响 |
|---|---|
| omp `/btw` | 真实 TUI 复现 400；旁问题不能获得回复；修复后 200 |
| omp idle recap | 真实 TUI 复现 400；闲置后的自动进度回顾失败；修复后 200 |
| omp 主对话与标题 | 此次原策略正常，不能称为全量请求故障；标题可能使用独立 metadata 身份，不强行合并 |
| omp IRC 自动回复 | 安装代码调用同一 `runEphemeralTurn`，有同类阻塞风险；未实际向他人发消息或测试 |
| omp 规则草稿生成旁请求 | 安装代码调用同一函数，有同类风险；未做交互端到端测试 |
| Claude Code 2.1.263 `/btw` | 实测头和 body 都保持主 ID，新策略下正常；没有复现 omp 的字段冲突，不能将故障笼统推广给 Claude |
| fork、子 agent、compact、handoff、其他 provider | 不在此次交互覆盖范围；应逐功能核对父会话、子会话、缓存身份的具体语义 |

Claude 官方将 `/btw` 描述为读取当前上下文、独立运行且不写入主历史的旁问题，
并说明其可复用父对话缓存：[交互模式](https://code.claude.com/docs/en/interactive-mode#side-questions-with-btw)。
本轮验证了历史不受旁问题污染；未测真实缓存命中、费用、思考签名或供应商行为。

## 实现选择

`identity_source metadata` 为 Caddyfile / JSON Handler 提供明确默认预设：

- `mode=strict`、metadata 开启。
- conversation / prompt_cache_key 后备来源关闭。
- 已知会话头不参与识别，但转发时仍移除。
- 缺 metadata、非法 JSON、重复键仍拒绝，不使用凭证或内容哈希兜底。
- 默认多来源入口的冲突拒绝逻辑不变，不根据 UA 或 `:side:` 前缀推断关系。

控制台提供相同预设，可更新既有持久化入口规则。**持久化规则优先于
Caddyfile 默认值**，所以已有部署需要保存 `main` 的新规则；详细步骤见
[部署说明](../deploy/live/README.md#claude--omp-旁请求)。

“关闭 metadata 校验”与此方案不同：它会选择临时 header ID，每个旁请求可能
产生新的渠道绑定，不能实现本次选择的跟随主会话。无需增加 warn-only 或任意冲突放行。

同一凭证、metadata 会话、分组、请求模型得到相同 canonical，new-api 复用原有
持久绑定；出站按相同站点和 canonical 派生相同供应商会话身份。
这不增加显式父子关系字段。不同模型仍可能分开绑定，单固定 key 渠道才保证同账号。
恢复同一会话时原本 header=metadata，因此 canonical 算法和既有正常主会话绑定不变。

## 测试环境与结果

独立项目 `affinity-side`，独立 PostgreSQL/Redis/抓包卷，真实 Caddy 插件和补丁
new-api；末端为现有 Python Messages/SSE 模拟器。CLI 仅连接 `internal: true`
网络，不挂载用户配置、项目工作区或生产凭证；只使用测试 token。omp 18.1.15
只额外只读挂载本机编译二进制，在容器内使用全新的 HOME。

出站使用测试 Caddyfile 的 `session_affinity mode outbound / policy derive`
静态路由。**这不是 `affinity_egress` 的 `opencode-go-session@1.1.0` 适配器路径**。
后者目前只派生 `x-opencode-session`，不执行这里验证的 body 身份重写。
因此，本报告不能证明 Claude/omp 旁请求经该适配器进入真实 OpenCode Go 后的
完整兼容性、缓存效果或供应商规则符合性；需要对实际供应商路径单独验证。

```text
真实 TUI → native tap → Caddy 入站 → canonical tap → new-api
         → egress tap → Caddy 出站 → supplier mock
```

独立 Python oracle 重算 HMAC，并断言入站 body 字节不变、已识别头清理、
new-api 透传、同会话同模型渠道不漂移、出站身份派生和限定字段改写。
拒绝请求不得出现 canonical/egress/supplier 记录。
最终驱动还在关闭 `/btw` 后发送普通主对话消息，检查前轮用户和助手历史仍存在，
旁问题没有混入主请求。

| CLI / 策略 | 实际交互结果 |
|---|---|
| omp 18.1.11 / 默认 strict | 主请求及标题 2×200，recap 和 `/btw` 2×400，拒绝不进入下游 |
| omp 18.1.11 / metadata | 主请求、标题、recap、`/btw` 4×200；旁请求与主请求同渠道同出站 ID |
| omp 18.1.15 / metadata | 主请求、标题、recap、`/btw`、后续主对话 5×200；身份/渠道/历史断言通过 |
| Claude Code 2.1.263 / metadata | 两次标题、主请求、`/btw`、后续主对话 5×200；身份/渠道/历史断言通过 |

Go `go test -race ./...`、`go vet ./...` 通过；metadata 与原 strict 准入定向回归
通过；前端入站规则测试 5/5、类型检查通过；Python 模拟器/oracle 原有测试 21/21
通过；live Caddyfile 可被新二进制适配。

探索记录不计入最终通过率：首次 TUI 被新用户向导/API key 本地确认阻断，配置
相应原生设置后重跑；共享旧测试栈发生暂时 503，随后改用独立实例；Claude 的
`[mock:resume:2]` 测试标记也进入标题请求，模拟器因标题不含前轮历史而返回 400。
最终改用普通续聊文本，保留标题功能，未修改客户端或网关来绕过该测试错误。

## 复现

先构建当前源码网关镜像及 CLI 镜像；以下在专用测试项目执行，不与其他推理探针并行。

```sh
export TEST_PORT=19236
# 下列命令的 compose 参数相同，可使用 shell 函数减少重复。
side_compose() {
  docker compose -p affinity-side -f deploy/compose.test.yaml -f deploy/compose.side-test.yaml "$@"
}
side_compose build affinity-gateway
side_compose --profile tools build harness
side_compose up -d affinity-gateway new-api postgres redis capture mock
side_compose run --rm --no-deps -e SEED_ONLY=1 probe python /test/console_seed.py
side_compose run --rm --no-deps -e SIDE_POLICY=strict harness python3 /test/side_sessions.py
side_compose run --rm --no-deps probe python /test/side_rules.py metadata
side_compose run --rm --no-deps harness python3 /test/side_sessions.py
side_compose run --rm --no-deps -e SIDE_CLIENT=claude harness python3 /test/side_sessions.py
# 完成或异常后均恢复；revision 冲突会报错，禁止直接覆盖当前规则。
side_compose run --rm --no-deps probe python /test/side_rules.py restore
side_compose down
```

`down` 不加 `-v`，保留证据。强制中断后检查备份与入口实际 revision。
首次 API 更新前写入备份；若进程在请求发出前中止，先核对 revision 再人工恢复。
测试驱动的完整机器断言以非零退出码报告失败，不能只依据 CLI 进程是否启动判断成功。

## 证据

原始测试请求、终端记录与校验报告保存在测试卷 `/artifacts/side-*`，仅含合成测试
内容。脱敏导出到仓库忽略的 `artifacts/side-study/`；不导出 token 或客户端 HOME。

- 默认拒绝基线：[omp 18.1.11](../artifacts/side-study/side-0e6a4b4c-39ac-4f8f-820c-9f9be20954e4/validation.json)。
- 初次修复：[omp 18.1.11](../artifacts/side-study/side-4b621290-7aec-4b49-97ed-c2b49296f975/validation.json)。
- 当前 omp 二进制：[18.1.15](../artifacts/side-study/side-caf10890-b579-49ed-acb3-be25149dea8d/validation.json)。
- 最终 Claude：[2.1.263](../artifacts/side-study/side-3f820b11-9ee8-4575-a4f1-9f46d6b714b6/validation.json)。

没有覆盖真实供应商、生产回滚、并行主/旁流式请求、OAuth、多 key、跨模型、所有
fork/子 agent 交互。`/btw` 在主请求仍流式输出时的并行模式仍应另加回归；本次
真实 TUI 测试是在主回答完成后触发旁问题。
