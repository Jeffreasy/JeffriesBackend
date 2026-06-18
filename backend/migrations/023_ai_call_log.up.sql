-- AI call observability: one row per Grok chat / web-search interaction.
-- NOTE: the application applies schema via store.EnsureRuntimeSchema at boot
-- (ensureAICallLogSchema); this file mirrors that for human/tooling reference.
CREATE TABLE IF NOT EXISTS ai_call_log (
    id                BIGSERIAL PRIMARY KEY,
    user_id           TEXT        NOT NULL,
    agent_id          TEXT        NOT NULL DEFAULT '',
    model             TEXT        NOT NULL DEFAULT '',
    kind              TEXT        NOT NULL DEFAULT 'chat', -- chat | web_search
    prompt_tokens     INTEGER     NOT NULL DEFAULT 0,
    completion_tokens INTEGER     NOT NULL DEFAULT 0,
    total_tokens      INTEGER     NOT NULL DEFAULT 0,
    rounds            INTEGER     NOT NULL DEFAULT 0,
    duration_ms       INTEGER     NOT NULL DEFAULT 0,
    tools_used        TEXT        NOT NULL DEFAULT '',
    finish_reason     TEXT        NOT NULL DEFAULT '',
    ok                BOOLEAN     NOT NULL DEFAULT TRUE,
    error             TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ai_call_log_created_at ON ai_call_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_call_log_user_created ON ai_call_log (user_id, created_at DESC);
