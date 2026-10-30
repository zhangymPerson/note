import { afterEach, describe, expect, it, vi } from "vitest";

import { defaultModel, hostOf, optimizeNote } from "./ai";

let requests: { url: string; init: RequestInit }[] = [];

function stubFetch(result: unknown, status = 200): void {
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, init });
      return new Response(JSON.stringify(result), {
        status,
        headers: { "Content-Type": "application/json" },
      });
    }),
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("ai", () => {
  it("optimizeNote 省略模型时不带 model 字段", async () => {
    stubFetch({ content: "# 优化后", model: "m1" });

    const result = await optimizeNote("正文");
    expect(result).toEqual({ content: "# 优化后", model: "m1" });

    const optimize = requests.find((request) => request.url === "/api/ai/optimize");
    expect(optimize?.init.method).toBe("POST");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({ content: "正文" });
  });

  it("optimizeNote 传入模型时带上 model 字段", async () => {
    stubFetch({ content: "x", model: "m2" });

    await optimizeNote("正文", "m2");

    const optimize = requests.find((request) => request.url === "/api/ai/optimize");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
      model: "m2",
    });
  });

  it("optimizeNote 把服务端错误原样抛出（调用方只负责显示）", async () => {
    stubFetch(
      { error: { code: "ai_not_configured", message: "AI 还没配置好" } },
      409,
    );

    await expect(optimizeNote("正文")).rejects.toMatchObject({
      code: "ai_not_configured",
    });
  });

  it("defaultModel 取第一个，hostOf 取主机", () => {
    expect(defaultModel([])).toBe("");
    expect(defaultModel(["a", "b"])).toBe("a");
    expect(hostOf("https://api.example.com/v1")).toBe("api.example.com");
    expect(hostOf("不是地址")).toBe("不是地址");
  });
});
