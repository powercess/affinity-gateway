import { afterEach, expect, it, vi } from "vitest";
import { copyText } from "./clipboard";

afterEach(() => {
  vi.unstubAllGlobals();
  delete (document as Partial<Document>).execCommand;
});

it.each([false, true])("falls back when clipboard is unavailable or rejected: %s", async (rejected) => {
  vi.stubGlobal("navigator", rejected ? {clipboard: {writeText: vi.fn().mockRejectedValue(new Error("denied"))}} : {});
  const button = document.createElement("button");
  document.body.appendChild(button);
  button.focus();
  document.execCommand = vi.fn(() => {
    expect(document.querySelector("textarea")?.value).toBe("http://gateway/r/test");
    return true;
  });
  await copyText("http://gateway/r/test");
  expect(document.execCommand).toHaveBeenCalledWith("copy");
  expect(document.querySelector("textarea")).toBeNull();
  expect(document.activeElement).toBe(button);
  button.remove();
});

it("rejects and cleans up when fallback copying fails", async () => {
  vi.stubGlobal("navigator", {});
  document.execCommand = vi.fn(() => false);
  await expect(copyText("test")).rejects.toThrow("Copy failed");
  expect(document.querySelector("textarea")).toBeNull();
});
