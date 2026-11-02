// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { flushSync, mount, unmount } from "svelte";
import ThemeSwitch from "./ThemeSwitch.svelte";
import { setTheme } from "../lib/theme";

let app: ReturnType<typeof mount> | null = null;

function render() {
  const target = document.createElement("div");
  document.body.append(target);
  app = mount(ThemeSwitch, { target });
  flushSync();
  const button = target.querySelector("button");
  if (!button) {
    throw new Error("ThemeSwitch 应当只渲染一个按钮");
  }
  return { target, button };
}

function click(button: HTMLButtonElement) {
  button.dispatchEvent(new MouseEvent("click", { bubbles: true }));
  flushSync();
}

beforeEach(() => {
  setTheme("light");
});

afterEach(() => {
  if (app) {
    unmount(app);
    app = null;
  }
  document.body.innerHTML = "";
});

describe("ThemeSwitch", () => {
  it("三种主题合成一个按钮，不再并排占三格", () => {
    const { target } = render();
    expect(target.querySelectorAll("button")).toHaveLength(1);
    expect(target.textContent).not.toContain("暗色");
  });

  it("点击在 亮色 → 暗色 → 护眼 之间循环，并落到 html[data-theme]", () => {
    const { target, button } = render();
    expect(button.getAttribute("aria-label")).toContain("亮色");
    expect(button.getAttribute("aria-label")).toContain("暗色");
    expect(document.documentElement.dataset.theme).toBe("light");

    click(button);
    expect(document.documentElement.dataset.theme).toBe("dark");
    expect(target.textContent).toContain("暗色");
    expect(localStorage.getItem("note.theme")).toBe("dark");

    click(button);
    expect(document.documentElement.dataset.theme).toBe("eye-care");
    expect(target.textContent).toContain("护眼");

    // 绕回起点：三个状态之后回到亮色，不留死循环。
    click(button);
    expect(document.documentElement.dataset.theme).toBe("light");
    expect(target.textContent).toContain("亮色");
  });

  it("当前主题在 aria-label 与 title 里都写明下一步是什么", () => {
    const { button } = render();
    expect(button.getAttribute("title")).toBe("配色：亮色（点击切换到暗色）");
  });
});
