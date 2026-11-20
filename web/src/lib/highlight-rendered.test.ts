// @vitest-environment jsdom
import { describe, expect, it } from "vitest";

import { renderMarkdown } from "./markdown";
import { highlightRendered } from "./highlight";

describe("highlightRendered", () => {
  it("无查询词时原样返回", () => {
    const html = renderMarkdown("**会议**纪要");
    expect(highlightRendered(html, "")).toBe(html);
  });

  it("未命中时原样返回", () => {
    const html = renderMarkdown("正文无关");
    expect(highlightRendered(html, "xyz")).toBe(html);
  });

  it("在渲染后的标签里给命中词包上 <mark>", () => {
    const html = highlightRendered(renderMarkdown("**会议**纪要"), "会议");
    expect(html).toContain("<strong><mark>会议</mark></strong>");
    expect(html).toContain("纪要");
  });

  it("忽略 ASCII 大小写", () => {
    const html = highlightRendered(renderMarkdown("Alpha **beta**"), "alpha");
    expect(html).toContain("<mark>Alpha</mark>");
  });

  it("同一词多处出现每处都命中", () => {
    const html = highlightRendered(renderMarkdown("会议一、会议二"), "会议");
    expect(html.match(/<mark>/g)).toHaveLength(2);
  });

  it("代码块里的命中词也高亮，源码不被二次解析", () => {
    const html = highlightRendered(
      renderMarkdown("```\nfoo 会议 bar\n```"),
      "会议",
    );
    expect(html).toContain("<mark>会议</mark>");
    expect(html).not.toContain("<strong>");
  });
});
