import { useEffect, useRef, useState, type FormEvent } from "react";
import { NavLink, useLocation } from "react-router-dom";
import {
  Activity,
  GitBranch,
  Layers,
  Terminal,
  FileSliders,
  Network,
  Moon,
  Copy,
  Trash2,
  Sun,
} from "lucide-react";
import { Button } from "./components/ui/button";
import { Input } from "./components/ui/input";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "./components/ui/table";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
} from "./components/ui/sheet";

export type Observation = {
  id: string;
  at: string;
  mode: string;
  profile: string;
  model?: string;
  source?: string;
  session?: string;
  policy?: string;
  scope?: string;
  status?: number;
  duration: number;
  error?: string;
  before: Record<string, string>;
  after: Record<string, string> | null;
};
type Snapshot = {
  items: Observation[];
  revision: number;
  capacity: number;
  retention: string;
};
type PluginRef = { id: string; version: string };
type Supplier = { id: string; origin: string; plugins?: PluginRef[]; internal_base_url: string; created_at: string };
const nav = [
  ["/", "概览", Activity],
  ["/routes", "路由", GitBranch],
  ["/sessions", "会话", Layers],
  ["/requests", "请求", Terminal],
  ["/config", "配置", FileSliders],
] as const;

export default function LiveConsole() {
  const [authRequired, setAuthRequired] = useState(true);
  useEffect(() => {
    const controller = new AbortController();
    fetch("/api/auth", { signal: controller.signal, cache: "no-store" })
      .then(async (response) => {
        if (response.ok) {
          const data = await response.json();
          if (!controller.signal.aborted) setAuthRequired(data.required !== false);
        }
      })
      .catch(() => {});
    return () => controller.abort();
  }, []);
  const [credential, setCredential] = useState(""),
    [password, setPassword] = useState("");
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null),
    [suppliers, setSuppliers] = useState<Supplier[]>([]),
    [error, setError] = useState(""),
    [connected, setConnected] = useState(false);
  const [query, setQuery] = useState(""),
    [selected, setSelected] = useState<Observation | null>(null),
    [dark, setDark] = useState(false);
  const location = useLocation();
  const heading = nav.find((n) => n[0] === location.pathname)?.[1] ?? "请求";
  const [profile, setProfile] = useState(""),
    [session, setSession] = useState("");
  const [supplierID, setSupplierID] = useState(""),
    [supplierOrigin, setSupplierOrigin] = useState(""),
    [supplierAdapter, setSupplierAdapter] = useState(""),
    [pluginDrafts, setPluginDrafts] = useState<Record<string, string>>({}),
    [savingPlugin, setSavingPlugin] = useState(""),
    [savingSupplier, setSavingSupplier] = useState(false);
  const generation = useRef(0);
  useEffect(() => {
    document.documentElement.classList.toggle("dark", dark);
  }, [dark]);
  useEffect(() => {
    if (authRequired && !credential) return;
    const controller = new AbortController();
    const token = ++generation.current;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const headers: Record<string, string> = authRequired ? {
      Authorization: `Basic ${btoa(String.fromCharCode(...new TextEncoder().encode(`admin:${credential}`)))}`,
    } : {};
    async function refresh() {
      const [response, supplierResponse] = await Promise.all([fetch("/api/observations", {
        headers,
        signal: controller.signal,
        cache: "no-store",
      }), fetch("/api/suppliers", { headers, signal: controller.signal, cache: "no-store" })]);
      if (!response.ok || !supplierResponse.ok)
        throw new Error(
          response.status === 401 || supplierResponse.status === 401
            ? "认证失败，请检查密码"
            : `查询失败（${!response.ok ? response.status : supplierResponse.status}）`,
        );
      const data = (await response.json()) as Snapshot;
      const supplierData = (await supplierResponse.json()) as {items: Supplier[]};
      if (!Array.isArray(data.items) || typeof data.revision !== "number")
        throw new Error("数据接口格式不正确");
      if (!Array.isArray(supplierData.items)) throw new Error("出口接口格式不正确");
      if (token === generation.current) {
        setSnapshot(data);
        setSuppliers(supplierData.items);
        setError("");
      }
    }
    async function connect() {
      let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
      try {
        await refresh();
        const response = await fetch("/api/events", {
          headers,
          signal: controller.signal,
          cache: "no-store",
        });
        if (
          !response.ok ||
          !response.headers.get("content-type")?.includes("text/event-stream")
        )
          throw new Error("事件连接失败");
        reader = response.body?.getReader();
        if (!reader) throw new Error("浏览器不支持流式读取");
        setConnected(true);
        let buffer = "";
        const decoder = new TextDecoder();
        while (!controller.signal.aborted) {
          const { value, done } = await reader.read();
          if (done) throw new Error("事件连接已断开");
          buffer += decoder.decode(value, { stream: true });
          if (buffer.length > 65536) throw new Error("事件数据超限");
          const boundary = buffer.lastIndexOf("\n\n");
          if (boundary >= 0) {
            const batch = buffer.slice(0, boundary);
            buffer = buffer.slice(boundary + 2);
            if (batch.includes("event: snapshot")) await refresh();
          }
        }
      } catch (e) {
        if (controller.signal.aborted) return;
        setConnected(false);
        setError(e instanceof Error ? e.message : "连接失败");
        if (e instanceof Error && e.message.startsWith("认证失败")) {
          setCredential(""); setSnapshot(null); setSelected(null);
        } else timer = setTimeout(connect, 3000);
      } finally {
        try {
          await reader?.cancel();
        } catch {
          /* Connection already closed. */
        }
      }
    }
    void connect();
    return () => {
      generation.current++;
      controller.abort();
      clearTimeout(timer);
      setConnected(false);
    };
  }, [credential, authRequired]);
  const rows = snapshot?.items ?? [];
  const filtered = rows.filter(
    (r) =>
      (!profile || r.profile === profile) &&
      (!session || r.session === session) &&
      [r.id, r.model, r.profile, r.session, r.source].some((v) =>
        v?.toLowerCase().includes(query.toLowerCase()),
      ),
  );
  const profiles = [...new Set(rows.map((r) => r.profile))];
  const sessions = [
    ...new Set(rows.flatMap((r) => (r.session ? [r.session] : []))),
  ];
  const [page, setPage] = useState(0);
  const authHeaders: Record<string, string> = authRequired ? { Authorization: `Basic ${btoa(String.fromCharCode(...new TextEncoder().encode(`admin:${credential}`)))}` } : {};
  async function addSupplier(e: FormEvent) {
    e.preventDefault(); setSavingSupplier(true); setError("");
    try {
      const [id, version] = supplierAdapter.split("@");
      const plugins = supplierAdapter ? [{id, version}] : [];
      const response = await fetch("/api/suppliers", { method: "POST", headers: {...authHeaders, "Content-Type":"application/json"}, body: JSON.stringify({id:supplierID, origin:supplierOrigin, plugins}) });
      if (!response.ok) throw new Error((await response.text()).trim() || `保存失败（${response.status}）`);
      const data = await response.json() as {items: Supplier[]}; setSuppliers(data.items); setSupplierID(""); setSupplierOrigin(""); setSupplierAdapter("");
    } catch (e) { setError(e instanceof Error ? e.message : "保存失败"); }
    finally { setSavingSupplier(false); }
  }
  async function deleteSupplier(id: string) {
    if (!confirm(`删除出口 ${id}？`)) return;
    setError("");
    const response = await fetch(`/api/suppliers/${id}`, {method:"DELETE", headers:authHeaders});
    if (response.ok) setSuppliers((rows) => rows.filter((row) => row.id !== id));
    else setError((await response.text()).trim() || `删除失败（${response.status}）`);
  }
  async function updateSupplierPlugins(supplier: Supplier) {
    const adapter = pluginDrafts[supplier.id] ?? (supplier.plugins?.[0] ? `${supplier.plugins[0].id}@${supplier.plugins[0].version}` : "");
    setSavingPlugin(supplier.id); setError("");
    try {
      const [id, version] = adapter.split("@");
      const plugins = adapter ? [{id, version}] : [];
      const response = await fetch(`/api/suppliers/${supplier.id}/plugins`, {method:"PUT", headers:{...authHeaders, "Content-Type":"application/json"}, body:JSON.stringify({plugins})});
      if (!response.ok) throw new Error((await response.text()).trim() || `保存失败（${response.status}）`);
      const data = await response.json() as {items: Supplier[]};
      setSuppliers(data.items); setPluginDrafts((drafts)=>{const next={...drafts}; delete next[supplier.id]; return next;});
    } catch (e) { setError(e instanceof Error ? e.message : "保存失败"); }
    finally { setSavingPlugin(""); }
  }
  useEffect(() => {
    setPage(0);
  }, [query, profile, session, location.pathname]);
  const pageCount = Math.max(1, Math.ceil(filtered.length / 20));
  const currentPage = Math.min(page, pageCount - 1);
  function list(data: Observation[]) {
    return (
      <Table>
        <TableHeader>
          <TableRow>
            {["时间", "方向 / 配置", "模型", "结果", "耗时", ""].map((h) => (
              <TableHead key={h}>{h}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {data.map((r) => (
            <TableRow key={r.id}>
              <TableCell>{new Date(r.at).toLocaleTimeString()}</TableCell>
              <TableCell>
                {r.mode === "inbound" ? "入口" : "出口"} / {r.profile}
              </TableCell>
              <TableCell>{r.model ?? "—"}</TableCell>
              <TableCell>
                <span
                  className={`status ${r.error || (r.status ?? 0) >= 400 ? "error" : "success"}`}
                >
                  {r.error ?? r.status ?? "未知"}
                </span>
              </TableCell>
              <TableCell>{r.duration} ms</TableCell>
              <TableCell>
                <Button variant="ghost" onClick={() => setSelected(r)}>
                  详情
                </Button>
              </TableCell>
            </TableRow>
          ))}
          {!data.length && (
            <TableRow>
              <TableCell colSpan={6}>
                <div className="empty">暂无记录</div>
              </TableCell>
            </TableRow>
          )}
        </TableBody>
      </Table>
    );
  }
  if (!snapshot && !authRequired) {
    return <main className="login-screen"><p role="status">{error || "连接中…"}</p></main>;
  }
  if (!snapshot) {
    return (
      <main className="login-screen">
        <form className="panel login-form" onSubmit={(e) => {
          e.preventDefault(); setError(""); setCredential(password); setPassword("");
        }}>
          <h1>连接网关</h1>
          <label htmlFor="console-password">访问密码</label>
          <Input id="console-password" type="password" autoComplete="current-password"
            autoFocus required value={password} onChange={(e) => setPassword(e.target.value)} />
          {error && <p role="status" className="login-error">{error}</p>}
          <Button type="submit" disabled={!!credential && !error}>{credential && !error ? "连接中…" : "连接"}</Button>
        </form>
      </main>
    );
  }
  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <Network />
          Affinity
        </div>
        <nav aria-label="主导航">
          {nav.map(([path, title, Icon]) => (
            <NavLink
              key={path}
              end={path === "/"}
              to={path}
              onClick={() => {
                setProfile("");
                setSession("");
              }}
            >
              <Icon size={18} />
              {title}
            </NavLink>
          ))}
        </nav>
      </aside>
      <div className="main-shell">
        <header className="topbar">
          <span>{heading}</span>
          <div className="topbar-actions">
            <span className="connection">
              {connected ? "已连接" : "未连接"}
            </span>
            <Button
              variant="ghost"
              size="icon"
              aria-label="切换主题"
              onClick={() => setDark(!dark)}
            >
              {dark ? <Sun /> : <Moon />}
            </Button>
            {credential && (
              <Button
                variant="ghost"
                onClick={() => {
                  setCredential("");
                  setSnapshot(null);
                  setSelected(null);
                  setPassword("");
                }}
              >
                退出
              </Button>
            )}
          </div>
        </header>
        <main>
          <div className="page-heading">
            <h1>{heading}</h1>
          </div>
          {(
            <>
              {error && (
                <div className="notice warning" role="status">
                  {error}
                  {snapshot ? " · 显示上次数据" : ""}
                </div>
              )}
              {location.pathname === "/" && (
                <>
                  <div className="metrics">
                    {[
                      ["记录", rows.length],
                      ["入口", rows.filter((r) => r.mode === "inbound").length],
                      [
                        "出口",
                        rows.filter((r) => r.mode === "outbound").length,
                      ],
                      ["会话", sessions.length],
                    ].map(([k, v]) => (
                      <div className="metric" key={k}>
                        <span>{k}</span>
                        <strong>{v}</strong>
                      </div>
                    ))}
                  </div>
                  <section className="panel">
                    <div className="panel-heading">
                      <h2>最近请求</h2>
                    </div>
                    {list(rows.slice(0, 10))}
                  </section>
                </>
              )}
              {location.pathname === "/routes" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>已观测配置</h2>
                  </div>
                  {profiles.map((p) => {
                    const entries = rows.filter((r) => r.profile === p);
                    return (
                      <div className="session-row" key={p}>
                        <GitBranch />
                        <strong>{p}</strong>
                        <span>
                          {entries[0].mode === "inbound" ? "入口" : "出口"}
                        </span>
                        <span>{entries.length} 条</span>
                        <Button
                          variant="ghost"
                          onClick={() => {
                            setProfile(p);
                            setSession("");
                          }}
                        >
                          查看请求
                        </Button>
                      </div>
                    );
                  })}
                  {!profiles.length && <div className="empty">暂无记录</div>}
                  {profile && list(filtered.slice(0, 20))}
                </section>
              )}
              {location.pathname === "/sessions" && (
                <section className="panel">
                  <div className="panel-heading">
                    <h2>会话</h2>
                  </div>
                  {sessions.map((s) => (
                    <div className="session-row" key={s}>
                      <span className="mono">{s.slice(0, 20)}…</span>
                      <span>
                        {rows.filter((r) => r.session === s).length} 条
                      </span>
                      <Button
                        variant="ghost"
                        onClick={() => {
                          setSession(s);
                          setProfile("");
                        }}
                      >
                        查看请求
                      </Button>
                    </div>
                  ))}
                  {!sessions.length && <div className="empty">暂无记录</div>}
                  {session && list(filtered.slice(0, 20))}
                </section>
              )}
              {location.pathname === "/requests" && (
                <>
                  <div className="filters">
                    <Input
                      aria-label="搜索请求"
                      placeholder="搜索请求、模型、配置或会话"
                      value={query}
                      onChange={(e) => setQuery(e.target.value)}
                    />
                    <select
                      aria-label="配置筛选"
                      value={profile}
                      onChange={(e) => setProfile(e.target.value)}
                    >
                      <option value="">全部配置</option>
                      {profiles.map((p) => (
                        <option key={p}>{p}</option>
                      ))}
                    </select>
                  </div>
                  <section className="panel">
                    {list(
                      filtered.slice(currentPage * 20, currentPage * 20 + 20),
                    )}
                    <div className="pagination">
                      <span>
                        {filtered.length} 条 · {currentPage + 1} / {pageCount}
                      </span>
                      <div>
                        <Button
                          variant="outline"
                          disabled={currentPage === 0}
                          onClick={() => setPage(currentPage - 1)}
                        >
                          上一页
                        </Button>
                        <Button
                          variant="outline"
                          disabled={currentPage + 1 >= pageCount}
                          onClick={() => setPage(currentPage + 1)}
                        >
                          下一页
                        </Button>
                      </div>
                    </div>
                  </section>
                </>
              )}
              {location.pathname === "/config" && (
                <div className="config-stack">
                  <section className="panel supplier-form-panel">
                    <div className="panel-heading"><h2>添加出口</h2></div>
                    <form className="supplier-form" onSubmit={addSupplier}>
                      <div><label htmlFor="supplier-id">出口 ID</label><Input id="supplier-id" required pattern="[a-z][a-z0-9-]{0,47}" placeholder="openai-main" value={supplierID} onChange={(e)=>setSupplierID(e.target.value)} /></div>
                      <div><label htmlFor="supplier-origin">真实 Origin</label><Input id="supplier-origin" required type="url" placeholder="https://api.example.com" value={supplierOrigin} onChange={(e)=>setSupplierOrigin(e.target.value)} /></div>
                      <div><label htmlFor="supplier-adapter">出站插件</label><select id="supplier-adapter" value={supplierAdapter} onChange={(e)=>setSupplierAdapter(e.target.value)}><option value="">无</option><option value="opencode-go-session@1.1.0">OpenCode Go 会话 · 1.1.0</option><option value="opencode-go-session@1.0.0">OpenCode Go 会话 · 1.0.0（兼容）</option></select></div>
                      <Button type="submit" disabled={savingSupplier}>{savingSupplier ? "保存中…" : "添加"}</Button>
                    </form>
                  </section>
                  <section className="panel">
                    <div className="panel-heading"><h2>出口供应商</h2></div>
                    <Table><TableHeader><TableRow><TableHead>出口 ID</TableHead><TableHead>内部 Base URL</TableHead><TableHead>真实 Origin</TableHead><TableHead>插件</TableHead><TableHead /></TableRow></TableHeader>
                    <TableBody>{suppliers.map((supplier)=>{const current=supplier.plugins?.[0] ? `${supplier.plugins[0].id}@${supplier.plugins[0].version}` : ""; const draft=pluginDrafts[supplier.id] ?? current; return <TableRow key={supplier.id}><TableCell className="mono">{supplier.id}</TableCell><TableCell><span className="mono">{supplier.internal_base_url}</span><Button variant="ghost" size="icon" aria-label={`复制 ${supplier.id} 内部地址`} onClick={()=>void navigator.clipboard.writeText(supplier.internal_base_url)}><Copy size={15}/></Button></TableCell><TableCell className="mono">{supplier.origin}</TableCell><TableCell><div className="binding-plugin-editor"><select aria-label={`${supplier.id} 插件`} value={draft} onChange={(e)=>setPluginDrafts((rows)=>({...rows,[supplier.id]:e.target.value}))}><option value="">无</option><option value="opencode-go-session@1.1.0">OpenCode Go 会话 · 1.1.0</option><option value="opencode-go-session@1.0.0">OpenCode Go 会话 · 1.0.0（兼容）</option></select><Button variant="outline" disabled={draft===current || savingPlugin===supplier.id} onClick={()=>void updateSupplierPlugins(supplier)}>{savingPlugin===supplier.id ? "保存中…" : "保存"}</Button></div></TableCell><TableCell><Button variant="ghost" size="icon" aria-label={`删除 ${supplier.id}`} onClick={()=>void deleteSupplier(supplier.id)}><Trash2 size={15}/></Button></TableCell></TableRow>})}{!suppliers.length&&<TableRow><TableCell colSpan={5}><div className="empty">暂无出口</div></TableCell></TableRow>}</TableBody></Table>
                  </section>
                </div>
              )}
            </>
          )}
        </main>
      </div>
      <Sheet
        open={!!selected}
        onOpenChange={(v) => {
          if (!v) setSelected(null);
        }}
      >
        <SheetContent className="inspector">
          {selected && (
            <>
              <SheetHeader>
                <SheetTitle className="mono">{selected.id}</SheetTitle>
                <SheetDescription>
                  {selected.profile} ·{" "}
                  {selected.mode === "inbound" ? "入口" : "出口"}
                </SheetDescription>
              </SheetHeader>
              <div className="detail-body">
                <dl className="fields">
                  {[
                    ["模型", selected.model],
                    ["命中字段", selected.source],
                    ["会话指纹", selected.session],
                    ["策略", selected.policy],
                    ["作用域", selected.scope],
                    ["状态", selected.error ?? selected.status],
                    ["总耗时", `${selected.duration} ms`],
                  ].map(([k, v]) => (
                    <div key={k}>
                      <dt>{k}</dt>
                      <dd className="mono">{v || "—"}</dd>
                    </div>
                  ))}
                </dl>
                <h3>处理前</h3>
                <pre className="header-json">
                  {JSON.stringify(selected.before, null, 2)}
                </pre>
                <h3>处理后</h3>
                <pre className="header-json">
                  {selected.after
                    ? JSON.stringify(selected.after, null, 2)
                    : "未转发"}
                </pre>
              </div>
            </>
          )}
        </SheetContent>
      </Sheet>
    </div>
  );
}
