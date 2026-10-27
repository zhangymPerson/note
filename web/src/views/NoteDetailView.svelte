<script lang="ts">
  import Markdown from '../components/Markdown.svelte'
  import { decryptNoteContent } from '../lib/crypto'
  import { deleteNote, getMasterPassword, getNote, type NoteMeta } from '../lib/notes'
  import { hasKey, lockSession, sessionKey, unlockWithPassword } from '../lib/session-key'
  import { navigate } from '../router/router'

  let { id = '' }: { id: string } = $props()

  let note = $state<NoteMeta | null>(null)
  // 解密出来的正文只活在这一个变量里：锁定或离开本页必须清空（设计文档 §5.1）。
  let plain = $state('')
  let password = $state('')
  let salt = $state('')
  let question = $state('')
  let hintOpen = $state(false)
  let fields = $state<Record<string, string>>({})
  let error = $state('')
  let busy = $state(false)
  let loading = $state(true)
  // 会话里可能有别的密码派生的密钥：这条解不开就回到解锁表单，而不是显示空正文。
  let decryptFailed = $state(false)

  const encrypted = $derived(note?.encrypted === true)
  const awaitingKey = $derived(encrypted && (!$hasKey || decryptFailed))
  const body = $derived(encrypted ? plain : (note?.content ?? ''))
  // 模板里的点击回调是闭包，读不到 {#if note} 的收窄，所以预先取出 id。
  const noteId = $derived(note?.id ?? '')

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  async function load(): Promise<void> {
    try {
      const [found, master] = await Promise.all([getNote(id), getMasterPassword()])
      note = found
      salt = master.salt_b64
      question = master.question
      if (found.encrypted && sessionKey.current()) {
        try {
          await revealWithKey(sessionKey.current())
        } catch {
          decryptFailed = true
        }
      }
      error = ''
    } catch (cause) {
      error = message(cause)
    } finally {
      loading = false
    }
  }

  async function revealWithKey(key: CryptoKey | null): Promise<void> {
    if (!key || !note?.cipher_b64) return
    plain = await decryptNoteContent(key, note.id, note.cipher_b64)
  }

  $effect(() => {
    load()
  })

  // 从顶栏锁定时，本页内存里的解密正文也要一起清掉（设计文档 §5.1）。
  $effect(() => {
    if (!$hasKey) {
      plain = ''
      password = ''
      decryptFailed = false
    }
  })

  async function unlock(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    fields = {}
    error = ''
    busy = true
    try {
      const out = await unlockWithPassword(password, salt, note?.id, note?.cipher_b64)
      plain = out ?? ''
      password = ''
      decryptFailed = false
    } catch (cause) {
      fields.password = message(cause)
    } finally {
      busy = false
    }
  }

  function lock(): void {
    lockSession()
    plain = ''
    password = ''
    error = ''
    decryptFailed = false
  }

  async function remove(): Promise<void> {
    if (!note) return
    if (!window.confirm(`永久删除「${note.title}」？删除后无法恢复。`)) return
    busy = true
    try {
      await deleteNote(note.id)
      lock()
      navigate('/notes')
    } catch (cause) {
      error = message(cause)
    } finally {
      busy = false
    }
  }

  function shownAt(value: string): string {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) return value
    const pad = (input: number): string => String(input).padStart(2, '0')
    return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`
  }
</script>

<section class="page detail">
  {#if loading}
    <p class="muted">笔记载入中…</p>
  {:else if error && !note}
    <p class="form-error" role="alert">{error}</p>
    <a class="button button-quiet" href="/notes" onclick={(event) => { event.preventDefault(); navigate('/notes') }}>
      返回列表
    </a>
  {:else if note}
    <header class="detail-head">
      <h1>
        {note.title}
        {#if encrypted}
          <span class="badge">已加密</span>
        {/if}
      </h1>
      <p class="note-meta">
        更新于 {shownAt(note.updated_at)} · 第 {note.rev} 版
        {#if note.category_name}
          · 分类「{note.category_name}」
        {/if}
      </p>
    </header>

    {#if awaitingKey}
      <div class="card lock-card">
        <h2>这条笔记已加密</h2>
        <p class="muted">输入主密码即可在本页解开，密钥只留在内存。</p>

        <form class="unlock-form" onsubmit={unlock}>
          <label for="detail-password">主密码</label>
          <input
            id="detail-password"
            type="password"
            autocomplete="current-password"
            bind:value={password}
          />
          <button class="button" type="submit" disabled={busy || password === ''}>解锁</button>
        </form>
        {#if fields.password}
          <p class="field-error" role="alert">{fields.password}</p>
        {/if}

        <button class="linklike" type="button" onclick={() => { hintOpen = !hintOpen }}>
          {hintOpen ? '收起密码提示' : '查看密码提示'}
        </button>
        {#if hintOpen}
          <p class="hint">助记问题：{question === '' ? '还没有设置助记问题。' : question}</p>
        {/if}
        <p class="muted">
          <a href="/notes/{noteId}/forgot" onclick={(event) => { event.preventDefault(); navigate(`/notes/${noteId}/forgot`) }}>
            忘记密码？
          </a>
        </p>
      </div>
    {:else}
      <article class="detail-body">
        <Markdown source={body} />
      </article>
    {/if}

    {#if error}
      <p class="form-error" role="alert">{error}</p>
    {/if}

    <div class="actions">
      <a class="button" href="/notes/{noteId}/edit" onclick={(event) => { event.preventDefault(); navigate(`/notes/${noteId}/edit`) }}>
        编辑
      </a>
      {#if encrypted && !awaitingKey}
        <button class="button button-quiet" type="button" onclick={lock}>锁定</button>
      {/if}
      <button class="button button-quiet" type="button" disabled={busy} onclick={remove}>删除</button>
      <a class="button button-quiet" href="/notes" onclick={(event) => { event.preventDefault(); navigate('/notes') }}>
        返回
      </a>
    </div>

    {#if encrypted && $hasKey}
      <p class="muted">解密正文只保留在当前页面，刷新或点「锁定」后需要重新输入密码。</p>
    {/if}
  {/if}
</section>
