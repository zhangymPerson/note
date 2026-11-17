// @vitest-environment node
import { describe, expect, it } from "vitest";
import { highlightParts } from "./highlight";

describe("highlightParts", () => {
  it("无查询词时原样返回单段非命中", () => {
    expect(highlightParts("hello 世界", "")).toEqual([
      { text: "hello 世界", hit: false },
    ]);
  });

  it("无命中时返回单段非命中", () => {
    expect(highlightParts("hello 世界", "xyz")).toEqual([
      { text: "hello 世界", hit: false },
    ]);
  });

  it("命中处切成三段，命中段单独标出", () => {
    expect(highlightParts("正文里有会议关键词", "会议")).toEqual([
      { text: "正文里有", hit: false },
      { text: "会议", hit: true },
      { text: "关键词", hit: false },
    ]);
  });

  it("忽略 ASCII 大小写", () => {
    const parts = highlightParts("Alpha beta Gamma", "alpha");
    expect(parts).toEqual([
      { text: "Alpha", hit: true },
      { text: " beta Gamma", hit: false },
    ]);
  });

  it("同一词出现多次时每处都命中", () => {
    const parts = highlightParts("会议一、会议二", "会议");
    expect(parts.filter((part) => part.hit)).toEqual([
      { text: "会议", hit: true },
      { text: "会议", hit: true },
    ]);
    expect(parts[0]).toEqual({ text: "会议", hit: true });
    expect(parts[1]).toEqual({ text: "一、", hit: false });
    expect(parts[2]).toEqual({ text: "会议", hit: true });
    expect(parts[3]).toEqual({ text: "二", hit: false });
  });

  it("查询词两端空白由调用方负责，这里按原样匹配", () => {
    // 组件只对 trim 后的词做高亮，工具本身不 trim
    expect(highlightParts("abc", "a b")).toEqual([{ text: "abc", hit: false }]);
  });
});
