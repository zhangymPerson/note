// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";

const mocks = vi.hoisted(() => ({
  listNotes: vi.fn(),
  restoreNote: vi.fn(),
  purgeNote: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("../lib/notes", () => mocks);
vi.mock("../lib/toast", () => ({ toast: mocks.toast }));

const { default: TrashView } = await import("./TrashView.svelte");

const PLAIN = {
  id: "n1",
  title: "第一条",
  encrypted: false,
  content: "摘要",
  category_id: 2,
  category_name: "工作",
  deleted_at: "2026-10-28T10:00:00Z",
  updated_at: "2026-10-27T10:00:00Z",
};

const SECRET = {
  id: "n2",
  title: "加密的",
  encrypted: true,
  category_id: null,
  category_name: null,
  deleted_at: "2026-10-29T10:00:00Z",
  updated_at: "2026-10-28T10:00:00Z",
};

let app: ReturnType<typeof mount> | null = null;

function render(): HTMLElement {
  const target = document.createElement("div");
  document.body.append(target);
  app = mount(TrashView, { target });
  flushSync();
  return target;
}

beforeEach(() => {
  mocks.listNotes.mockResolvedValue({
    items: [PLAIN, SECRET],
    total: 2,
    page: 1,
    per_page: 20,
  });
  mocks.restoreNote.mockResolvedValue({ ...PLAIN, deleted_at: null });
  mocks.purgeNote.mockResolvedValue(undefined);
  history.replaceState(null, "", "/trash");
});

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = "";
  vi.clearAllMocks();
  vi.restoreAllMocks();
});

describe("TrashView", () => {
  it("只查回收站（deleted=1）并按移入时间显示", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );
    expect(mocks.listNotes).toHaveBeenCalledWith({ deleted: true, page: 1 });
    const meta = target.querySelector(".note-meta")?.textContent ?? "";
    expect(meta).toContain("移入于");
    expect(meta).toContain("原分类「工作」");
  });

  it("恢复后重新拉取回收站并给出提示", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );
    mocks.listNotes.mockResolvedValue({
      items: [SECRET],
      total: 1,
      page: 1,
      per_page: 20,
    });
    target.querySelectorAll<HTMLElement>(".trash-actions button")[0].click();
    await vi.waitFor(() =>
      expect(mocks.restoreNote).toHaveBeenCalledWith("n1"),
    );
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(1),
    );
    expect(mocks.toast).toHaveBeenCalledWith("已恢复「第一条」");
  });

  it("彻底删除要二次确认，取消时一个请求都不发", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );
    const confirm = vi.spyOn(window, "confirm").mockReturnValue(false);
    target.querySelectorAll<HTMLElement>(".trash-actions button")[1].click();
    flushSync();
    expect(confirm).toHaveBeenCalled();
    expect(mocks.purgeNote).not.toHaveBeenCalled();

    confirm.mockReturnValue(true);
    target.querySelectorAll<HTMLElement>(".trash-actions button")[1].click();
    await vi.waitFor(() => expect(mocks.purgeNote).toHaveBeenCalledWith("n1"));
  });

  it("加密笔记的彻底删除按钮禁用，并说明要去详情页解锁", async () => {
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".note-row")).toHaveLength(2),
    );
    const rows = target.querySelectorAll(".note-row");
    const secretPurge = rows[1].querySelectorAll<HTMLButtonElement>(
      ".trash-actions button",
    )[1];
    expect(secretPurge.disabled).toBe(true);
    expect(secretPurge.getAttribute("title")).toContain("解锁");
    // 恢复不受加密影响：恢复不是读取明文
    expect(
      rows[1].querySelectorAll<HTMLButtonElement>(".trash-actions button")[0]
        .disabled,
    ).toBe(false);
    expect(target.textContent).toContain("先打开详情并解锁，才能彻底删除");
  });

  it("空回收站给出独立空状态", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [],
      total: 0,
      page: 1,
      per_page: 20,
    });
    const target = render();
    await vi.waitFor(() =>
      expect(target.querySelector(".empty-hint")?.textContent).toContain(
        "回收站是空的",
      ),
    );
    expect(target.querySelector(".note-row")).toBeNull();
  });
});
