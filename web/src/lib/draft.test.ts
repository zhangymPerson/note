import { describe, expect, it } from 'vitest'

import { editorDraft } from './draft'

describe('editorDraft', () => {
  it('保存后读回，取出即清空（只消费一次）', () => {
    editorDraft.clear()
    expect(editorDraft.current()).toBeNull()

    editorDraft.save({ title: '草稿', content: '正文', categoryId: 2, encrypt: true })
    expect(editorDraft.current()).toMatchObject({ title: '草稿', encrypt: true })

    expect(editorDraft.take()).toMatchObject({ content: '正文' })
    expect(editorDraft.current()).toBeNull()
  })

  it('清空后不留任何明文', () => {
    editorDraft.save({ title: 'a', content: 'b', categoryId: null, encrypt: false })
    editorDraft.clear()
    expect(editorDraft.current()).toBeNull()
  })
})
