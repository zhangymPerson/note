// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";

const mocks = vi.hoisted(() => ({
  listNotes: vi.fn(),
  listCategories: vi.fn(),
  deleteNote: vi.fn(),
  createCategory: vi.fn(),
  renameCategory: vi.fn(),
  deleteCategory: vi.fn(),
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
const CATS = [
  { id: 1, name: "未分类", note_count: 1 },
  { id: 2, name: "工作", note_count: 2 },
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
  mocks.listNotes.mockResolvedValue({ items: [NOTE], total: 1, page: 1, per_page: 20 });
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
  it("笔记列表与右侧分类树同时渲染，分类树里不再有 chips", async () => {
    const target = render();
    await vi.waitFor(() => expect(target.querySelector(".note-title")?.textContent).toContain("第一条"));
    await vi.waitFor(() => expect(target.querySelectorAll(".cat-list .cat-item")).toHaveLength(2));
    expect(target.querySelector(".list-layout > nav.cat-tree")).not.toBeNull();
    expect(target.querySelector(".chips")).toBeNull();
  });

  it("点击分类树里的分类，按该分类从第 1 页重新拉取", async () => {
    const target = render();
    await vi.waitFor(() => expect(target.querySelectorAll(".cat-list .cat-row")).toHaveLength(2));
    target.querySelectorAll<HTMLElement>(".cat-list .cat-row")[1].click();
    flushSync();
    await vi.waitFor(() => expect(mocks.listNotes).toHaveBeenLastCalledWith({ q: "", category: 2, page: 1 }));
  });

  it("删除正在筛选的分类后回到「全部」第 1 页", async () => {
    const target = render();
    await vi.waitFor(() => expect(target.querySelectorAll(".cat-list .cat-row")).toHaveLength(2));
    target.querySelectorAll<HTMLElement>(".cat-list .cat-row")[1].click();
    flushSync();
    await vi.waitFor(() => expect(mocks.listNotes).toHaveBeenLastCalledWith({ q: "", category: 2, page: 1 }));

    mocks.listCategories.mockResolvedValue([CATS[0]]);
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const row = target.querySelectorAll(".cat-list .cat-item")[1];
    row.querySelectorAll<HTMLElement>(".cat-act")[1].click();
    await vi.waitFor(() => expect(mocks.deleteCategory).toHaveBeenCalledWith(2));
    await vi.waitFor(() => expect(mocks.listNotes).toHaveBeenLastCalledWith({ q: "", category: 0, page: 1 }));
  });

  it("分类接口失败时列表仍可读，树只剩根节点", async () => {
    mocks.listCategories.mockRejectedValue(new Error("分类读取失败"));
    const target = render();
    await vi.waitFor(() => expect(target.querySelector(".note-title")?.textContent).toContain("第一条"));
    expect(target.querySelectorAll(".cat-list .cat-item")).toHaveLength(0);
    expect(target.querySelector(".cat-root .cat-row")).not.toBeNull();
  });

  it("URL 指向不存在的分类时不崩、无高亮项，列表照常显示", async () => {
    history.replaceState(null, "", "/notes?category=99");
    const target = render();
    await vi.waitFor(() => expect(target.querySelector(".note-title")?.textContent).toContain("第一条"));
    expect(target.querySelectorAll('.cat-row[data-active="true"]')).toHaveLength(0);
  });
});
