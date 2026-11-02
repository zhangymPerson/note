-- v7 速记分类 + 同分类标题唯一。
-- created_at 与其它表约定一致：UTC 秒级文本；种子行沿用 0004 的常量写法。
--
-- 「速记」（id=2）是第二个固定分类：不可改名、不可删除，守卫在应用层
-- （internal/store 的 IsReservedCategory → ErrReservedCategory）。

INSERT INTO categories (id, name, created_at) VALUES (2, '速记', '2000-01-01T00:00:00Z');

-- 存量重名清理：唯一索引的前置条件。
-- 分组键 = (category_id, 标题 NOCASE)，未分类（category_id IS NULL）自成一组。
-- 每组保留一条原名，顺序：活着的优先 → updated_at 新优先 → id 大优先；
-- 其余追加 " (序号)"，若该名字在本分类里已被占用，退化成 " (序号 · id 前 8 位)"。
-- 两种后缀在组内两两不同；改名后按 200 字符截断（应用层上限，DB 不强制）。
-- 注意 category_id 的比较必须用 IS（NULL 与 NULL 相等），不能用 =。
WITH ordered AS (
    SELECT id, title, category_id,
           ROW_NUMBER() OVER (
               PARTITION BY category_id, title COLLATE NOCASE
               ORDER BY (deleted_at IS NULL) DESC, updated_at DESC, id DESC
           ) AS rn
    FROM notes
),
plan AS (
    SELECT id, title, category_id, rn, title || ' (' || rn || ')' AS candidate
    FROM ordered
    WHERE rn > 1
),
renamed AS (
    SELECT p.id,
           CASE
               WHEN EXISTS (
                   SELECT 1 FROM notes x
                   WHERE x.id <> p.id
                     AND x.category_id IS p.category_id
                     AND x.title = p.candidate COLLATE NOCASE
               )
               THEN substr(p.title, 1, 200 - length(' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'))
                    || ' (' || p.rn || ' · ' || substr(p.id, 1, 8) || ')'
               ELSE substr(p.title, 1, 200 - length(' (' || p.rn || ')'))
                    || ' (' || p.rn || ')'
           END AS new_title
    FROM plan p
)
UPDATE notes
SET title = (SELECT new_title FROM renamed WHERE renamed.id = notes.id)
WHERE id IN (SELECT id FROM renamed);

-- 唯一索引包含回收站里的行：恢复笔记因此永远不会撞名（回收站里的笔记不可编辑，
-- 一旦能撞名用户就没有解除手段）。未分类存的是 category_id IS NULL，而 NULL 在
-- 普通唯一索引里互不相等，所以那一组必须单独用 partial index。
CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_category
    ON notes(title COLLATE NOCASE, category_id) WHERE category_id IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_notes_title_uncategorized
    ON notes(title COLLATE NOCASE) WHERE category_id IS NULL;
