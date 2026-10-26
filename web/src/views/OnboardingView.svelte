<script lang="ts">
  import { saveName } from '../lib/settings'

  let name = $state('')
  let submitting = $state(false)
  let error = $state('')

  async function submit(event: SubmitEvent) {
    event.preventDefault()
    if (submitting) return
    submitting = true
    error = ''
    try {
      await saveName(name)
    } catch (err) {
      error = err instanceof Error ? err.message : '保存失败，请重试'
      submitting = false
    }
  }
</script>

<div class="onboarding">
  <div class="onboarding-inner">
    <h1 class="onboarding-title">第一次打开，<br />先写下你的名字。</h1>
    <p class="onboarding-sub">这本笔记只属于你。名字会保存在本机配置文件里，不会联网发送。</p>
    <form class="name-form" onsubmit={submit}>
      <label class="sr-only" for="name-input">你的名字</label>
      <input
        id="name-input"
        class="name-input"
        type="text"
        autocomplete="name"
        maxlength="32"
        placeholder="名字"
        bind:value={name}
      />

      {#if error}
        <p class="name-error" role="alert">{error}</p>
      {/if}

      <div class="name-actions">
        <button class="button" type="submit" disabled={name.trim() === '' || submitting}>
          {submitting ? '正在保存…' : '开始使用'}
        </button>
      </div>
    </form>
  </div>
</div>
