// 主密码改密的浏览器端迁移引擎（设计文档 §5、§6）。
// 密钥与明文都只在本模块的调用栈里存在；失败行的 message 只放服务端文案，绝不放正文。

import { KDF_PARAMS, DecryptError } from './crypto'

export interface EncryptedItem {
  id: string
  cipher_b64: string
  kdf_params?: string
}

export type MigrationOutcome = 'done' | 'already_done' | 'skipped_gone' | 'failed_old_key' | 'failed_network'

export interface MigrationRow {
  id: string
  outcome: MigrationOutcome
  message?: string
}

export interface MigrationReport {
  rows: MigrationRow[]
  done: number
  skipped: number
  failed: number
  remaining: number
}

export interface ReencryptDeps {
  decrypt: (key: CryptoKey, id: string, cipherB64: string) => Promise<string>
  encrypt: (key: CryptoKey, id: string, plain: string) => Promise<string>
  putCipher: (id: string, cipherB64: string, kdfParams: string) => Promise<void>
  paramsJSON?: string
}

// 迁移途中笔记被删（404）或已不是加密笔记（409）：这条不算失败，用户也不需要重试。
const GONE_CODES = new Set(['not_found', 'not_encrypted'])

function errorCode(error: unknown): string {
  const code = (error as { code?: unknown })?.code
  return typeof code === 'string' ? code : ''
}

/**
 * 旧密码验证：逐条试解，第一条能解开即通过。
 * 不能只试第一条（库里可能混着更早一次改密留下的笔记），也不要求全部可解。
 * 清单为空说明库里没有加密笔记，没有可验证的东西，直接通过。
 */
export async function verifyOldPassword(
  items: EncryptedItem[],
  oldKey: CryptoKey,
  deps: ReencryptDeps,
): Promise<boolean> {
  if (items.length === 0) return true
  for (const item of items) {
    try {
      await deps.decrypt(oldKey, item.id, item.cipher_b64)
      return true
    } catch (error) {
      if (!(error instanceof DecryptError)) throw error
    }
  }
  return false
}

/** 新密钥已经能解开的条目判为已迁移，从待办里排除，使整条流程天然幂等。 */
export async function planMigration(
  items: EncryptedItem[],
  newKey: CryptoKey,
  deps: ReencryptDeps,
): Promise<EncryptedItem[]> {
  const pending: EncryptedItem[] = []
  for (const item of items) {
    try {
      await deps.decrypt(newKey, item.id, item.cipher_b64)
    } catch (error) {
      if (!(error instanceof DecryptError)) throw error
      pending.push(item)
    }
  }
  return pending
}

export async function runMigration(
  items: EncryptedItem[],
  oldKey: CryptoKey,
  newKey: CryptoKey,
  deps: ReencryptDeps,
  onProgress?: (row: MigrationRow) => void,
): Promise<MigrationReport> {
  const paramsJSON = deps.paramsJSON ?? JSON.stringify(KDF_PARAMS)
  const pending = new Set((await planMigration(items, newKey, deps)).map((item) => item.id))

  const rows: MigrationRow[] = []
  const report: MigrationReport = { rows, done: 0, skipped: 0, failed: 0, remaining: 0 }
  const bump = (row: MigrationRow) => {
    if (row.outcome === 'done') report.done += 1
    else if (row.outcome === 'failed_old_key' || row.outcome === 'failed_network') {
      report.failed += 1
      report.remaining += 1
    } else report.skipped += 1
    rows.push(row)
    onProgress?.(row)
  }

  for (const item of items) {
    if (!pending.has(item.id)) {
      bump({ id: item.id, outcome: 'already_done' })
      continue
    }

    let plain: string
    try {
      plain = await deps.decrypt(oldKey, item.id, item.cipher_b64)
    } catch (error) {
      if (!(error instanceof DecryptError)) throw error
      bump({ id: item.id, outcome: 'failed_old_key' })
      continue
    }

    try {
      const cipher = await deps.encrypt(newKey, item.id, plain)
      await deps.putCipher(item.id, cipher, paramsJSON)
      bump({ id: item.id, outcome: 'done' })
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      bump(
        GONE_CODES.has(errorCode(error))
          ? { id: item.id, outcome: 'skipped_gone', message }
          : { id: item.id, outcome: 'failed_network', message },
      )
    }
  }

  return report
}
