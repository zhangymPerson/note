// 会话密钥只存在于页面内存：不落 localStorage、不进数据库、不写日志。
// 刷新页面即失效，需要重新输入主密码（设计文档 §2）。

import { writable, type Readable } from 'svelte/store'

import { decryptNoteContent, deriveKey, DecryptError, KDF_PARAMS } from './crypto'

let current: CryptoKey | null = null

const unlocked = writable(false)

/** hasKey 供顶栏锁定/解锁按钮显隐。 */
export const hasKey: Readable<boolean> = { subscribe: unlocked.subscribe }

export const sessionKey = {
  current(): CryptoKey | null {
    return current
  },
  set(key: CryptoKey | null): void {
    current = key
    unlocked.set(key !== null)
  },
}

/** 用主密码与服务端公开盐值派生会话密钥；盐值为空时抛 DecryptError。 */
export async function unlockKey(password: string, saltB64: string): Promise<void> {
  if (!saltB64) throw new DecryptError()
  const key = await deriveKey(password, saltB64, KDF_PARAMS)
  sessionKey.set(key)
}

/**
 * 解锁并（给了试解密素材时）用当前笔记当场验证：验证不通过就抛 DecryptError 且不写入密钥，
 * 免得一个错密码派生的密钥留在会话里让后面的笔记全都报同一个错。
 */
export async function unlockWithPassword(
  password: string,
  saltB64: string,
  noteId?: string,
  cipherB64?: string,
): Promise<string | null> {
  if (!saltB64) throw new DecryptError()
  const key = await deriveKey(password, saltB64, KDF_PARAMS)

  if (cipherB64 !== undefined && noteId !== undefined) {
    const plain = await decryptNoteContent(key, noteId, cipherB64)
    sessionKey.set(key)
    return plain
  }

  sessionKey.set(key)
  return null
}

export function lockSession(): void {
  sessionKey.set(null)
}
