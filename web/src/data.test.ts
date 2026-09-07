import { describe, expect, it } from "vitest";
import { filterRequests, requests, routes, summarize, timeline } from "./data";

describe("demo observability data", () => {
  it("keeps rejected and unobserved requests out of supplier traffic", () => {
    for (const row of requests) {
      if (row.outcome === "rejected") {
        expect(row.egress).toBeUndefined();
        expect(row.session).toBeUndefined();
        expect(row.status).toBe(400);
      }
      if (row.outcome === "unobserved") {
        expect(row.egress).toBeUndefined();
        expect(row.status).toBeUndefined();
        expect(row.duration).toBeUndefined();
      }
      if (row.egress)
        expect(routes.some((r) => r.id === row.egress)).toBe(true);
    }
  });
  it("does not show route drift within the demo session + model scope", () => {
    const bindings = new Map<string, string>();
    for (const r of requests.filter((r) => r.egress && r.session)) {
      const key = `${r.session}:${r.model}`;
      if (bindings.has(key)) expect(r.egress).toBe(bindings.get(key));
      bindings.set(key, r.egress!);
    }
  });
  it("filters by query, result, egress and time", () => {
    expect(
      filterRequests(requests, "no-such-id", "all", "all", 60),
    ).toHaveLength(0);
    expect(filterRequests(requests, "", "rejected", "all", 60)).toHaveLength(8);
    expect(filterRequests(requests, "", "all", "all", 15)).toHaveLength(21);
    const rows = filterRequests(
      requests,
      "OpenCode",
      "success",
      "opencode-a",
      60,
    );
    expect(rows.length).toBeGreaterThan(0);
    expect(
      rows.every(
        (r) =>
          r.client === "OpenCode" &&
          r.egress === "opencode-a" &&
          r.outcome === "success",
      ),
    ).toBe(true);
  });
  it("derives charts and metrics from the same snapshot", () => {
    const summary = summarize(requests);
    expect(summary).toMatchObject({ total: 48, rejected: 8, sessions: 6 });
    expect(timeline(requests).reduce((sum, b) => sum + b.requests, 0)).toBe(48);
    expect(timeline(requests).reduce((sum, b) => sum + b.rejected, 0)).toBe(8);
    expect(summarize([]).p95).toBeUndefined();
  });
});
