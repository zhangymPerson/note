CREATE TABLE IF NOT EXISTS notes (
    id           TEXT PRIMARY KEY,
    title        TEXT NOT NULL,
    content      TEXT,
    is_encrypted INTEGER NOT NULL DEFAULT 0 CHECK (is_encrypted IN (0, 1)),
    cipher       BLOB,
    kdf_salt     BLOB,
    kdf_params   TEXT,
    hints        TEXT,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
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

CREATE INDEX IF NOT EXISTS idx_notes_updated_at
    ON notes(updated_at DESC);
