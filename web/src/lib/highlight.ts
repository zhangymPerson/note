// 检索词高亮：把一段文本切成「命中/非命中」片段，供 <mark> 渲染。
// 只做展示，不碰任何 HTML 拼接（{@html} 只允许出现在 Markdown.svelte 并先过 DOMPurify）。

export interface HighlightPart {
  text: string;
  hit: boolean;
}

// highlightParts 返回 text 被 query 切成的不重叠片段：每个出现处一段 hit=true，
// 其余为 hit=false。匹配忽略 ASCII 大小写（与服务端 matchLines 同口径）；
// query 为空或未命中时返回单段非命中。query 应为调用方 trim 后的词。
export function highlightParts(text: string, query: string): HighlightPart[] {
  if (query === "") return [{ text, hit: false }];
  const lowerText = text.toLocaleLowerCase();
  const needle = query.toLocaleLowerCase();
  const parts: HighlightPart[] = [];
  let cursor = 0;
  while (cursor < text.length) {
    const index = lowerText.indexOf(needle, cursor);
    if (index < 0) break;
    if (index > cursor) {
      parts.push({ text: text.slice(cursor, index), hit: false });
    }
    parts.push({ text: text.slice(index, index + query.length), hit: true });
    cursor = index + query.length;
  }
  if (cursor < text.length) {
    parts.push({ text: text.slice(cursor), hit: false });
  }
  if (parts.length === 0) {
    parts.push({ text, hit: false });
  }
  return parts;
}
