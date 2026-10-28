<script lang="ts">
  import {
    deleteNote,
    getMasterPassword,
    getNote,
    purgeNote,
    type NoteMeta,
  } from '../lib/notes'
  import { lockSession } from '../lib/session-key'
  import { navigate } from '../router/router'

  let { id = '' }: { id: string } = $props()

  let note = $state<NoteMeta | null>(null)
  let question = $state('')
  let error = $state('')
  let busy = $state(false)
  let loading = $state(true)

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  async function load(): Promise<void> {
    try {
      const [found, master] = await Promise.all([getNote(id), getMasterPassword()])
      note = found
      question = master.question
    } catch (cause) {
      error = message(cause)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    load()
  })

  // 忘记密码只能丢弃：先软删除，再彻底删除。
  // 这条笔记谁也解不开，留在回收站只会变成一条永远无法解锁、也无法清除的残留。
  async function remove(): Promise<void> {
    if (!note) return
    if (!window.confirm(`永久删除「${note.title}」？密码无法找回，删除后这条笔记就彻底没了。`)) return
    busy = true
    try {
      await deleteNote(note.id)
      await purgeNote(note.id)
      lockSession()
      navigate('/notes')
    } catch (cause) {
      error = message(cause)
    } finally {
      busy = false
    }
  }
</script>

<section class="page detail">
  <h1>忘记密码</h1>

  {#if loading}
    <p class="muted">载入中…</p>
  {:else if error && !note}
    <p class="form-error" role="alert">{error}</p>
  {/if}

  <div class="card lock-card">
    <h2>助记问题</h2>
    <p class="hint">{question === '' ? '还没有设置助记问题，所以这里没有可显示的提示。' : question}</p>
    <p class="form-error" role="alert">
      主密码没有找回、没有重置、也没有后台可查询的通道。忘记它，这条加密笔记就永久打不开了。
    </p>
    <p class="muted">这里只能提示问题本身，永远不会显示密码。</p>
  </div>

  {#if error && note}
    <p class="form-error" role="alert">{error}</p>
  {/if}

  <div class="actions">
    <button class="button button-quiet" type="button" disabled={busy} onclick={remove}>删除笔记</button>
    <a
      class="button"
      href="/notes/{note?.id ?? id}"
      onclick={(event) => {
        event.preventDefault()
        navigate(`/notes/${note?.id ?? id}`)
      }}
    >
      返回
    </a>
  </div>
</section>
