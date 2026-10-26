-- v1.1 笔记表扩展字段。
-- SQLite 的 ALTER TABLE ADD COLUMN 只能逐列追加；带 NOT NULL 的列必须给默认值，
-- 存量行自动回填。列级 CHECK 与 0001 的约束风格保持一致。
ALTER TABLE notes ADD COLUMN kind TEXT NOT NULL DEFAULT 'note' CHECK (kind <> '');
-- 笔记类型。取值由应用层维护（如 note/todo/diary/reference），
-- 新增类型不需要迁移；如需强约束，再用后续迁移收紧为 CHECK IN (...)。

ALTER TABLE notes ADD COLUMN remark TEXT NOT NULL DEFAULT '';
-- 备注（纯文本），供用户书写说明性文字，可清空。

ALTER TABLE notes ADD COLUMN extra TEXT
    CHECK (extra IS NULL OR json_valid(extra));
-- 备注/扩展信息（JSON 文本），为未来功能预留：提醒时间、颜色、来源、
-- 内部键值等一律先进这里，稳定后再考虑提升为独立列。NULL 表示无扩展信息。

ALTER TABLE notes ADD COLUMN rev INTEGER NOT NULL DEFAULT 1 CHECK (rev > 0);
-- 修订号：每次编辑由应用 +1，用于乐观并发控制与变更检测。

ALTER TABLE notes ADD COLUMN last_visited_at TEXT;
-- 最近查看时间，UTC 秒级文本 2006-01-02T15:04:05Z；NULL 表示从未打开。
-- 与 activities 统计配合，支撑"最近打开"类排序。
