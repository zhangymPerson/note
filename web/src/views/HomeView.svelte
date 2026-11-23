<script lang="ts">
  import ActivityHeatmap from '../components/ActivityHeatmap.svelte'
  import Highlight from '../components/Highlight.svelte'
  import Markdown from '../components/Markdown.svelte'
  import { aiQuickNote, type QuickNoteResult } from '../lib/ai'
  import { createQuickNote, listNotes, MAX_CONTENT_CODEPOINTS, type NoteListItem } from '../lib/notes'
  import { settings } from '../lib/settings'
  import { HEATMAP_WEEKS, groupEvents, loadStats, type Stats } from '../lib/stats'
  import { navigate } from '../router/router'
  import { catColorVar } from '../lib/catColor'

  let stats = $state<Stats | null>(null)
  let statsError = $state('')
  let query = $state('')
  let searchedQ = $state('') // 已提交执行的检索词，高亮用它而不是输入框实时值
  let results = $state<NoteListItem[] | null>(null)
  let searching = $state(false)
  let searchError = $state('')

  // 速记卡片：正文框 + 直接速记 / AI 速记（先预览确认）。
  let quickText = $state('')
  let quickBusy = $state(false)
  let quickMessage = $state('')
  let quickError = $state('')
  let aiBusy = $state(false)
  let aiError = $state('')
  let aiPreview = $state<QuickNoteResult | null>(null)
  let aiTitle = $state('')

  function fail(error: unknown): string {
    return error instanceof Error ? error.message : '操作失败，请稍后重试'
  }

  function aiNotConfigured(error: unknown): string {
    const message = fail(error)
    if (message.includes('还没配置 AI') || message.includes('ai.base_url')) {
      return '还没配置 AI，去信息页填写 ai.base_url / ai.api_key / ai.models 再试'
    }
    return message
  }

  // 直接速记：正文原样落库，标题交给服务端自动生成。
  async function saveQuick(): Promise<void> {
    const content = quickText.trim()
    if (content === '' || quickBusy) return
    quickBusy = true
    quickError = ''
    quickMessage = ''
    try {
      const note = await createQuickNote(content)
      quickText = ''
      quickMessage = `已记入速记「${note.title}」`
      await refreshStats()
    } catch (error) {
      quickError = fail(error)
    } finally {
      quickBusy = false
    }
  }

  // AI 速记：先预览确认，绝不绕过预览直接落库。
  async function runAIQuick(): Promise<void> {
    const content = quickText.trim()
    if (content === '' || aiBusy) return
    aiPreview = null
    aiError = ''
    aiBusy = true
    try {
      const result = await aiQuickNote(content)
      aiPreview = result
      aiTitle = result.title
    } catch (error) {
      aiError = aiNotConfigured(error)
    } finally {
      aiBusy = false
    }
  }

  // 保存预览：内容用预览锁定的 aiPreview.content，不读输入框当前值；
  // 标题可编辑（AI 生成的是建议）。
  async function saveAI(): Promise<void> {
    if (!aiPreview || quickBusy) return
    quickBusy = true
    quickError = ''
    quickMessage = ''
    try {
      const note = await createQuickNote(aiPreview.content, aiTitle.trim())
      quickText = ''
      aiPreview = null
      quickMessage = `已记入速记「${note.title}」`
      await refreshStats()
    } catch (error) {
      quickError = fail(error)
    } finally {
      quickBusy = false
    }
  }

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
    searchedQ = q
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

  <section class="card quick-card" aria-label="速记">
    <h2 class="quick-head">
      <span class="catdot" style="--c: var(--c-short)" aria-hidden="true"></span>
      速记
    </h2>
    <textarea
      class="quick-input"
      placeholder="记一条速记…（标题自动生成，或让 AI 帮你整理）"
      maxlength={MAX_CONTENT_CODEPOINTS}
      bind:value={quickText}
      disabled={quickBusy || aiBusy}
    ></textarea>
    <div class="quick-actions">
      <button class="button" type="button" disabled={quickBusy || aiBusy || quickText.trim() === ''} onclick={saveQuick}>
        {quickBusy ? '保存中…' : '速记'}
      </button>
      <button
        class="button button-quiet"
        type="button"
        disabled={aiBusy || quickBusy || quickText.trim() === ''}
        onclick={runAIQuick}
      >
        {aiBusy ? '整理中…' : 'AI 速记'}
      </button>
    </div>

    {#if aiPreview}
      <div class="quick-preview">
        <h3 class="quick-preview-head">AI 整理预览</h3>
        <label class="sr-only" for="quick-ai-title">AI 生成的标题（可修改）</label>
        <input
          id="quick-ai-title"
          class="quick-title-input"
          placeholder="AI 生成的标题（可修改）"
          maxlength="60"
          bind:value={aiTitle}
        />
        <Markdown source={aiPreview.content} />
        <p class="muted">模型：{aiPreview.model}</p>
        <div class="quick-actions">
          <button class="button" type="button" disabled={quickBusy || aiTitle.trim() === ''} onclick={saveAI}>
            {quickBusy ? '保存中…' : '保存'}
          </button>
          <button class="button button-quiet" type="button" disabled={quickBusy} onclick={() => (aiPreview = null)}>
            返回修改
          </button>
        </div>
      </div>
    {/if}

    {#if quickMessage}
      <p class="quick-message" role="status">{quickMessage}</p>
    {/if}
    {#if quickError}
      <p class="search-error" role="alert">{quickError}</p>
    {/if}
    {#if aiError}
      <p class="search-error" role="alert">{aiError}</p>
    {/if}
  </section>

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
              <span class="catdot sm" style="--c: {catColorVar(hit.category_id)}" aria-hidden="true"></span>
              <Highlight text={hit.title} query={searchedQ} />
            </a>
            {#if hit.encrypted}
              <span class="badge">已加密</span>
            {/if}
            {#if hit.match_lines && hit.match_lines.length > 0}
              <div class="search-hit-lines">
                {#each hit.match_lines as line}
                  <div class="search-hit-line">
                    <Markdown source={line} query={searchedQ} compact />
                  </div>
                {/each}
              </div>
            {:else if hit.content}
              <div class="search-hit-lines">
                <div class="search-hit-line">
                  <Markdown source={hit.content} query={searchedQ} compact />
                </div>
              </div>
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
