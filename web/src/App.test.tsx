import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, it, expect } from "vitest";
import App from "./App";
function mount(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <App />
    </MemoryRouter>,
  );
}

describe("read-only console", () => {
  it("labels demo mode and disconnected live data on every page", async () => {
    const user = userEvent.setup();
    mount("/routes");
    for (const name of ["会话", "请求", "配置", "路由"]) {
      await user.click(
        within(screen.getByRole("navigation", { name: "主导航" })).getByRole(
          "link",
          { name: new RegExp(name) },
        ),
      );
      expect(screen.getByText("演示数据")).toBeInTheDocument();
      expect(screen.getByTitle("合成样本，未连接实时接口")).toBeInTheDocument();
      expect(screen.queryByText("观测不参与路由决策")).not.toBeInTheDocument();
      expect(
        screen.getByRole("heading", { level: 1, name }),
      ).toBeInTheDocument();
    }
  });
  it("searches and clears empty results", async () => {
    const user = userEvent.setup();
    mount("/requests");
    await user.type(
      screen.getByRole("textbox", { name: "搜索请求或会话" }),
      "nothing-matches",
    );
    expect(
      screen.getByText("没有匹配的请求。请调整筛选条件。"),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "清除筛选" }));
    expect(
      screen.getByRole("link", { name: "demo-req-1048" }),
    ).toBeInTheDocument();
  });
  it("opens route, session and request details through real links", async () => {
    const user = userEvent.setup();
    mount("/routes?route=opencode-a");
    await user.click(
      within(screen.getByRole("dialog")).getAllByRole("link", {
        name: /demo-session-01/,
      })[0],
    );
    await user.click(
      within(screen.getByRole("dialog")).getByRole("link", {
        name: "demo-req-1048",
      }),
    );
    await user.click(screen.getByRole("tab", { name: "识别依据" }));
    expect(
      within(screen.getByRole("tabpanel")).getByText("X-Session-Id"),
    ).toBeInTheDocument();
    await user.click(screen.getByRole("tab", { name: "头部对照" }));
    expect(screen.getAllByText("[已脱敏]")).toHaveLength(2);
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });
  it("supports pagination and theme switching", async () => {
    const user = userEvent.setup();
    mount("/requests");
    await user.click(screen.getByRole("button", { name: "下一页" }));
    expect(screen.getByText("共 48 条 · 第 2 / 5 页")).toBeInTheDocument();
    const themeButton = screen.getByRole("button", { name: /切换.*主题/ });
    const before = document.documentElement.classList.contains("dark");
    await user.click(themeButton);
    expect(document.documentElement.classList.contains("dark")).toBe(!before);
  });
  it("renders missing records explicitly", () => {
    mount("/requests?request=missing");
    expect(
      within(screen.getByRole("dialog")).getByText("未找到记录"),
    ).toBeInTheDocument();
  });
  it("never labels missing egress evidence as a successful route", () => {
    mount("/requests?request=demo-req-1040");
    const dialog = within(screen.getByRole("dialog"));
    expect(dialog.getByText("出口未观测")).toBeInTheDocument();
    expect(dialog.queryByText(/200 · 成功/)).not.toBeInTheDocument();
  });
});
