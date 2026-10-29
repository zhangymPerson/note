import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { getTemplate, MAX_TEMPLATE_CODEPOINTS, saveTemplate } from "./template";

interface Reply {
  status?: number;
  body?: unknown;
}

interface Call {
  url: string;
  method: string;
  headers: Record<string, string>;
  body?: string;
}

const sample = {
  content: "# 骨架",
  path: "/home/me/.config/note/note-example.md",
  source: "file",
  default_content: "# 默认",
};

let calls: Call[] = [];
let nextReply = 0;

function jsonResponse(body: unknown, status: number): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mockFetch(replies: Reply[]): void {
  calls = [];
  nextReply = 0;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | Request, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.url;
      calls.push({
        url,
        method: (init?.method ?? "GET").toUpperCase(),
        headers: (init?.headers ?? {}) as Record<string, string>,
        body: init?.body as string | undefined,
      });

      if (url.split("?")[0] === "/api/csrf")
        return jsonResponse({ token: "tok-" + calls.length }, 200);

      const reply = replies[nextReply] ?? {};
      nextReply += 1;
      return jsonResponse(reply.body ?? {}, reply.status ?? 200);
    }),
  );
}

beforeEach(() => {
  mockFetch([]);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("getTemplate", () => {
  it("读 /api/template 并带回文件位置与默认内容", async () => {
    mockFetch([{ body: sample }]);
    const tpl = await getTemplate();
    expect(tpl).toEqual(sample);
    expect(calls[0].url).toBe("/api/template");
    expect(calls[0].method).toBe("GET");
  });
});

describe("saveTemplate", () => {
  it("PUT 携带 CSRF 令牌与 content", async () => {
    mockFetch([{ body: sample }]);
    const tpl = await saveTemplate("# 骨架");
    expect(tpl.content).toBe("# 骨架");

    const put = calls.find((call) => call.method === "PUT");
    expect(put?.url).toBe("/api/template");
    expect(put?.headers["X-CSRF-Token"]).toMatch(/^tok-/);
    expect(JSON.parse(put?.body ?? "{}")).toEqual({ content: "# 骨架" });
  });

  it("400 的字段文案透传到 fields", async () => {
    mockFetch([
      {
        status: 400,
        body: {
          error: {
            code: "validation_failed",
            message: "请修正输入内容",
            fields: { content: "模板请控制在 2000 字以内，当前 2001 字" },
          },
        },
      },
    ]);

    await expect(saveTemplate("x")).rejects.toMatchObject({
      code: "validation_failed",
      fields: { content: "模板请控制在 2000 字以内，当前 2001 字" },
    });
  });
});

describe("MAX_TEMPLATE_CODEPOINTS", () => {
  it("模板上限与正文上限一致", () => {
    expect(MAX_TEMPLATE_CODEPOINTS).toBe(2000);
  });
});
