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
    await waitFor(() => expect(screen.getByText("实时更新中")).toBeInTheDocument());
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
  await waitFor(() => expect(screen.getByText("实时更新中")).toBeInTheDocument());
  expect(screen.queryByLabelText("访问密码")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", {name: "退出"})).not.toBeInTheDocument();
  for (const call of fetch.mock.calls) {
    expect((call as unknown as [string, RequestInit])[1]?.headers ?? {}).not.toHaveProperty("Authorization");
  }
});

describe("supplier configuration", () => {
  const supplier = {
    id: "go-main", origin: "https://opencode.ai",
    internal_base_url: "http://affinity-gateway:8237/r/go-main",
    plugins: [{id: "opencode-go-session", version: "1.0.0"}],
  };
  function setup(saveStatus = 200) {
    const fetch = vi.fn(async (url: string, init?: RequestInit) => {
      if (url === "/api/auth") return Response.json({required: false});
      if (url === "/api/events") return new Response(new ReadableStream(), {headers: {"Content-Type": "text/event-stream"}});
      if (init?.method === "POST") {
        if (saveStatus !== 200) return new Response("保存被拒绝", {status: saveStatus});
        return Response.json({items: [{...supplier, ...JSON.parse(init.body as string)}]});
      }
      if (url === "/api/suppliers") return Response.json({items: [supplier]});
      return Response.json({items: [], revision: 1, capacity: 1000});
    });
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    render(<MemoryRouter initialEntries={["/config"]}><LiveConsole /></MemoryRouter>);
    return {user, fetch};
  }
  it("copies the internal URL and reports success", async () => {
    const {user} = setup();
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", {clipboard: {writeText}});
    await user.click(await screen.findByRole("button", {name: "复制 go-main 内部地址"}));
    expect(writeText).toHaveBeenCalledWith(supplier.internal_base_url);
    expect(await screen.findByText("已复制 go-main 内部地址")).toBeVisible();
  });
  it("allows repeated copy notifications and manual dismissal", async () => {
    const {user} = setup();
    const copy = await screen.findByRole("button", {name: "复制 go-main 内部地址"});
    await user.click(copy);
    await user.click(copy);
    expect(await screen.findAllByRole("button", {name: "关闭通知"})).toHaveLength(2);
    await user.click(screen.getAllByRole("button", {name: "关闭通知"})[0]);
    await waitFor(() => expect(screen.getAllByRole("button", {name: "关闭通知"})).toHaveLength(1));
  });
  it.each(["success", "http", "network"])("reports deletion result: %s", async (result) => {
    const {user, fetch} = setup();
    const remove = await screen.findByRole("button", {name: "删除 go-main"});
    vi.stubGlobal("confirm", vi.fn(() => true));
    if (result === "network") fetch.mockRejectedValueOnce(new Error("网络断开"));
    else fetch.mockResolvedValueOnce(new Response(result === "http" ? "删除被拒绝" : null, {status: result === "http" ? 500 : 204}));
    await user.click(remove);
    expect(await screen.findByText(result === "success" ? "已删除出口 go-main" : result === "http" ? "删除被拒绝" : "网络断开")).toBeVisible();
    if (result === "success") expect(screen.queryByRole("button", {name: "删除 go-main"})).not.toBeInTheDocument();
    else expect(screen.getByRole("button", {name: "删除 go-main"})).toBeEnabled();
  });
  it("keeps notification containers outside the two-column page layout", async () => {
    const {user} = setup();
    const copy = await screen.findByRole("button", {name: "复制 go-main 内部地址"});
    const shell = screen.getByRole("navigation").closest(".app-shell")!;
    function expectLayout() {
      expect(Array.from(shell.children).map((child) => child.className)).toEqual(["sidebar", "main-shell"]);
      expect(shell).not.toContainElement(screen.getByRole("region", {name: "通知（F8）"}));
    }
    expectLayout();
    await user.click(copy);
    expectLayout();
    await user.click(screen.getByRole("button", {name: "关闭通知"}));
    expectLayout();
  });
  it("reports copy failure with a manually copyable address", async () => {
    const {user} = setup();
    vi.stubGlobal("navigator", {});
    await user.click(await screen.findByRole("button", {name: "复制 go-main 内部地址"}));
    expect(await screen.findByText(`复制失败，请手动选择并复制内部地址：${supplier.internal_base_url}`)).toBeVisible();
  });
  it("has no edit action and still adds suppliers", async () => {
    const {user, fetch} = setup();
    await screen.findByRole("button", {name: "复制 go-main 内部地址"});
    expect(screen.queryByRole("button", {name: /编辑/})).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", {name: "添加供应商"}));
    await user.type(screen.getByLabelText("出口 ID"), "new-main");
    await user.type(screen.getByLabelText("真实 Origin"), "https://example.com");
    await user.click(screen.getByRole("button", {name: "添加"}));
    expect(fetch).toHaveBeenCalledWith("/api/suppliers", expect.objectContaining({method: "POST", body: JSON.stringify({id: "new-main", origin: "https://example.com", plugins: []})}));
    expect(await screen.findByText("已添加 new-main")).toBeVisible();
    expect(screen.queryByLabelText("出口 ID")).not.toBeInTheDocument();
  });
  it("retains form values when adding fails", async () => {
    const {user} = setup(409);
    await screen.findByRole("button", {name: "复制 go-main 内部地址"});
    await user.click(screen.getByRole("button", {name: "添加供应商"}));
    await user.type(screen.getByLabelText("出口 ID"), "new-main");
    await user.type(screen.getByLabelText("真实 Origin"), "https://example.com");
    await user.click(screen.getByRole("button", {name: "添加"}));
    expect(await screen.findByText("保存被拒绝")).toBeVisible();
    expect(screen.getByLabelText("出口 ID")).toHaveValue("new-main");
  });
});

it.each(["/routes", "/sessions"])("opens grouped requests in a paginated dialog on %s", async (path) => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/api/auth") return Response.json({required: false});
    if (url === "/api/events") return new Response(new ReadableStream(), {headers: {"Content-Type": "text/event-stream"}});
    if (url === "/api/suppliers") return Response.json({items: []});
    return Response.json({items: Array.from({length: 21}, (_, i) => ({id: `request-${i}`, at: new Date().toISOString(), profile: "route-a", session: "session-a", mode: "inbound", model: `model-${i}`, duration: 1, before: {}, after: {}})), revision: 1, capacity: 1000});
  }));
  const user = userEvent.setup();
  render(<MemoryRouter initialEntries={[path]}><LiveConsole /></MemoryRouter>);
  const trigger = await screen.findByRole("button", {name: "查看请求"});
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  await user.click(trigger);
  expect(screen.getByRole("dialog", {name: "查看请求"})).toBeVisible();
  expect(screen.getByText("model-0")).toBeVisible();
  await user.click(screen.getByRole("button", {name: "下一页"}));
  expect(screen.getByText("model-20")).toBeVisible();
  await user.click(screen.getByRole("button", {name: "详情"}));
  expect(screen.getByRole("dialog", {name: "request-20"})).toBeVisible();
  await user.keyboard("{Escape}");
  expect(screen.getByRole("dialog", {name: "查看请求"})).toBeVisible();
  await user.click(screen.getByRole("button", {name: "关闭"}));
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
});

it("supports direct page selection and changing page size in the request dialog", async () => {
  vi.stubGlobal("fetch", vi.fn(async (url: string) => {
    if (url === "/api/auth") return Response.json({required: false});
    if (url === "/api/events") return new Response(new ReadableStream(), {headers: {"Content-Type": "text/event-stream"}});
    if (url === "/api/suppliers") return Response.json({items: []});
    return Response.json({items: Array.from({length: 45}, (_, i) => ({id: `r-${i}`, at: new Date().toISOString(), profile: "route-a", mode: "inbound", model: `model-${i}`, duration: 1, before: {}, after: {}})), revision: 1, capacity: 1000});
  }));
  const user = userEvent.setup();
  render(<MemoryRouter initialEntries={["/routes"]}><LiveConsole /></MemoryRouter>);
  await user.click(await screen.findByRole("button", {name: "查看请求"}));
  await user.click(screen.getByRole("button", {name: "第 3 页"}));
  expect(screen.getByText("model-40")).toBeVisible();
  expect(screen.getByRole("button", {name: "下一页"})).toBeDisabled();
  await user.selectOptions(screen.getByLabelText("每页条数"), "50");
  expect(screen.getByText("model-0")).toBeVisible();
  expect(screen.getByText("model-44")).toBeVisible();
  expect(screen.getByRole("button", {name: "第 1 页"})).toHaveAttribute("aria-current", "page");
  expect(screen.getByRole("button", {name: "上一页"})).toBeDisabled();
});
