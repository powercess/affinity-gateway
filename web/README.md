# Affinity Console

默认入口使用 `LiveConsole.tsx`，连接网关快照 API 与 SSE，支持认证、重新连接、配置/会话分组、请求查询和头部详情。查询失败不会回退到演示数据。网关配置和数据合同见 [观测接口](../docs/observability.md)。

原交互原型 `App.tsx` 仍可在 `?demo=1#/` 查看，使用 `src/data.ts` 中的合成样本。

## 运行

在 `web/` 下执行 `bun install --frozen-lockfile`，然后 `bun run dev`。仅监听 `127.0.0.1`，不改变现有 Docker 测试环境。`bun run build` 进行 TypeScript 校验并生成 `dist/`；`bun run test` 运行数据和交互测试。


Bun 固定为 **1.4.0**（根目录 `.bun-version`），本地、CI 和前端构建镜像使用同一版本。依赖只维护 `bun.lock`；添加依赖使用 `bun add`，安装使用 `bun install --frozen-lockfile`。

在仓库根目录可以使用：

- `just web-install`：安装锁定依赖。
- `just web-dev`：启动热更新开发服务，默认代理网关 `127.0.0.1:18242`。
- `just web-check`：依次运行测试、类型检查和生产构建。
- `just web-build`：类型检查并构建静态文件。
- `just web-test-watch`：修改代码后自动重跑相关测试。

也可以在 `web/` 运行对应的 `bun run` 脚本。使用 `bun run test`，不要使用 `bun test`：本项目的测试依赖 Vitest 和 jsdom。脚本通过 `--bun` 显式使用 Bun 运行 Vite、TypeScript 和 Vitest。`bun run preview` 可预览构建产物；与开发服务使用同一端口，需要先停止开发服务。

Docker 将依赖安装与源码构建分层，并缓存 Bun 下载目录。Go/Caddy 继续使用 Go 工具链；测试 harness 中第三方 CLI 的 Node/Python 环境保持各自要求。

技术栈：React / TypeScript / Vite、官方 shadcn/ui（Radix）组件、Tailwind CSS、Recharts、TanStack Table、React Router。使用 hash 路由，让静态托管下的详情链接可直接打开。官方组件通过 shadcn CLI 获取，保留其依赖与锁文件。

## 页面

真实数据界面当前展示最近 1000 个已完成的入口/出口处理事件，路由页列出已经产生事件的配置。详情是单个处理阶段，不是完整跨 new-api 请求链路。配置页管理持久化的“出口 ID → 真实 HTTPS Origin”映射，并生成 new-api 使用的内部 Base URL。WebSocket 尚未实现。

以下是保留的演示原型能力：

- 概览：从同一演示快照计算的指标、趋势和出口分布；入口拒绝和出口未观测不计入供应商分布。
- 路由：入口配置、出口卡片、出口详情及关联会话。
- 会话：按已识别指纹查看关联请求；不是 new-api 内部绑定表。
- 请求：查询、结果/出口/时间筛选、排序、分页、链接化详情；链路、识别依据、脱敏头部对照、响应四个标签。
- 配置：演示配置快照，只读，不是当前运行配置的自动导出。

所有时间显示 UTC。时间窗口相对于固定演示快照，不相对于浏览器当前时间。详情展示对应实体的完整演示历史，不受列表筛选影响。不存在的详情 ID 显式显示未找到。

## 后续接入边界

`src/data.ts` 的类型是前端演示模型，不是后端 API 合同。真实接口类型在 `LiveConsole.tsx` 和 `observation.go`。下一阶段完善跨阶段关联、完整路由目录和持久历史。实际响应必须继续经过 new-api；前端订阅不能影响代理转发。

后端已使用 allowlist 脱敏与 HMAC 指纹，不保留凭证原值。真实界面访问密码仅保存在页面内存，不写浏览器存储。公网部署需要 HTTPS 和管理访问限制；演示原型不连接 API。

测试覆盖数据一致性、过滤、详情导航、标签切换、Escape 关闭、分页、主题和缺失记录。jsdom 不提供实际布局，测试不等同于浏览器视觉验收。
