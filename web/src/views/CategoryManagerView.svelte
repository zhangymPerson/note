<script lang="ts">
  import { ApiRequestError } from '../lib/api'
  import {
    createCategory,
    deleteCategory,
    listCategories,
    renameCategory,
    type Category,
  } from '../lib/notes'

  let categories = $state<Category[]>([])
  let newName = $state('')
  let editingId = $state(0)
  let editingName = $state('')
  let rowErrors = $state<Record<number, string>>({})
  let formError = $state('')
  let busy = $state(false)
  let loading = $state(true)

  const DEFAULT_CATEGORY_ID = 1

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  async function refresh(): Promise<void> {
    try {
      categories = await listCategories()
      formError = ''
    } catch (cause) {
      formError = message(cause)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    refresh()
  })

  async function create(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (newName.trim() === '') return
    busy = true
    try {
      await createCategory(newName.trim())
      newName = ''
      await refresh()
    } catch (cause) {
      formError =
        cause instanceof ApiRequestError && cause.fields?.name ? cause.fields.name : message(cause)
    } finally {
      busy = false
    }
  }

  function startRename(category: Category): void {
    editingId = category.id
    editingName = category.name
    rowErrors = { ...rowErrors, [category.id]: '' }
  }

  function cancelRename(): void {
    editingId = 0
    editingName = ''
  }

  async function saveRename(event: SubmitEvent, id: number): Promise<void> {
    event.preventDefault()
    busy = true
    try {
      categories = await renameCategory(id, editingName.trim())
      cancelRename()
    } catch (cause) {
      rowErrors = {
        ...rowErrors,
        [id]: cause instanceof ApiRequestError && cause.fields?.name ? cause.fields.name : message(cause),
      }
    } finally {
      busy = false
    }
  }

  async function remove(category: Category): Promise<void> {
    if (!window.confirm(`删除分类「${category.name}」？`)) return
    busy = true
    try {
      await deleteCategory(category.id)
      rowErrors = { ...rowErrors, [category.id]: '' }
      await refresh()
    } catch (cause) {
      // 409 带笔记数，直接显示在该行，避免用户以为删除失败是别的故障。
      rowErrors = { ...rowErrors, [category.id]: message(cause) }
    } finally {
      busy = false
    }
  }
</script>

<section class="page detail">
  <h1>分类管理</h1>
  <p class="muted">新建笔记时不选分类，就归入「默认」；默认分类不能改名或删除。</p>

  <form class="inline-form" onsubmit={create}>
    <input
      type="text"
      maxlength="30"
      placeholder="新分类名称"
      aria-label="新分类名称"
      bind:value={newName}
    />
    <button class="button" type="submit" disabled={busy || newName.trim() === ''}>新增</button>
  </form>

  {#if formError}
    <p class="form-error" role="alert">{formError}</p>
  {/if}

  {#if loading}
    <p class="muted">分类载入中…</p>
  {:else}
    <ul class="card list manager">
      {#each categories as category (category.id)}
        <li>
          <div class="row">
            <span class="row-name">
              {category.name}
              {#if category.id === DEFAULT_CATEGORY_ID}
                <span class="badge">默认</span>
              {/if}
            </span>
            <span class="muted">{category.note_count} 条笔记</span>
            <span class="row-actions">
              {#if category.id === DEFAULT_CATEGORY_ID}
                <span class="muted">不可修改</span>
              {:else if editingId === category.id}
                <form class="inline-form" onsubmit={(event) => saveRename(event, category.id)}>
                  <input
                    type="text"
                    maxlength="30"
                    aria-label="分类名称"
                    bind:value={editingName}
                  />
                  <button class="button" type="submit" disabled={busy || editingName.trim() === ''}>
                    保存
                  </button>
                  <button class="button button-quiet" type="button" onclick={cancelRename}>
                    取消
                  </button>
                </form>
              {:else}
                <button class="button button-quiet" type="button" onclick={() => startRename(category)}>
                  改名
                </button>
                <button class="button button-quiet" type="button" disabled={busy} onclick={() => remove(category)}>
                  删除
                </button>
              {/if}
            </span>
          </div>
          {#if rowErrors[category.id]}
            <p class="field-error" role="alert">{rowErrors[category.id]}</p>
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
</section>
