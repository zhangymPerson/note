<script lang="ts">
  import { nextTheme, setTheme, theme, themeLabel } from '../lib/theme'

  const current = $derived(themeLabel($theme))
  const upcoming = $derived(themeLabel(nextTheme($theme)))
</script>

<button
  type="button"
  class="theme-cycle"
  title="配色：{current}（点击切换到{upcoming}）"
  aria-label="配色主题：{current}，点击切换到{upcoming}"
  onclick={() => setTheme(nextTheme($theme))}
>
  <svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round">
    {#if $theme === 'light'}
      <circle cx="8" cy="8" r="3.1" />
      <path d="M8 1.2v1.6M8 13.2v1.6M1.2 8h1.6M13.2 8h1.6M3.2 3.2l1.1 1.1M11.7 11.7l1.1 1.1M12.8 3.2l-1.1 1.1M4.3 11.7l-1.1 1.1" />
    {:else if $theme === 'dark'}
      <path d="M13 9.6A5.6 5.6 0 0 1 6.4 3a5.6 5.6 0 1 0 6.6 6.6Z" />
    {:else}
      <path d="M13.4 2.6c-6 0-9.1 2.6-9.1 6.6 0 1.2.3 2.2.9 3.1 3.7.5 8.2-2 8.2-9.7Z" />
      <path d="M4.8 12.8c1.6-2.3 3.7-4 6.1-5.3" />
    {/if}
  </svg>
  <span class="theme-cycle-label">{current}</span>
</button>
