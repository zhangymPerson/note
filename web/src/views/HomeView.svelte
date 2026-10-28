<script lang="ts">
  import ActivityHeatmap from '../components/ActivityHeatmap.svelte'
  import { listNotes, type NoteListItem } from '../lib/notes'
  import { settings } from '../lib/settings'
  import { HEATMAP_WEEKS, groupEvents, loadStats, type Stats } from '../lib/stats'
  import { navigate } from '../router/router'

  let stats = $state<Stats | null>(null)
  let statsError = $state('')
  let query = $state('')
  let results = $state<NoteListItem[] | null>(null)
  let searching = $state(false)
  let searchError = $state('')

  const lookbackDays = HEATMAP_WEEKS * 7

  async function refreshStats() {
    try {
      stats = await loadStats(lookbackDays)
      statsError = ''
    } catch (error) {
      statsError = error instanceof Error ? error.message : '统计加载失败'
    }
  }

  $effect(() => {
    refreshStats()
  })

  async function search(event: SubmitEvent) {
    event.preventDefault()
    const q = query.trim()
    if (q === '' || searching) return
    searching = true
    searchError = ''
    try {
      const result = await listNotes({ q })
      results = result.items
      await refreshStats()
    } catch (error) {
      searchError = error instanceof Error ? error.message : '查询失败，请稍后重试'
    } finally {
      searching = false
    }
  }

  function openNote(id: string): void {
    navigate(`/notes/${encodeURIComponent(id)}`)
  }

  const today = new Date()
  const weekdayNames = ['日', '一', '二', '三', '四', '五', '六']
  const todayText = `${today.getFullYear()} 年 ${today.getMonth() + 1} 月 ${today.getDate()} 日 · 星期${weekdayNames[today.getDay()]}`
</script>

<section class="page home">
  <header class="home-head">
    <h1 class="greeting">你好，{$settings.name}。</h1>
    <p class="muted">{todayText}，这是你的使用账簿。</p>
  </header>

  <form class="search-form" onsubmit={search}>
    <label class="sr-only" for="home-search">查询笔记</label>
    <input
      id="home-search"
      class="search-input"
      type="search"
      placeholder="查询笔记标题或内容…"
      maxlength="200"
      bind:value={query}
    />
    <button class="button" type="submit" disabled={query.trim() === '' || searching}>
      {searching ? '查询中…' : '查询'}
    </button>
  </form>

  {#if searchError}
    <p class="search-error" role="alert">{searchError}</p>
  {:else if results !== null}
    {#if results.length === 0}
      <p class="search-empty">没有匹配的笔记。换个词试试。</p>
    {:else}
      <ul class="search-results">
        {#each results as hit}
          <li>
            <a href="/notes/{hit.id}" onclick={(e) => { e.preventDefault(); openNote(hit.id) }}>
              {hit.title}
            </a>
            {#if hit.encrypted}
              <span class="badge">已加密</span>
            {/if}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}

  <section class="heat-section" aria-label="使用频率">
    <h2>近一年使用频率</h2>
    <p class="muted">查询、查看与增删改都算一次使用，颜色越深代表当天越忙。</p>
    {#if stats}
      <ActivityHeatmap days={stats.days} />
    {:else if statsError}
      <p class="search-error" role="alert">{statsError}</p>
    {:else}
      <p class="muted">图表载入中…</p>
    {/if}
  </section>

  {#if stats}
    {@const groups = groupEvents(stats.events_by_kind)}
    <section class="ledger" aria-label="使用统计">
      <h2>使用统计</h2>
      <dl class="ledger-rows">
        <div class="ledger-row">
          <dt>累计事件</dt>
          <dd class="num">{stats.total_events}</dd>
        </div>
        <div class="ledger-row">
          <dt>活跃天数</dt>
          <dd class="num">{stats.active_days}</dd>
        </div>
      </dl>

      <h3>事件构成</h3>
      <ul class="event-groups">
        {#each groups as group (group.key)}
          <li>
            <span class="event-label">{group.label}</span>
            <span class="event-bar" aria-hidden="true">
              <span class="event-fill" style="width: {Math.round(group.share * 100)}%"></span>
            </span>
            <span class="num muted">{group.count}</span>
          </li>
        {/each}
      </ul>

      <h3>查询最多的内容</h3>
      {#if stats.top_queries.length === 0}
        <p class="muted">还没有查询记录。在上方搜索一次，这里就会开始累积。</p>
      {:else}
        <ol class="top-queries">
          {#each stats.top_queries as item}
            <li>
              <span class="top-query">{item.query}</span>
              <span class="num muted">{item.count} 次</span>
            </li>
          {/each}
        </ol>
      {/if}
    </section>
  {/if}
</section>
