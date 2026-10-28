-- v6 笔记逻辑删除（软删除 + 回收站）。
-- deleted_at 与其它时间戳列一致：UTC 秒级文本 2006-01-02T15:04:05Z，NULL = 未删除。
--
-- 语义：DELETE /api/notes/{id} 只写 deleted_at，笔记正文/密文原样留在库里，
-- 因此「删除即清页」的安全承诺只对彻底删除（purge）成立——那条路径仍是
-- DELETE FROM notes + PRAGMA wal_checkpoint(TRUNCATE)。
--
-- 不修改既有迁移与 notes 的 CHECK 约束：软删除行仍满足「明文/密文二选一」。
-- rev 与 updated_at 在软删除、恢复时都不变（不是内容修订，也不该让回收站里的
-- 笔记在按更新时间排序时跳位）。

ALTER TABLE notes ADD COLUMN deleted_at TEXT;

-- 默认列表与搜索：只扫未删除行
CREATE INDEX IF NOT EXISTS idx_notes_active
    ON notes(updated_at DESC) WHERE deleted_at IS NULL;

-- 回收站列表：按移入时间倒序
CREATE INDEX IF NOT EXISTS idx_notes_deleted_at
    ON notes(deleted_at DESC) WHERE deleted_at IS NOT NULL;
