import { afterEach, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { InboundRulesPanel } from "./inbound-rules";

afterEach(() => vi.unstubAllGlobals());
const initial = {profile: "main", revision: 0, mode: "strict", body_limit: 2097152, metadata: true, conversation: true, cache_key: false, headers: [{name: "X-Session-Id", enabled: true, strip: true}]};
function setup(fail = false) {
 const notify = {success: vi.fn(), error: vi.fn()};
 const fetch = vi.fn(async (url: string, init?: RequestInit) => {
  if (url.endsWith("/preview")) return Response.json({status: 200, source: "X-My-Session", error: ""});
  if (init?.method === "PUT") return fail ? new Response("rules changed; reload before saving", {status: 409}) : Response.json({items: [{...JSON.parse(init.body as string), revision: 1}]});
  return Response.json({items: [initial]});
 });
 vi.stubGlobal("fetch", fetch);
 render(<InboundRulesPanel authHeaders={{Authorization: "Basic test"}} notify={notify}/>);
 return {user: userEvent.setup(), fetch, notify};
}
it("saves independent source/strip flags and headers-only mode with revision", async () => {
 const {user, fetch, notify} = setup();
 await user.click(await screen.findByRole("button", {name: "配置入站 main"}));
 await user.click(screen.getByText("高级规则", {selector: "strong"}));
 await user.click(screen.getByText("身份请求头", {selector: "strong"}));
 await user.click(screen.getByLabelText("X-Session-Id 参与识别"));
 await user.type(screen.getByLabelText("自定义身份请求头"), "X-My-Session");
 await user.click(screen.getByRole("button", {name: "添加请求头"}));
 await user.click(screen.getByLabelText("X-My-Session 转发时移除"));
 await user.selectOptions(screen.getByLabelText("校验模式"), "headers_only");
 expect(screen.queryByLabelText("请求体上限")).not.toBeInTheDocument();
 await user.click(screen.getByRole("button", {name: "保存入站规则"}));
 expect(notify.success).toHaveBeenCalled();
 const call = fetch.mock.calls.find(([, init]) => init?.method === "PUT")!;
 expect(JSON.parse(call[1]!.body as string)).toMatchObject({revision: 0, mode: "headers_only", headers: [{name: "X-Session-Id", enabled: false, strip: true}, {name: "X-My-Session", enabled: true, strip: false}]});
 expect(call[1]?.headers).toMatchObject({Authorization: "Basic test"});
});
it("keeps draft on conflict and can discard changes", async () => {
 const {user, notify} = setup(true);
 await user.click(await screen.findByRole("button", {name: "配置入站 main"}));
 await user.selectOptions(screen.getByLabelText("校验模式"), "headers_only");
 await user.click(screen.getByRole("button", {name: "保存入站规则"}));
 expect(notify.error).toHaveBeenCalledWith("rules changed; reload before saving");
 expect(screen.getByLabelText("校验模式")).toHaveValue("headers_only");
 await user.click(screen.getByRole("button", {name: "放弃修改"}));
 expect(screen.getByLabelText("校验模式")).toHaveValue("strict");
});
it("previews unsaved rules without saving them", async () => {
 const {user, fetch} = setup();
 await user.click(await screen.findByRole("button", {name: "配置入站 main"}));
 await user.click(screen.getByText("测试规则", {selector: "strong"}));
 await user.click(screen.getByRole("button", {name: "测试规则"}));
 expect(await screen.findByRole("status")).toHaveTextContent("通过 · HTTP 200 · 命中来源：X-My-Session");
 expect(fetch.mock.calls.some(([, init]) => init?.method === "PUT")).toBe(false);
});
it("shows summaries first and retains the draft when the drawer closes", async () => {
 const {user} = setup();
 const trigger = await screen.findByRole("button", {name: "配置入站 main"});
 expect(screen.queryByLabelText("校验模式")).not.toBeInTheDocument();
 await user.click(trigger);
 expect(screen.getByRole("dialog", {name: "配置入站：main"})).toBeVisible();
 expect(screen.getByLabelText("X-Session-Id 参与识别")).not.toBeVisible();
 await user.selectOptions(screen.getByLabelText("校验模式"), "headers_only");
 await user.keyboard("{Escape}");
 expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
 expect(screen.getByText("严格检查")).toBeVisible();
 await user.click(trigger);
 expect(screen.getByLabelText("校验模式")).toHaveValue("headers_only");
});
