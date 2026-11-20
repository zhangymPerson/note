<script lang="ts">
  import { renderMarkdown } from '../lib/markdown'
  import { highlightRendered } from '../lib/highlight'

  // query 非空时，在渲染（并净化）后的 HTML 里把命中词包上 <mark>，供搜索命中片段使用；
  // compact 压缩块级间距，供列表行内的短片段使用（大标题/代码块/图片都压小）。
  let { source, query = '', compact = false }: { source: string; query?: string; compact?: boolean } = $props()

  // 全仓唯一允许 {@html} 的位置：输入必须先经 renderMarkdown 净化；
  // highlightRendered 只往净化结果里加 <mark>，不引入新风险。
  const html = $derived(
    query === '' ? renderMarkdown(source) : highlightRendered(renderMarkdown(source), query),
  )
</script>

<div class="md" class:compact>{@html html}</div>

<style>
  .md {
    overflow-wrap: break-word;
  }

  .md :global(h1),
  .md :global(h2) {
    font-family: var(--font-display);
    font-weight: 600;
    line-height: 1.3;
    margin: 1.2em 0 0.5em;
    padding-bottom: 0.3em;
    border-bottom: 1px solid var(--border);
  }

  .md :global(h1) {
    font-size: 1.5rem;
  }

  .md :global(h2) {
    font-size: 1.22rem;
  }

  .md :global(h3),
  .md :global(h4),
  .md :global(h5),
  .md :global(h6) {
    font-family: var(--font-display);
    font-weight: 600;
    margin: 1.1em 0 0.4em;
  }

  .md :global(p),
  .md :global(ul),
  .md :global(ol),
  .md :global(blockquote),
  .md :global(table),
  .md :global(pre) {
    margin: 0 0 0.9em;
  }

  .md :global(ul),
  .md :global(ol) {
    padding-left: 1.5em;
  }

  .md :global(li) {
    margin: 0.2em 0;
  }

  .md :global(a) {
    color: var(--accent);
    text-decoration: none;
  }

  .md :global(a:hover) {
    text-decoration: underline;
  }

  .md :global(blockquote) {
    padding: 0 0.9em;
    color: var(--muted);
    border-left: 3px solid var(--border);
  }

  .md :global(code) {
    font-family: var(--font-mono);
    font-size: 0.92em;
    padding: 0.15em 0.35em;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 4px;
  }

  .md :global(pre) {
    padding: 0.8em 1em;
    overflow-x: auto;
    background: var(--surface);
    border: 1px solid var(--border);
    border-radius: 6px;
  }

  /* 保留代码块里的换行与缩进，这是编辑器预览最要紧的一条。 */
  .md :global(pre) {
    white-space: pre;
  }

  .md :global(pre code) {
    padding: 0;
    background: none;
    border: 0;
  }

  .md :global(table) {
    border-collapse: collapse;
    display: block;
    overflow-x: auto;
  }

  .md :global(th),
  .md :global(td) {
    padding: 0.3em 0.7em;
    border: 1px solid var(--border);
  }

  .md :global(img) {
    max-width: 100%;
  }

  .md :global(hr) {
    height: 1px;
    margin: 1.4em 0;
    background: var(--border);
    border: 0;
  }

  /* 搜索命中片段：compact 压平块级间距，避免列表行里出现大标题/大代码块/大图 */
  .md.compact :global(p),
  .md.compact :global(ul),
  .md.compact :global(ol),
  .md.compact :global(blockquote),
  .md.compact :global(table),
  .md.compact :global(pre) {
    margin: 0;
  }

  .md.compact :global(h1),
  .md.compact :global(h2),
  .md.compact :global(h3),
  .md.compact :global(h4),
  .md.compact :global(h5),
  .md.compact :global(h6) {
    font-family: inherit;
    font-weight: 600;
    font-size: 0.95rem;
    margin: 0;
    padding: 0;
    border: 0;
  }

  .md.compact :global(ul),
  .md.compact :global(ol) {
    padding-left: 1.2em;
  }

  .md.compact :global(pre) {
    padding: 0.3em 0.5em;
    max-height: 6em;
    overflow: auto;
  }

  .md.compact :global(img) {
    max-height: 2em;
    width: auto;
  }
</style>
