<script lang="ts">
  import {
    deleteNote,
    listCategories,
    listNotes,
    type Category,
    type NoteListItem,
  } from '../lib/notes'
  import { navigate } from '../router/router'

  function paramsFromUrl(): URLSearchParams {
    return new URLSearchParams(typeof location === 'undefined' ? '' : location.search)
  }

  const initial = paramsFromUrl()

  let query = $state(initial.get('q') ?? '')
  let committedQ = $state(initial.get('q') ?? '')
  let category = $state(Number(initial.get('category') ?? 0) || 0)
  let page = $state(Number(initial.get('page') ?? 1) || 1)

  let rows = $state<NoteListItem[]>([])
  let total = $state(0)
  let perPage = $state(20)
  let categories = $state<Category[]>([])
  let loading = $state(true)
  let error = $state('')
  let pendingId = $state('')

  const pageCount = $derived(Math.max(1, Math.ceil(total / perPage)))
  const activeCategory = $derived(categories.find((cat) => cat.id === category) ?? null)

  async function refresh(wantedQ: string, wantedCategory: number, wantedPage: number): Promise<void> {
    loading = true
    try {
      const result = await listNotes({ q: wantedQ, category: wantedCategory, page: wantedPage })
      rows = result.items
      total = result.total
      perPage = result.per_page
      error = ''
      // 删除后当前页可能已超出范围，回落并按新页码重新拉取（effect 会因 page 变化再跑一次）
      const last = Math.max(1, Math.ceil(total / perPage))
      if (wantedPage > last) page = last
    } catch (cause) {
      error = cause instanceof Error ? cause.message : '读取笔记失败，请稍后重试'
    } finally {
      loading = false
    }
    syncUrl()
  }

  function syncUrl(): void {
    const params = new URLSearchParams()
    if (committedQ !== '') params.set('q', committedQ)
    if (category !== 0) params.set('category', String(category))
    if (page > 1) params.set('page', String(page))
    const text = params.toString()
    history.replaceState(null, '', text ? `/notes?${text}` : '/notes')
  }

  // 筛选条件变了就重新拉取；page 由 chips/搜索提交时归 1。
  $effect(() => {
    refresh(committedQ, category, page)
  })

  $effect(() => {
    listCategories()
      .then((cats) => {
        categories = cats
      })
      .catch(() => {
        // 分类条读不到时列表本身仍可用，静默降级
      })
  })

  function submitSearch(event: SubmitEvent): void {
    event.preventDefault()
    page = 1
    committedQ = query.trim()
  }

  function pickCategory(id: number): void {
    page = 1
    category = id
  }

  function step(delta: number): void {
    page = Math.min(pageCount, Math.max(1, page + delta))
  }

  function open(note: NoteListItem): void {
    navigate(`/notes/${encodeURIComponent(note.id)}`)
  }

  async function remove(note: NoteListItem): Promise<void> {
    if (!window.confirm(`永久删除「${note.title}」？删除后无法恢复。`)) return
    pendingId = note.id
    try {
      await deleteNote(note.id)
      await refresh(committedQ, category, page)
      categories = await listCategories()
    } catch (cause) {
      error = cause instanceof Error ? cause.message : '删除失败，请稍后重试'
    } finally {
      pendingId = ''
    }
  }

  // 后端存的是 UTC 秒级文本，这里按本机时区显示到分钟。
  function shownAt(value: string): string {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    const pad = (input: number): string => String(input).padStart(2, '0')
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
  }
</script>

<section class="page list">
  <header class="list-head">
    <h1>全部笔记</h1>
    <a class="button" href="/notes/new" onclick={(event) => { event.preventDefault(); navigate('/notes/new') }}>
      新建笔记
    </a>
  </header>

  <form class="search-form" onsubmit={submitSearch}>
    <label class="sr-only" for="list-search">搜索笔记</label>
    <input
      id="list-search"
      class="search-input"
      type="search"
      placeholder="按标题或正文搜索…"
      bind:value={query}
    />
    <button class="button" type="submit" disabled={loading}>搜索</button>
  </form>

  <div class="chips" role="group" aria-label="按分类筛选">
    <button class="chip" type="button" data-active={category === 0} onclick={() => pickCategory(0)}>
      全部 <span class="chip-num">{categories.reduce((sum, cat) => sum + cat.note_count, 0)}</span>
    </button>
    {#each categories as cat (cat.id)}
      <button class="chip" type="button" data-active={category === cat.id} onclick={() => pickCategory(cat.id)}>
        {cat.name} <span class="chip-num">{cat.note_count}</span>
      </button>
    {/each}
  </div>

  {#if error}
    <p class="form-error" role="alert">{error}</p>
  {/if}

  {#if loading}
    <p class="muted">笔记载入中…</p>
  {:else if rows.length === 0}
    <p class="empty-hint">
      {#if committedQ !== '' || category !== 0}
        没有符合条件的笔记。换个词，或回到「全部」。
      {:else}
        还没有笔记。点击右上角「新建笔记」写下第一条。
      {/if}
    </p>
  {:else}
    <ul class="note-rows">
      {#each rows as note (note.id)}
        <li class="note-row">
          <button class="note-main" type="button" onclick={() => open(note)}>
            <span class="note-title">
              {note.title}
              {#if note.encrypted}
                <span class="badge">已加密</span>
              {/if}
            </span>
            {#if note.content}
              <span class="note-summary">{note.content}</span>
            {:else if note.encrypted}
              <span class="note-summary muted">密文内容不在列表展示。</span>
            {/if}
            <span class="note-meta">
              {shownAt(note.updated_at)}
              {#if note.category_name}
                · {note.category_name}
              {/if}
            </span>
          </button>
          <button
            class="button button-quiet"
            type="button"
            disabled={pendingId === note.id}
            onclick={() => remove(note)}
          >
            {pendingId === note.id ? '删除中…' : '删除'}
          </button>
        </li>
      {/each}
    </ul>

    <nav class="pager" aria-label="分页">
      <button class="button button-quiet" type="button" disabled={page <= 1} onclick={() => step(-1)}>
        上一页
      </button>
      <span class="pager-state">
        第 {page} / {pageCount} 页 · 共 {total} 条
        {#if activeCategory}· 分类「{activeCategory.name}」{/if}
      </span>
      <button class="button button-quiet" type="button" disabled={page >= pageCount} onclick={() => step(1)}>
        下一页
      </button>
    </nav>
  {/if}
</section>
