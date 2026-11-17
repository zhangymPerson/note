import { afterEach, describe, expect, it, vi } from "vitest";

import {
  aiQuickNote,
  defaultModel,
  hostOf,
  optimizeNote,
  optimizeNoteStream,
} from "./ai";

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

    const optimize = requests.find(
      (request) => request.url === "/api/ai/optimize",
    );
    expect(optimize?.init.method).toBe("POST");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
    });
  });

  it("optimizeNote 传入模型时带上 model 字段", async () => {
    stubFetch({ content: "x", model: "m2" });

    await optimizeNote("正文", "m2");

    const optimize = requests.find(
      (request) => request.url === "/api/ai/optimize",
    );
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
      model: "m2",
    });
  });

  it("optimizeNote 传入自定义提示词时带上 custom_prompt，空白不算要求", async () => {
    stubFetch({ content: "x", model: "m1" });

    await optimizeNote("正文", "m1", "  用 mermaid 图表展示  ");
    let optimize = requests.find(
      (request) => request.url === "/api/ai/optimize",
    );
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
      model: "m1",
      custom_prompt: "用 mermaid 图表展示",
    });

    requests = [];
    await optimizeNote("正文", "m1", "   ");
    optimize = requests.find((request) => request.url === "/api/ai/optimize");
    expect(JSON.parse(String(optimize?.init.body))).toEqual({
      content: "正文",
      model: "m1",
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

  it("aiQuickNote 只带 content，返回标题+正文+模型", async () => {
    stubFetch({ title: "待办", content: "- 买牛奶", model: "m1" });

    const result = await aiQuickNote("买牛奶");
    expect(result).toEqual({ title: "待办", content: "- 买牛奶", model: "m1" });

    const quick = requests.find((request) => request.url === "/api/ai/quick");
    expect(quick?.init.method).toBe("POST");
    expect(JSON.parse(String(quick?.init.body))).toEqual({ content: "买牛奶" });
  });

  it("defaultModel 取第一个，hostOf 取主机", () => {
    expect(defaultModel([])).toBe("");
    expect(defaultModel(["a", "b"])).toBe("a");
    expect(hostOf("https://api.example.com/v1")).toBe("api.example.com");
    expect(hostOf("不是地址")).toBe("不是地址");
  });
});

// csrfResponse 让 /api/csrf 拿到一个真令牌：真实运行里 postEventStream 会先取它。
function csrfResponse(): Response {
  return new Response(JSON.stringify({ token: "csrf-test" }), {
    status: 200,
    headers: { "Content-Type": "application/json" },
  });
}

// stubStream 让 fetch 回一个逐块可读的 text/event-stream：
// 故意把一块事件切成两半（含一个多字节汉字），验证分块解码与缓冲拼接。
function stubStream(chunks: string[], status = 200): void {
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit) => {
      requests.push({ url, init });
      if (url === "/api/csrf") return csrfResponse();
      const encoder = new TextEncoder();
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          for (const chunk of chunks) controller.enqueue(encoder.encode(chunk));
          controller.close();
        },
      });
      return new Response(body, {
        status,
        headers: { "Content-Type": "text/event-stream" },
      });
    }),
  );
}

describe("optimizeNoteStream", () => {
  it("逐段转发 delta，最后以 done 的完整结果结束", async () => {
    const sse =
      'event: delta\ndata: {"text":"# 标"}\n\n' +
      'event: delta\ndata: {"text":"题"}\n\n' +
      'event: done\ndata: {"content":"# 标题","model":"m1"}\n\n';
    // 把第一块从汉字中间切开，模拟 TCP 分片
    stubStream([sse.slice(0, 30), sse.slice(30)]);

    const deltas: string[] = [];
    const result = await optimizeNoteStream("正文", {
      onDelta: (text) => deltas.push(text),
    });

    expect(deltas).toEqual(["# 标", "题"]);
    expect(result).toEqual({ content: "# 标题", model: "m1" });
  });

  it("请求体带上 stream:true 与 csrf 令牌", async () => {
    stubStream(['event: done\ndata: {"content":"x","model":"m1"}\n\n']);

    await optimizeNoteStream("正文", {}, "m2", "  用列表  ");

    const request = requests.find((item) => item.url === "/api/ai/optimize");
    expect(JSON.parse(String(request?.init.body))).toEqual({
      content: "正文",
      model: "m2",
      custom_prompt: "用列表",
      stream: true,
    });
    expect(new Headers(request?.init.headers).get("X-CSRF-Token")).toBeTruthy();
  });

  it("error 事件变成 ApiRequestError（例如被截断）", async () => {
    stubStream([
      'event: error\ndata: {"code":"ai_truncated","message":"结果不完整"}\n\n',
    ]);

    await expect(optimizeNoteStream("正文", {})).rejects.toMatchObject({
      code: "ai_truncated",
      message: "结果不完整",
    });
  });

  it("没有 done 就结束时报错，不拿半截当成品", async () => {
    stubStream(['event: delta\ndata: {"text":"写到一半"}\n\n']);

    await expect(optimizeNoteStream("正文", {})).rejects.toMatchObject({
      code: "ai_bad_response",
    });
  });

  it("开流之前就失败时仍是普通 JSON 错误", async () => {
    requests = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit) => {
        requests.push({ url, init });
        if (url === "/api/csrf") return csrfResponse();
        return new Response(
          JSON.stringify({
            error: { code: "ai_timeout", message: "超时了" },
          }),
          { status: 504, headers: { "Content-Type": "application/json" } },
        );
      }),
    );

    await expect(optimizeNoteStream("正文", {})).rejects.toMatchObject({
      code: "ai_timeout",
    });
  });

  it("服务端退化成整段 JSON 也能拿到结果", async () => {
    requests = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit) => {
        requests.push({ url, init });
        if (url === "/api/csrf") return csrfResponse();
        return new Response(JSON.stringify({ content: "x", model: "m1" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    );

    await expect(optimizeNoteStream("正文", {})).resolves.toEqual({
      content: "x",
      model: "m1",
    });
  });
});
