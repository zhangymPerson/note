<script lang="ts">
  import { ApiRequestError } from '../lib/api'
  import { KDF_PARAMS, decryptNoteContent, deriveKey, encryptNoteContent } from '../lib/crypto'
  import { buildRows, getInfo, type ServerInfo } from '../lib/info'
  import {
    getMasterPassword,
    listEncryptedCiphers,
    updateMasterQuestion,
    updateNoteCipher,
  } from '../lib/notes'
  import {
    countRemaining,
    needsPreviousPassword,
    runMigration,
    verifyOldPassword,
    type MigrationReport,
    type MigrationRow,
    type ReencryptDeps,
  } from '../lib/reencrypt'
  import { sessionKey } from '../lib/session-key'
  import { saveName, settings } from '../lib/settings'
  import { toast } from '../lib/toast'

  // 迁移用真实 WebCrypto + 真实密文替换端点；纯函数本体在 reencrypt.ts 里单测。
  const deps: ReencryptDeps = {
    decrypt: decryptNoteContent,
    encrypt: encryptNoteContent,
    putCipher: updateNoteCipher,
  }

  function message(cause: unknown): string {
    return cause instanceof Error ? cause.message : '操作失败，请稍后重试'
  }

  function outcomeText(row: MigrationRow): string {
    if (row.outcome === 'failed_old_key') return '：这条笔记不是用刚才那个旧密码加密的'
    return row.message === undefined ? '：提交失败，可重试' : `：提交失败（${row.message}）`
  }

  let info = $state<ServerInfo | null>(null)
  let loading = $state(true)
  let loadError = $state('')
  let rows = $derived(info === null ? [] : buildRows(info))

  // 用户名
  let name = $state($settings.name)
  let nameField = $state('')
  let nameBusy = $state(false)

  async function submitName(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    nameField = ''
    nameBusy = true
    try {
      await saveName(name)
      toast('用户名已更新')
    } catch (cause) {
      nameField =
        cause instanceof ApiRequestError && cause.fields?.name !== undefined
          ? cause.fields.name
          : message(cause)
    } finally {
      nameBusy = false
    }
  }

  // 主密码
  let configured = $state(false)
  let question = $state('')
  let encryptedCount = $state(0)
  let questionEditing = $state(false)
  let questionDraft = $state('')
  let questionField = $state('')
  let questionBusy = $state(false)

  async function submitQuestion(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    questionField = ''
    const trimmed = questionDraft.trim()
    if (trimmed === '') {
      questionField = '请填写助记问题'
      return
    }
    questionBusy = true
    try {
      const updated = await updateMasterQuestion(trimmed)
      question = updated.question
      questionEditing = false
      questionDraft = ''
      toast('助记问题已更新')
    } catch (cause) {
      // 服务端拒绝时界面上显示的仍是旧问题，只有输入框标红
      questionField =
        cause instanceof ApiRequestError && cause.fields?.question !== undefined
          ? cause.fields.question
          : message(cause)
    } finally {
      questionBusy = false
    }
  }

  let formOpen = $state(false)
  let oldPassword = $state('')
  let newPassword = $state('')
  let confirm = $state('')
  let newQuestion = $state('')
  let acknowledged = $state(false)
  let pwdFields = $state<Record<string, string>>({})
  let pwdError = $state('')
  let busy = $state(false)
  let progress = $state<{ done: number; total: number } | null>(null)
  let report = $state<MigrationReport | null>(null)
  let remaining = $state(0)

  const failing = $derived(
    report === null ? [] : report.rows.filter((row) => row.outcome === 'failed_old_key' || row.outcome === 'failed_network'),
  )
  const canSubmitPassword = $derived(
    !busy && newPassword !== '' && confirm !== '' && acknowledged && (encryptedCount === 0 || oldPassword !== ''),
  )

  function closePasswordForm(): void {
    formOpen = false
    pwdFields = {}
    pwdError = ''
    report = null
    remaining = 0
  }

  async function submitPassword(event: SubmitEvent): Promise<void> {
    event.preventDefault()
    pwdFields = {}
    pwdError = ''
    report = null
    remaining = 0

    if (newPassword !== confirm) pwdFields.confirm = '两次输入的主密码不一致'
    if (encryptedCount > 0 && oldPassword === '') pwdFields.old = '请输入当前主密码'
    if (!acknowledged) pwdFields.ack = '请先勾选确认'
    if (Object.keys(pwdFields).length > 0) return

    busy = true
    progress = { done: 0, total: encryptedCount }
    try {
      const master = await getMasterPassword()
      if (!master.configured) throw new Error('尚未设置主密码，无法修改')

      const items = (await listEncryptedCiphers()).notes
      // 旧密码只在库里确有加密笔记时才需要：没有密文就没有可验证、可迁移的东西
      const oldKey = items.length > 0 ? await deriveKey(oldPassword, master.salt_b64, KDF_PARAMS) : null
      if (oldKey !== null && !(await verifyOldPassword(items, oldKey, deps))) {
        // 验证失败：会话密钥、库里密文都不动
        pwdFields.old = '主密码不正确或加密数据已损坏'
        return
      }

      const newKey = await deriveKey(newPassword, master.salt_b64, KDF_PARAMS)
      const result = await runMigration(items, oldKey ?? newKey, newKey, deps, () => {
        if (progress !== null) progress = { done: progress.done + 1, total: items.length }
      })
      sessionKey.set(newKey)
      // 迁移已经落库：报告先落地，后面两个附带步骤失败都不能让界面以为整次改密没成功
      report = result
      remaining = result.remaining

      const trimmedQuestion = newQuestion.trim()
      if (trimmedQuestion !== '') {
        try {
          const updated = await updateMasterQuestion(trimmedQuestion)
          question = updated.question
          newQuestion = ''
        } catch (cause) {
          pwdError = `主密码已更新，但助记问题没改成功：${message(cause)}`
        }
      }

      // 复核：库里还剩几条新密钥解不开；清单请求失败就沿用迁移报告的计数
      try {
        const recheck = (await listEncryptedCiphers()).notes
        encryptedCount = recheck.length
        remaining = await countRemaining(recheck, newKey, deps, result.remaining)
      } catch {
        // 不去改动已经成立的结论，迁移状态卡照常按报告计数显示
      }

      if (remaining === 0) {
        toast(`主密码已更新，${result.done} 条笔记已重新加密`)
        oldPassword = ''
        newPassword = ''
        confirm = ''
        acknowledged = false
        if (pwdError === '') closePasswordForm()
      } else if (needsPreviousPassword(result)) {
        pwdError = `还有 ${remaining} 条笔记仍受上一个主密码保护，点「继续迁移」后请输入上一个主密码。`
      } else {
        pwdError = `还有 ${remaining} 条笔记没迁移完，可以点「继续迁移」重试。`
      }
    } catch (cause) {
      // 只有「迁移没跑完」才会走到这里：报告还没落地时保持原样，界面报失败是对的
      pwdError = message(cause)
    } finally {
      busy = false
      progress = null
    }
  }

  // 中途退出后接着跑：全是提交失败时新密码此刻就是旧密码；
  // 但 failed_old_key 那几条仍受上一代口令保护，预填新密码等于让用户再撞一次同样的失败。
  const previousNeeded = $derived(report !== null && needsPreviousPassword(report))

  function resume(): void {
    oldPassword = previousNeeded ? '' : newPassword === '' ? oldPassword : newPassword
    newPassword = ''
    confirm = ''
    formOpen = true
    pwdError = ''
  }

  $effect(() => {
    (async () => {
      try {
        const [serverInfo, master, inventory] = await Promise.all([
          getInfo(),
          getMasterPassword(),
          listEncryptedCiphers(),
        ])
        info = serverInfo
        configured = master.configured
        question = master.question
        encryptedCount = inventory.notes.length
      } catch (cause) {
        loadError = message(cause)
      } finally {
        loading = false
      }
    })()
  })
</script>

<section class="page info-page">
  <h1>用户与信息</h1>

  {#if loading}
    <p class="muted">载入中…</p>
  {:else if loadError !== ''}
    <p class="form-error" role="alert">{loadError}</p>
  {:else}
    <div class="card info-card">
      <h2>运行信息</h2>
      <dl class="ledger-rows info-rows">
        {#each rows as row (row.label)}
          <div class="ledger-row">
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        {/each}
      </dl>
    </div>

    <div class="card info-card">
      <h2>用户名</h2>
      <p class="muted">只用于本机显示，改完刷新仍在。</p>
      <form class="stack-form" onsubmit={submitName}>
        <div>
          <label for="info-name">用户名</label>
          <input id="info-name" type="text" maxlength="40" bind:value={name} />
          {#if nameField !== ''}
            <p class="field-error" role="alert">{nameField}</p>
          {/if}
        </div>
        <button class="button" type="submit" disabled={nameBusy || name.trim() === ''}>保存用户名</button>
      </form>
    </div>

    <div class="card info-card">
      <h2>主密码</h2>
      {#if !configured}
        <p class="muted">还没有设置主密码。加密笔记需要先设置一次主密码。</p>
        <div class="actions">
          <a class="button" href="/master-password">去设置主密码</a>
        </div>
      {:else}
        <p class="hint">助记问题：{question === '' ? '（未设置）' : question}</p>
        <p class="muted">本机有 {encryptedCount} 条加密笔记。</p>

        {#if questionEditing}
          <form class="stack-form" onsubmit={submitQuestion}>
            <div>
              <label for="info-question">新的助记问题</label>
              <input id="info-question" type="text" maxlength="100" bind:value={questionDraft} />
              <p class="muted">忘记主密码时只会显示这个问题，永远不会显示密码。</p>
              {#if questionField !== ''}
                <p class="field-error" role="alert">{questionField}</p>
              {/if}
            </div>
            <div class="actions">
              <button class="button" type="submit" disabled={questionBusy || questionDraft.trim() === ''}>
                保存问题
              </button>
              <button
                class="button button-quiet"
                type="button"
                onclick={() => {
                  questionEditing = false
                  questionDraft = ''
                  questionField = ''
                }}>取消</button>
            </div>
          </form>
        {:else}
          <div class="actions">
            <button class="button button-quiet" type="button" onclick={() => (questionEditing = true)}>
              修改助记问题
            </button>
            {#if !formOpen}
              <button class="button" type="button" onclick={() => (formOpen = true)}>修改主密码</button>
            {/if}
          </div>
        {/if}

        {#if formOpen}
          <form class="stack-form" onsubmit={submitPassword}>
            {#if encryptedCount > 0}
              <div>
                <label for="pwd-old">当前主密码</label>
                <input id="pwd-old" type="password" autocomplete="current-password" bind:value={oldPassword} />
                <p class="muted">用它解开现有密文；填错不会改动任何笔记。</p>
                {#if pwdFields.old}
                  <p class="field-error" role="alert">{pwdFields.old}</p>
                {/if}
              </div>
            {:else}
              <p class="muted">本机没有加密笔记，这次只需要设定新主密码。</p>
            {/if}

            <div>
              <label for="pwd-new">新主密码</label>
              <input id="pwd-new" type="password" autocomplete="new-password" bind:value={newPassword} />
              <p class="muted">建议使用 12 个字符以上的长口令：密钥由它派生，弱口令会被离线爆破。</p>
            </div>
            <div>
              <label for="pwd-confirm">确认新主密码</label>
              <input id="pwd-confirm" type="password" autocomplete="new-password" bind:value={confirm} />
              {#if pwdFields.confirm}
                <p class="field-error" role="alert">{pwdFields.confirm}</p>
              {/if}
            </div>
            <div>
              <label for="pwd-question">新的助记问题（可留空）</label>
              <input id="pwd-question" type="text" maxlength="100" bind:value={newQuestion} />
            </div>

            <label class="ack">
              <input type="checkbox" bind:checked={acknowledged} />
              <span>我已知晓：改密会把所有加密笔记用新密码重新加密，过程中请勿关闭页面。</span>
            </label>
            {#if pwdFields.ack}
              <p class="field-error" role="alert">{pwdFields.ack}</p>
            {/if}

            {#if progress !== null}
              <p role="status" class="muted">正在重新加密 {progress.done} / {progress.total}…</p>
            {/if}

            <div class="actions">
              <button class="button" type="submit" disabled={!canSubmitPassword}>
                {busy ? '迁移中…' : '修改主密码'}
              </button>
              {#if !busy}
                <button class="button button-quiet" type="button" onclick={closePasswordForm}>取消</button>
              {/if}
            </div>
            {#if pwdError !== ''}
              <p class="form-error" role="alert">{pwdError}</p>
            {/if}
          </form>
        {/if}
      {/if}
    </div>

    {#if report !== null && remaining > 0}
      <div class="card info-card">
        <h2>迁移状态</h2>
        <p class="muted">
          成功 {report.done} · 跳过 {report.skipped} · 失败 {report.failed}（还有 {remaining} 条未迁移）
        </p>
        <ul class="migration-rows">
          {#each failing as row (row.id)}
            <li>
              <code>{row.id}</code>
              <span>{outcomeText(row)}</span>
            </li>
          {/each}
        </ul>
        <div class="actions">
          <button class="button" type="button" onclick={resume}>
            {previousNeeded ? '继续迁移（请输入上一个主密码）' : '继续迁移'}
          </button>
        </div>
      </div>
    {/if}
  {/if}
</section>
