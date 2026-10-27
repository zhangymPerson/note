// 笔记与分类的前端 API 客户端：只搬运 JSON，密文对客户端同样是黑盒。
import { deleteJSON, getJSON, postJSON, putJSON } from './api'

export interface NotePayload {
  title: string
  content?: string | null
  cipher_b64?: string
  kdf_params?: string
  category_id?: number | null
  id?: string
}

export interface NoteMeta {
  id: string
  title: string
  encrypted: boolean
  category_id: number | null
  category_name: string | null
  kind: string
  remark: string
  rev: number
  content: string | null
  cipher_b64?: string
  kdf_params?: string
  created_at: string
  updated_at: string
}

// 列表项：后端只给截断后的摘要，加密笔记连摘要都没有。
export interface NoteListItem {
  id: string
  title: string
  encrypted: boolean
  content?: string
  category_id: number | null
  category_name?: string
  updated_at: string
}

export interface NotePage {
  items: NoteListItem[]
  total: number
  page: number
  per_page: number
}

export interface Category {
  id: number
  name: string
  note_count: number
}

export interface MasterPasswordState {
  configured: boolean
  salt_b64: string
  question: string
}

export interface ListNotesFilter {
  q?: string
  category?: number
  page?: number
}

// 加密笔记的 AAD 绑定笔记 id，所以新建时 id 必须先在浏览器生成，才能加密后再提交。
export function newClientNoteID(): string {
  return crypto.randomUUID()
}

// 与后端 maxContentRunes 同步的正文上限；按 Unicode 码点数（Array.from），代理对算一个字，
// 这样 emoji / 生僻字在 Go 的 utf8.RuneCountInString 与这里的判定一致。
export const MAX_CONTENT_CODEPOINTS = 2000

export function countCodePoints(value: string): number {
  return Array.from(value).length
}

// 只把非空条件拼进查询串，避免后端把空值当条件。
export function listNotes(filter: ListNotesFilter): Promise<NotePage> {
  const params = new URLSearchParams()
  if (filter.q) params.set('q', filter.q)
  if (filter.category) params.set('category', String(filter.category))
  if (filter.page) params.set('page', String(filter.page))
  const query = params.toString()
  return getJSON<NotePage>(query ? `/api/notes?${query}` : '/api/notes')
}

export function createNote(payload: NotePayload): Promise<NoteMeta> {
  return postJSON<NoteMeta>('/api/notes', payload)
}

export function getNote(id: string): Promise<NoteMeta> {
  return getJSON<NoteMeta>(`/api/notes/${encodeURIComponent(id)}`)
}

export function updateNote(id: string, payload: NotePayload): Promise<NoteMeta> {
  return putJSON<NoteMeta>(`/api/notes/${encodeURIComponent(id)}`, payload)
}

export function deleteNote(id: string): Promise<void> {
  return deleteJSON(`/api/notes/${encodeURIComponent(id)}`)
}

export function listCategories(): Promise<Category[]> {
  return getJSON<Category[]>('/api/categories')
}

export function createCategory(name: string): Promise<Category> {
  return postJSON<Category>('/api/categories', { name })
}

// 改名后端直接返回最新全量列表，省一次往返。
export function renameCategory(id: number, name: string): Promise<Category[]> {
  return putJSON<Category[]>(`/api/categories/${id}`, { name })
}

export function deleteCategory(id: number): Promise<void> {
  return deleteJSON(`/api/categories/${id}`)
}

export function getMasterPassword(): Promise<MasterPasswordState> {
  return getJSON<MasterPasswordState>('/api/master-password')
}

export function setMasterPassword(saltB64: string, question: string): Promise<void> {
  return postJSON<void>('/api/master-password', { salt_b64: saltB64, question })
}
