import { describe, expect, it } from "vitest";
import { catColorVar } from "./catColor";

describe("catColorVar", () => {
  it("未分类(1) 与空值用灰色", () => {
    expect(catColorVar(1)).toBe("var(--c-uncat)");
    expect(catColorVar(null)).toBe("var(--c-uncat)");
    expect(catColorVar(undefined)).toBe("var(--c-uncat)");
    expect(catColorVar(0)).toBe("var(--c-uncat)");
  });

  it("速记(2) 固定墨绿", () => {
    expect(catColorVar(2)).toBe("var(--c-short)");
  });

  it("用户分类按 (id-3) % 4 循环其余四色", () => {
    expect(catColorVar(3)).toBe("var(--c-work)");
    expect(catColorVar(4)).toBe("var(--c-life)");
    expect(catColorVar(5)).toBe("var(--c-read)");
    expect(catColorVar(6)).toBe("var(--c-more)");
    expect(catColorVar(7)).toBe("var(--c-work)");
    expect(catColorVar(10)).toBe("var(--c-more)");
  });
});
