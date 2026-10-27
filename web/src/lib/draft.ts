// 编辑草稿只活在这一页的内存里：跳去设置主密码再回来时用它续上，
// 绝不写 localStorage/sessionStorage（设计文档 §5.1）。

export interface DraftNote {
  // 草稿属于哪条笔记：'' 表示新建。跳去设置主密码再回来时按同一条笔记续上。
  noteId: string
  title: string
  content: string
  categoryId: number | null
  encrypt: boolean
}

let current: DraftNote | null = null

export const editorDraft = {
  current(): DraftNote | null {
    return current
  },
  save(draft: DraftNote): void {
    current = draft
  },
  // 取出即清空：同一段明文不该被消费两次。别的笔记取不走——把甲的正文灌进乙的编辑器，
  // 保存就是拿甲覆盖乙。
  take(noteId: string): DraftNote | null {
    if (!current || current.noteId !== noteId) return null
    const taken = current
    current = null
    return taken
  },
  clear(): void {
    current = null
  },
}
