// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import CategoryTree from "./CategoryTree.svelte";
import type { Category } from "../lib/notes";

const CATS: Category[] = [
  { id: 1, name: "未分类", note_count: 2 },
  { id: 2, name: "工作", note_count: 3 },
];

interface TreeProps {
  categories: Category[];
  active: number;
  busy: boolean;
  onPick: (id: number) => void;
  onCreate: (name: string) => Promise<string>;
  onRename: (id: number, name: string) => Promise<string>;
  onDelete: (id: number) => Promise<string>;
}

let app: ReturnType<typeof mount> | null = null;

function render(overrides: Partial<TreeProps> = {}) {
  const target = document.createElement("div");
  document.body.append(target);
  const props: TreeProps = {
    categories: CATS,
    active: 0,
    busy: false,
    onPick: vi.fn(),
    onCreate: vi.fn().mockResolvedValue(""),
    onRename: vi.fn().mockResolvedValue(""),
    onDelete: vi.fn().mockResolvedValue(""),
    ...overrides,
  };
  app = mount(CategoryTree, { target, props });
  flushSync();
  return { target, props };
}

function typeInto(input: HTMLInputElement, value: string) {
  input.value = value;
  input.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
}

function submit(form: Element) {
  form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  flushSync();
}

afterEach(() => {
  if (app) unmount(app);
  app = null;
  document.body.innerHTML = "";
  vi.restoreAllMocks();
});

describe("CategoryTree", () => {
  it("根节点显示总数，点击回到「全部」", () => {
    const { target, props } = render({ active: 2 });
    const root = target.querySelector(".cat-root .cat-row") as HTMLElement;
    expect(root.textContent).toContain("全部笔记");
    expect(root.textContent).toContain("5");
    expect(root.dataset.active).toBe("false");
    root.click();
    flushSync();
    expect(props.onPick).toHaveBeenCalledWith(0);
  });

  it("点击分类行回传该分类 id，并按 active 标记高亮", () => {
    const { target, props } = render({ active: 2 });
    const rows = target.querySelectorAll<HTMLElement>(".cat-list .cat-row");
    expect(rows).toHaveLength(2);
    expect(rows[1].dataset.active).toBe("true");
    rows[1].click();
    flushSync();
    expect(props.onPick).toHaveBeenCalledWith(2);
  });

  it("「未分类」行没有改名/删除按钮，其它分类各有一个", () => {
    const { target } = render();
    const items = target.querySelectorAll(".cat-list .cat-item");
    expect(items[0].querySelectorAll(".cat-act")).toHaveLength(0);
    expect(items[1].querySelectorAll(".cat-act")).toHaveLength(2);
  });

  it("根节点可收起子项，收起后子项不再渲染", () => {
    const { target } = render();
    (target.querySelector(".cat-toggle") as HTMLElement).click();
    flushSync();
    expect(target.querySelector(".cat-list")).toBeNull();
    expect(target.querySelector(".cat-root .cat-row")).not.toBeNull();
  });

  it("新建分类：回车提交、成功后清空输入", async () => {
    const { target, props } = render();
    const input = target.querySelector(".cat-create input") as HTMLInputElement;
    typeInto(input, "  灵感  ");
    submit(target.querySelector(".cat-create") as HTMLFormElement);
    await vi.waitFor(() => expect(props.onCreate).toHaveBeenCalledWith("灵感"));
    await vi.waitFor(() => expect(input.value).toBe(""));
  });

  it("空白名不提交", () => {
    const { target, props } = render();
    const input = target.querySelector(".cat-create input") as HTMLInputElement;
    typeInto(input, "   ");
    submit(target.querySelector(".cat-create") as HTMLFormElement);
    expect(props.onCreate).not.toHaveBeenCalled();
  });

  it("新建失败把文案显示在输入框下", async () => {
    const { target, props } = render({
      onCreate: vi.fn().mockResolvedValue("分类名已存在"),
    });
    const input = target.querySelector(".cat-create input") as HTMLInputElement;
    typeInto(input, "工作");
    submit(target.querySelector(".cat-create") as HTMLFormElement);
    await vi.waitFor(() =>
      expect(target.querySelector(".cat-create-error")?.textContent).toContain(
        "分类名已存在",
      ),
    );
    expect(
      (target.querySelector(".cat-create input") as HTMLInputElement).value,
    ).toBe("工作");
    expect(props.onCreate).toHaveBeenCalledTimes(1);
  });

  it("改名：行内输入框回车提交", async () => {
    const { target, props } = render();
    const workItem = target.querySelectorAll(".cat-list .cat-item")[1];
    workItem.querySelectorAll<HTMLElement>(".cat-act")[0].click();
    flushSync();
    const input = target.querySelector(".cat-rename input") as HTMLInputElement;
    expect(input.value).toBe("工作");
    typeInto(input, "项目");
    submit(target.querySelector(".cat-rename") as HTMLFormElement);
    await vi.waitFor(() =>
      expect(props.onRename).toHaveBeenCalledWith(2, "项目"),
    );
  });

  it("改名 Esc 取消，不调用 onRename", () => {
    const { target, props } = render();
    const workItem = target.querySelectorAll(".cat-list .cat-item")[1];
    workItem.querySelectorAll<HTMLElement>(".cat-act")[0].click();
    flushSync();
    const input = target.querySelector(".cat-rename input") as HTMLInputElement;
    input.dispatchEvent(
      new KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
    );
    flushSync();
    expect(target.querySelector(".cat-rename")).toBeNull();
    expect(props.onRename).not.toHaveBeenCalled();
  });

  it("删除需确认，取消则不调用", () => {
    const { target, props } = render();
    vi.spyOn(window, "confirm").mockReturnValue(false);
    const workItem = target.querySelectorAll(".cat-list .cat-item")[1];
    workItem.querySelectorAll<HTMLElement>(".cat-act")[1].click();
    expect(props.onDelete).not.toHaveBeenCalled();
  });

  it("删除失败把 409 文案显示在该分类下", async () => {
    const { target } = render({
      onDelete: vi.fn().mockResolvedValue("分类下仍有 3 条笔记"),
    });
    vi.spyOn(window, "confirm").mockReturnValue(true);
    const workItem = target.querySelectorAll(".cat-list .cat-item")[1];
    workItem.querySelectorAll<HTMLElement>(".cat-act")[1].click();
    await vi.waitFor(() =>
      expect(workItem.querySelector(".field-error")?.textContent).toContain(
        "分类下仍有 3 条笔记",
      ),
    );
  });

  it("busy 时操作按钮禁用", () => {
    const { target } = render({ busy: true });
    const workItem = target.querySelectorAll(".cat-list .cat-item")[1];
    for (const button of workItem.querySelectorAll<HTMLButtonElement>(
      ".cat-act",
    )) {
      expect(button.disabled).toBe(true);
    }
  });
});
