import { describe, expect, it } from "vitest";
import { resolveTheme } from "./theme";

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
