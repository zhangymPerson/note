-- v5 使用频率事件口径扩展：activities 从「只记查询」升级为「查询 + 笔记与分类的读写事件」。
-- created_at 与 notes 表约定一致：UTC 秒级文本 2006-01-02T15:04:05Z。
--
-- SQLite 不能就地修改 CHECK 约束，只能重建表：新建 → 拷数据 → 删旧 → 改名。
-- 旧索引随旧表一起被删掉，重建时必须按 0002 的定义补回来（索引名保持不变）。
--
-- kind 枚举里保留 note_restore / note_purge：软删除与回收站上线后无需再重建一次表，
-- 当前没有代码写入这两个值。
--
-- query 列对非搜索事件写空串：保持 NOT NULL，不引入 NULL 三值语义。

CREATE TABLE activities_v5 (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL CHECK (kind IN (
        'search',
        'note_view',
        'note_create',
        'note_update',
        'note_delete',
        'note_restore',
        'note_purge',
        'category_create',
        'category_rename',
        'category_delete'
    )),
    query      TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

INSERT INTO activities_v5 (id, kind, query, created_at)
SELECT id, kind, query, created_at FROM activities;

DROP TABLE activities;

ALTER TABLE activities_v5 RENAME TO activities;

-- 热力图与活跃天数按天分组查询
CREATE INDEX IF NOT EXISTS idx_activities_created_at
    ON activities(created_at);

-- 事件构成按 kind 聚合
CREATE INDEX IF NOT EXISTS idx_activities_kind
    ON activities(kind);

-- 高频查询词统计
CREATE INDEX IF NOT EXISTS idx_activities_query
    ON activities(query);
