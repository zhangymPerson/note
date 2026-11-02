// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import { readable } from "svelte/store";

const mocks = vi.hoisted(() => ({
  getAIConfig: vi.fn(),
  optimizeNote: vi.fn(),
  listCategories: vi.fn(),
  getMasterPassword: vi.fn(),
  getNote: vi.fn(),
  createNote: vi.fn(),
  updateNote: vi.fn(),
  getTemplate: vi.fn(),
  navigate: vi.fn(),
}));

vi.mock("../lib/ai-config", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/ai-config")>(
      "../lib/ai-config",
    );
  return { ...actual, getAIConfig: mocks.getAIConfig };
});
vi.mock("../lib/ai", async () => {
  const actual = await vi.importActual<typeof import("../lib/ai")>("../lib/ai");
  return { ...actual, optimizeNote: mocks.optimizeNote };
});
// 保留真实的码点计数与上限：优化结果的超限判断必须与保存时同一口径。
vi.mock("../lib/notes", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/notes")>("../lib/notes");
  return {
    ...actual,
    listCategories: mocks.listCategories,
    getMasterPassword: mocks.getMasterPassword,
    getNote: mocks.getNote,
    createNote: mocks.createNote,
    updateNote: mocks.updateNote,
  };
});
vi.mock("../lib/crypto", () => ({
  KDF_PARAMS: {},
  decryptNoteContent: vi.fn(),
  deriveKey: vi.fn(),
  encryptNoteContent: vi.fn(),
  paramsFromNote: vi.fn(() => ({})),
}));
vi.mock("../lib/template", () => ({ getTemplate: mocks.getTemplate }));
vi.mock("../lib/draft", () => ({
  editorDraft: { take: () => null, save: vi.fn(), clear: vi.fn() },
}));
vi.mock("../lib/session-key", () => ({
  hasKey: readable(false),
  passwordMismatch: () => "",
  sessionKey: { current: () => null, set: vi.fn() },
  unlockWithPassword: vi.fn(),
}));
vi.mock("../router/router", () => ({ navigate: mocks.navigate }));

const { default: NoteEditorView } = await import("./NoteEditorView.svelte");

const AI_ON = {
  base_url: "https://api.example.com/v1",
  models: ["m1", "m2"],
  prompt: "",
  default_prompt: "整理：{{content}}",
  prompt_variable: "{{content}}",
  api_key_set: true,
  api_key_masked: "sk-…abcd",
  path: "/tmp/note.yaml",
};

let app: ReturnType<typeof mount> | null = null;

async function renderEditor(): Promise<HTMLDivElement> {
  const target = document.createElement("div");
  document.body.append(target);
  app = mount(NoteEditorView, { target, props: { id: "" } });
  flushSync();
  await vi.waitFor(() =>
    expect(target.querySelector("#note-content")).not.toBeNull(),
  );
  return target;
}

function typeInto(
  node: HTMLInputElement | HTMLTextAreaElement,
  value: string,
): void {
  node.value = value;
  node.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
}

function aiButton(element: HTMLElement): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll("button")).find(
    (candidate) =>
      candidate.textContent?.includes("用 AI 优化格式") ||
      candidate.textContent?.includes("优化中"),
  );
  if (!button) throw new Error("找不到 AI 优化按钮");
  return button as HTMLButtonElement;
}

function contentBox(element: HTMLElement): HTMLTextAreaElement {
  return element.querySelector("#note-content") as HTMLTextAreaElement;
}

function buttonWith(element: HTMLElement, label: string): HTMLButtonElement {
  const button = Array.from(element.querySelectorAll("button")).find(
    (candidate) => candidate.textContent?.includes(label),
  );
  if (!button) throw new Error("找不到按钮: " + label);
  return button as HTMLButtonElement;
}

function submit(form: HTMLFormElement): void {
  form.dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  flushSync();
}

function click(node: Element): void {
  node.dispatchEvent(new Event("click", { bubbles: true }));
  flushSync();
}

beforeEach(() => {
  mocks.getAIConfig.mockResolvedValue({ ...AI_ON });
  mocks.listCategories.mockResolvedValue([]);
  mocks.getMasterPassword.mockResolvedValue({
    configured: true,
    salt_b64: "salt",
    question: "",
  });
  mocks.getTemplate.mockResolvedValue({ content: "" });
  mocks.optimizeNote.mockResolvedValue({ content: "# 优化后", model: "m1" });
});

afterEach(() => {
  if (app !== null) {
    unmount(app);
    app = null;
  }
  document.body.innerHTML = "";
  vi.clearAllMocks();
});

describe("NoteEditorView 的 AI 优化", () => {
  it("未配置 AI 时按钮禁用并指向信息页", async () => {
    mocks.getAIConfig.mockResolvedValue({ ...AI_ON, api_key_set: false });
    const element = await renderEditor();

    expect(aiButton(element).disabled).toBe(true);
    expect(element.querySelector('a[href="/info"]')).not.toBeNull();
  });

  it("优化成功后预览出现，「替换正文」写入正文", async () => {
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");

    // 等配置真的读进来：jsdom 里 disabled 按钮的 click 监听仍会被触发
    await vi.waitFor(() =>
      expect(element.textContent).toContain("正文会发送到"),
    );
    click(aiButton(element));
    // 预览走 Markdown 渲染，所以看到的是标题而不是字面的 "# 优化后"
    await vi.waitFor(() =>
      expect(element.querySelector(".ai-result h1")?.textContent).toBe(
        "优化后",
      ),
    );
    expect(element.textContent).toContain("AI 优化结果 · m1");
    expect(mocks.optimizeNote).toHaveBeenCalledWith("原始正文", "m1");

    click(buttonWith(element, "替换正文"));
    expect(contentBox(element).value).toBe("# 优化后");
  });

  it("优化期间正文被改动时禁止替换", async () => {
    let resolve!: (value: unknown) => void;
    mocks.optimizeNote.mockImplementation(
      () =>
        new Promise((done) => {
          resolve = done;
        }),
    );
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");
    click(aiButton(element));

    typeInto(contentBox(element), "原始正文改过了");
    resolve({ content: "# 优化后", model: "m1" });
    await vi.waitFor(() => expect(element.textContent).toContain("请重新优化"));
    expect(buttonWith(element, "替换正文").disabled).toBe(true);
  });

  it("上游超时：错误原文 + 指向信息页的可操作提示", async () => {
    const { ApiRequestError } =
      await vi.importActual<typeof import("../lib/api")>("../lib/api");
    mocks.optimizeNote.mockRejectedValue(
      new ApiRequestError(
        "ai_timeout",
        "模型 m1 在 60 秒内没有响应，可以稍后重试、换一个模型，或检查接口地址是否可用",
      ),
    );
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");
    await vi.waitFor(() =>
      expect(element.textContent).toContain("正文会发送到"),
    );
    click(aiButton(element));

    await vi.waitFor(() =>
      expect(element.querySelector(".ai-result .form-error")).not.toBeNull(),
    );
    const alert = element.querySelector(".ai-result") as HTMLElement;
    expect(alert.textContent).toContain("60 秒");
    // 只说「失败了」没用：要给下一步——重试，或换模型（模型配置在信息页）
    expect(alert.textContent).toContain("换一个更快的模型");
    expect(alert.querySelector('a[href="/info"]')).not.toBeNull();
    expect(buttonWith(element, "重新优化")).toBeTruthy();
    // 失败不许动正文
    expect(contentBox(element).value).toBe("原始正文");
  });

  it("结果超 2000 码点时禁止替换", async () => {
    mocks.optimizeNote.mockResolvedValue({
      content: "😀".repeat(2001),
      model: "m1",
    });
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");
    click(aiButton(element));

    await vi.waitFor(() => expect(element.textContent).toContain("2001"));
    expect(buttonWith(element, "替换正文").disabled).toBe(true);
  });

  it("优化失败时显示错误且正文不变", async () => {
    mocks.optimizeNote.mockRejectedValue(new Error("AI 接口返回 500：boom"));
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");
    click(aiButton(element));

    await vi.waitFor(() => expect(element.textContent).toContain("boom"));
    expect(contentBox(element).value).toBe("原始正文");
    expect(element.textContent).toContain("重新优化");
  });

  it("只有一个模型时不渲染模型下拉", async () => {
    mocks.getAIConfig.mockResolvedValue({ ...AI_ON, models: ["m1"] });
    const element = await renderEditor();

    expect(element.querySelector("#ai-model")).toBeNull();
  });

  it("多个模型时下拉可切换，并用选中的模型重发", async () => {
    const element = await renderEditor();
    typeInto(contentBox(element), "原始正文");

    await vi.waitFor(() =>
      expect(element.querySelector("#ai-model")).not.toBeNull(),
    );
    const select = element.querySelector("#ai-model") as HTMLSelectElement;
    expect(select.value).toBe("m1");
    select.value = "m2";
    select.dispatchEvent(new Event("change", { bubbles: true }));
    flushSync();

    click(aiButton(element));
    await vi.waitFor(() =>
      expect(mocks.optimizeNote).toHaveBeenCalledWith("原始正文", "m2"),
    );
  });

  it("勾选加密后提示正文会以明文发送", async () => {
    const element = await renderEditor();

    const encrypt = element.querySelector(
      ".encrypt-field input[type=checkbox]",
    ) as HTMLInputElement;
    encrypt.checked = true;
    encrypt.dispatchEvent(new Event("change", { bubbles: true }));
    flushSync();

    await vi.waitFor(() => expect(element.textContent).toContain("以明文发送"));
    expect(element.textContent).toContain("api.example.com");
  });
});

describe("NoteEditorView 的标题冲突", () => {
  it("同名 409 把文案挂到标题字段上", async () => {
    const { ApiRequestError } = await import("../lib/api");
    mocks.createNote.mockRejectedValue(
      new ApiRequestError("title_conflict", "该分类下已有同名笔记", {
        title: "该分类下已有同名笔记",
      }),
    );
    const element = await renderEditor();

    typeInto(element.querySelector("#note-title") as HTMLInputElement, "周报");
    typeInto(contentBox(element), "正文");
    submit(element.querySelector(".editor-form") as HTMLFormElement);

    await vi.waitFor(() => {
      expect(mocks.createNote).toHaveBeenCalledTimes(1);
    });
    await vi.waitFor(() => {
      expect(element.querySelector(".field-error")?.textContent).toContain(
        "该分类下已有同名笔记",
      );
    });
  });
});
