// Markdown 渲染管道：marked 解析 → DOMPurify 净化。
// 全仓唯一允许 {@html} 的地方是 Markdown.svelte，且它的输入必须来自本函数。
import { marked } from 'marked'
import DOMPurify from 'dompurify'

// 正文里出现的链接一律加 rel，绝不把 window.opener 交给外部站点。
DOMPurify.addHook('afterSanitizeAttributes', (node) => {
  if (node.nodeName !== 'A') return
  const rel = new Set((node.getAttribute('rel') ?? '').split(/\s+/).filter(Boolean))
  rel.add('noopener')
  rel.add('noreferrer')
  node.setAttribute('rel', [...rel].join(' '))
})

export function renderMarkdown(source: string): string {
  const html = marked.parse(source, { async: false, gfm: true })
  return DOMPurify.sanitize(html, { ADD_ATTR: ['target'] })
}
