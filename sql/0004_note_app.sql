-- v4 笔记应用：分类表、全局主密码表、笔记分类外键。
-- created_at 与 notes 表约定一致：UTC 秒级文本 2006-01-02T15:04:05Z。
--
-- 默认分类固定为 id=1「未分类」：不可删除、不可改名。该守卫在应用层实现
-- （internal/store 分类层返回 ErrReservedCategory），SQLite 触发器会引入
-- 无法向用户回传语义化错误码的复杂度，故不在此处强制。

CREATE TABLE IF NOT EXISTS categories (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    -- 分类名大小写不敏感唯一；长度与空白由应用层校验
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE,
    created_at TEXT NOT NULL
);

INSERT INTO categories (id, name, created_at)
VALUES (1, '未分类', '2000-01-01T00:00:00Z');

CREATE TABLE IF NOT EXISTS master_password (
    -- 单行表：全局只有一个主密码配置
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    -- PBKDF2 的 16 字节随机盐（公开参数，不反推密码）
    salt       BLOB NOT NULL,
    -- 助记问题原文，仅用于提示，不能用于查询或校验密码
    question   TEXT NOT NULL,
    created_at TEXT NOT NULL
);

-- 笔记所属分类；NULL 表示未分类，与 id=1 在展示上等价但无需回填。
-- 加密笔记照常可选分类：分类与标题始终是明文元数据。
ALTER TABLE notes ADD COLUMN category_id INTEGER REFERENCES categories(id);
