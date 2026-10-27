<script lang="ts">
  import { generateSaltB64 } from '../lib/crypto'
  import { getMasterPassword, setMasterPassword } from '../lib/notes'
  import type { OnboardingStep } from '../lib/onboarding'
  import { unlockKey } from '../lib/session-key'
  import { loadSettings, saveName } from '../lib/settings'
  import { navigate } from '../router/router'

  let { initialStep = 1 }: { initialStep?: OnboardingStep } = $props()

  // 步骤 1 存好名字后本地推进到步骤 2；不重读设置，避免半套数据把页面打回第一步。
  // svelte-ignore state_referenced_locally
  let step = $state<OnboardingStep>(initialStep === 0 ? 1 : initialStep)

  let name = $state('')
  let submitting = $state(false)
  let error = $state('')

  let password = $state('')
  let confirmValue = $state('')
  let question = $state('')
  let acknowledged = $state(false)
  let fields = $state<Record<string, string>>({})
  let masterError = $state('')
  let masterBusy = $state(false)
  let loadingMaster = $state(true)
  let configured = $state(false)
  let saltB64 = $state('')

  const canSetMaster = $derived(
    password !== '' && confirmValue !== '' && question.trim() !== '' && acknowledged && !masterBusy,
  )

  async function submitName(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (submitting) return
    submitting = true
    error = ''
    try {
      await saveName(name)
      step = 2
    } catch (err) {
      error = err instanceof Error ? err.message : '保存失败，请重试'
    } finally {
      submitting = false
    }
  }

  $effect(() => {
    if (step !== 2) return
    (async () => {
      try {
        const state = await getMasterPassword()
        configured = state.configured
        saltB64 = state.salt_b64
      } catch (err) {
        masterError = err instanceof Error ? err.message : '读取主密码配置失败'
      } finally {
        loadingMaster = false
      }
    })()
  })

  async function finish(key: string, salt: string): Promise<void> {
    await unlockKey(key, salt)
    await loadSettings()
    navigate('/')
  }

  // 库里已经有主密码（上一次 POST 成功、解锁前刷新）：这条路径只解锁，绝不重复 POST。
  async function submitUnlock(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (password === '' || masterBusy) return
    masterBusy = true
    masterError = ''
    try {
      await finish(password, saltB64)
    } catch (err) {
      masterError = err instanceof Error ? err.message : '解锁失败'
    } finally {
      masterBusy = false
    }
  }

  async function submitMaster(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    if (masterBusy) return
    fields = {}
    masterError = ''
    if (password !== confirmValue) fields.confirm = '两次输入的密码不一致'
    if (!acknowledged) fields.ack = '请先勾选确认：主密码无法找回'
    if (question.trim() === '') fields.question = '请填写助记问题'
    if (Object.keys(fields).length > 0) return

    masterBusy = true
    try {
      const salt = generateSaltB64()
      await setMasterPassword(salt, question.trim())
      await finish(password, salt)
    } catch (err) {
      masterError = err instanceof Error ? err.message : '设置失败，请重试'
    } finally {
      masterBusy = false
    }
  }
</script>

<div class="onboarding">
  <div class="onboarding-inner">
    <ol class="onboarding-steps">
      <li aria-current={step === 1 ? 'step' : undefined}>1/2 名字</li>
      <li aria-current={step === 2 ? 'step' : undefined}>2/2 主密码</li>
    </ol>

    {#if step === 1}
      <h1 class="onboarding-title">第一次打开，<br />先写下你的名字。</h1>
      <p class="onboarding-sub">这本笔记只属于你。名字会保存在本机配置文件里，不会联网发送。</p>
      <form class="name-form" onsubmit={submitName}>
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
            {submitting ? '正在保存…' : '下一步'}
          </button>
        </div>
      </form>
    {:else}
      <h1 class="onboarding-title">设定主密码，<br />加密笔记才有地方可藏。</h1>
      <p class="onboarding-sub">
        密码只在你这台浏览器里派生密钥，服务器只存密文；它无法找回、无法重置。
      </p>

      {#if loadingMaster}
        <p class="muted">载入中…</p>
      {:else if configured}
        <form class="stack-form" onsubmit={submitUnlock}>
          <div>
            <label for="master-unlock">主密码</label>
            <input
              id="master-unlock"
              type="password"
              autocomplete="current-password"
              bind:value={password}
            />
            <p class="muted">这台设备已经设置过主密码了，输入即可开始；不会重复设置。</p>
          </div>
          <button class="button" type="submit" disabled={password === '' || masterBusy}>
            {masterBusy ? '正在解锁…' : '解锁并进入'}
          </button>
          {#if masterError}
            <p class="form-error" role="alert">{masterError}</p>
          {/if}
        </form>
      {:else}
        <form class="stack-form" onsubmit={submitMaster}>
          <div>
            <label for="master-password">主密码</label>
            <input
              id="master-password"
              type="password"
              autocomplete="new-password"
              bind:value={password}
            />
            <p class="muted">建议使用 12 个字符以上的长口令：密钥由它派生，弱口令会被离线爆破。</p>
          </div>
          <div>
            <label for="master-confirm">确认主密码</label>
            <input
              id="master-confirm"
              type="password"
              autocomplete="new-password"
              bind:value={confirmValue}
            />
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
          <button class="button" type="submit" disabled={!canSetMaster}>
            {masterBusy ? '正在保存…' : '开始使用'}
          </button>
          {#if masterError}
            <p class="form-error" role="alert">{masterError}</p>
          {/if}
        </form>
      {/if}
    {/if}
  </div>
</div>
