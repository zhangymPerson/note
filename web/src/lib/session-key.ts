// 会话密钥只存在于页面内存：不落 localStorage、不进数据库、不写日志。
// 刷新页面即失效，需要重新输入主密码（设计文档 §2）。

import { writable, type Readable } from 'svelte/store'

import { deriveKey, DecryptError, KDF_PARAMS } from './crypto'

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

export function lockSession(): void {
  sessionKey.set(null)
}
