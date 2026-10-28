# 分类搬进「全部笔记」页右侧树 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 删掉独立的「分类」页与导航项，把分类的筛选和增删改做进「全部笔记」页右侧的一棵分类树里。

**Architecture:** 新增纯展示组件 `components/CategoryTree.svelte`，只负责渲染树、持有展开/改名输入/行错误这类 UI 状态；`categories` 数组和全部 API 调用留在 `views/NoteListView.svelte`，通过返回错误文案的回调注入树。`/categories` 路由映射到 `list`，旧书签由 `NoteListView` 的 `syncUrl()` 自动纠正为 `/notes`。后端与数据库零改动。

**Tech Stack:** Svelte 5（runes：`$state`/`$derived`/`$props`/`$effect`）、TypeScript、Vite、Vitest（`// @vitest-environment jsdom` + `svelte` 自带的 `mount`/`flushSync`，不新增依赖）、Go `net/http` 后端（本次不改）。

**Spec:** `docs/superpowers/specs/2026-10-28-list-category-tree-design.md`

## Global Constraints

- 只改 `web/src`、`docs`、`CHANGELOG.md`；不碰 Go 代码、`sql/` 迁移、加密与主密码逻辑。
- 分类名 1–30 字符：输入框 `maxlength="30"`，提交前 `trim()`，`trim()` 后为空一律不提交。
- 默认分类 id 固定为 `1`（常量 `DEFAULT_CATEGORY_ID`），不渲染「改名 / 删除」按钮。
- 分类名只用文本插值渲染；`{@html}` 仍只允许出现在 `web/src/components/Markdown.svelte`。
- 所有写操作走 `web/src/lib/notes.ts` 的既有客户端（`X-CSRF-Token` 由 `lib/api.ts` 统一注入），不得直接 `fetch`。
- 样式写进全局 `web/src/app.css`，沿用现有变量（`--border --surface --bg --text --muted --accent --accent-contrast --font-mono`），不新增组件内 `<style>`。
- URL 约定不变：`/notes?q=&category=&page=`；`category=0` 表示全部。
- 合入前 `just test` 与 `just check` 必须全绿；`CHANGELOG.md` 的 `[Unreleased]` 条目在两者通过之后才写。
- 注释与用户可见文案用中文，风格对齐现有文件（解释「为什么」，不解释语法）。

## Review Focus

1. **「未分类」（id=1）行**没有任何改名/删除按钮——误触会拿 409 `category_reserved`。测试只覆盖普通分类的行为。
2. **删除正在筛选的那个分类**时筛选必须回「全部」且页码回 1，否则列表停在一个空结果页。
3. **分类名重复 / 全是空白**：后端 409 `category_exists` 要显示在输入框下方，空白名前端不提交；不是整页报错。
4. **`listCategories` 失败**时列表本身仍可读（现有的静默降级承诺），树只剩根节点。
5. **URL 带着已删除的分类 id**（`?category=99`）时不崩、无任何高亮项，列表正常显示。

---

### Task 1: `CategoryTree` 组件

**Files:**

- Create: `web/src/components/CategoryTree.svelte`
- Create: `web/src/components/CategoryTree.test.ts`
- Modify: `web/src/app.css`（文件末尾「分类树」小节）

**Interfaces:**

- Consumes: `Category` 类型（`web/src/lib/notes.ts`：`{ id: number; name: string; note_count: number }`）。
- Produces: 组件 props（Task 2 按此使用）：
  ```ts
  interface Props {
    categories: Category[];
    active: number; // 0 = 全部，否则分类 id
    busy: boolean;
    onPick: (id: number) => void;
    onCreate: (name: string) => Promise<string>; // '' = 成功，非空 = 错误文案
    onRename: (id: number, name: string) => Promise<string>;
    onDelete: (id: number) => Promise<string>;
  }
  ```

**DOM 契约**（测试按此查询，实现必须照此产出；`busy` 时所有操作按钮 `disabled`）：

```
nav.cat-tree[aria-label="分类"]
  div.cat-root
    button.cat-toggle[aria-expanded="true|false"]          文案「▾」
    button.cat-row[data-active]                            「全部笔记」+ span.cat-name + span.cat-num
  ul.cat-list                                              open 时才渲染
    li.cat-item                                            （每个分类一个）
      div.cat-line
        button.cat-row[data-active]                        span.cat-name + span.cat-num
        span.cat-actions                                   仅当 id !== 1
          button.cat-act「改名」
          button.cat-act「删除」
      （改名中，替代 div.cat-line）form.cat-rename > input[aria-label="分类名称"]
      p.field-error[role="alert"]                          该行错误（有错才渲染）
  form.cat-create
    input[aria-label="新分类名称"][maxlength="30"]
    button[type="submit"]「+」
  p.field-error.cat-create-error[role="alert"]             新建错误（有错才渲染，与 form 同级）
```

- [ ] **Step 1: 写失败的组件测试**

Create `web/src/components/CategoryTree.test.ts`:

```ts
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
  busy?: boolean;
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
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `npm --prefix web test -- src/components/CategoryTree.test.ts`
Expected: FAIL，`Failed to resolve import "./CategoryTree.svelte"`

- [ ] **Step 3: 实现 `web/src/components/CategoryTree.svelte`**

按上面的 DOM 契约实现。要点（这些是测试与设计固定的决策，其余随你）：

- `let { categories, active, busy = false, onPick, onCreate, onRename, onDelete }: Props = $props();`
- 组件内部状态只有：`open = $state(true)`、`editingId = $state(0)`、`editingName = $state("")`、
  `newName = $state("")`、`createError = $state("")`、`errors = $state<Record<number, string>>({})`。
- `const total = $derived(categories.reduce((sum, cat) => sum + cat.note_count, 0));`
- 根节点数字用 `total`；子项数字用 `cat.note_count`。
- 提交新建：`const name = newName.trim(); if (name === "") return; const error = await onCreate(name); if (error) createError = error; else { createError = ""; newName = ""; }`
- 改名提交：同样 `trim()` 后为空直接 return；成功才 `editingId = 0`；失败写 `errors[id]`。
- Esc：输入框上 `onkeydown={(event) => { if (event.key === "Escape") { editingId = 0; editingName = ""; } }}`；
  **不绑定 `onblur` 保存**。
- 删除：`if (!window.confirm(\`删除分类「${cat.name}」？\`)) return;`然后`const error = await onDelete(cat.id); if (error) errors[cat.id] = error;`
- `DEFAULT_CATEGORY_ID = 1`；改名按钮点击时清掉该行旧错误再进入编辑。

- [ ] **Step 4: 加 `web/src/app.css` 的「分类树」小节**

追加在「分类管理」小节之后（该小节 Task 3 会删掉，先贴着写即可）。必须覆盖的选择器：
`.cat-tree`、`.cat-root`、`.cat-toggle`、`.cat-row`（含 `.cat-row[data-active="true"]`）、`.cat-name`、
`.cat-num`（沿用 `.chip-num` 的 monospace + 0.78rem 风格）、`.cat-list`、`.cat-item`、`.cat-line`、
`.cat-actions`、`.cat-act`、`.cat-rename`、`.cat-create`。行为要求：

- `.cat-actions { opacity: 0 }`，`.cat-line:hover .cat-actions, .cat-line:focus-within .cat-actions { opacity: 1 }`
  （按钮始终留在 DOM 里，只是视觉隐藏）。
- `.cat-row` 是整行宽度、左对齐的按钮，选中态用 `--accent` 边框 + `--surface` 底色；
  子项相对 `.cat-root` 缩进 12px 并用 1px `--border` 竖线表示层级。
- `.cat-act` 是小号 `button-quiet` 风格（无边框、`--muted` 文字、hover 变 `--text`）。

- [ ] **Step 5: 运行测试，确认通过**

Run: `npm --prefix web test -- src/components/CategoryTree.test.ts`
Expected: PASS，12 passed

- [ ] **Step 6: 类型检查**

Run: `npm --prefix web run check`
Expected: 无 error（新增的 `.svelte` 与测试都通过 svelte-check / tsc）

- [ ] **Step 7: 提交**

```bash
git add web/src/components/CategoryTree.svelte web/src/components/CategoryTree.test.ts web/src/app.css
git commit -m "feat(web): add category tree component"
```

---

### Task 2: 列表页接入分类树

**Files:**

- Modify: `web/src/views/NoteListView.svelte`（整体替换 `<section>` 结构 + 脚本里加包装函数、删 chips 相关代码）
- Create: `web/src/views/NoteListView.test.ts`
- Modify: `web/src/app.css`（新增 `.list-layout`；删除 `.chips`、`.chip`、`.chip[data-active="true"]`、`.chip:focus-visible`、`.chip-num`）

**Interfaces:**

- Consumes: `CategoryTree` 的 props（Task 1），`listNotes/listCategories/createCategory/renameCategory/deleteCategory`（`web/src/lib/notes.ts` 现有签名），`ApiRequestError`（`web/src/lib/api.ts`，字段 `fields?: Record<string, string>`）。
- Produces: 无对外接口；`/notes` 页面的 DOM 契约——`section.page.list > div.list-layout > (左侧列表, nav.cat-tree)`。

- [ ] **Step 1: 写失败的集成测试**

Create `web/src/views/NoteListView.test.ts`:

```ts
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
});
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `npm --prefix web test -- src/views/NoteListView.test.ts`
Expected: FAIL（`.cat-tree` 不存在、`.chips` 仍存在）

- [ ] **Step 3: 改 `NoteListView.svelte`**

脚本部分：

- 新增 `import CategoryTree from '../components/CategoryTree.svelte'`、`import { ApiRequestError } from '../lib/api'`，
  并把 `createCategory, deleteCategory, renameCategory` 加进 `../lib/notes` 的 import。
- 新增 `const DEFAULT_CATEGORY_ID = 1`、`let busy = $state(false)`。
- 抽出 `function message(cause: unknown): string { return cause instanceof Error ? cause.message : '操作失败，请稍后重试' }`。
- 删掉 `pickCategory`，改成三个包装函数（签名固定，树按此调用）：
  ```ts
  async function createNamed(name: string): Promise<string>; // 成功刷新 categories 并返回 ''
  async function renameTo(id: number, name: string): Promise<string>; // 成功用后端返回的全量列表覆盖 categories
  async function removeCategory(id: number): Promise<string>; // 成功后 category = 0 且 page = 1，再刷新 categories
  ```
  错误分支：`ApiRequestError` 且 `fields?.name` 优先用它，否则 `message(cause)`。
  三个函数都用 `busy = true/false` 包住，`busy` 传给树。
- 页面不再自己算分类总数：根节点数字由 `CategoryTree` 内部 `$derived` 求和。
- `refresh`、`syncUrl`、`$effect`、`remove(note)`、`shownAt` 全部保持原样。

模板部分（这是布局决策，照此写）：

```svelte
<section class="page list">
  <header class="list-head">…（标题 +「新建笔记」按钮，原样）</header>
  <div class="list-layout">
    <div class="list-main">
      <form class="search-form">…（原样）</form>
      {#if error}…{/if}
      {#if loading}…{:else if rows.length === 0}…{:else}
        <ul class="note-rows">…</ul>
        <nav class="pager">…</nav>
      {/if}
    </div>
    <CategoryTree
      categories={categories}
      active={category}
      {busy}
      onPick={(id) => { page = 1; category = id }}
      onCreate={createNamed}
      onRename={renameTo}
      onDelete={removeCategory}
    />
  </div>
</section>
```

- 空状态文案里 `category !== 0` 的分支保留，但「回到『全部』」改成「点右侧『全部笔记』」。
- 分页区 `{#if activeCategory}· 分类「{activeCategory.name}」{/if}` 保留；`activeCategory` 的 `$derived` 保留。

- [ ] **Step 4: 改 `web/src/app.css`**

- 新增 `.list-layout { display: grid; grid-template-columns: minmax(0, 1fr) 240px; gap: 24px; align-items: start; }`
  和 `.list-main { display: flex; flex-direction: column; gap: 18px; min-width: 0; }`。
- 在现有 `@media (max-width: 720px)` 断点里（没有就在文件里 `@media (max-width: 720px)` 内）追加：
  `.list-layout { grid-template-columns: minmax(0, 1fr); }`，让窄屏单列堆叠时树排在列表上方（DOM 顺序已保证）。
- 删除只被 chips 使用的 `.chips`、`.chip`、`.chip[data-active="true"]`、`.chip:focus-visible`、`.chip-num`。
- 把「列表页：分类 chips + 行式条目 + 分页」的注释改成「列表页：笔记列表 + 右侧分类树 + 分页」。

- [ ] **Step 5: 运行测试，确认通过**

Run: `npm --prefix web test`
Expected: 全绿（含 Task 1 的 12 个用例与新增 5 个集成用例）

- [ ] **Step 6: 类型检查**

Run: `npm --prefix web run check`
Expected: 无 error

- [ ] **Step 7: 提交**

```bash
git add web/src/views/NoteListView.svelte web/src/views/NoteListView.test.ts web/src/app.css
git commit -m "feat(web): show category tree beside the note list"
```

---

### Task 3: 移除独立的分类页

**Files:**

- Modify: `web/src/router/router.test.ts`（先改断言）
- Modify: `web/src/router/router.ts`
- Modify: `web/src/App.svelte`
- Modify: `web/src/components/Layout.svelte`
- Delete: `web/src/views/CategoryManagerView.svelte`
- Modify: `web/src/app.css`（删除「分类管理」小节的 `.inline-form*` 与 `.manager*`）

**Interfaces:**

- Consumes: 无（Task 2 之后列表页已不再依赖独立分类页）。
- Produces: `matchRoute("/categories")` → `{ name: "list", params: {} }`；`RouteName` 不再包含 `"categories"`。后续任务不得再引用 `categories` 路由或 `CategoryManagerView`。

- [ ] **Step 1: 改路由测试（先失败）**

在 `web/src/router/router.test.ts` 里，把 `it("matches the master password and category manager routes", …)` 拆成两条：

```ts
it("matches the master password route", () => {
  expect(matchRoute("/master-password")).toEqual({
    name: "master",
    params: {},
  });
});

// 分类管理页并入列表页后，旧地址（含旧书签里的查询串）仍落到列表页，
// NoteListView 挂载时的 syncUrl() 会把地址纠正成 /notes。
it("分类页已并入列表页，旧地址落到列表", () => {
  expect(matchRoute("/categories")).toEqual({ name: "list", params: {} });
  expect(matchRoute("/categories?category=3")).toEqual({
    name: "list",
    params: {},
  });
});
```

- [ ] **Step 2: 运行测试，确认失败**

Run: `npm --prefix web test -- src/router/router.test.ts`
Expected: FAIL，断言得到 `{ name: "categories", params: {} }`

- [ ] **Step 3: 改 `router.ts`**

- `RouteName` 联合类型删掉 `| "categories"`。
- `patterns` 里把 `{ name: "categories", pattern: /^\/categories$/, keys: [] }` 换成
  `{ name: "list", pattern: /^\/categories$/, keys: [] }`，紧跟 `list` 那条，并加一行注释说明是旧地址兼容。

- [ ] **Step 4: 运行路由测试，确认通过**

Run: `npm --prefix web test -- src/router/router.test.ts`
Expected: PASS

- [ ] **Step 5: 改 `App.svelte` 与 `Layout.svelte`，删除分类页组件**

- `App.svelte`：删 `import CategoryManagerView`、`titles` 里的 `categories: '分类管理'`、模板里的
  `{:else if $route.name === 'categories'}` 分支。
- `Layout.svelte`：删导航里的 `<a href="/categories" …>分类</a>`。
- `git rm web/src/views/CategoryManagerView.svelte`。
- `app.css`：删掉「分类管理」整节的注释与 `.inline-form`、`.inline-form input`、
  `.inline-form input:focus-visible`、`.manager`、`.manager li`、`.manager li:last-child`、
  `.manager .row`、`.manager .row-name`、`.manager .row-actions`。

- [ ] **Step 6: 全量测试与类型检查**

Run: `npm --prefix web test`
Expected: 全绿
Run: `npm --prefix web run check`
Expected: 无 error（若报 `CategoryManagerView` 残留引用或 `titles` 缺键，说明第 5 步没删干净）

- [ ] **Step 7: 确认没有残留引用**

Run: `grep -rn "CategoryManager\|/categories" web/src`
Expected: 只剩 `router.ts` 的兼容注释与 `router.test.ts` 的断言（`app.css`、`App.svelte`、`Layout.svelte` 均不应出现）

- [ ] **Step 8: 提交**

```bash
git add -A web/src
git commit -m "refactor(web): drop the standalone category page"
```

---

### Task 4: 文档同步、浏览器验收与 CHANGELOG

**Files:**

- Modify: `docs/PRD.md`
- Modify: `docs/TECH_DESIGN.md`
- Modify: `CHANGELOG.md`

**Interfaces:**

- Consumes: Task 1–3 的最终行为。
- Produces: 无代码接口。

- [ ] **Step 1: 同步 PRD**

先 `grep -n "分类\|/categories" docs/PRD.md` 定位，再改这几处：

- 页面/路由表里 `/categories | 分类管理` 那一行：删除，或改注为「`/categories` 兼容重定向到 `/notes`」。
- 5.5 分类管理 FR-4.3 与 FR-4.6：改成「分类树常驻在 `/notes` 右侧，树内完成新增/改名/删除与筛选；
  不再有独立分类页」。
- 页面清单里出现「分类管理」的地方（含"无弹窗式复杂交互"那段）改成列表页右侧分类树。
- 列表页描述里「搜索框下方是分类筛选条（含『全部』与『未分类』）」改成「右侧分类树（根节点『全部笔记』+『未分类』+ 各分类）」。

- [ ] **Step 2: 同步 TECH_DESIGN**

`grep -n "分类\|/categories\|CategoryManager" docs/TECH_DESIGN.md`，改：

- 前端目录树里 `views/` 的说明删掉「分类」；`components/` 说明加上 `CategoryTree`。
- 前端页面/路由小节里 `/categories` 的条目改成旧地址兼容说明。

- [ ] **Step 3: 跑全量验证（写 CHANGELOG 之前必须做）**

Run: `just test`
Expected: Go 测试与 Vitest 全绿（末尾 `Test Files … passed`，无 failed）
Run: `just check`
Expected: `go vet` 无输出、svelte-check 与 tsc 0 error

- [ ] **Step 4: 浏览器验收**

Run: `just start`，打开 `http://127.0.0.1:5173/notes`（vite dev，API 代理到 :8080），确认：

1. 顶部导航没有「分类」。
2. 右侧是分类树：根节点「全部笔记」+ 数、可收起；下面是「未分类」（无操作按钮）与各分类（有数）。
3. 点某分类 → 列表按它筛选，URL 变 `/notes?category=N`，选中项高亮。
4. 树底部输入框新建分类 → 立刻出现在树里，输入框清空。
5. hover 分类行 → 出「改名 / 删除」；改名走行内输入框，Enter 生效、Esc 取消。
6. 删除非空分类 → 提示「分类下仍有 N 条笔记」显示在该行；删除空分类 → 消失；删掉的正是当前筛选分类时回到「全部」。
7. 直接访问 `/categories` → 渲染列表页，地址被纠正成 `/notes`。
8. 窗口缩到 <720px → 单列，树在列表上方。

若没有可用浏览器，退化为：`npm --prefix web run build` 成功 + Step 3 全绿，并在 CHANGELOG 里写明「未做浏览器手测」。

- [ ] **Step 5: 写 CHANGELOG**

在 `CHANGELOG.md` 的 `[Unreleased]` 小节写条目（Keep a Changelog 格式，只写已验证的事实）：

- **Added**：列表页右侧分类树（`components/CategoryTree.svelte`），树内完成筛选、新增、改名、删除；
  其中新增 `CategoryTree.test.ts` 与 `NoteListView.test.ts` 组件/集成测试。
- **Changed**：`/notes` 改为两栏布局（`minmax(0,1fr) 240px`，≤720px 单列）；分类管理从独立页面并入列表页右侧。
- **Removed**：`views/CategoryManagerView.svelte`、`categories` 路由与导航项；`app.css` 里 chips 与分类管理页的死样式；
  `/categories` 现映射到列表页（旧书签由 `syncUrl()` 纠正为 `/notes`）。

- [ ] **Step 6: 提交**

```bash
git add docs/PRD.md docs/TECH_DESIGN.md CHANGELOG.md
git commit -m "docs: sync PRD/TECH_DESIGN and changelog for the inline category tree"
```
