import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ApiRequestError } from "./api";
import {
  MAX_CONTENT_CODEPOINTS,
  countCodePoints,
  createCategory,
  createNote,
  deleteCategory,
  deleteNote,
  getMasterPassword,
  getNote,
  listCategories,
  listEncryptedCiphers,
  listNotes,
  newClientNoteID,
  renameCategory,
  setMasterPassword,
  updateMasterQuestion,
  updateNote,
  updateNoteCipher,
} from "./notes";

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
      if (reply.status === 204) return new Response(null, { status: 204 });
      return jsonResponse(reply.body ?? {}, reply.status ?? 200);
    }),
  );
}

const ok = (body: unknown): Reply => ({ status: 200, body });

beforeEach(() => {
  mockFetch([]);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("listNotes", () => {
  it("只把非空条件拼进查询串", async () => {
    mockFetch([ok({ items: [], total: 0, page: 1, per_page: 20 })]);
    const page = await listNotes({ q: "测试", category: 3, page: 2 });
    expect(page.per_page).toBe(20);
    expect(calls[0].url).toContain("q=" + encodeURIComponent("测试"));
    expect(calls[0].url).toContain("category=3");
    expect(calls[0].url).toContain("page=2");
  });

  it("全空条件不产生查询串", async () => {
    mockFetch([ok({ items: [], total: 0, page: 1, per_page: 20 })]);
    await listNotes({});
    expect(calls[0].url).toBe("/api/notes");
  });
});

describe("createNote", () => {
  it("POST 携带 CSRF 令牌与 JSON 正文", async () => {
    mockFetch([ok({ id: "n1", title: "标题", encrypted: false })]);
    const note = await createNote({ title: "标题", content: "正文" });
    expect(note.id).toBe("n1");
    const post = calls.find((call) => call.method === "POST");
    expect(post?.url).toBe("/api/notes");
    expect(post?.headers["X-CSRF-Token"]).toMatch(/^tok-/);
    expect(JSON.parse(post?.body ?? "{}")).toEqual({
      title: "标题",
      content: "正文",
    });
  });

  it("403 后刷新令牌并重试一次", async () => {
    mockFetch([
      {
        status: 403,
        body: { error: { code: "csrf_invalid", message: "令牌失效" } },
      },
      { status: 201, body: { id: "n2" } },
    ]);
    const note = await createNote({ title: "标题" });
    expect(note.id).toBe("n2");
    const posts = calls.filter((call) => call.method === "POST");
    expect(posts).toHaveLength(2);
    expect(posts[1].headers["X-CSRF-Token"]).not.toBe(
      posts[0].headers["X-CSRF-Token"],
    );
  });
});

describe("deleteNote", () => {
  it("204 无响应体也返回 undefined", async () => {
    mockFetch([{ status: 204 }]);
    await expect(deleteNote("n3")).resolves.toBeUndefined();
    expect(
      calls.some(
        (call) => call.method === "DELETE" && call.url === "/api/notes/n3",
      ),
    ).toBe(true);
  });
});

describe("newClientNoteID", () => {
  it("生成后端接受的 UUIDv4 且每次不同", () => {
    const id = newClientNoteID();
    expect(id).toMatch(
      /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/,
    );
    expect(newClientNoteID()).not.toBe(id);
  });
});

describe("错误透传", () => {
  it("后端 error.fields 原样带出", async () => {
    mockFetch([
      {
        status: 400,
        body: {
          error: {
            code: "validation_failed",
            message: "正文请控制在 2000 字以内",
            fields: { content: "正文请控制在 2000 字以内" },
          },
        },
      },
    ]);
    const error = await createNote({
      title: "标题",
      content: "x".repeat(2001),
    }).catch((caught) => caught);
    expect((error as ApiRequestError).fields).toEqual({
      content: "正文请控制在 2000 字以内",
    });
  });

  it("后端 error.code 原样带出", async () => {
    mockFetch([
      {
        status: 409,
        body: {
          error: { code: "category_not_empty", message: "分类下仍有 3 条笔记" },
        },
      },
    ]);
    const error = await deleteCategory(4).catch((caught) => caught);
    expect(error).toBeInstanceOf(ApiRequestError);
    expect((error as ApiRequestError).code).toBe("category_not_empty");
    expect((error as ApiRequestError).message).toBe("分类下仍有 3 条笔记");
  });
});

describe("分类与主密码", () => {
  it("listCategories 直接返回数组", async () => {
    mockFetch([ok([{ id: 1, name: "未分类", note_count: 0 }])]);
    await expect(listCategories()).resolves.toEqual([
      { id: 1, name: "未分类", note_count: 0 },
    ]);
  });

  it("createCategory POST、renameCategory PUT", async () => {
    mockFetch([
      { status: 201, body: { id: 5, name: "工作", note_count: 0 } },
      ok([{ id: 5, name: "生活", note_count: 0 }]),
    ]);
    await expect(createCategory("工作")).resolves.toMatchObject({ id: 5 });
    await expect(renameCategory(5, "生活")).resolves.toEqual([
      { id: 5, name: "生活", note_count: 0 },
    ]);
    expect(calls.find((call) => call.method === "POST")?.url).toBe(
      "/api/categories",
    );
    expect(calls.find((call) => call.method === "PUT")?.url).toBe(
      "/api/categories/5",
    );
  });

  it("getMasterPassword 带回公开盐", async () => {
    mockFetch([
      ok({
        configured: true,
        salt_b64: "AAAAAAAAAAAAAAAAAAAAAA==",
        question: "生日",
      }),
    ]);
    await expect(getMasterPassword()).resolves.toMatchObject({
      salt_b64: "AAAAAAAAAAAAAAAAAAAAAA==",
    });
  });

  it("setMasterPassword POST 一次性写入", async () => {
    mockFetch([{ status: 201, body: { configured: true, question: "生日" } }]);
    await setMasterPassword("AAAAAAAAAAAAAAAAAAAAAA==", "生日");
    const post = calls.find((call) => call.method === "POST");
    expect(JSON.parse(post?.body ?? "{}")).toEqual({
      salt_b64: "AAAAAAAAAAAAAAAAAAAAAA==",
      question: "生日",
    });
  });

  it("getNote / updateNote 走 /api/notes/{id}", async () => {
    mockFetch([
      ok({ id: "n9", title: "旧", rev: 1 }),
      ok({ id: "n9", title: "新", rev: 2 }),
    ]);
    await expect(getNote("n9")).resolves.toMatchObject({ rev: 1 });
    await expect(updateNote("n9", { title: "新" })).resolves.toMatchObject({
      rev: 2,
    });
    expect(
      calls.some(
        (call) => call.method === "PUT" && call.url === "/api/notes/n9",
      ),
    ).toBe(true);
  });
});

// 后端按 utf8.RuneCountInString 数，前端必须按 Unicode 码点（Array.from）数：
// 代理对（emoji / 生僻字）若按 UTF-16 长度算，两端判定会不一致。
describe("正文长度按码点计", () => {
  it("代理对算一个字，上限与后端常量一致", () => {
    expect(countCodePoints("🚀a")).toBe(2);
    expect(countCodePoints("𠮷野家")).toBe(3);
    expect(countCodePoints("")).toBe(0);
    expect(MAX_CONTENT_CODEPOINTS).toBe(2000);
    expect(countCodePoints("字".repeat(2001))).toBe(2001);
  });
});

// 信息页与改密迁移所需三个客户端方法；EncryptedItem 复用 reencrypt 的定义，不在此重复声明。
describe("信息页客户端", () => {
  it("listEncryptedCiphers GET /api/notes/encrypted 原样返回清单", async () => {
    mockFetch([
      ok({ notes: [{ id: "n1", cipher_b64: "YWJj", kdf_params: "{}" }] }),
    ]);
    const result = await listEncryptedCiphers();
    expect(calls[0].url).toBe("/api/notes/encrypted");
    expect(calls[0].method).toBe("GET");
    expect(result.notes[0].id).toBe("n1");
  });

  it("updateNoteCipher PUT 到 /api/notes/{id}/cipher 并带两个字段", async () => {
    mockFetch([ok({ id: "n1" })]);
    await updateNoteCipher("n 1", "YWJj", '{"algo":"pbkdf2","iter":150000}');
    const put = calls.find((call) => call.method === "PUT");
    expect(put?.url).toBe("/api/notes/n%201/cipher");
    expect(JSON.parse(put?.body ?? "{}")).toEqual({
      cipher_b64: "YWJj",
      kdf_params: '{"algo":"pbkdf2","iter":150000}',
    });
  });

  it("updateNoteCipher 把 409 的 not_encrypted code 透出去", async () => {
    mockFetch([
      {
        status: 409,
        body: {
          error: { code: "not_encrypted", message: "这条笔记不是加密笔记" },
        },
      },
    ]);
    const error = await updateNoteCipher("n1", "YWJj", "{}").catch(
      (caught) => caught,
    );
    expect((error as ApiRequestError).code).toBe("not_encrypted");
  });

  it("updateMasterQuestion PUT /api/master-password/question 返回最新问题", async () => {
    mockFetch([ok({ configured: true, question: "新问题" })]);
    const result = await updateMasterQuestion("新问题");
    expect(result).toEqual({ configured: true, question: "新问题" });
    const put = calls.find((call) => call.method === "PUT");
    expect(put?.url).toBe("/api/master-password/question");
    expect(JSON.parse(put?.body ?? "{}")).toEqual({ question: "新问题" });
  });
});
