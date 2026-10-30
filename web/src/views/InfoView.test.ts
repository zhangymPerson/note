// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import { readable } from "svelte/store";

const mocks = vi.hoisted(() => ({
  getAIConfig: vi.fn(),
  saveAIConfig: vi.fn(),
  toast: vi.fn(),
}));

vi.mock("../lib/ai-config", async () => {
  const actual =
    await vi.importActual<typeof import("../lib/ai-config")>(
      "../lib/ai-config",
    );
  return {
    ...actual,
    getAIConfig: mocks.getAIConfig,
    saveAIConfig: mocks.saveAIConfig,
  };
});
vi.mock("../lib/api", () => ({
  ApiRequestError: class ApiRequestError extends Error {
    code: string;
    fields?: Record<string, string>;
    constructor(
      code: string,
      message: string,
      fields?: Record<string, string>,
    ) {
      super(message);
      this.code = code;
      this.fields = fields;
    }
  },
}));
vi.mock("../lib/crypto", () => ({
  KDF_PARAMS: {},
  decryptNoteContent: vi.fn(),
  deriveKey: vi.fn(),
  encryptNoteContent: vi.fn(),
}));
vi.mock("../lib/info", () => ({
  buildRows: () => [],
  getInfo: vi.fn(async () => ({})),
}));
vi.mock("../lib/notes", () => ({
  getMasterPassword: vi.fn(async () => ({
    configured: false,
    salt_b64: "",
    question: "",
  })),
  listEncryptedCiphers: vi.fn(async () => ({ notes: [] })),
  updateMasterQuestion: vi.fn(),
  updateNoteCipher: vi.fn(),
}));
vi.mock("../lib/reencrypt", () => ({
  countRemaining: vi.fn(),
  needsPreviousPassword: () => false,
  runMigration: vi.fn(),
  verifyOldPassword: vi.fn(),
}));
vi.mock("../lib/session-key", () => ({
  sessionKey: { set: vi.fn(), current: () => null },
}));
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
vi.mock("../lib/toast", () => ({ toast: mocks.toast }));

const { default: InfoView } = await import("./InfoView.svelte");

const CONFIGURED = {
  base_url: "https://api.openai.com/v1",
  prompt: "整理成笔记",
  api_key_set: true,
  api_key_masked: "sk-…abcd",
  path: "/home/u/.config/note/note.yaml",
};

let app: Record<string, unknown> | null = null;
let target: HTMLDivElement | null = null;

async function render(waitFor = "#ai-base-url"): Promise<HTMLDivElement> {
  target = document.createElement("div");
  document.body.append(target);
  app = mount(InfoView, { target }) as Record<string, unknown>;
  flushSync();

  const element = target;
  await vi.waitFor(() => expect(element.querySelector(waitFor)).not.toBeNull());
  return element;
}

function typeInto(
  node: HTMLInputElement | HTMLTextAreaElement,
  value: string,
): void {
  node.value = value;
  node.dispatchEvent(new Event("input", { bubbles: true }));
  flushSync();
}

function submit(node: Element): void {
  node
    .closest("form")!
    .dispatchEvent(new Event("submit", { bubbles: true, cancelable: true }));
  flushSync();
}

beforeEach(() => {
  mocks.getAIConfig.mockResolvedValue({ ...CONFIGURED });
  mocks.saveAIConfig.mockImplementation(async (payload: unknown) => ({
    ...CONFIGURED,
    ...(payload as Record<string, unknown>),
    api_key_set: true,
    api_key_masked: "sk-…new1",
  }));
});

afterEach(() => {
  if (app !== null && target !== null) {
    unmount(app);
    target.remove();
  }
  app = null;
  target = null;
  vi.clearAllMocks();
});

describe("InfoView AI 配置卡片", () => {
  it("展示地址、提示词与密钥掩码，并给出配置文件路径", async () => {
    const element = await render();

    expect(
      (element.querySelector("#ai-base-url") as HTMLInputElement).value,
    ).toBe(CONFIGURED.base_url);
    expect(
      (element.querySelector("#ai-prompt") as HTMLTextAreaElement).value,
    ).toBe(CONFIGURED.prompt);
    expect(element.textContent).toContain("sk-…abcd");
    expect(element.textContent).toContain("留空表示不改动");
    expect(element.textContent).toContain(CONFIGURED.path);
    // 明文密钥不会出现在页面上（服务端也不回传）
    expect(element.innerHTML).not.toContain("api_key_masked");
  });

  it("密钥框留空时保存不带 api_key（沿用已有密钥）", async () => {
    const element = await render();

    typeInto(
      element.querySelector("#ai-base-url") as HTMLInputElement,
      "https://proxy.example.com/v1",
    );
    submit(element.querySelector("#ai-base-url")!);
    await vi.waitFor(() => expect(mocks.saveAIConfig).toHaveBeenCalled());

    expect(mocks.saveAIConfig).toHaveBeenCalledWith({
      base_url: "https://proxy.example.com/v1",
      prompt: CONFIGURED.prompt,
    });
  });

  it("输入新密钥后保存会带上它，并清空输入框", async () => {
    const element = await render();

    typeInto(
      element.querySelector("#ai-api-key") as HTMLInputElement,
      "sk-new",
    );
    submit(element.querySelector("#ai-api-key")!);
    await vi.waitFor(() => expect(mocks.saveAIConfig).toHaveBeenCalled());

    expect(mocks.saveAIConfig).toHaveBeenCalledWith(
      expect.objectContaining({ api_key: "sk-new" }),
    );
    await vi.waitFor(() =>
      expect(
        (element.querySelector("#ai-api-key") as HTMLInputElement).value,
      ).toBe(""),
    );
    expect(mocks.toast).toHaveBeenCalledWith("AI 配置已保存");
  });

  it("点「清除密钥」后保存发空串", async () => {
    const element = await render();

    const clear = Array.from(element.querySelectorAll("button")).find(
      (button) => button.textContent?.includes("清除密钥"),
    );
    expect(clear).toBeDefined();
    clear!.dispatchEvent(new Event("click", { bubbles: true }));
    flushSync();
    expect(element.textContent).toContain("保存后将清除密钥");

    submit(element.querySelector("#ai-api-key")!);
    await vi.waitFor(() => expect(mocks.saveAIConfig).toHaveBeenCalled());
    expect(mocks.saveAIConfig).toHaveBeenCalledWith(
      expect.objectContaining({ api_key: "" }),
    );
  });

  it("字段级错误标在对应输入框下面", async () => {
    const { ApiRequestError } = (await import("../lib/api")) as unknown as {
      ApiRequestError: new (
        code: string,
        message: string,
        fields?: Record<string, string>,
      ) => Error;
    };
    mocks.saveAIConfig.mockRejectedValueOnce(
      new ApiRequestError("validation_failed", "请修正输入内容", {
        base_url: "这里填的地址不合法",
      }),
    );

    const element = await render();
    submit(element.querySelector("#ai-base-url")!);

    await vi.waitFor(() =>
      expect(element.querySelector(".field-error")?.textContent).toContain(
        "这里填的地址不合法",
      ),
    );
  });

  it("读取失败只在卡片里报错，不影响页面其余部分", async () => {
    mocks.getAIConfig.mockRejectedValueOnce(new Error("配置文件读取失败"));
    const element = await render(".form-error");

    expect(element.querySelector(".form-error")?.textContent).toContain(
      "配置文件读取失败",
    );
    // 其它卡片照常渲染
    expect(element.querySelector("#info-name")).not.toBeNull();
  });
});
