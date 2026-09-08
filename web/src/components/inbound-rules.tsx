import { useEffect, useRef, useState, type FormEvent } from "react";
import { ChevronRight, ArrowDownToLine } from "lucide-react";
import { Sheet, SheetContent, SheetHeader, SheetTitle, SheetDescription } from "./ui/sheet";
import { Button } from "./ui/button";
import { Input } from "./ui/input";
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from "./ui/table";

type HeaderRule = {name: string; enabled: boolean; strip: boolean};
type Rules = {profile: string; revision: number; mode: string; body_limit: number; metadata: boolean; conversation: boolean; cache_key: boolean; headers: HeaderRule[]};
type Props = {authHeaders: Record<string, string>; notify: {success: (message: string) => void; error: (message: string) => void}};

export function InboundRulesPanel({authHeaders, notify}: Props) {
  const drafts = useRef<Record<string, Rules>>({});
  const [open, setOpen] = useState(false);
  const [rules, setRules] = useState<Rules[]>([]);
  const [draft, setDraft] = useState<Rules | null>(null);
  const [name, setName] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [loadError, setLoadError] = useState("");
  const [sampleHeaders, setSampleHeaders] = useState('{"X-Session-Id":"example-session"}');
  const [sampleBody, setSampleBody] = useState('{"model":"example-model"}');
  const [preview, setPreview] = useState<{status: number; error: string; source: string} | null>(null);
  const [testing, setTesting] = useState(false);
  const [reload, setReload] = useState(0);
  const authorization = authHeaders.Authorization;
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true); setLoadError("");
    fetch("/api/inbound-rules", {headers: authorization ? {Authorization: authorization} : {}, signal: controller.signal, cache: "no-store"})
      .then(async (response) => {
        if (!response.ok) throw new Error("入站规则加载失败");
        const data = await response.json() as {items: Rules[]};
        if (!Array.isArray(data.items)) throw new Error("入站规则格式不正确");
        if (!controller.signal.aborted) {drafts.current = {}; setRules(data.items); setDraft((current) => data.items.find((row) => row.profile === current?.profile) ?? data.items[0] ?? null);}
      }).catch((error: Error) => {if (!controller.signal.aborted) setLoadError(error.message);})
      .finally(() => {if (!controller.signal.aborted) setLoading(false);});
    return () => controller.abort();
  }, [authorization, reload]);
  useEffect(() => {setPreview(null); if (draft) drafts.current[draft.profile] = draft;}, [draft]);
  function updateHeader(index: number, change: Partial<HeaderRule>) {
    if (draft) setDraft({...draft, headers: draft.headers.map((row, i) => i === index ? {...row, ...change} : row)});
  }
  function addHeader() {
    const trimmed = name.trim();
    if (!draft || !trimmed) return;
    if (!/^[!#$%&'*+.^_`|~0-9A-Za-z-]+$/.test(trimmed) || draft.headers.some((row) => row.name.toLowerCase() === trimmed.toLowerCase())) {notify.error("请求头名称无效或已存在"); return;}
    setDraft({...draft, headers: [...draft.headers, {name: trimmed, enabled: true, strip: true}]}); setName("");
  }
  async function save(event: FormEvent) {
    event.preventDefault(); if (!draft) return;
    setSaving(true);
    try {
      const response = await fetch("/api/inbound-rules", {method: "PUT", headers: {...authHeaders, "Content-Type": "application/json"}, body: JSON.stringify(draft)});
      if (!response.ok) throw new Error((await response.text()).trim() || "保存入站规则失败");
      const data = await response.json() as {items: Rules[]};
      setRules(data.items); setDraft(data.items.find((row) => row.profile === draft.profile) ?? null);
      notify.success("入站规则已保存，将用于后续请求");
      setOpen(false);
    } catch (error) {notify.error(error instanceof Error ? error.message : "保存入站规则失败");}
    finally {setSaving(false);}
  }
  async function testRules() {
    setTesting(true); setPreview(null);
    try {
      const headers: unknown = JSON.parse(sampleHeaders);
      if (!headers || Array.isArray(headers) || typeof headers !== "object" || Object.values(headers).some((value) => typeof value !== "string")) throw new Error("示例请求头需为 JSON 对象，值为字符串");
      const response = await fetch("/api/inbound-rules/preview", {method: "POST", headers: {...authHeaders, "Content-Type": "application/json"}, body: JSON.stringify({rules: draft, headers, body: sampleBody})});
      if (!response.ok) throw new Error((await response.text()).trim() || "规则测试失败");
      setPreview(await response.json());
    } catch (error) {notify.error(error instanceof Error ? error.message : "规则测试失败");}
    finally {setTesting(false);}
  }
  const dirty = !!draft && JSON.stringify(draft) !== JSON.stringify(rules.find((row) => row.profile === draft.profile));
  return <section className="panel inbound-summary">
    <div className="settings-section-heading"><div className="settings-section-title"><ArrowDownToLine size={18} /><div><h2>入站入口</h2><p>选择一个入口，配置它的会话识别和请求校验。</p></div></div>
      <Sheet open={open} onOpenChange={(value) => {if (!saving && !testing) setOpen(value);}}>
        <SheetContent className="settings-drawer" showCloseButton={!saving && !testing}>
          <SheetHeader><SheetTitle>配置入站：{draft?.profile}</SheetTitle><SheetDescription>仅修改此入口，保存后对后续请求生效。</SheetDescription></SheetHeader>
          {draft && <div className="settings-drawer-scroll">
      <form onSubmit={save}>
        <fieldset className="rules-fields" disabled={saving || testing}>
          <div className="rules-options">
            <label>校验模式<select aria-label="校验模式" value={draft.mode} onChange={(event) => setDraft({...draft, mode: event.target.value})}><option value="strict">严格一致性检查</option><option value="headers_only">仅请求头识别</option></select></label>

          </div>
          <p className="rules-hint">{draft.mode === "strict" ? "检查 JSON 与启用的 metadata 会话来源是否冲突；conversation 和缓存键仅作为后备来源。" : "不读取请求体，仅使用启用的请求头识别会话。出口插件仍可能检查请求体。"} 所有模式都要求有效会话 ID；多个启用的请求头必须一致。</p>
          <details className="settings-disclosure advanced-rules"><summary><span><strong>高级规则</strong><small>自定义身份请求头、请求体来源与解析限制</small></span><ChevronRight size={16} /></summary>
          {draft.mode === "strict" && <details className="settings-disclosure"><summary><span><strong>请求体识别</strong><small>来源字段与解析限制</small></span><ChevronRight size={16} /></summary><div className="rules-options"><label>请求体上限（字节）<Input aria-label="请求体上限" type="number" min={1} max={16777216} required value={draft.body_limit} onChange={(event) => setDraft({...draft, body_limit: Number(event.target.value)})} /></label></div>
          {draft.mode === "strict" && <div className="rules-options">
            <label className="rules-check"><input type="checkbox" checked={draft.metadata} onChange={(event) => setDraft({...draft, metadata: event.target.checked})} />识别 metadata.user_id 中的 session_id</label>
            <label className="rules-check"><input type="checkbox" checked={draft.conversation} onChange={(event) => setDraft({...draft, conversation: event.target.checked})} />识别 conversation / conversation.id</label>
            <label className="rules-check"><input type="checkbox" checked={draft.cache_key} onChange={(event) => setDraft({...draft, cache_key: event.target.checked})} />将 prompt_cache_key 作为会话 ID（需客户端保证每会话稳定且唯一）</label>
          </div>}</details>}
          <details className="settings-disclosure"><summary><span><strong>身份请求头</strong><small>{draft.headers.filter((row) => row.enabled).length} 个来源启用 · 识别与移除规则</small></span><ChevronRight size={16} /></summary>
          <Table><TableHeader><TableRow><TableHead>身份请求头</TableHead><TableHead>参与会话识别</TableHead><TableHead>转发时移除</TableHead></TableRow></TableHeader><TableBody>{draft.headers.map((row, index) => <TableRow key={row.name}><TableCell className="mono">{row.name}</TableCell><TableCell><input type="checkbox" aria-label={`${row.name} 参与识别`} checked={row.enabled} onChange={(event) => updateHeader(index, {enabled: event.target.checked})} /></TableCell><TableCell><input type="checkbox" aria-label={`${row.name} 转发时移除`} checked={row.strip} onChange={(event) => updateHeader(index, {strip: event.target.checked})} /></TableCell></TableRow>)}</TableBody></Table>
          <div className="rules-actions"><Input aria-label="自定义身份请求头" placeholder="例如 X-My-Session" value={name} onChange={(event) => setName(event.target.value)} /><Button type="button" variant="outline" disabled={!name.trim() || draft.headers.length >= 64} onClick={addHeader}>添加请求头</Button></div>
          <p className="rules-hint">X-Session-Affinity 为系统内部字段，始终由网关生成。配置身份字段只显示脱敏状态；取消“转发时移除”会将原始值发往下游。</p>
          </details>
          </details>
          <details className="rules-preview settings-disclosure"><summary><span><strong>测试规则</strong><small>使用示例验证当前草稿，不保存</small></span><ChevronRight size={16} /></summary><p className="rules-hint">仅填写虚构的会话值，无需填写真实凭据。测试不转发请求，不写入观测记录。</p><label>示例请求头（JSON）<textarea aria-label="示例请求头" value={sampleHeaders} onChange={(event) => {setSampleHeaders(event.target.value);setPreview(null);}} /></label><label>示例请求体<textarea aria-label="示例请求体" value={sampleBody} onChange={(event) => {setSampleBody(event.target.value);setPreview(null);}} /></label><Button type="button" variant="outline" disabled={testing} onClick={() => void testRules()}>{testing ? "测试中…" : "测试规则"}</Button>{preview && <p role="status">{preview.status === 200 ? "通过" : "拒绝"} · HTTP {preview.status} · 命中来源：{preview.source || "无"}{preview.error && ` · ${preview.error}`}</p>}</details>
          <div className="rules-actions settings-savebar"><Button type="submit" disabled={!dirty}>{saving ? "保存中…" : "保存入站规则"}</Button><Button type="button" variant="outline" onClick={() => {setDraft(rules.find((row) => row.profile === draft.profile) ?? null);setName("");}}>放弃修改</Button><Button type="button" variant="outline" onClick={() => {setPreview(null);setReload(reload + 1);}}>重新加载规则</Button><span className="muted">{dirty ? "有未保存修改" : `已保存 · 版本 ${draft.revision}`}</span></div>
        </fieldset>
      </form></div>}
        </SheetContent>
      </Sheet>
    </div>
    {loading ? <p className="rules-hint">加载入站规则…</p> : loadError ? <div className="rules-hint" role="alert">{loadError} <Button variant="outline" onClick={() => setReload(reload + 1)}>重新加载</Button></div> : !rules.length ? <p className="rules-hint">暂无可配置入站</p> : <div className="inbound-summary-list"><div className="inbound-summary-row inbound-summary-labels" aria-hidden="true"><span>入口名称</span><span>校验模式</span><span>识别来源</span><span /></div>{rules.map((row) => {
      const sources = [`${row.headers.filter((header) => header.enabled).length} 个请求头`, ...(row.mode === "strict" ? [row.metadata && "metadata", row.conversation && "conversation", row.cache_key && "缓存键"].filter(Boolean) : [])].join(" · ");
      return <div className="inbound-summary-row" key={row.profile}><span className="mono">{row.profile}</span><span className="settings-state">{row.mode === "strict" ? "严格检查" : "仅请求头"}</span><span className="muted inbound-source-summary">{sources}</span><Button variant="outline" size="sm" aria-label={`配置入站 ${row.profile}`} onClick={() => {setDraft(drafts.current[row.profile] ?? row);setName("");setOpen(true);}}>配置</Button></div>;
    })}</div>}

  </section>;
}
