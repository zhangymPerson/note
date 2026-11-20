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

// highlightRendered 在已经过 DOMPurify 净化的 HTML 里，把查询词出现的文本节点包上
// <mark>（忽略 ASCII 大小写、每处都命中），供「渲染成 Markdown 后再高亮」的搜索命中
// 片段使用。只接受 renderMarkdown 的输出；新增的只有 <mark> 元素，内容取自净化后的
// 文本节点，不引入任何新标签或属性，{@html} 约束不变。query 为空或未命中时原样返回。
export function highlightRendered(html: string, query: string): string {
  const needle = query.trim().toLocaleLowerCase();
  if (needle === "") return html;
  const doc = new DOMParser().parseFromString(html, "text/html");
  const walker = doc.createTreeWalker(doc.body, NodeFilter.SHOW_TEXT);
  const targets: Text[] = [];
  while (walker.nextNode()) {
    const node = walker.currentNode as Text;
    if (node.data.toLocaleLowerCase().includes(needle)) targets.push(node);
  }
  if (targets.length === 0) return html;
  for (const node of targets) {
    const lower = node.data.toLocaleLowerCase();
    const frag = doc.createDocumentFragment();
    let cursor = 0;
    while (cursor < node.data.length) {
      const index = lower.indexOf(needle, cursor);
      if (index < 0) {
        frag.append(doc.createTextNode(node.data.slice(cursor)));
        break;
      }
      if (index > cursor) {
        frag.append(doc.createTextNode(node.data.slice(cursor, index)));
      }
      const mark = doc.createElement("mark");
      mark.textContent = node.data.slice(index, index + needle.length);
      frag.append(mark);
      cursor = index + needle.length;
    }
    node.replaceWith(frag);
  }
  return doc.body.innerHTML;
}
