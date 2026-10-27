// 浏览器端笔记加密：PBKDF2-HMAC-SHA256 派生 AES-256-GCM 密钥，密文自描述，
// 密码原文与派生密钥都永不离开浏览器、永不落盘（见设计文档 §2）。

export interface KdfParams {
  algo: 'pbkdf2'
  prf: 'HMAC-SHA256'
  iter: number
  keylen: number
}

export const KDF_PARAMS: KdfParams = {
  algo: 'pbkdf2',
  prf: 'HMAC-SHA256',
  iter: 150_000,
  keylen: 32,
}

const MIN_ITER = 100_000
const MAX_ITER = 1_000_000
const NONCE_BYTES = 12
const AAD_PREFIX = 'note:v1:'
const SALT_BYTES = 16

/** 所有解密失败（密码错误、密文损坏、id 不匹配）都归并为同一个错误。 */
export class DecryptError extends Error {
  constructor() {
    super('密码错误或数据损坏')
    this.name = 'DecryptError'
  }
}

// 密码按 NFC 规范化：同一密码在浏览器、CLI、未来实现里派生出同一个密钥。
export function normalizePassword(password: string): string {
  return password.normalize('NFC')
}

export function bytesToB64(bytes: Uint8Array): string {
  let binary = ''
  for (const byte of bytes) binary += String.fromCharCode(byte)
  return btoa(binary)
}

export function b64ToBytes(value: string) {
  const binary = atob(value)
  const bytes = new Uint8Array(binary.length)
  for (let i = 0; i < binary.length; i += 1) bytes[i] = binary.charCodeAt(i)
  return bytes
}

export function generateSaltB64(): string {
  const salt = new Uint8Array(SALT_BYTES)
  crypto.getRandomValues(salt)
  return bytesToB64(salt)
}

export async function deriveKey(
  password: string,
  saltB64: string,
  params: KdfParams,
): Promise<CryptoKey> {
  if (params.iter < MIN_ITER || params.iter > MAX_ITER || params.keylen !== 32) {
    throw new DecryptError()
  }

  const baseKey = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(normalizePassword(password)),
    'PBKDF2',
    false,
    ['deriveKey'],
  )

  return crypto.subtle.deriveKey(
    { name: 'PBKDF2', salt: b64ToBytes(saltB64), iterations: params.iter, hash: 'SHA-256' },
    baseKey,
    { name: 'AES-GCM', length: 256 },
    false,
    ['encrypt', 'decrypt'],
  )
}

function aadFor(noteId: string) {
  return new TextEncoder().encode(AAD_PREFIX + noteId)
}

/** 返回 nonce(12) || ciphertext || tag(16) 的 base64；GCM 标签保证校验。 */
export async function encryptNoteContent(
  key: CryptoKey,
  noteId: string,
  content: string,
): Promise<string> {
  const nonce = new Uint8Array(NONCE_BYTES)
  crypto.getRandomValues(nonce)

  const sealed = await crypto.subtle.encrypt(
    { name: 'AES-GCM', iv: nonce, additionalData: aadFor(noteId), tagLength: 128 },
    key,
    new TextEncoder().encode(content),
  )

  const cipher = new Uint8Array(sealed)
  const joined = new Uint8Array(nonce.length + cipher.length)
  joined.set(nonce, 0)
  joined.set(cipher, nonce.length)
  return bytesToB64(joined)
}

export async function decryptNoteContent(
  key: CryptoKey,
  noteId: string,
  cipherB64: string,
): Promise<string> {
  try {
    const raw = b64ToBytes(cipherB64)
    if (raw.length <= NONCE_BYTES) throw new DecryptError()

    const plain = await crypto.subtle.decrypt(
      {
        name: 'AES-GCM',
        iv: raw.subarray(0, NONCE_BYTES),
        additionalData: aadFor(noteId),
        tagLength: 128,
      },
      key,
      raw.subarray(NONCE_BYTES),
    )
    return new TextDecoder().decode(plain)
  } catch {
    // atob 抛的 InvalidCharacterError 等一律归并为同一个用户可见错误
    throw new DecryptError()
  }
}
