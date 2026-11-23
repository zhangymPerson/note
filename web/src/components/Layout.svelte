<script lang="ts">
  import type { Snippet } from 'svelte'
  import { navigate, route } from '../router/router'
  import { settings } from '../lib/settings'
  import { hasKey, lockSession } from '../lib/session-key'
  import { toast, toasts } from '../lib/toast'
  import ThemeSwitch from './ThemeSwitch.svelte'

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

  function lock(): void {
    lockSession()
    toast('已锁定')
  }
</script>

<div class="shell">
  <header class="topbar">
    <a class="brand" href="/" aria-label="笔记" onclick={(event) => link(event, '/')}>
      <span class="brand-dot" aria-hidden="true"></span>
      <span>笔记</span>
    </a>
    <nav class="topnav" aria-label="主导航">
      <a href="/" data-active={$route.name === 'home'} onclick={(event) => link(event, '/')}>概览</a>
      <a
        href="/notes"
        data-active={['list', 'detail', 'edit'].includes($route.name)}
        onclick={(event) => link(event, '/notes')}
      >全部笔记</a
      >
      <a href="/notes/new" data-active={$route.name === 'new'} onclick={(event) => link(event, '/notes/new')}>新建</a>
      <a href="/trash" data-active={$route.name === 'trash'} onclick={(event) => link(event, '/trash')}>回收站</a>
    </nav>
    <div class="spacer" aria-hidden="true"></div>
    <a class="who" href="/info" title="用户与信息" onclick={(event) => link(event, '/info')}>
      {$settings.name}
    </a>
    {#if $hasKey}
      <button class="lock-btn" type="button" onclick={lock}>锁定</button>
    {/if}
    <ThemeSwitch />
  </header>

  <main>
    {@render children()}
  </main>

  {#if $toasts.length > 0}
    <div class="toasts" role="status" aria-live="polite">
      {#each $toasts as message, index (index)}
        <div class="toast">{message}</div>
      {/each}
    </div>
  {/if}
</div>
