import { useEffect, useState, type ReactNode } from "react";
import { Link, NavLink, useLocation, useSearchParams } from "react-router-dom";
import {
  Activity,
  ArrowRight,
  ArrowUpRight,
  Check,
  ChevronRight,
  Copy,
  FileSliders,
  GitBranch,
  Layers,
  Moon,
  Network,
  Search,
  Sun,
  Terminal,
} from "lucide-react";
import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { RequestTable, Status } from "@/components/request-table";
import {
  requests,
  routes,
  filterRequests,
  summarize,
  timeline,
  timeLabel,
  outcomeLabels,
  type RequestRecord,
} from "@/data";

const navigation = [
  { path: "/", title: "概览", icon: Activity },
  { path: "/routes", title: "路由", icon: GitBranch },
  { path: "/sessions", title: "会话", icon: Layers },
  { path: "/requests", title: "请求", icon: Terminal },
  { path: "/config", title: "配置", icon: FileSliders },
];
function Panel({
  title,
  extra,
  children,
  className = "",
}: {
  title: string;
  extra?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <section className={`panel ${className}`}>
      <div className="panel-heading">
        <h2>{title}</h2>
        {extra}
      </div>
      {children}
    </section>
  );
}
function Fields({ items }: { items: [string, ReactNode][] }) {
  return (
    <dl className="fields">
      {items.map(([key, value]) => (
        <div key={key}>
          <dt>{key}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  );
}
function Flow({ record }: { record?: RequestRecord }) {
  const stopped = record?.outcome === "rejected";
  const unknown = record?.outcome === "unobserved";
  const nodes = ["Harness", "网关入口", "new-api", "网关出口", "供应商"];
  return (
    <>
      <div className="flow" aria-label="请求转发路径">
        {nodes.map((name, i) => (
          <div className="flow-step" key={name}>
            <div
              className={`flow-node ${(stopped && i > 1) || (unknown && i > 2) ? "unseen" : ""}`}
            >
              <span className="mono">0{i + 1}</span>
              <strong>{name}</strong>
              <small>
                {stopped && i > 1
                  ? "未转发"
                  : unknown && i > 2
                    ? "未观测"
                    : i === 2
                      ? "渠道调度"
                      : i === 3
                        ? (record?.egress ?? "出口匹配")
                        : i === 1
                          ? "识别 · 归一化"
                          : i === 4
                            ? "供应商响应"
                            : "客户端请求"}
              </small>
            </div>
            {i < 4 && <ArrowRight className="flow-arrow" size={16} />}
          </div>
        ))}
      </div>
      {!stopped && !unknown && <p className="flow-caption">响应：供应商 → 网关出口 → new-api → 网关入口 → Harness</p>}
    </>
  );
}
function RequestDetail({ row }: { row: RequestRecord }) {
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const route = routes.find((r) => r.id === row.egress);
  const canonical = row.session
    ? `sa:v1:…${row.session.slice(-2)}（指纹示意）`
    : "未生成";
  const scoped = route
    ? `${route.scope}:…${row.session?.slice(-2)}（指纹示意）`
    : "未观测";
  async function copyLink() {
    try {
      await navigator.clipboard.writeText(window.location.href);
      setCopied(true);
      setCopyError(false);
    } catch {
      setCopyError(true);
    }
  }
  return (
    <>
      <SheetHeader>
        <SheetTitle className="mono">{row.id}</SheetTitle>
        <SheetDescription>
          {row.client} · {timeLabel(row.at)} · {row.protocol}
        </SheetDescription>
      </SheetHeader>
      <div className="detail-body">
        <div className="detail-actions">
          <Status row={row} />
          <Button variant="outline" size="sm" onClick={copyLink}>
            {copied ? <Check /> : <Copy />}
            {copied ? "已复制" : "复制详情链接"}
          </Button>
        </div>
        {copyError && (
          <p role="status">无法访问剪贴板，请复制浏览器地址栏链接。</p>
        )}
        {row.reason && (
          <div
            className={`notice ${row.outcome === "unobserved" ? "" : "warning"}`}
          >
            {row.reason}
          </div>
        )}
        <Tabs defaultValue="trace">
          <TabsList className="detail-tabs">
            <TabsTrigger value="trace">链路</TabsTrigger>
            <TabsTrigger value="identity">识别依据</TabsTrigger>
            <TabsTrigger value="headers">头部对照</TabsTrigger>
            <TabsTrigger value="response">响应</TabsTrigger>
          </TabsList>
          <TabsContent value="trace">
            <Flow record={row} />
            <Fields
              items={[
                ["入口配置", row.ingress],
                [
                  "进入 new-api",
                  row.outcome === "rejected"
                    ? "未进入"
                    : "http://new-api:3000/v1/…",
                ],
                [
                  "观察到的出口",
                  route ? (
                    <Link to={`/routes?route=${route.id}`}>{route.name} ↗</Link>
                  ) : (
                    "未观测"
                  ),
                ],
                [
                  "new-api → 网关",
                  route
                    ? `http://affinity-gateway:8237${route.path}/v1/…`
                    : "未观测",
                ],
                ["供应商目标", route?.host ?? "未观测"],
                ["关联依据", "请求 ID"],
              ]}
            />
          </TabsContent>
          <TabsContent value="identity">
            <h3>会话识别</h3>
            <Fields
              items={[
                ["命中字段", row.source ?? "无明确会话字段"],
                [
                  "客户端标识",
                  row.session ?? "缺失",
                ],
                ["统一标识", canonical],
                [
                  "选择原因",
                  row.source
                    ? "明确标识，无冲突"
                    : "缺少会话标识",
                ],
                [
                  "出站策略",
                  route ? "derive · 按供应商作用域派生" : "未执行 / 未观测",
                ],
                ["供应商作用域", route?.scope ?? "—"],
                ["派生标识", scoped],
              ]}
            />
          </TabsContent>
          <TabsContent value="headers">
            <h3>入站 / 出站观测字段</h3>
            <div className="header-diff">
              <div className="diff-head">
                <span>字段</span>
                <span>进入网关</span>
                <span>发往供应商</span>
              </div>
              {[
                ["Authorization", "[已脱敏]", route ? "[已脱敏]" : "未观测"],
                [
                  "X-Session-Id",
                  row.source === "X-Session-Id" ? row.session! : "未提供",
                  route ? scoped : "未观测",
                ],
                [
                  "X-Session-Affinity",
                  "由网关生成",
                  route ? "移除内部标识" : "未观测",
                ],
                [
                  "Content-Type",
                  "application/json",
                  route ? "application/json" : "未观测",
                ],
              ].map((fields) => (
                <div key={fields[0]}>
                  {fields.map((field, i) => (
                    <span className="mono" key={i}>
                      {field}
                    </span>
                  ))}
                </div>
              ))}
            </div>
            {row.source?.startsWith("metadata") && (
              <div className="notice">
                本请求命中的是请求体字段 {row.source}，不是请求头。
              </div>
            )}
          </TabsContent>
          <TabsContent value="response">
            <Fields
              items={[
                ["HTTP 状态", row.status ?? "未知"],
                ["首字节耗时", row.ttft !== undefined ? `${row.ttft} ms` : "—"],
                [
                  "总耗时",
                  row.duration !== undefined ? `${row.duration} ms` : "—",
                ],
                [
                  "流式状态",
                  row.outcome === "success"
                    ? "SSE 正常结束（演示）"
                    : row.outcome === "unobserved"
                      ? "未知"
                      : "非成功流式响应",
                ],
                [
                  "返回路径",
                  row.egress
                    ? "供应商 → 网关出口 → new-api → 网关入口 → 客户端"
                    : "无完整供应商返回链路",
                ],
                ["Token / 费用", "不推断；由 new-api 负责统计"],
              ]}
            />
          </TabsContent>
        </Tabs>
      </div>
    </>
  );
}
function Overview({ rows }: { rows: RequestRecord[] }) {
  const summary = summarize(rows);
  return (
    <>
      <div className="metrics">
        {[
          ["请求总数", summary.total, "当前时间窗口"],
          ["识别会话", summary.sessions, "按会话指纹去重"],
          [
            "入口拒绝率",
            `${summary.total ? ((summary.rejected / summary.total) * 100).toFixed(1) : "0.0"}%`,
            `${summary.rejected} 条缺少明确标识`,
          ],
          [
            "P95 总耗时",
            summary.p95 === undefined ? "—" : `${summary.p95} ms`,
            "有完成耗时的请求，含入口拒绝",
          ],
        ].map(([label, value, hint]) => (
          <div className="metric" key={label} title={String(hint)}>
            <span>{label}</span>
            <strong>{value}</strong>
          </div>
        ))}
      </div>
      <div className="overview-grid">
        <Panel
          title="请求趋势"
          extra={
            <span className="legend">
              <i />
              请求 <i className="red" />
              拒绝
            </span>
          }
        >
          <div
            className="chart"
            role="img"
            aria-label={`演示请求趋势，共 ${summary.total} 条请求，其中 ${summary.rejected} 条入口拒绝`}
          >
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart
                data={timeline(rows)}
                margin={{ top: 20, right: 22, bottom: 0, left: -20 }}
              >
                <defs>
                  <linearGradient id="volume" x1="0" y1="0" x2="0" y2="1">
                    <stop
                      offset="0%"
                      stopColor="var(--accent-strong)"
                      stopOpacity={0.2}
                    />
                    <stop
                      offset="100%"
                      stopColor="var(--accent-strong)"
                      stopOpacity={0}
                    />
                  </linearGradient>
                </defs>
                <CartesianGrid stroke="var(--border)" vertical={false} />
                <XAxis
                  dataKey="time"
                  tick={{ fill: "var(--muted-foreground)", fontSize: 12 }}
                  axisLine={false}
                  tickLine={false}
                  minTickGap={35}
                />
                <YAxis
                  allowDecimals={false}
                  tick={{ fill: "var(--muted-foreground)", fontSize: 12 }}
                  axisLine={false}
                  tickLine={false}
                />
                <Tooltip
                  contentStyle={{
                    background: "var(--popover)",
                    color: "var(--foreground)",
                    border: "1px solid var(--border)",
                    borderRadius: 8,
                  }}
                />
                <Area
                  name="请求"
                  type="monotone"
                  dataKey="requests"
                  stroke="var(--accent-strong)"
                  fill="url(#volume)"
                  strokeWidth={2}
                  isAnimationActive={false}
                />
                <Area
                  name="拒绝"
                  type="monotone"
                  dataKey="rejected"
                  stroke="var(--danger)"
                  fill="transparent"
                  strokeWidth={1.5}
                  isAnimationActive={false}
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
          <div className="chart-note">UTC · 每分钟</div>
        </Panel>
        <Panel title="出口分布" extra={<Network size={16} className="muted" />}>
          <div className="distribution">
            {routes.map((route) => {
              const count = rows.filter((r) => r.egress === route.id).length;
              return (
                <Link to={`/routes?route=${route.id}`} key={route.id}>
                  <div>
                    <span>{route.name}</span>
                    <span className="mono">
                      {count} <ChevronRight size={14} />
                    </span>
                  </div>
                  <div className="bar-track">
                    <div
                      style={{
                        width: `${rows.length ? (count / rows.length) * 100 : 0}%`,
                      }}
                    />
                  </div>
                </Link>
              );
            })}
            <p className="muted">
              另有 {rows.filter((r) => !r.egress).length}{" "}
              条请求没有观察到出口，未计入上方分布。
            </p>
          </div>
        </Panel>
      </div>
      <Panel title="请求链路">
        <Flow />
      </Panel>
      <Panel
        title="近期异常"
        extra={
          <Link className="text-link" to="/requests?outcome=rejected">
            查看请求 <ArrowUpRight size={14} />
          </Link>
        }
      >
        <RequestTable
          rows={rows.filter((r) => r.outcome !== "success").slice(0, 5)}
        />
      </Panel>
    </>
  );
}

export default function App() {
  const location = useLocation();
  const [params, setParams] = useSearchParams();
  const [dark, setDark] = useState(() => {
    try {
      const saved = localStorage.getItem("affinity-theme");
      return saved
        ? saved === "dark"
        : (window.matchMedia?.("(prefers-color-scheme: dark)").matches ??
            false);
    } catch {
      return false;
    }
  });
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
    try {
      localStorage.setItem("affinity-theme", dark ? "dark" : "light");
    } catch {
      /* Theme persistence is optional. */
    }
  }, [dark]);
  const page = navigation.find((n) => n.path === location.pathname);
  useEffect(() => {
    document.title = `${page?.title ?? "页面未找到"} · Affinity`;
  }, [page?.title]);
  const query = params.get("q") ?? "",
    outcome = params.get("outcome") ?? "all",
    egress = params.get("egress") ?? "all";
  const minutes = params.get("window") === "15" ? 15 : 60;
  const rows = filterRequests(requests, query, outcome, egress, minutes);
  const requestID = params.get("request"),
    routeID = params.get("route"),
    sessionID = params.get("session");
  const row = requests.find((r) => r.id === requestID),
    route = routes.find((r) => r.id === routeID);
  const sessionRows = requests.filter((r) => r.session === sessionID);
  const open = Boolean(requestID || routeID || sessionID);
  function update(key: string, value: string) {
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      if (value && value !== "all") next.set(key, value);
      else next.delete(key);
      return next;
    });
  }
  function close() {
    setParams((previous) => {
      const next = new URLSearchParams(previous);
      ["request", "route", "session"].forEach((key) => next.delete(key));
      return next;
    });
  }
  const sessionIDs = [
    ...new Set(rows.flatMap((r) => (r.session ? [r.session] : []))),
  ];
  return (
    <div className="app-shell">
      <a className="skip-link" href="#main">
        跳到主内容
      </a>
      <aside className="sidebar">
        <Link to="/" className="brand">
          <div className="brand-mark">
            <Network size={22} />
          </div>
          <span>
            Affinity<small>SESSION GATEWAY</small>
          </span>
        </Link>
        <nav aria-label="主导航">
          {navigation.map((item) => (
            <NavLink end={item.path === "/"} to={item.path} key={item.path}>
              <item.icon size={18} />
              <span>{item.title}</span>
              {item.path === "/requests" && (
                <span className="nav-count">48</span>
              )}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <div className="breadcrumbs">
            工作空间 <ChevronRight size={14} />
            <span>{page?.title ?? "未找到"}</span>
          </div>
          <div className="topbar-actions">
            <span className="connection" title="合成样本，未连接实时接口">演示数据</span>
            <Button
              variant="ghost"
              size="icon"
              aria-label={dark ? "切换浅色主题" : "切换深色主题"}
              onClick={() => setDark(!dark)}
            >
              {dark ? <Sun /> : <Moon />}
            </Button>
          </div>
        </header>
        <main id="main">
          <div className="page-heading">
            <div>
              <h1>{page?.title ?? "页面未找到"}</h1>
            </div>
            {page && page.path !== "/config" && (
              <label className="window-select">
                时间窗口
                <select
                  aria-label="时间窗口"
                  value={minutes}
                  onChange={(event) => update("window", event.target.value)}
                >
                  <option value="15">最近 15 分钟</option>
                  <option value="60">最近 1 小时</option>
                </select>
              </label>
            )}
          </div>
          {location.pathname === "/" && <Overview rows={rows} />}
          {location.pathname === "/routes" && (
            <>
              <Panel
                title="入站配置"
                extra={<Badge variant="outline">1</Badge>}
              >
                <div className="ingress">
                  <div className="route-icon">
                    <Network />
                  </div>
                  <div>
                    <h3>harness-main</h3>
                    <p className="mono">:8236 /v1/* → new-api:3000</p>
                  </div>
                  <span className="status success">明确 session 标识</span>
                </div>
              </Panel>
              <Panel title="出站供应商">
                <div className="route-grid">
                  {routes.map((route) => (
                    <Link
                      className="route-card"
                      to={`/routes?route=${route.id}`}
                      key={route.id}
                    >
                      <div className="route-card-title">
                        <div className="route-icon">
                          <GitBranch size={20} />
                        </div>
                        <ArrowUpRight size={16} />
                      </div>
                      <h3>{route.name}</h3>
                      <p className="mono">{route.path}</p>
                      <div className="route-card-meta">
                        <Badge variant="secondary">{route.protocol}</Badge>
                        <span className="mono">derive</span>
                      </div>
                      <div className="route-card-count">
                        <strong>
                          {rows.filter((r) => r.egress === route.id).length}
                        </strong>
                        <span>观察到的请求</span>
                      </div>
                    </Link>
                  ))}
                </div>
              </Panel>
              <Panel title="转发与返回路径">
                <Flow />
              </Panel>
            </>
          )}
          {(location.pathname === "/requests" ||
            location.pathname === "/sessions") && (
            <>
              <div className="filters">
                <div className="search-field">
                  <Search size={16} />
                  <Input
                    aria-label="搜索请求或会话"
                    placeholder="搜索 ID、客户端、模型或命中字段…"
                    value={query}
                    onChange={(event) => update("q", event.target.value)}
                  />
                </div>
                <select
                  aria-label="请求结果"
                  value={outcome}
                  onChange={(event) => update("outcome", event.target.value)}
                >
                  <option value="all">所有结果</option>
                  {Object.entries(outcomeLabels).map(([key, value]) => (
                    <option key={key} value={key}>
                      {value}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="出口筛选"
                  value={egress}
                  onChange={(event) => update("egress", event.target.value)}
                >
                  <option value="all">所有出口</option>
                  {routes.map((route) => (
                    <option key={route.id} value={route.id}>
                      {route.name}
                    </option>
                  ))}
                </select>
                {(query || outcome !== "all" || egress !== "all") && (
                  <Button
                    variant="ghost"
                    onClick={() =>
                      setParams(minutes === 15 ? { window: "15" } : {})
                    }
                  >
                    清除筛选
                  </Button>
                )}
              </div>
              {location.pathname === "/requests" ? (
                <Panel
                  title="请求记录"
                  extra={<span className="muted">{rows.length} 条</span>}
                >
                  <RequestTable rows={rows} />
                </Panel>
              ) : (
                <Panel
                  title="已识别会话"
                  extra={
                    <span className="muted">
                      当前筛选范围 · {sessionIDs.length} 个
                    </span>
                  }
                >
                  <div className="session-list">
                    {sessionIDs.map((id) => {
                      const group = rows.filter((r) => r.session === id);
                      return (
                        <Link
                          to={`/sessions?session=${id}`}
                          className="session-row"
                          key={id}
                        >
                          <Layers size={18} />
                          <div>
                            <strong className="mono">{id}</strong>
                            <small>
                              {[...new Set(group.map((r) => r.source))].join(
                                " · ",
                              )}
                            </small>
                          </div>
                          <span>{group.length} 条请求</span>
                          <span className="muted">
                            {[
                              ...new Set(
                                group.flatMap((r) =>
                                  r.egress ? [r.egress] : [],
                                ),
                              ),
                            ].join(" / ") || "出口未观测"}
                          </span>
                          <ChevronRight size={16} />
                        </Link>
                      );
                    })}
                    {!sessionIDs.length && (
                      <div className="empty">没有匹配的已识别会话。</div>
                    )}
                  </div>
                </Panel>
              )}
            </>
          )}
          {location.pathname === "/config" && (
            <Panel title="网关配置">
              <Fields
                items={[
                  ["配置版本", "demo-config-v1"],
                  ["入口 ID", "harness-main"],
                  ["入口地址", ":8236"],
                  ["new-api 上游", "http://new-api:3000"],
                  ["内部出口监听", ":8237 · 不对外公开"],
                  ["亲和模式", "仅明确标识；缺失或冲突时拒绝"],
                  ["供应商配置", "opencode-a / opencode-b / deepseek"],
                  ["凭证", "[不向前端提供]"],
                  ["软亲和 / 内容哈希", "不启用"],
                  ["页面数据源", "前端固定合成样本"],
                  ["SSE / WebSocket", "尚未接入"],
                ]}
              />
            </Panel>
          )}
          {!page && (
            <div className="empty">
              这个页面不存在。
              <Link className="text-link" to="/">
                返回概览
              </Link>
            </div>
          )}
        </main>
      </div>
      <Sheet
        open={open}
        onOpenChange={(value) => {
          if (!value) close();
        }}
      >
        <SheetContent className="inspector">
          {requestID && row ? (
            <RequestDetail key={row.id} row={row} />
          ) : routeID && route ? (
            <>
              <SheetHeader>
                <SheetTitle>{route.name}</SheetTitle>
                <SheetDescription>
                  {route.path} · {route.protocol}
                </SheetDescription>
              </SheetHeader>
              <div className="detail-body">
                <Fields
                  items={[
                    ["出口 ID", route.id],
                    ["匹配路径", route.path],
                    [
                      "new-api → 网关",
                      `http://affinity-gateway:8237${route.path}`,
                    ],
                    ["供应商目标", route.host],
                    ["重写策略", route.strategy],
                    ["作用域", route.scope],
                    ["配置版本", "demo-config-v1"],
                  ]}
                />
                <h3>经此出口发送的会话</h3>
                {[
                  ...new Set(
                    requests
                      .filter((r) => r.egress === route.id)
                      .map((r) => r.session),
                  ),
                ].map((id) => (
                  <Link
                    className="detail-session-link"
                    key={id}
                    to={`/sessions?session=${id}`}
                  >
                    <Layers size={16} />
                    <span className="mono">{id}</span>
                    <ChevronRight size={16} />
                  </Link>
                ))}
                <h3>全部请求</h3>
                <RequestTable
                  rows={requests.filter((r) => r.egress === route.id)}
                />
              </div>
            </>
          ) : sessionID && sessionRows.length ? (
            <>
              <SheetHeader>
                <SheetTitle className="mono">{sessionID}</SheetTitle>
                <SheetDescription>
                  {sessionRows.length} 条请求
                </SheetDescription>
              </SheetHeader>
              <div className="detail-body">
                <Fields
                  items={[
                    ["入口", "harness-main"],
                    [
                      "命中字段",
                      [...new Set(sessionRows.map((r) => r.source))].join(
                        " / ",
                      ),
                    ],
                    ["请求数量", sessionRows.length],
                    [
                      "观察到的出口",
                      [
                        ...new Set(
                          sessionRows.flatMap((r) =>
                            r.egress ? [r.egress] : [],
                          ),
                        ),
                      ].join(" / "),
                    ],
                  ]}
                />
                <h3>全部请求</h3>
                <RequestTable rows={sessionRows} />
              </div>
            </>
          ) : (
            <SheetHeader>
              <SheetTitle>未找到记录</SheetTitle>
              <SheetDescription>
                该链接不属于当前演示快照。关闭详情后可重新选择记录。
              </SheetDescription>
            </SheetHeader>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
