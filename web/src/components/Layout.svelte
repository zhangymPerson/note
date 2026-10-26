<script lang="ts">
  import type { Snippet } from 'svelte'
  import { navigate } from '../router/router'
  import { settings } from '../lib/settings'

  let { children }: { children: Snippet } = $props()

  function link(event: MouseEvent, path: string): void {
    if (
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    ) {
      return
    }
    event.preventDefault()
    navigate(path)
  }
</script>

<div class="shell">
  <header class="topbar">
    <a class="brand" href="/" onclick={(event) => link(event, '/')}>笔记</a>
    <nav>
      <a href="/" onclick={(event) => link(event, '/')}>概览</a>
      <a href="/notes" onclick={(event) => link(event, '/notes')}>全部笔记</a>
      <a href="/notes/new" onclick={(event) => link(event, '/notes/new')}>新建</a>
    </nav>
    <span class="who" title="当前用户">{$settings.name}</span>
  </header>

  <main>
    {@render children()}
  </main>
</div>
