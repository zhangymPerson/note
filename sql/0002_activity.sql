-- v1 使用活动表：记录每次「查询」（首页搜索）行为，用于统计与频率热力图。
-- created_at 与 notes 表约定一致：UTC 秒级文本 2006-01-02T15:04:05Z。
CREATE TABLE IF NOT EXISTS activities (
    -- 自增 id，仅用于排序
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    -- 活动类型；当前只有 search，后续扩展不破坏既有数据
    kind       TEXT NOT NULL CHECK (kind IN ('search')),
    -- 查询关键词原文（本地单用户，不上网、不加密存储查询串本身）
    query      TEXT NOT NULL,
    -- 发生时间，UTC
    created_at TEXT NOT NULL
);

-- 热力图按天分组查询
CREATE INDEX IF NOT EXISTS idx_activities_created_at
    ON activities(created_at);

-- 高频查询词统计
CREATE INDEX IF NOT EXISTS idx_activities_query
    ON activities(query);
