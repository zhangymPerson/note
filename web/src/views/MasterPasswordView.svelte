<script lang="ts">
  import { generateSaltB64 } from '../lib/crypto'
  import { getMasterPassword, setMasterPassword } from '../lib/notes'
  import { unlockKey } from '../lib/session-key'
  import { navigate } from '../router/router'

  let password = $state('')
  let confirm = $state('')
  let question = $state('')
  let acknowledged = $state(false)
  let configured = $state(false)
  let existingQuestion = $state('')
  let fields = $state<Record<string, string>>({})
  let error = $state('')
  let busy = $state(false)
  let loading = $state(true)

  const canSubmit = $derived(
    acknowledged && password !== '' && confirm !== '' && question.trim() !== '' && !busy,
  )

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  // ?next= 是编辑器在跳转前留下的回跳路径，只接受站内以 / 开头的绝对路径。
  function nextPath(): string {
    const raw = new URLSearchParams(location.search).get('next') ?? ''
    if (raw === '' || !raw.startsWith('/') || raw.startsWith('//')) return ''
    return raw
  }

  async function load(): Promise<void> {
    try {
      const state = await getMasterPassword()
      configured = state.configured
      existingQuestion = state.question
    } catch (cause) {
      error = message(cause)
    } finally {
      loading = false
    }
  }

  $effect(() => {
    load()
  })

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    fields = {}
    error = ''
    if (password !== confirm) fields.confirm = '两次输入的密码不一致'
    if (!acknowledged) fields.ack = '请先勾选确认：主密码无法找回'
    if (question.trim() === '') fields.question = '请填写助记问题'
    if (Object.keys(fields).length > 0) return

    busy = true
    try {
      const salt = generateSaltB64()
      await setMasterPassword(salt, question.trim())
      await unlockKey(password, salt)
      password = ''
      confirm = ''
      const next = nextPath()
      navigate(next === '' ? '/notes' : next)
    } catch (cause) {
      error = message(cause)
    } finally {
      busy = false
    }
  }
</script>

<section class="page detail">
  <h1>主密码</h1>

  {#if loading}
    <p class="muted">载入中…</p>
  {:else if configured}
    <div class="card lock-card">
      <h2>已经配置过了</h2>
      <p class="muted">主密码只能配置一次，之后无法修改，也无法找回。</p>
      <p class="hint">助记问题：{existingQuestion === '' ? '（未设置）' : existingQuestion}</p>
      <div class="actions">
        <a class="button" href="/notes" onclick={(event) => { event.preventDefault(); navigate('/notes') }}>
          返回笔记
        </a>
      </div>
    </div>
  {:else}
    <div class="card lock-card">
      <h2>设置主密码</h2>
      <p class="muted">加密笔记的内容由这个密码派生的密钥保护，浏览器直接把密文解开，服务器只存密文。</p>
      <form class="stack-form" onsubmit={submit}>
        <div>
          <label for="master-password">主密码</label>
          <input id="master-password" type="password" autocomplete="new-password" bind:value={password} />
          <p class="muted">建议使用 12 个字符以上的长口令：密钥由它派生，弱口令会被离线爆破。</p>
          {#if fields.password}
            <p class="field-error" role="alert">{fields.password}</p>
          {/if}
        </div>
        <div>
          <label for="master-confirm">确认主密码</label>
          <input id="master-confirm" type="password" autocomplete="new-password" bind:value={confirm} />
          {#if fields.confirm}
            <p class="field-error" role="alert">{fields.confirm}</p>
          {/if}
        </div>
        <div>
          <label for="master-question">助记问题</label>
          <input
            id="master-question"
            type="text"
            maxlength="100"
            placeholder="例如：你的生日是多少？"
            bind:value={question}
          />
          <p class="muted">忘记主密码时，这里只会显示问题本身，永远不会显示密码。</p>
          {#if fields.question}
            <p class="field-error" role="alert">{fields.question}</p>
          {/if}
        </div>
        <label class="ack">
          <input type="checkbox" bind:checked={acknowledged} />
          <span>我已确认：主密码无法找回、无法重置，忘记后加密笔记永久打不开。</span>
        </label>
        {#if fields.ack}
          <p class="field-error" role="alert">{fields.ack}</p>
        {/if}
        <button class="button" type="submit" disabled={!canSubmit}>设置主密码</button>
      </form>
      {#if error}
        <p class="form-error" role="alert">{error}</p>
      {/if}
    </div>
  {/if}
</section>
