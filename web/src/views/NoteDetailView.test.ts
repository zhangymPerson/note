// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import { readable } from "svelte/store";

const mocks = vi.hoisted(() => ({
  getNote: vi.fn(),
  getMasterPassword: vi.fn(),
  copyText: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("../lib/notes", () => ({
  getNote: mocks.getNote,
  getMasterPassword: mocks.getMasterPassword,
  deleteNote: vi.fn(),
  purgeNote: vi.fn(),
  restoreNote: vi.fn(),
}));
vi.mock("../lib/crypto", () => ({
  decryptNoteContent: vi.fn(),
  paramsFromNote: vi.fn(),
}));
vi.mock("../lib/session-key", () => ({
  hasKey: readable(false),
  sessionKey: { current: () => null },
  lockSession: vi.fn(),
  unlockWithPassword: vi.fn(),
}));
vi.mock("../router/router", () => ({ navigate: vi.fn() }));
vi.mock("../lib/toast", () => ({ toast: mocks.toast }));
vi.mock("../lib/clipboard", () => ({ copyText: mocks.copyText }));

const { default: NoteDetailView } = await import("./NoteDetailView.svelte");

const PLAIN = {
  id: "n1",
  title: "会议纪要",
  encrypted: false,
  content: "# 小标题\n\n先说结论。",
  category_id: 2,
  category_name: "工作",
  rev: 3,
  updated_at: "2026-10-29T10:00:00Z",
};

const SECRET = {
  id: "n2",
  title: "加密的",
  encrypted: true,
  cipher_b64: "AAAA",
  kdf_params: "{}",
  category_id: null,
  category_name: null,
  rev: 1,
  updated_at: "2026-10-28T10:00:00Z",
};

let app: ReturnType<typeof mount> | null = null;

async function render(note: unknown): Promise<HTMLElement> {
  mocks.getNote.mockResolvedValue(note);
  mocks.getMasterPassword.mockResolvedValue({
    configured: true,
    salt_b64: "c2FsdA==",
    question: "",
  });

  const target = document.createElement("div");
  document.body.append(target);
  app = mount(NoteDetailView, { target, props: { id: "n1" } });
  flushSync();
  // load() 在 $effect 里异步跑：等它把 $state 落地再断言
  await vi.waitFor(() =>
    expect(target.textContent).not.toContain("笔记载入中"),
  );
  flushSync();
  return target;
}

beforeEach(() => {
  mocks.copyText.mockReset().mockResolvedValue(true);
  mocks.toast.mockReset();
});

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = "";
});

describe("NoteDetailView 复制原文", () => {
  it("明文笔记有一键复制图标，点击复制原文 Markdown", async () => {
    const target = await render(PLAIN);

    const button =
      target.querySelector<HTMLButtonElement>("button.icon-button");
    expect(button).not.toBeNull();
    expect(button?.getAttribute("aria-label")).toBe("复制原文");

    button?.click();
    await vi.waitFor(() =>
      expect(mocks.copyText).toHaveBeenCalledWith(PLAIN.content),
    );

    // 复制的是原文（不是渲染后的页面文本）
    expect(mocks.copyText).toHaveBeenCalledWith(PLAIN.content);
    await vi.waitFor(() =>
      expect(mocks.toast).toHaveBeenCalledWith("已复制原文"),
    );
  });

  it("复制失败时提示用户手动选中", async () => {
    mocks.copyText.mockResolvedValue(false);
    const target = await render(PLAIN);

    target.querySelector<HTMLButtonElement>("button.icon-button")?.click();
    await vi.waitFor(() =>
      expect(mocks.toast).toHaveBeenCalledWith("复制失败，请手动选中正文"),
    );
  });

  it("加密笔记未解锁时没有复制图标（没有明文可复制）", async () => {
    const target = await render(SECRET);

    expect(target.textContent).toContain("这条笔记已加密");
    expect(target.querySelector("button.icon-button")).toBeNull();
    expect(mocks.copyText).not.toHaveBeenCalled();
  });
});
