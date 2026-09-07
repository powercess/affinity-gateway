// Synthetic, already-redacted presentation data. Never import captured traffic here.
export type Outcome = "success" | "rejected" | "error" | "unobserved";
export type RequestRecord = {
  id: string;
  at: string;
  client: string;
  model: string;
  protocol: string;
  ingress: string;
  egress?: string;
  session?: string;
  source?: string;
  outcome: Outcome;
  status?: number;
  duration?: number;
  ttft?: number;
  reason?: string;
};
export const outcomeLabels: Record<Outcome, string> = {
  success: "成功",
  rejected: "入口拒绝",
  error: "供应商错误",
  unobserved: "出口未观测",
};
export const routes = [
  {
    id: "opencode-a",
    name: "OpenCode A",
    host: "https://supplier-a.example/v1",
    path: "/opencode-a",
    scope: "opencode-a",
    protocol: "OpenAI",
    strategy: "derive",
  },
  {
    id: "opencode-b",
    name: "OpenCode B",
    host: "https://supplier-b.example/v1",
    path: "/opencode-b",
    scope: "opencode-b",
    protocol: "Anthropic",
    strategy: "derive",
  },
  {
    id: "deepseek",
    name: "DeepSeek",
    host: "https://supplier-c.example/v1",
    path: "/deepseek",
    scope: "deepseek",
    protocol: "OpenAI",
    strategy: "derive",
  },
];
export const requests: RequestRecord[] = Array.from({ length: 48 }, (_, i) => {
  const n = i % 12;
  const rejected = n === 7 || n === 11;
  const unobserved = n === 8;
  const outcome: Outcome = rejected
    ? "rejected"
    : unobserved
      ? "unobserved"
      : n === 9
        ? "error"
        : "success";
  const client = ["OpenCode", "Claude Code", "pi", "Qwen Code"][i % 4];
  return {
    id: `demo-req-${String(1048 - i).padStart(4, "0")}`,
    at: new Date(Date.parse("2026-09-06T06:30:00Z") - i * 45_000).toISOString(),
    client,
    model: i % 3 === 1 ? "claude-demo" : "chat-demo",
    protocol:
      i % 3 === 1
        ? "Anthropic Messages"
        : i % 3 === 2
          ? "OpenAI Responses"
          : "OpenAI Chat",
    ingress: "harness-main",
    egress: rejected || unobserved ? undefined : routes[i % 3].id,
    session: rejected
      ? undefined
      : `demo-session-${String((i % 6) + 1).padStart(2, "0")}`,
    source: rejected
      ? undefined
      : client === "Claude Code"
        ? "metadata.user_id.session_id"
        : "X-Session-Id",
    outcome,
    status: rejected
      ? 400
      : unobserved
        ? undefined
        : outcome === "error"
          ? 503
          : 200,
    duration: unobserved ? undefined : rejected ? 3 : 480 + ((i * 137) % 2400),
    ttft: rejected || unobserved ? undefined : 80 + ((i * 23) % 400),
    reason: rejected
      ? "missing_session_identity：请补充明确的会话标识。"
      : outcome === "error"
        ? "供应商返回 HTTP 503；本次没有观察到其他出口尝试。"
        : unobserved
          ? "仅观察到入口转发；无法确认 new-api 内部处理结果。"
          : undefined,
  };
});
export function filterRequests(
  rows: RequestRecord[],
  query: string,
  outcome: string,
  egress: string,
  minutes: number,
) {
  const q = query.trim().toLowerCase();
  const newest = Date.parse(requests[0].at);
  return rows.filter(
    (r) =>
      (!q ||
        [r.id, r.client, r.model, r.session, r.source].some((v) =>
          v?.toLowerCase().includes(q),
        )) &&
      (outcome === "all" || r.outcome === outcome) &&
      (egress === "all" || r.egress === egress) &&
      newest - Date.parse(r.at) <= minutes * 60_000,
  );
}
export function summarize(rows: RequestRecord[]) {
  const completed = rows
    .flatMap((r) => (r.duration === undefined ? [] : [r.duration]))
    .sort((a, b) => a - b);
  return {
    total: rows.length,
    rejected: rows.filter((r) => r.outcome === "rejected").length,
    sessions: new Set(rows.flatMap((r) => (r.session ? [r.session] : []))).size,
    p95: completed.length
      ? completed[Math.ceil(completed.length * 0.95) - 1]
      : undefined,
  };
}
export function timeline(rows: RequestRecord[]) {
  const buckets = new Map<
    string,
    { time: string; requests: number; rejected: number }
  >();
  for (const row of [...rows].reverse()) {
    const time = row.at.slice(11, 16);
    const bucket = buckets.get(time) ?? { time, requests: 0, rejected: 0 };
    bucket.requests++;
    if (row.outcome === "rejected") bucket.rejected++;
    buckets.set(time, bucket);
  }
  return [...buckets.values()];
}
export const timeLabel = (at: string) => `${at.slice(11, 19)} UTC`;
