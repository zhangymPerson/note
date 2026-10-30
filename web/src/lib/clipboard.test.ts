// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";

import { copyText } from "./clipboard";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("copyText", () => {
  it("uses the async clipboard API when available", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    vi.stubGlobal("navigator", { clipboard: { writeText } });

    await expect(copyText("# 标题\n\n正文")).resolves.toBe(true);
    expect(writeText).toHaveBeenCalledWith("# 标题\n\n正文");
    // 异步 API 成功时不该再走 textarea 兜底
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("falls back to execCommand when the async API throws", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    vi.stubGlobal("navigator", { clipboard: { writeText } });
    const execCommand = vi.fn().mockReturnValue(true);
    document.execCommand = execCommand;

    await expect(copyText("正文")).resolves.toBe(true);
    expect(execCommand).toHaveBeenCalledWith("copy");
    // 临时 textarea 用完必须删掉，不能留在 DOM 里
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("falls back when the API is missing entirely", async () => {
    vi.stubGlobal("navigator", {});
    document.execCommand = vi.fn().mockReturnValue(false);

    await expect(copyText("正文")).resolves.toBe(false);
    expect(document.querySelector("textarea")).toBeNull();
  });

  it("reports failure when both paths fail", async () => {
    vi.stubGlobal("navigator", {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error("no")) },
    });
    document.execCommand = vi.fn().mockImplementation(() => {
      throw new Error("unsupported");
    });

    await expect(copyText("正文")).resolves.toBe(false);
    expect(document.querySelector("textarea")).toBeNull();
  });
});
