// 编辑草稿只活在这一页的内存里：跳去设置主密码再回来时用它续上，
// 绝不写 localStorage/sessionStorage（设计文档 §5.1）。

export interface DraftNote {
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
  // 取出即清空：同一段明文不该被消费两次。
  take(): DraftNote | null {
    const taken = current
    current = null
    return taken
  },
  clear(): void {
    current = null
  },
}
