import { describe, expect, it } from "vitest";
import { nextTheme, resolveTheme, themeLabel } from "./theme";

describe("resolveTheme", () => {
  it("使用已保存且合法的显式主题", () => {
    expect(resolveTheme("light", true)).toBe("light");
    expect(resolveTheme("dark", false)).toBe("dark");
    expect(resolveTheme("eye-care", false)).toBe("eye-care");
  });

  it("未保存时跟随系统偏好", () => {
    expect(resolveTheme(null, true)).toBe("dark");
    expect(resolveTheme(null, false)).toBe("light");
  });

  it("忽略非法的保存值，回退到系统偏好", () => {
    expect(resolveTheme("neon", true)).toBe("dark");
    expect(resolveTheme("", false)).toBe("light");
  });
});

describe("nextTheme", () => {
  it("按 亮色 → 暗色 → 护眼 循环，绕回起点", () => {
    expect(nextTheme("light")).toBe("dark");
    expect(nextTheme("dark")).toBe("eye-care");
    expect(nextTheme("eye-care")).toBe("light");
  });
});

describe("themeLabel", () => {
  it("给出中文名，供单按钮的 title 与 aria-label 使用", () => {
    expect(themeLabel("light")).toBe("亮色");
    expect(themeLabel("dark")).toBe("暗色");
    expect(themeLabel("eye-care")).toBe("护眼");
  });
});
