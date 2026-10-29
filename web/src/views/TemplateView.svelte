<script lang="ts">
  import Markdown from '../components/Markdown.svelte'
  import { ApiRequestError } from '../lib/api'
  import { countCodePoints } from '../lib/notes'
  import {
    getTemplate,
    saveTemplate,
    MAX_TEMPLATE_CODEPOINTS,
  } from '../lib/template'
  import { toast } from '../lib/toast'
  import { navigate } from '../router/router'

  // 返回目标来自 /template?next=…（新建笔记页交棒过来时带上）；直接打开就回新建页。
  const next = new URLSearchParams(location.search).get('next') || '/notes/new'

  let content = $state('')
  // original 是磁盘上（或内置默认）的内容，用来判断「有没有改过」。
  let original = $state('')
  let path = $state('')
  let source = $state<'file' | 'default'>('default')
  let defaultContent = $state('')
  let error = $state('')
  let notice = $state('')
  let busy = $state(false)
  let loading = $state(true)
  let previewSource = $state('')

  const length = $derived(countCodePoints(content))
  const tooLong = $derived(length > MAX_TEMPLATE_CODEPOINTS)
  const canSave = $derived(
    !busy && !loading && !tooLong && content !== original,
  )

  // 预览防抖 300ms：与编辑器一致，边打边渲染没必要。
  $effect(() => {
    const text = content
    const timer = setTimeout(() => {
      previewSource = text
    }, 300)
    return () => clearTimeout(timer)
  })

  $effect(() => {
    load()
  })

  async function load(): Promise<void> {
    loading = true
    try {
      const tpl = await getTemplate()
      content = tpl.content
      original = tpl.content
      path = tpl.path
      source = tpl.source
      defaultContent = tpl.default_content
      previewSource = tpl.content
      error = ''
    } catch (cause) {
      error = cause instanceof Error ? cause.message : '模板载入失败，请稍后重试'
    } finally {
      loading = false
    }
  }

  // 恢复默认只回填，不落盘：误点一下不该覆盖用户改过的文件。
  function restoreDefault(): void {
    content = defaultContent
    error = ''
    notice = '已填入内置默认内容，点「保存模板」才会写回文件。'
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (tooLong) return
    error = ''
    notice = ''
    busy = true
    try {
      const saved = await saveTemplate(content)
      content = saved.content
      original = saved.content
      path = saved.path
      source = saved.source
      defaultContent = saved.default_content
      toast('模板已保存')
    } catch (cause) {
      if (cause instanceof ApiRequestError && cause.fields?.content) {
        error = cause.fields.content
      } else {
        error = cause instanceof Error ? cause.message : '保存失败，请稍后重试'
      }
    } finally {
      busy = false
    }
  }
</script>

<section class="page editor">
  <header class="editor-head">
    <h1>新建笔记模板</h1>
    <p class="muted">新建笔记时正文会预填这份 markdown 骨架，不需要就自己删掉。</p>
    {#if path}
      <p class="template-path">文件：{path}</p>
    {/if}
  </header>

  {#if error}
    <p class="form-error" role="alert">{error}</p>
  {:else if notice}
    <p class="form-notice">{notice}</p>
  {/if}

  {#if loading}
    <p class="muted">模板载入中…</p>
  {:else if error && path === ''}
    <button class="button" type="button" onclick={load}>重新载入</button>
  {:else}
    <div class="editor-grid">
      <form class="editor-form" onsubmit={submit}>
        <div class="field">
          <div class="field-label-row">
            <label for="template-content">骨架</label>
            <span class="counter {tooLong ? 'counter-over' : ''}" aria-live="polite">
              {length} / {MAX_TEMPLATE_CODEPOINTS}
            </span>
          </div>
          <textarea id="template-content" rows="18" bind:value={content}></textarea>
          {#if tooLong}
            <span class="field-error">
              模板请控制在 {MAX_TEMPLATE_CODEPOINTS} 字以内，当前 {length} 字
            </span>
          {/if}
          {#if source === 'default'}
            <p class="muted">
              模板文件还没创建，现在显示的是内置默认内容；保存后会写回该文件。
            </p>
          {/if}
        </div>

        <div class="actions">
          <button class="button" type="submit" disabled={!canSave}>
            {busy ? '保存中…' : '保存模板'}
          </button>
          <button class="button button-quiet" type="button" onclick={restoreDefault}>
            恢复默认
          </button>
          <button class="button button-quiet" type="button" onclick={() => navigate(next)}>
            返回
          </button>
        </div>
      </form>

      <section class="preview" aria-label="Markdown 预览">
        <h2>预览</h2>
        {#if previewSource === ''}
          <p class="muted">模板是空的，新笔记的正文也会是空的。</p>
        {:else}
          <Markdown source={previewSource} />
        {/if}
      </section>
    </div>
  {/if}
</section>
