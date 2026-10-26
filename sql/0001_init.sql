-- v1 笔记表。
-- created_at / updated_at 均为 UTC 秒级文本，固定格式 2006-01-02T15:04:05Z，
-- 便于直接按字符串排序；同一时间以 id DESC 兜底。
CREATE TABLE IF NOT EXISTS notes (
    -- UUID v4 字符串，由应用生成
    id           TEXT PRIMARY KEY,
    -- 标题，始终明文，用于列表展示与搜索
    title        TEXT NOT NULL,
    -- 明文正文；笔记加密后必须为 NULL，正文只保留在 cipher 中
    content      TEXT,
    -- 是否已加密：0=明文，1=加密；加密后不可取消
    is_encrypted INTEGER NOT NULL DEFAULT 0 CHECK (is_encrypted IN (0, 1)),
    -- AES-256-GCM 密文，布局为 nonce(12) || ciphertext || tag(16)
    cipher       BLOB,
    -- Argon2id 的 16 字节随机盐
    kdf_salt     BLOB,
    -- Argon2id 参数 JSON（内存、迭代次数、并行度、密钥长度）；参数非法时无法解密
    kdf_params   TEXT,
    -- 密码提示 JSON 数组 [{"question":"...","answer":"..."}]；
    -- 明文保存，仅辅助记忆，不参与解密；无提示时为 NULL
    hints        TEXT,
    -- 创建时间，UTC
    created_at   TEXT NOT NULL,
    -- 最后更新时间，UTC；列表按此字段倒序
    updated_at   TEXT NOT NULL,
    -- 明文与加密字段必须成对出现：
    -- 明文笔记只允许 content，加密笔记只允许 cipher/kdf_salt/kdf_params
    CHECK (
        (is_encrypted = 0
            AND content IS NOT NULL
            AND cipher IS NULL
            AND kdf_salt IS NULL
            AND kdf_params IS NULL
            AND hints IS NULL)
        OR
        (is_encrypted = 1
            AND content IS NULL
            AND cipher IS NOT NULL
            AND kdf_salt IS NOT NULL
            AND kdf_params IS NOT NULL)
    )
);

-- 列表默认按更新时间倒序
CREATE INDEX IF NOT EXISTS idx_notes_updated_at
    ON notes(updated_at DESC);
