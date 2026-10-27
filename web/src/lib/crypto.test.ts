import { describe, expect, it } from 'vitest'

import {
  DecryptError,
  KDF_PARAMS,
  b64ToBytes,
  bytesToB64,
  deriveKey,
  decryptNoteContent,
  encryptNoteContent,
  generateSaltB64,
  normalizePassword,
} from './crypto'
import { hasKey, sessionKey, unlockKey } from './session-key'

const SALT = bytesToB64(new Uint8Array(16).fill(7))

describe('crypto roundtrip', () => {
  it('derives a key and encrypts then decrypts back to the same text', async () => {
    const key = await deriveKey('密码pàss123…', SALT, KDF_PARAMS)
    const original = '第一行\n带 emoji 🚀 与符号 <>&"\n末行'

    const cipher = await encryptNoteContent(key, 'note-1', original)
    const restored = await decryptNoteContent(key, 'note-1', cipher)

    expect(restored).toBe(original)
  })

  it('rejects a wrong password with the fixed decrypt error', async () => {
    const key = await deriveKey('正确密码', SALT, KDF_PARAMS)
    const cipher = await encryptNoteContent(key, 'note-2', '秘密')

    const wrong = await deriveKey('错误密码', SALT, KDF_PARAMS)
    await expect(decryptNoteContent(wrong, 'note-2', cipher)).rejects.toThrow(DecryptError)
    await expect(decryptNoteContent(wrong, 'note-2', cipher)).rejects.toThrow('密码错误或数据损坏')
  })

  it('rejects tampered ciphertext', async () => {
    const key = await deriveKey('pw', SALT, KDF_PARAMS)
    const cipher = await encryptNoteContent(key, 'note-3', '正文')

    const raw = b64ToBytes(cipher)
    const middle = Math.floor(raw.length / 2)
    raw[middle] = raw[middle] ^ 0xff
    const tampered = bytesToB64(raw)

    await expect(decryptNoteContent(key, 'note-3', tampered)).rejects.toThrow('密码错误或数据损坏')
  })

  it('refuses to move a ciphertext to another note id (AAD binding)', async () => {
    const key = await deriveKey('pw', SALT, KDF_PARAMS)
    const cipher = await encryptNoteContent(key, 'note-a', '正文')

    await expect(decryptNoteContent(key, 'note-b', cipher)).rejects.toThrow('密码错误或数据损坏')
  })

  it('rejects malformed or truncated ciphertext', async () => {
    const key = await deriveKey('pw', SALT, KDF_PARAMS)

    await expect(decryptNoteContent(key, 'note-x', '!!!')).rejects.toThrow(DecryptError)
    await expect(decryptNoteContent(key, 'note-x', '')).rejects.toThrow(DecryptError)
    await expect(decryptNoteContent(key, 'note-x', bytesToB64(new Uint8Array(8)))).rejects.toThrow(
      DecryptError,
    )
  })
})

describe('kdf parameters', () => {
  it('refuses iteration counts outside the allowed band', async () => {
    await expect(
      deriveKey('pw', SALT, { ...KDF_PARAMS, iter: 99999 }),
    ).rejects.toThrow(DecryptError)
    await expect(
      deriveKey('pw', SALT, { ...KDF_PARAMS, iter: 1_000_001 }),
    ).rejects.toThrow(DecryptError)
    await expect(
      deriveKey('pw', SALT, { ...KDF_PARAMS, keylen: 16 }),
    ).rejects.toThrow(DecryptError)
  })

  it('produces the same key for the same password and salt', async () => {
    const a = await deriveKey('相同', SALT, KDF_PARAMS)
    const b = await deriveKey('相同', SALT, KDF_PARAMS)
    const cipher = await encryptNoteContent(a, 'note-y', '文本')

    expect(await decryptNoteContent(b, 'note-y', cipher)).toBe('文本')
  })
})

describe('salt and encoding helpers', () => {
  it('generates distinct 16-byte salts', () => {
    const first = generateSaltB64()
    const second = generateSaltB64()

    expect(b64ToBytes(first)).toHaveLength(16)
    expect(b64ToBytes(second)).toHaveLength(16)
    expect(first).not.toBe(second)
  })

  it('roundtrips arbitrary bytes through base64', () => {
    const bytes = new Uint8Array(Array.from({ length: 256 }, (_, i) => i))

    expect(b64ToBytes(bytesToB64(bytes))).toEqual(bytes)
  })

  it('normalizes the password to NFC', () => {
    // é 的分解形式（e + 组合重音）与预组合形式规范化后必须相等
    const decomposed = 'pass\u0065\u0301'
    const composed = 'pass\u00e9'

    expect(normalizePassword(decomposed)).toBe(normalizePassword(composed))
  })
})

describe('session key holder', () => {
  it('keeps the key in memory only and mirrors hasKey', async () => {
    const salt = generateSaltB64()

    expect(sessionKey.current()).toBeNull()

    let seen: boolean[] = []
    const stop = hasKey.subscribe((value) => seen.push(value))
    expect(seen[0]).toBe(false)

    await unlockKey('会话密码', salt)
    expect(sessionKey.current()).not.toBeNull()
    expect(seen[seen.length - 1]).toBe(true)

    sessionKey.set(null)
    expect(sessionKey.current()).toBeNull()
    expect(seen[seen.length - 1]).toBe(false)

    await expect(unlockKey('会话密码', '')).rejects.toThrow(DecryptError)
    expect(sessionKey.current()).toBeNull()

    stop()
  })
})
