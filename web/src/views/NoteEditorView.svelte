<script lang="ts">
  import Markdown from '../components/Markdown.svelte'
  import { ApiRequestError } from '../lib/api'
  import { decryptNoteContent, encryptNoteContent, paramsFromNote, KDF_PARAMS } from '../lib/crypto'
  import type { KdfParams } from '../lib/crypto'
  import { editorDraft } from '../lib/draft'
  import { getTemplate } from '../lib/template'
  import {
    createNote,
    getMasterPassword,
    getNote,
    listCategories,
    MAX_CONTENT_CODEPOINTS,
    countCodePoints,
    newClientNoteID,
    updateNote,
    type Category,
    type NotePayload,
  } from '../lib/notes'
  import {
    hasKey,
    passwordMismatch,
    sessionKey,
    unlockWithPassword,
  } from '../lib/session-key'
  import { navigate } from '../router/router'

  let { id = '' }: { id?: string } = $props()

  // 每次进入编辑路由都会重建本组件，id 在实例生命周期内不变。
  // svelte-ignore state_referenced_locally
  const isEdit = id !== ''

  let title = $state('')
  let content = $state('')
  let categoryId = $state<number | null>(null)
  let encrypt = $state(false)
  let password = $state('')
  let confirm = $state('')
  let categories = $state<Category[]>([])
  let salt = $state('')
  let question = $state('')
  let masterConfigured = $state(false)
  let cipher = $state('')
  // 派生用这条笔记自己声明的参数（notes.kdf_params）；新建笔记用当前应用参数。
  let noteParams = $state<KdfParams>(KDF_PARAMS)
  // svelte-ignore state_referenced_locally
  let noteId = $state(id)
  let fields = $state<Record<string, string>>({})
  let formError = $state('')
  let notice = $state('')
  let busy = $state(false)
  let loading = $state(isEdit)
  let dirty = $state(false)
  let previewSource = $state('')
  // 会话里的密钥可能解不开这一条：这时要回到解锁表单，而不是给一个空编辑器。
  let revealFailed = $state(false)
  // 跳去设置主密码是「交棒」，不是离开编辑器，不能顺手清掉刚存的草稿。
  let handingOff = false
  // 草稿（哪怕是用户清空后的空草稿）优先于模板骨架，不能再被骨架填一遍。
  let restoredDraft = false

  const contentLength = $derived(countCodePoints(content))
  const tooLong = $derived(contentLength > MAX_CONTENT_CODEPOINTS)
  const unlocked = $derived($hasKey)
  // 加密笔记没密钥时读不出正文；勾选加密但还没派生密钥时需要临时输入。
  const awaitingUnlock = $derived(cipher !== '' && (!unlocked || revealFailed))
  const needsPassword = $derived((encrypt || awaitingUnlock) && !unlocked)
  // 手里没有密文可试解，就无法当场验密码：只能要求再输入一次。
  const needsConfirm = $derived(encrypt && cipher === '' && !unlocked)
  const canSave = $derived(
    !busy && !loading && title.trim() !== '' && !tooLong && (!needsPassword || password !== ''),
  )

  // 预览防抖 300ms：边打字不重渲染，停下来才更新。
  $effect(() => {
    const text = content
    const timer = setTimeout(() => {
      previewSource = text
    }, 300)
    return () => clearTimeout(timer)
  })

  // 从顶栏锁定时，本页内存里的解密正文也要一起清掉（设计文档 §5.1）。
  $effect(() => {
    if (!$hasKey && cipher !== '') {
      content = ''
      previewSource = ''
      revealFailed = false
    }
  })

  $effect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (dirty) event.preventDefault()
    }
    window.addEventListener('beforeunload', handler)
    return () => window.removeEventListener('beforeunload', handler)
  })

  $effect(() => {
    const draft = editorDraft.take(id)
    if (draft) {
      restoredDraft = true
      title = draft.title
      content = draft.content
      categoryId = draft.categoryId
      encrypt = draft.encrypt
      dirty = true
      notice = '已恢复跳去设置主密码前的草稿。'
    }

    loadFormChrome()

    // 离开编辑器就把草稿连同里面的明文一起丢掉；跳去设置主密码那次除外。
    return () => {
      if (!handingOff) editorDraft.clear()
    }
  })

  function fail(error: unknown): string {
    return error instanceof Error ? error.message : '操作失败，请稍后重试'
  }

  async function loadFormChrome(): Promise<void> {
    try {
      const [cats, master] = await Promise.all([listCategories(), getMasterPassword()])
      categories = cats
      salt = master.salt_b64
      question = master.question
      masterConfigured = master.configured
      // 新建笔记预填模板骨架；有草稿时草稿优先（草稿在上面的 $effect 里已先填入）。
      if (!isEdit && !restoredDraft && content === '') {
        try {
          const tpl = await getTemplate()
          if (content === '') content = tpl.content
        } catch {
          // 模板拿不到就是空正文：编辑器不能因此打不开。
        }
      }
      if (isEdit) await loadNote()
    } catch (error) {
      formError = fail(error)
    } finally {
      loading = false
    }
  }

  async function loadNote(): Promise<void> {
    const note = await getNote(id)
    title = note.title
    categoryId = note.category_id
    noteId = note.id
    if (note.encrypted) {
      cipher = note.cipher_b64 ?? ''
      encrypt = true
      try {
        // 参数不合法（被人手改过）就当数据损坏，不拿它派生密钥
        noteParams = paramsFromNote(note.kdf_params)
      } catch {
        revealFailed = true
      }
      const key = sessionKey.current()
      if (key && cipher !== '' && !revealFailed) {
        try {
          content = await decryptNoteContent(key, note.id, cipher)
        } catch {
          content = ''
          revealFailed = true
        }
      } else {
        content = ''
      }
    } else {
      content = note.content ?? ''
    }
    dirty = false
  }

  // 解锁：编辑加密笔记时顺手把正文填回编辑器。
  async function unlockNow(): Promise<void> {
    fields = {}
    formError = ''
    busy = true
    try {
      const plain = await unlockWithPassword(
        password,
        salt,
        cipher === '' ? undefined : noteId,
        cipher === '' ? undefined : cipher,
        noteParams,
      )
      if (plain !== null) content = plain
      password = ''
      confirm = ''
      revealFailed = false
      notice = '已解锁，密钥只留在本页内存。'
      dirty = cipher !== ''
    } catch (error) {
      fields.password = fail(error)
    } finally {
      busy = false
    }
  }

  // 跳去改模板属于「交棒」：先把当前表单存成草稿，返回时接着编。
  function editTemplate(): void {
    handingOff = true
    editorDraft.save({ noteId: id, title, content, categoryId, encrypt })
    navigate(`/template?next=${encodeURIComponent('/notes/new')}`)
  }

  function goToMasterPassword(): void {
    handingOff = true
    editorDraft.save({ noteId: id, title, content, categoryId, encrypt })
    const next = encodeURIComponent(isEdit ? `/notes/${id}/edit` : '/notes/new')
    navigate(`/master-password?next=${next}`)
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    fields = {}
    formError = ''

    if (title.trim() === '') fields.title = '请填写标题'
    if (tooLong)
      fields.content = `正文请控制在 ${MAX_CONTENT_CODEPOINTS} 字以内，当前 ${contentLength} 字`
    if (needsConfirm) {
      const mismatch = passwordMismatch(password, confirm)
      if (mismatch) fields.confirm = mismatch
    }
    if (Object.keys(fields).length > 0) return

    if (encrypt && !masterConfigured) {
      goToMasterPassword()
      return
    }

    busy = true
    try {
      if (needsPassword) {
        await unlockWithPassword(password, salt, undefined, undefined, noteParams)
        password = ''
        confirm = ''
      }

      const payload: NotePayload = { title: title.trim(), category_id: categoryId }
      if (encrypt) {
        if (noteId === '') noteId = newClientNoteID()
        const key = sessionKey.current()
        if (!key) throw new Error('会话密钥缺失，请重新输入主密码')
        payload.id = isEdit ? undefined : noteId
        payload.cipher_b64 = await encryptNoteContent(key, noteId, content)
        payload.kdf_params = JSON.stringify(noteParams)
      } else {
        payload.content = content
      }

      const saved = isEdit ? await updateNote(noteId, payload) : await createNote(payload)
      dirty = false
      editorDraft.clear()
      navigate(`/notes/${saved.id}`)
    } catch (error) {
      if (error instanceof ApiRequestError && error.fields) {
        fields = error.fields
        formError = error.message
      } else {
        formError = fail(error)
      }
    } finally {
      busy = false
    }
  }

  function discard(): void {
    dirty = false
    editorDraft.clear()
    navigate(isEdit ? `/notes/${id}` : '/notes')
  }
</script>

<section class="page editor">
  <header class="editor-head">
    <h1>{isEdit ? '编辑笔记' : '新建笔记'}</h1>
    <p class="muted">正文只存原始 Markdown，渲染在本机完成。</p>
  </header>

  {#if formError}
    <p class="form-error" role="alert">{formError}</p>
  {:else if notice}
    <p class="form-notice">{notice}</p>
  {/if}

  {#if awaitingUnlock}
    <div class="card unlock-card">
      <h2>这条笔记已加密</h2>
      <p class="muted">输入主密码才能编辑正文。{question ? `助记问题：${question}` : ''}</p>
      <form class="unlock-form" onsubmit={(event) => { event.preventDefault(); unlockNow() }}>
        <label for="editor-password">主密码</label>
        <input
          id="editor-password"
          type="password"
          autocomplete="current-password"
          bind:value={password}
        />
        {#if fields.password}
          <span class="field-error">{fields.password}</span>
        {/if}
        <button class="button" type="submit" disabled={busy || password === ''}>解锁</button>
      </form>
    </div>
  {:else}
    <div class="editor-grid">
      <form class="editor-form" onsubmit={submit}>
      <div class="field">
        <label for="note-title">标题</label>
        <input
          id="note-title"
          maxlength="200"
          bind:value={title}
          oninput={() => {
            dirty = true
          }}
        />
        {#if fields.title}
          <span class="field-error">{fields.title}</span>
        {/if}
      </div>

      <div class="field">
        <label for="note-category">分类</label>
        <select
          id="note-category"
          bind:value={categoryId}
          onchange={() => {
            dirty = true
          }}
        >
          <option value={null}>未分类</option>
          {#each categories.filter((cat) => cat.id !== 1) as cat (cat.id)}
            <option value={cat.id}>{cat.name}</option>
          {/each}
        </select>
        {#if fields.category_id}
          <span class="field-error">{fields.category_id}</span>
        {/if}
      </div>

      <div class="field">
        <div class="field-label-row">
          <label for="note-content">正文</label>
          {#if !isEdit}
            <button class="linklike" type="button" onclick={editTemplate}>编辑模板</button>
          {/if}
          <span class="counter {tooLong ? 'counter-over' : ''}" aria-live="polite">
            {contentLength} / {MAX_CONTENT_CODEPOINTS}
          </span>
        </div>
        <textarea
          id="note-content"
          rows="14"
          bind:value={content}
          oninput={() => {
            dirty = true
          }}
        ></textarea>
        {#if fields.content || tooLong}
          <span class="field-error">
            {fields.content ?? `正文请控制在 ${MAX_CONTENT_CODEPOINTS} 字以内，当前 ${contentLength} 字`}
          </span>
        {/if}
      </div>

      <div class="field encrypt-field">
        <label class="checkbox">
          <input
            type="checkbox"
            bind:checked={encrypt}
            disabled={isEdit && cipher !== ''}
            onchange={() => {
              dirty = true
            }}
          />
          用主密码加密这条笔记
        </label>
        {#if isEdit && cipher !== ''}
          <p class="muted">加密笔记不能改回明文。</p>
        {/if}
        {#if encrypt && !masterConfigured}
          <p class="form-notice">还没有设置主密码，保存时会先带你去设置。</p>
        {/if}
      </div>

      {#if needsPassword}
        <div class="field">
          <label for="session-password">主密码</label>
          <p class="muted">密码只在浏览器内派生密钥，不会发往服务器。{question ? `助记问题：${question}` : ''}</p>
          <input
            id="session-password"
            type="password"
            autocomplete="current-password"
            bind:value={password}
          />
          {#if fields.password}
            <span class="field-error">{fields.password}</span>
          {/if}
        </div>
      {/if}

      {#if needsConfirm}
        <div class="field">
          <label for="session-confirm">再输入一次主密码</label>
          <p class="muted">这条笔记还没有密文可以当场验证密码；密码一旦记错，笔记将永久解不开。</p>
          <input
            id="session-confirm"
            type="password"
            autocomplete="new-password"
            bind:value={confirm}
          />
          {#if fields.confirm}
            <span class="field-error">{fields.confirm}</span>
          {/if}
        </div>
      {/if}

      <div class="actions">
        <button class="button" type="submit" disabled={!canSave}>
          {busy ? '保存中…' : '保存'}
        </button>
        <button class="button button-quiet" type="button" onclick={discard}>取消</button>
      </div>
    </form>

      <section class="preview" aria-label="Markdown 预览">
        <h2>预览</h2>
        {#if previewSource === ''}
          <p class="muted">开始输入即可看到渲染结果。</p>
        {:else}
          <Markdown source={previewSource} />
        {/if}
      </section>
    </div>
  {/if}
</section>
