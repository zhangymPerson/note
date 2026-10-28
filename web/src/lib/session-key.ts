// 会话密钥只存在于页面内存：不落 localStorage、不进数据库、不写日志。
// 刷新页面即失效，需要重新输入主密码（设计文档 §2）。

import { writable, type Readable } from "svelte/store";

import {
  decryptNoteContent,
  deriveKey,
  DecryptError,
  KDF_PARAMS,
  type KdfParams,
} from "./crypto";

let current: CryptoKey | null = null;

const unlocked = writable(false);

/** hasKey 供顶栏锁定/解锁按钮显隐。 */
export const hasKey: Readable<boolean> = { subscribe: unlocked.subscribe };

export const sessionKey = {
  current(): CryptoKey | null {
    return current;
  },
  set(key: CryptoKey | null): void {
    current = key;
    unlocked.set(key !== null);
  },
};

/** 用主密码与服务端公开盐值派生会话密钥；盐值为空时抛 DecryptError。 */
export async function unlockKey(
  password: string,
  saltB64: string,
  params: KdfParams = KDF_PARAMS,
): Promise<void> {
  if (!saltB64) throw new DecryptError();
  const key = await deriveKey(password, saltB64, params);
  sessionKey.set(key);
}

/**
 * 解锁并（给了试解密素材时）用当前笔记当场验证：验证不通过就抛 DecryptError 且不写入密钥，
 * 免得一个错密码派生的密钥留在会话里让后面的笔记全都报同一个错。
 * params 取自这条笔记自己声明的 kdf_params，缺省为当前应用参数。
 */
export async function unlockWithPassword(
  password: string,
  saltB64: string,
  noteId?: string,
  cipherB64?: string,
  params: KdfParams = KDF_PARAMS,
): Promise<string | null> {
  if (!saltB64) throw new DecryptError();
  const key = await deriveKey(password, saltB64, params);

  if (cipherB64 !== undefined && noteId !== undefined) {
    const plain = await decryptNoteContent(key, noteId, cipherB64);
    sessionKey.set(key);
    return plain;
  }

  sessionKey.set(key);
  return null;
}

/**
 * 首次加密没有可试解密的密文，一个手滑的密码会派生出永远对不上的密钥并锁死这条笔记，
 * 所以这条路必须两次输入一致。一致时返回 null，否则返回给输入框显示的文案。
 */
export function passwordMismatch(
  password: string,
  confirm: string,
): string | null {
  if (confirm === "") return "请再输入一次主密码";
  if (password !== confirm) return "两次输入的主密码不一致";
  return null;
}

// 锁定即清掉内存草稿：里面可能已经装着解密出来的正文。
import { editorDraft } from "./draft";

export function lockSession(): void {
  sessionKey.set(null);
  editorDraft.clear();
}
