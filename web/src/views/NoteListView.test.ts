// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import { get } from "svelte/store";
import { toasts } from "../lib/toast";

const mocks = vi.hoisted(() => ({
  listNotes: vi.fn(),
  listCategories: vi.fn(),
  deleteNote: vi.fn(),
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  deleteCategory: vi.fn(),
  mergeNotes: vi.fn(),
}));

vi.mock("../lib/notes", () => mocks);

const { default: NoteListView } = await import("./NoteListView.svelte");

const NOTE = {
  id: "n1",
  title: "第一条",
  encrypted: false,
  content: "摘要",
  category_id: 2,
  category_name: "工作",
  updated_at: "2026-10-28T10:00:00Z",
};
const NOTE2 = {
  id: "n2",
  title: "第二条",
  encrypted: false,
  content: "摘要二",
  category_id: 1,
  category_name: "未分类",
  updated_at: "2026-10-29T10:00:00Z",
};
const CATS = [
  { id: 1, name: "未分类", note_count: 1, reserved: true },
  { id: 2, name: "工作", note_count: 2, reserved: false },
];

let app: ReturnType<typeof mount> | null = null;

function render() {
  const target = document.createElement("div");
  document.body.append(target);
  app = mount(NoteListView, { target });
  flushSync();
  return target;
}

beforeEach(() => {
  mocks.listNotes.mockResolvedValue({
    items: [NOTE],
    total: 1,
    page: 1,
    per_page: 20,
  });
  mocks.listCategories.mockResolvedValue(CATS);
  mocks.deleteCategory.mockResolvedValue(undefined);
  history.replaceState(null, "", "/notes");
});

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = "";
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("NoteListView", () => {
  it("搜索命中时列表行显示匹配行而不是摘要", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [
        {
          ...NOTE,
          match_lines: ["第二行有 会议 关键词", "第三行又是 会议"],
        },
      ],
      total: 1,
      page: 1,
      per_page: 20,
    });
    history.replaceState(null, "", "/notes?q=%E4%BC%9A%E8%AE%AE");
    const target = render();
    await vi.waitFor(() =>
      expect(mocks.listNotes).toHaveBeenCalledWith({
        q: "会议",
        category: 0,
        page: 1,
      }),
    );
    await vi.waitFor(() => {
      const summary = target.querySelector(".note-summary");
      expect(summary?.textContent).toContain("第二行有 会议 关键词");
    });
    const summary = target.querySelector(".note-summary");
    expect(summary?.textContent).toContain("第三行又是 会议");
    // 匹配行之外的内容不展示（.content 摘要字段被忽略）
    expect(summary?.textContent).not.toContain("摘要");
  });

  it("笔记列表与右侧分类树同时渲染，分类树里不再有 chips", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelector(".note-title")?.textContent).toContain(
        "第一条",
      ),
    );
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".cat-list .cat-item")).toHaveLength(2),
    );
    expect(target.querySelector(".list-layout > nav.cat-tree")).not.toBeNull();
    expect(target.querySelector(".chips")).toBeNull();
  });

  it("点击分类树里的分类，按该分类从第 1 页重新拉取", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".cat-list .cat-row")).toHaveLength(2),
    );
    target.querySelectorAll<HTMLElement>(".cat-list .cat-row")[1].click();
    flushSync();
    await vi.waitFor(() =>
      expect(mocks.listNotes).toHaveBeenLastCalledWith({
        q: "",
        category: 2,
        page: 1,
      }),
    );
  });

  it("删除正在筛选的分类后回到「全部」第 1 页", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".cat-list .cat-row")).toHaveLength(2),
    );
    target.querySelectorAll<HTMLElement>(".cat-list .cat-row")[1].click();
    flushSync();
    await vi.waitFor(() =>
      expect(mocks.listNotes).toHaveBeenLastCalledWith({
        q: "",
        category: 2,
        page: 1,
      }),
    );

    mocks.listCategories.mockResolvedValue([CATS[0]]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const row = target.querySelectorAll(".cat-list .cat-item")[1];
    row.querySelectorAll<HTMLElement>(".cat-act")[1].click();
    await vi.waitFor(() =>
      expect(mocks.deleteCategory).toHaveBeenCalledWith(2),
    );
    await vi.waitFor(() =>
      expect(mocks.listNotes).toHaveBeenLastCalledWith({
        q: "",
        category: 0,
        page: 1,
      }),
    );
  });

  it("分类接口失败时列表仍可读，树只剩根节点", async () => {
    mocks.listCategories.mockRejectedValue(new Error("分类读取失败"));
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelector(".note-title")?.textContent).toContain(
        "第一条",
      ),
    );
    expect(target.querySelectorAll(".cat-list .cat-item")).toHaveLength(0);
    expect(target.querySelector(".cat-root .cat-row")).not.toBeNull();
  });

  it("URL 指向不存在的分类时不崩、无高亮项，列表照常显示", async () => {
    history.replaceState(null, "", "/notes?category=99");
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelector(".note-title")?.textContent).toContain(
        "第一条",
      ),
    );
    expect(
      target.querySelectorAll('.cat-row[data-active="true"]'),
    ).toHaveLength(0);
  });

  it("合并：勾选两条 → 默认标题合并 → 退出勾选模式、提示并刷新列表", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [NOTE, NOTE2],
      total: 2,
      page: 1,
      per_page: 20,
    });
    mocks.mergeNotes.mockResolvedValue({
      id: "m1",
      title: "合并-2026-11-16 20:00:00",
      encrypted: false,
      content: "摘要\n\n---\n\n摘要二",
      category_id: null,
      updated_at: "2026-11-16T12:00:00Z",
    });
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );

    target.querySelector<HTMLElement>("[data-merge]")!.click();
    flushSync();

    // 进入勾选模式：复选框与确认条出现，选 1 条时合并按钮禁用。
    const boxes = target.querySelectorAll<HTMLInputElement>(
      ".note-row input[type=checkbox]",
    );
    expect(boxes.length).toBe(2);
    const confirm = () =>
      target.querySelector<HTMLButtonElement>("[data-merge-confirm]");
    expect(confirm()?.disabled).toBe(true);

    boxes[0].click();
    flushSync();
    expect(confirm()?.disabled).toBe(true);
    expect(target.querySelector("[data-merge-count]")?.textContent).toContain(
      "1",
    );

    boxes[1].click();
    flushSync();
    expect(confirm()?.disabled).toBe(false);

    confirm()!.click();
    await vi.waitFor(() => expect(mocks.mergeNotes).toHaveBeenCalledTimes(1));
    const [ids, title] = mocks.mergeNotes.mock.calls[0];
    expect(ids).toEqual(["n1", "n2"]); // 顺序 = 列表顺序
    expect(title).toMatch(/^合并-\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}$/);

    // 成功后退出勾选模式、弹提示、列表重新拉取。
    await vi.waitFor(() =>
      expect(
        target.querySelectorAll(".note-row input[type=checkbox]"),
      ).toHaveLength(0),
    );
    expect(get(toasts)).toContain("已合并为「合并-2026-11-16 20:00:00」");
    expect(mocks.listNotes.mock.calls.length).toBeGreaterThan(1);
  });

  it("加密行的复选框禁用并带提示", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [
        { ...NOTE, id: "enc1", title: "密文", encrypted: true, content: null },
      ],
      total: 1,
      page: 1,
      per_page: 20,
    });
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelector(".note-row")).not.toBeNull(),
    );

    target.querySelector<HTMLElement>("[data-merge]")!.click();
    flushSync();

    const box = target.querySelector<HTMLInputElement>(
      ".note-row input[type=checkbox]",
    );
    expect(box?.disabled).toBe(true);
    expect(box?.title).toContain("加密");
  });

  it("清空标题后合并请求不带 title（服务端回退默认）", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [NOTE, NOTE2],
      total: 2,
      page: 1,
      per_page: 20,
    });
    mocks.mergeNotes.mockResolvedValue({
      id: "m1",
      title: "合并-2026-11-16 20:00:00",
      encrypted: false,
      content: "摘要\n\n---\n\n摘要二",
      category_id: null,
      updated_at: "2026-11-16T12:00:00Z",
    });
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );

    target.querySelector<HTMLElement>("[data-merge]")!.click();
    flushSync();
    const boxes = target.querySelectorAll<HTMLInputElement>(
      ".note-row input[type=checkbox]",
    );
    boxes[0].click();
    boxes[1].click();
    flushSync();

    const input = target.querySelector<HTMLInputElement>("[data-merge-title]");
    input!.value = "";
    input!.dispatchEvent(new Event("input", { bubbles: true }));
    flushSync();
    target.querySelector<HTMLElement>("[data-merge-confirm]")!.click();

    await vi.waitFor(() => expect(mocks.mergeNotes).toHaveBeenCalledTimes(1));
    const [, title] = mocks.mergeNotes.mock.calls[0];
    expect(title).toBeUndefined();
  });
});
