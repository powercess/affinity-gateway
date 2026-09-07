import { afterEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import LiveConsole from "./LiveConsole";
afterEach(() => vi.unstubAllGlobals());
describe("gateway data connection", () => {
  it("only discovers auth mode before authentication", () => {
    const fetch = vi.fn(async (_url: string) => new Response(JSON.stringify({required: true})));
    vi.stubGlobal("fetch", fetch);
    render(
      <MemoryRouter>
        <LiveConsole />
      </MemoryRouter>,
    );
    expect(screen.getByLabelText("访问密码")).toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(fetch.mock.calls[0][0]).toBe("/api/auth");
  });
  it("loads real snapshot and subscribes to events without retaining password", async () => {
    const setItem = vi.fn();
    vi.stubGlobal("localStorage", { setItem });
    const fetch = vi.fn(async (url: string) =>
      url === "/api/events"
        ? new Response(
            new ReadableStream({
              start(c) {
                c.enqueue(
                  new TextEncoder().encode("event: snapshot\ndata: 1\n\n"),
                );
              },
            }),
            { headers: { "Content-Type": "text/event-stream" } },
          )
        : new Response(
            JSON.stringify({
              items: [
                {
                  id: "actual-id",
                  at: new Date().toISOString(),
                  profile: "in-1",
                  mode: "inbound",
                  model: "model-from-api",
                  status: 200,
                  duration: 12,
                  before: {},
                  after: {},
                },
              ],
              revision: 1,
              capacity: 1000,
              retention: "process",
            }),
            { headers: { "Content-Type": "application/json" } },
          ),
    );
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    render(
      <MemoryRouter initialEntries={["/requests"]}>
        <LiveConsole />
      </MemoryRouter>,
    );
    await user.type(screen.getByLabelText("访问密码"), "test-password");
    await user.click(screen.getByRole("button", { name: "连接" }));
    expect(await screen.findByText("model-from-api")).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText("已连接")).toBeInTheDocument());
    expect(fetch.mock.calls.some(([url]) => url === "/api/events")).toBe(true);
    expect(setItem).not.toHaveBeenCalled();
    await user.click(screen.getByRole("button", { name: "退出" }));
    expect(screen.queryByText("model-from-api")).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
  });
  it("shows authentication errors rather than replacing them with demo data", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("", { status: 401 })),
    );
    const user = userEvent.setup();
    render(
      <MemoryRouter>
        <LiveConsole />
      </MemoryRouter>,
    );
    await user.type(screen.getByLabelText("访问密码"), "wrong");
    await user.click(screen.getByRole("button", { name: "连接" }));
    expect(await screen.findByRole("status")).toHaveTextContent("认证失败");
    expect(screen.queryByRole("navigation")).not.toBeInTheDocument();
    expect(screen.queryByText("demo-req-1048")).not.toBeInTheDocument();
  });
});

it("connects without credentials in the test environment", async () => {
  const fetch = vi.fn(async (url: string) => {
    if (url === "/api/auth") return new Response(JSON.stringify({required: false}));
    if (url === "/api/events") return new Response(new ReadableStream(), {headers: {"Content-Type": "text/event-stream"}});
    return new Response(JSON.stringify({items: [], revision: 1, capacity: 1000}));
  });
  vi.stubGlobal("fetch", fetch);
  render(<MemoryRouter><LiveConsole /></MemoryRouter>);
  await waitFor(() => expect(screen.getByText("已连接")).toBeInTheDocument());
  expect(screen.queryByLabelText("访问密码")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", {name: "退出"})).not.toBeInTheDocument();
  for (const call of fetch.mock.calls) {
    expect((call as unknown as [string, RequestInit])[1]?.headers ?? {}).not.toHaveProperty("Authorization");
  }
});
