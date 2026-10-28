<script lang="ts">
  import type { Category } from '../lib/notes'

  interface Props {
    categories: Category[]
    active: number
    busy: boolean
    onPick: (id: number) => void
    onCreate: (name: string) => Promise<string>
    onRename: (id: number, name: string) => Promise<string>
    onDelete: (id: number) => Promise<string>
  }

  let { categories, active, busy = false, onPick, onCreate, onRename, onDelete }: Props = $props()

  // 「未分类」是内置分类，后端改名/删除都返回 409，界面干脆不给按钮。
  const DEFAULT_CATEGORY_ID = 1

  let open = $state(true)
  let editingId = $state(0)
  let editingName = $state('')
  let newName = $state('')
  let createError = $state('')
  let errors = $state<Record<number, string>>({})

  const total = $derived(categories.reduce((sum, cat) => sum + cat.note_count, 0))

  // 写操作的回调都返回错误文案（空串表示成功）：数据与 API 调用在列表页，
  // 树只负责把失败文案摆到对应的位置。
  async function submitCreate(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    const name = newName.trim()
    if (name === '') return
    const error = await onCreate(name)
    if (error) {
      createError = error
      return
    }
    createError = ''
    newName = ''
  }

  function startRename(category: Category): void {
    editingId = category.id
    editingName = category.name
    errors = { ...errors, [category.id]: '' }
  }

  function cancelRename(): void {
    editingId = 0
    editingName = ''
  }

  async function submitRename(event: SubmitEvent, id: number): Promise<void> {
    event.preventDefault()
    const name = editingName.trim()
    if (name === '') return
    const error = await onRename(id, name)
    if (error) {
      errors = { ...errors, [id]: error }
      return
    }
    errors = { ...errors, [id]: '' }
    cancelRename()
  }

  async function removeCategory(category: Category): Promise<void> {
    if (!window.confirm(`删除分类「${category.name}」？`)) return
    errors = { ...errors, [category.id]: await onDelete(category.id) }
  }
</script>

<nav class="cat-tree" aria-label="分类">
  <div class="cat-root">
    <button
      class="cat-toggle"
      type="button"
      aria-expanded={open}
      aria-label={open ? '收起分类' : '展开分类'}
      onclick={() => {
        open = !open
      }}>▾</button
    >
    <button class="cat-row" type="button" data-active={active === 0} onclick={() => onPick(0)}>
      <span class="cat-name">全部笔记</span>
      <span class="cat-num">{total}</span>
    </button>
  </div>

  {#if open}
    <ul class="cat-list">
      {#each categories as category (category.id)}
        <li class="cat-item">
          {#if editingId === category.id}
            <form class="cat-rename" onsubmit={(event) => submitRename(event, category.id)}>
              <input
                type="text"
                maxlength="30"
                aria-label="分类名称"
                bind:value={editingName}
                onkeydown={(event) => {
                  if (event.key === 'Escape') cancelRename()
                }}
              />
            </form>
          {:else}
            <div class="cat-line">
              <button
                class="cat-row"
                type="button"
                data-active={active === category.id}
                onclick={() => onPick(category.id)}
              >
                <span class="cat-name">{category.name}</span>
                <span class="cat-num">{category.note_count}</span>
              </button>
              {#if category.id !== DEFAULT_CATEGORY_ID}
                <span class="cat-actions">
                  <button class="cat-act" type="button" disabled={busy} onclick={() => startRename(category)}>
                    改名
                  </button>
                  <button class="cat-act" type="button" disabled={busy} onclick={() => removeCategory(category)}>
                    删除
                  </button>
                </span>
              {/if}
            </div>
          {/if}
          {#if errors[category.id]}
            <p class="field-error" role="alert">{errors[category.id]}</p>
          {/if}
        </li>
      {/each}
    </ul>

    <form class="cat-create" onsubmit={submitCreate}>
      <input
        type="text"
        maxlength="30"
        placeholder="新分类"
        aria-label="新分类名称"
        disabled={busy}
        bind:value={newName}
      />
      <button class="button button-quiet" type="submit" disabled={busy || newName.trim() === ''}>+</button>
    </form>
    {#if createError}
      <p class="field-error cat-create-error" role="alert">{createError}</p>
    {/if}
  {/if}
</nav>
