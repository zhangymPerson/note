import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  AI_PROMPT_MAX_RUNES,
  countRunes,
  getAIConfig,
  keyHint,
  saveAIConfig,
} from "./ai-config";

const CONFIGURED = {
  base_url: "https://api.openai.com/v1",
  models: ["gpt-4o-mini", "deepseek-chat"],
  prompt: "整理成笔记 {{content}}",
  default_prompt: "默认提示词 {{content}}",
  prompt_variable: "{{content}}",
  api_key_set: true,
  api_key_masked: "sk-…abcd",
  path: "/home/u/.config/note/note.yaml",
};

let requests: {
  method: string;
  url: string;
  body: unknown;
  headers: unknown;
}[] = [];

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mockEndpoints(handlers: {
  get?: () => Response;
  put?: (body: unknown) => Response;
}): void {
  requests = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: string | Request, init?: RequestInit) => {
      const url = typeof input === "string" ? input : input.url;
      const method = init?.method ?? "GET";
      if (url === "/api/csrf") return json({ token: "tok" });
      requests.push({
        method,
        url,
        body:
          init?.body === undefined ? undefined : JSON.parse(String(init.body)),
        headers: init?.headers,
      });
      if (url !== "/api/ai-config")
        return json({ error: { code: "not_found", message: "意外" } }, 404);
      if (method === "PUT")
        return (
          handlers.put?.(JSON.parse(String(init?.body))) ?? json(CONFIGURED)
        );
      return handlers.get?.() ?? json(CONFIGURED);
    }),
  );
}

beforeEach(() => {
  vi.resetModules();
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("ai-config api", () => {
  it("读取配置时只拿到掩码，没有明文密钥字段", async () => {
    mockEndpoints({});
    const config = await getAIConfig();

    expect(config).toEqual(CONFIGURED);
    expect(Object.keys(config)).not.toContain("api_key");
    expect(requests[0]).toMatchObject({ method: "GET", url: "/api/ai-config" });
  });

  it("保存时带上 CSRF 令牌，并把四项一起提交", async () => {
    mockEndpoints({});
    await saveAIConfig({
      base_url: "https://x.example.com/v1",
      models: "m1, m2",
      prompt: "p :: {{content}}",
      api_key: "sk-new",
    });

    const put = requests.find((request) => request.method === "PUT");
    expect(put?.url).toBe("/api/ai-config");
    expect(put?.body).toEqual({
      base_url: "https://x.example.com/v1",
      models: "m1, m2",
      prompt: "p :: {{content}}",
      api_key: "sk-new",
    });
    expect((put?.headers as Record<string, string>)["X-CSRF-Token"]).toBe(
      "tok",
    );
  });

  it("省略 api_key 时请求体里没有这个字段（表示沿用旧密钥）", async () => {
    mockEndpoints({});
    await saveAIConfig({
      base_url: CONFIGURED.base_url,
      models: "gpt-4o-mini, deepseek-chat",
      prompt: CONFIGURED.prompt,
    });

    const put = requests.find((request) => request.method === "PUT");
    expect(put?.body).toEqual({
      base_url: CONFIGURED.base_url,
      models: "gpt-4o-mini, deepseek-chat",
      prompt: CONFIGURED.prompt,
    });
  });

  it("字段级错误会带出 fields 供表单标红", async () => {
    mockEndpoints({
      put: () =>
        json(
          {
            error: {
              code: "validation_failed",
              message: "ai.base_url 必须是 http(s) 绝对地址",
              fields: { base_url: "ai.base_url 必须是 http(s) 绝对地址" },
            },
          },
          400,
        ),
    });

    await expect(
      saveAIConfig({ base_url: "api.example.com", models: "", prompt: "" }),
    ).rejects.toMatchObject({
      code: "validation_failed",
      fields: { base_url: "ai.base_url 必须是 http(s) 绝对地址" },
    });
  });
});

describe("ai-config helpers", () => {
  it("按码点计数，与服务端 RuneCount 同口径", () => {
    expect(countRunes("你好")).toBe(2);
    expect(countRunes("🙂a")).toBe(2);
    expect(AI_PROMPT_MAX_RUNES).toBe(2000);
  });

  it("密钥提示区分「未配置 / 留空不改动 / 将清除」", () => {
    expect(keyHint({ api_key_set: false, api_key_masked: "" }, false)).toBe(
      "还没有配置密钥。",
    );

    expect(keyHint(CONFIGURED, false)).toContain("sk-…abcd");
    expect(keyHint(CONFIGURED, false)).toContain("留空表示不改动");

    expect(keyHint(CONFIGURED, true)).toContain("清除");
  });
});
