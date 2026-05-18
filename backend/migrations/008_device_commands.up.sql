-- 008_device_commands.up.sql
-- Device commands queue — replaces Convex deviceCommands table.

CREATE TABLE IF NOT EXISTS device_commands (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT        NOT NULL,
    device_id   UUID        REFERENCES devices(id) ON DELETE CASCADE,
    command     JSONB       NOT NULL DEFAULT '{}',
    status      TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'done', 'failed')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ
);

CREATE INDEX idx_device_commands_pending ON device_commands (status, created_at) WHERE status = 'pending';
