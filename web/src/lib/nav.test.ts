import { describe, expect, it } from "vitest";

import { safeNextPath } from "./nav";

describe("safeNextPath", () => {
  it("保留站内绝对路径", () => {
    expect(safeNextPath("/notes/new")).toBe("/notes/new");
    expect(safeNextPath("/notes/abc/edit")).toBe("/notes/abc/edit");
  });

  it("缺省值返回空串", () => {
    expect(safeNextPath(null)).toBe("");
    expect(safeNextPath("")).toBe("");
  });

  it("拒绝协议相对地址、外部地址与伪协议", () => {
    expect(safeNextPath("//evil.example")).toBe("");
    expect(safeNextPath("https://evil.example")).toBe("");
    expect(safeNextPath("javascript:alert(1)")).toBe("");
  });
});
