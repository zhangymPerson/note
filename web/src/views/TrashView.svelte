<script lang="ts">
  // 回收站：软删除（0006）的笔记在这里等着恢复或彻底删除。
  // 加密笔记的「彻底删除」需要先解锁，所以只放在详情页——本页把按钮禁掉并说明原因。
  import {
    listNotes,
    purgeNote,
    restoreNote,
    type NoteListItem,
  } from '../lib/notes'
  import { toast } from '../lib/toast'
  import { navigate } from '../router/router'

  let rows = $state<NoteListItem[]>([])
  let total = $state(0)
  let perPage = $state(20)
  let page = $state(1)
  let loading = $state(true)
  let error = $state('')
  let pendingId = $state('')
  let pendingAction = $state<'restore' | 'purge' | ''>('')

  const pageCount = $derived(Math.max(1, Math.ceil(total / perPage)))

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  async function refresh(wanted: number): Promise<void> {
    loading = true
    try {
      const result = await listNotes({ deleted: true, page: wanted })
      rows = result.items
      total = result.total
      perPage = result.per_page
      error = ''
      // 彻底删除最后一页的条目后当前页可能已超出范围
      const last = Math.max(1, Math.ceil(total / perPage))
      if (wanted > last) page = last
    } catch (cause) {
      error = message(cause)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    refresh(page)
  })

  async function restore(note: NoteListItem): Promise<void> {
    pendingId = note.id
    pendingAction = 'restore'
    try {
      await restoreNote(note.id)
      toast(`已恢复「${note.title}」`)
      await refresh(page)
    } catch (cause) {
      error = message(cause)
    } finally {
      pendingId = ''
      pendingAction = ''
    }
  }

  async function purge(note: NoteListItem): Promise<void> {
    if (!window.confirm(`彻底删除「${note.title}」？删除后无法恢复。`)) return
    pendingId = note.id
    pendingAction = 'purge'
    try {
      await purgeNote(note.id)
      toast('已彻底删除')
      await refresh(page)
    } catch (cause) {
      error = message(cause)
    } finally {
      pendingId = ''
      pendingAction = ''
    }
  }

  function step(delta: number): void {
    page = Math.min(pageCount, Math.max(1, page + delta))
  }

  function open(note: NoteListItem): void {
    navigate(`/notes/${encodeURIComponent(note.id)}`)
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
    <h1>回收站</h1>
    <a class="button button-quiet" href="/notes" onclick={(event) => { event.preventDefault(); navigate('/notes') }}>
      返回全部笔记
    </a>
  </header>

  <p class="muted">
    这里是想删掉但还没丢掉的笔记：正文、分类都原样保留，恢复后回到原来的分类。
    「彻底删除」才是真的清除数据，无法撤销。
  </p>

  {#if error}
    <p class="form-error" role="alert">{error}</p>
  {/if}

  {#if loading}
    <p class="muted">回收站载入中…</p>
  {:else if rows.length === 0}
    <p class="empty-hint">回收站是空的。在笔记里点「删除」，它会先到这里。</p>
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
              {#if note.deleted_at}
                移入于 {shownAt(note.deleted_at)}
              {:else}
                更新于 {shownAt(note.updated_at)}
              {/if}
              {#if note.category_name}
                · 原分类「{note.category_name}」
              {/if}
            </span>
          </button>

          <div class="trash-actions">
            <button
              class="button button-quiet"
              type="button"
              disabled={pendingId === note.id}
              onclick={() => restore(note)}
            >
              {pendingId === note.id && pendingAction === 'restore' ? '恢复中…' : '恢复'}
            </button>
            <button
              class="button button-quiet"
              type="button"
              disabled={pendingId === note.id || note.encrypted}
              title={note.encrypted ? '加密笔记请打开详情、解锁后再彻底删除' : '彻底删除后无法恢复'}
              onclick={() => purge(note)}
            >
              {pendingId === note.id && pendingAction === 'purge' ? '删除中…' : '彻底删除'}
            </button>
          </div>
        </li>
      {/each}
    </ul>

    {#if rows.some((note) => note.encrypted)}
      <p class="muted">带「已加密」的笔记要先打开详情并解锁，才能彻底删除。</p>
    {/if}

    <nav class="pager" aria-label="分页">
      <button class="button button-quiet" type="button" disabled={page <= 1} onclick={() => step(-1)}>
        上一页
      </button>
      <span class="pager-state">第 {page} / {pageCount} 页 · 共 {total} 条</span>
      <button class="button button-quiet" type="button" disabled={page >= pageCount} onclick={() => step(1)}>
        下一页
      </button>
    </nav>
  {/if}
</section>
