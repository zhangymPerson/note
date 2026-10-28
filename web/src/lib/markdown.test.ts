// @vitest-environment jsdom
import { describe, expect, it } from "vitest";

import { renderMarkdown } from "./markdown";

describe("renderMarkdown", () => {
  it("把 markdown 渲染成对应标签", () => {
    const html = renderMarkdown("# 标题\n\n这是**粗体**和*斜体*。");
    expect(html).toContain("<h1");
    expect(html).toContain("标题");
    expect(html).toContain("<strong>粗体</strong>");
    expect(html).toContain("<em>斜体</em>");
  });

  it("剥掉图片的 onerror 事件属性", () => {
    const html = renderMarkdown("<img src=x onerror=alert(1)>");
    expect(html).not.toContain("onerror");
    expect(html).not.toContain("alert");
  });

  it("剥掉 script 与 iframe 标签", () => {
    expect(renderMarkdown("<script>alert(1)</script>")).not.toContain(
      "<script",
    );
    expect(
      renderMarkdown('<iframe src="https://evil.example"></iframe>'),
    ).not.toContain("<iframe");
  });

  it("剥掉 javascript: 协议链接", () => {
    const html = renderMarkdown("[点我](javascript:alert(1))");
    expect(html).not.toContain("javascript:");
    expect(html).toContain("点我");
  });

  it("代码块内的 markdown 源码不被二次解析", () => {
    const html = renderMarkdown("```\n**不是粗体**\n```");
    expect(html).not.toContain("<strong>");
    expect(html).toContain("**不是粗体**");
  });

  it("所有链接统一带 noopener noreferrer", () => {
    expect(renderMarkdown("[站点](https://example.com)")).toContain("noopener");
    expect(renderMarkdown("[站点](https://example.com)")).toContain(
      "noreferrer",
    );
    const html = renderMarkdown(
      '<a href="https://example.com" target="_blank" rel="me">链接</a>',
    );
    expect(html).toContain('href="https://example.com"');
    expect(html).toContain('target="_blank"');
    expect(html).toContain("noopener");
    expect(html).toContain("me");
  });

  it("空输入渲染为空字符串", () => {
    expect(renderMarkdown("")).toBe("");
  });
});
