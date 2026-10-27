<script lang="ts">
  import { renderMarkdown } from '../lib/markdown'

  let { source }: { source: string } = $props()

  // 全仓唯一允许 {@html} 的位置：输入必须先经 renderMarkdown 净化。
  const html = $derived(renderMarkdown(source))
</script>

<div class="md">{@html html}</div>

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
</style>
