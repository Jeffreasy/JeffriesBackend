-- 003: Emails + Email Sync Meta tables
-- Gmail metadata + snippet storage (replaces Convex emails + emailSyncMeta tables)

CREATE TABLE IF NOT EXISTS emails (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT        NOT NULL,
    gmail_id        TEXT        NOT NULL,
    thread_id       TEXT        NOT NULL,

    -- Headers
    from_addr       TEXT        NOT NULL DEFAULT '',
    to_addr         TEXT        NOT NULL DEFAULT '',
    cc              TEXT,
    bcc             TEXT,
    subject         TEXT        NOT NULL DEFAULT '(geen onderwerp)',
    snippet         TEXT        NOT NULL DEFAULT '',

    -- Timestamps
    datum           DATE        NOT NULL,
    ontvangen       BIGINT      NOT NULL DEFAULT 0,   -- Unix ms (internalDate)

    -- Status flags
    is_gelezen      BOOLEAN     NOT NULL DEFAULT false,
    is_ster         BOOLEAN     NOT NULL DEFAULT false,
    is_verwijderd   BOOLEAN     NOT NULL DEFAULT false,
    is_draft        BOOLEAN     NOT NULL DEFAULT false,

    -- Labels
    label_ids       TEXT[]      NOT NULL DEFAULT '{}',
    categorie       TEXT        DEFAULT 'primary',

    -- Attachments
    heeft_bijlagen  BOOLEAN     NOT NULL DEFAULT false,
    bijlagen_count  INT         NOT NULL DEFAULT 0,

    -- Full-text search
    search_text     TEXT        NOT NULL DEFAULT '',

    -- Sync tracking
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Deduplication
    UNIQUE (user_id, gmail_id)
);

-- Performance indexes (mirroring Convex indexes)
CREATE INDEX IF NOT EXISTS idx_emails_user           ON emails (user_id);
CREATE INDEX IF NOT EXISTS idx_emails_user_datum     ON emails (user_id, datum DESC);
CREATE INDEX IF NOT EXISTS idx_emails_user_thread    ON emails (user_id, thread_id);
CREATE INDEX IF NOT EXISTS idx_emails_user_gelezen   ON emails (user_id, is_gelezen);
CREATE INDEX IF NOT EXISTS idx_emails_user_categorie ON emails (user_id, categorie);
CREATE INDEX IF NOT EXISTS idx_emails_user_verwijderd ON emails (user_id, is_verwijderd);

-- GIN index for full-text search (replaces Convex searchIndex)
CREATE INDEX IF NOT EXISTS idx_emails_search ON emails USING GIN (to_tsvector('dutch', search_text));

-- ─── Email Sync Meta ─────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS email_sync_meta (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT        NOT NULL UNIQUE,
    history_id      TEXT        NOT NULL DEFAULT '',
    last_full_sync  TIMESTAMPTZ,
    total_synced    INT         NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
