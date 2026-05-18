-- 007_ai_telegram.up.sql
-- AI chat, pending actions, brain preferences, and Telegram chat messages.

-- ─── Chat Messages ──────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS chat_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id     BIGINT      NOT NULL,
    role        TEXT        NOT NULL CHECK (role IN ('user', 'assistant')),
    content     TEXT        NOT NULL,
    agent_id    TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_chat_messages_chat_id ON chat_messages (chat_id, created_at DESC);

-- ─── AI Pending Actions ─────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS ai_pending_actions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL,
    tool_name   TEXT        NOT NULL,
    args_json   TEXT        NOT NULL DEFAULT '{}',
    summary     TEXT        NOT NULL,
    code        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'cancelled', 'failed', 'expired')),
    result      TEXT,
    error       TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_ai_pending_user_status ON ai_pending_actions (user_id, status, expires_at);

-- ─── Brain Preferences ──────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS brain_preferences (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             TEXT        NOT NULL UNIQUE,
    detail_level        TEXT        NOT NULL DEFAULT 'normaal' CHECK (detail_level IN ('kort', 'normaal', 'uitgebreid')),
    tone                TEXT        NOT NULL DEFAULT 'direct' CHECK (tone IN ('direct', 'warm', 'coachend')),
    proactive_level     TEXT        NOT NULL DEFAULT 'normaal' CHECK (proactive_level IN ('laag', 'normaal', 'hoog')),
    focus_areas         TEXT[]      NOT NULL DEFAULT '{}',
    briefing_time       TEXT,
    quiet_hours_start   TEXT,
    quiet_hours_end     TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
