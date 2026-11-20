// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import { readable } from "svelte/store";

const mocks = vi.hoisted(() => ({
  listNotes: vi.fn(),
  createQuickNote: vi.fn(),
  aiQuickNote: vi.fn(),
  loadStats: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("../lib/notes", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/notes")>("../lib/notes");
  return {
    ...actual,
    listNotes: mocks.listNotes,
    createQuickNote: mocks.createQuickNote,
  };
});
vi.mock("../lib/ai", async () => {
  const actual = await vi.importActual<typeof import("../lib/ai")>("../lib/ai");
  return { ...actual, aiQuickNote: mocks.aiQuickNote };
});
vi.mock("../lib/stats", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/stats")>("../lib/stats");
  return { ...actual, loadStats: mocks.loadStats };
});
vi.mock("../lib/settings", () => ({
  settings: readable({
    status: "ready",
    name: "小北",
    nameSet: true,
    masterConfigured: false,
    error: "",
  }),
  saveName: vi.fn(),
}));
vi.mock("../router/router", () => ({ navigate: mocks.navigate }));

const { default: HomeView } = await import("./HomeView.svelte");

const EMPTY_STATS = {
  total_events: 0,
  total_searches: 0,
  active_days: 0,
  events_by_kind: [],
  top_queries: [],
  days: [],
};

let app: ReturnType<typeof mount> | null = null;

function renderHome(): HTMLDivElement {
  const target = document.createElement("div");
  document.body.append(target);
  app = mount(HomeView, { target });
  flushSync();
  return target;
}

function quickBox(element: HTMLElement): HTMLTextAreaElement {
  return element.querySelector("textarea") as HTMLTextAreaElement;
}

function buttonWith(element: HTMLElement, label: string): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll("button")).find(
    (candidate) => candidate.textContent?.includes(label),
  );
  if (!button) throw new Error("找不到按钮: " + label);
  return button as HTMLButtonElement;
}

function typeInto(
  node: HTMLInputElement | HTMLTextAreaElement,
  value: string,
): void {
  node.value = value;
  node.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
}

function click(node: Element): void {
  node.dispatchEvent(new Event("click", { bubbles: true }));
  flushSync();
}

beforeEach(() => {
  mocks.listNotes.mockResolvedValue({
    items: [],
    total: 0,
    page: 1,
    per_page: 20,
  });
  mocks.loadStats.mockResolvedValue(EMPTY_STATS);
  mocks.createQuickNote.mockResolvedValue({
    id: "q1",
    title: "速记-2026-01-01T00:00:00Z",
    encrypted: false,
    category_id: 2,
    category_name: "速记",
    kind: "note",
    remark: "",
    rev: 1,
    content: "买牛奶",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  });
  mocks.aiQuickNote.mockResolvedValue({
    title: "待办",
    content: "- 买牛奶",
    model: "m1",
  });
});

afterEach(() => {
  unmount(app!);
  app = null;
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("HomeView 速记卡片", () => {
  it("速记按钮直接落库并清空输入、刷新统计", async () => {
    const target = renderHome();
    const box = quickBox(target);
    typeInto(box, "买牛奶");

    click(buttonWith(target, "速记"));

    expect(mocks.createQuickNote).toHaveBeenCalledWith("买牛奶");
    await vi.waitFor(() => expect(box.value).toBe(""));
    // 挂载时一次 + 速记成功后一次
    expect(mocks.loadStats.mock.calls.length).toBe(2);
  });

  it("AI 速记进入预览，改标题后保存用锁定的内容", async () => {
    const target = renderHome();
    typeInto(quickBox(target), "买牛奶");

    click(buttonWith(target, "AI 速记"));

    await vi.waitFor(() =>
      expect(mocks.aiQuickNote).toHaveBeenCalledWith("买牛奶"),
    );
    await vi.waitFor(() => {
      const titleInput = target.querySelector(
        "#quick-ai-title",
      ) as HTMLInputElement;
      expect(titleInput.value).toBe("待办");
    });
    expect(target.textContent).toContain("m1");

    const titleInput = target.querySelector(
      "#quick-ai-title",
    ) as HTMLInputElement;
    typeInto(titleInput, "今日待办");
    click(buttonWith(target, "保存"));

    await vi.waitFor(() =>
      expect(mocks.createQuickNote).toHaveBeenCalledWith(
        "- 买牛奶",
        "今日待办",
      ),
    );
    expect(mocks.createQuickNote).toHaveBeenCalledTimes(1);
  });

  it("返回修改不落库且正文保留", async () => {
    const target = renderHome();
    typeInto(quickBox(target), "买牛奶");

    click(buttonWith(target, "AI 速记"));
    await vi.waitFor(() =>
      expect(target.querySelector("#quick-ai-title")).not.toBeNull(),
    );

    click(buttonWith(target, "返回修改"));
    expect(mocks.createQuickNote).not.toHaveBeenCalled();
    expect(quickBox(target).value).toBe("买牛奶");
  });

  it("AI 未配置显示补齐提示", async () => {
    mocks.aiQuickNote.mockRejectedValue(
      new Error(
        "AI 还没配置好：缺 ai.base_url、ai.api_key。用 note config set 补上",
      ),
    );
    const target = renderHome();
    typeInto(quickBox(target), "买牛奶");

    click(buttonWith(target, "AI 速记"));
    await vi.waitFor(() => expect(target.textContent).toContain("还没配置 AI"));
  });

  it("正文为空时按钮禁用", () => {
    const target = renderHome();
    expect(buttonWith(target, "速记").disabled).toBe(true);
    expect(buttonWith(target, "AI 速记").disabled).toBe(true);
  });
});

describe("HomeView 查询结果", () => {
  it("查询命中显示标题 + 匹配行，不输出全文", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [
        {
          id: "n1",
          title: "会议纪要",
          encrypted: false,
          category_id: null,
          updated_at: "2026-11-16T12:00:00Z",
          match_lines: ["第二行有 会议 关键词", "第三行又是 会议"],
        },
        {
          id: "n2",
          title: "会议记录",
          encrypted: false,
          category_id: null,
          content: "正文无关",
          updated_at: "2026-11-16T12:00:00Z",
        },
        {
          id: "n3",
          title: "加密的",
          encrypted: true,
          category_id: null,
          updated_at: "2026-11-16T12:00:00Z",
        },
      ],
      total: 3,
      page: 1,
      per_page: 20,
    });
    const target = renderHome();
    typeInto(target.querySelector("#home-search") as HTMLInputElement, "会议");
    // 「查询」是 type=submit，合成 click 不会提交表单，直接派发 submit 事件
    const form = target.querySelector(".search-form") as HTMLFormElement;
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );

    await vi.waitFor(() =>
      expect(mocks.listNotes).toHaveBeenCalledWith({ q: "会议" }),
    );
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".search-hit-lines")).toHaveLength(2),
    );
    expect(target.textContent).toContain("第二行有 会议 关键词");
    expect(target.textContent).toContain("第三行又是 会议");
    // 标题命中、正文无关：回落显示摘要而不是匹配行
    expect(target.textContent).toContain("正文无关");
    // 加密笔记不展示任何正文
    expect(target.textContent).not.toContain("密文内容");
    // 检索词高亮：n1 标题 + 两行命中行各一次，n2 标题一次，共 4 个 <mark>
    const marks = Array.from(target.querySelectorAll("mark"));
    expect(marks).toHaveLength(4);
    for (const mark of marks) expect(mark.textContent).toBe("会议");
    // 高亮只出现在命中片段里，正文无关的内容不会被标
    expect(
      target.querySelector(".search-hit-lines")?.textContent,
    ).not.toContain("<mark>");
  });

  it("命中行先渲染成 Markdown，再在渲染结果里高亮命中词", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [
        {
          id: "n1",
          title: "会议纪要",
          encrypted: false,
          category_id: null,
          updated_at: "2026-11-16T12:00:00Z",
          match_lines: ["**会议**纪要", "- 列表项里有 会议"],
        },
      ],
      total: 1,
      page: 1,
      per_page: 20,
    });
    const target = renderHome();
    typeInto(target.querySelector("#home-search") as HTMLInputElement, "会议");
    const form = target.querySelector(".search-form") as HTMLFormElement;
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".search-hit-line")).toHaveLength(2),
    );
    const lines = Array.from(
      target.querySelectorAll<HTMLElement>(".search-hit-line"),
    );
    // 粗体语法渲染成 <strong>，命中词在渲染结果里包 <mark>
    expect(lines[0]?.querySelector("strong mark")?.textContent).toBe("会议");
    // 列表语法渲染成 <li>，命中词仍在
    expect(lines[1]?.textContent).toContain("会议");
    expect(lines[1]?.querySelector("mark")?.textContent).toBe("会议");
  });

  it("命中代码块/表格时整块渲染，不丢失结构", async () => {
    mocks.listNotes.mockResolvedValue({
      items: [
        {
          id: "n1",
          title: "会议纪要",
          encrypted: false,
          category_id: null,
          updated_at: "2026-11-16T12:00:00Z",
          match_lines: [
            '```\nprint("会议")\n```',
            "| 选项 | 说明 |\n| --- | --- |\n| 会议 | 周五 |",
          ],
        },
      ],
      total: 1,
      page: 1,
      per_page: 20,
    });
    const target = renderHome();
    typeInto(target.querySelector("#home-search") as HTMLInputElement, "会议");
    const form = target.querySelector(".search-form") as HTMLFormElement;
    form.dispatchEvent(
      new Event("submit", { bubbles: true, cancelable: true }),
    );
    await vi.waitFor(() =>
      expect(target.querySelectorAll(".search-hit-line")).toHaveLength(2),
    );
    const lines = Array.from(
      target.querySelectorAll<HTMLElement>(".search-hit-line"),
    );
    // 代码块整块渲染成 <pre><code>，命中词在渲染结果里高亮
    expect(lines[0]?.querySelector("pre code mark")?.textContent).toBe("会议");
    // 表格整块渲染成 <table>，含表头与数据行，命中词在单元格里高亮
    const table = lines[1]?.querySelector("table");
    expect(table?.textContent).toContain("选项");
    expect(table?.textContent).toContain("周五");
    expect(table?.querySelector("mark")?.textContent).toBe("会议");
  });

  it("空查询不发请求", async () => {
    const target = renderHome();
    const input = target.querySelector("#home-search") as HTMLInputElement;
    typeInto(input, "   ");
    click(buttonWith(target, "查询"));
    expect(mocks.listNotes).not.toHaveBeenCalled();
  });
});
