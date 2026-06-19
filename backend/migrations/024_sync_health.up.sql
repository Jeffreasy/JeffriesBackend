-- Current sync health for Gmail (UpdatedAt/TotalSynced reflect last SUCCESS only).
-- Applied at boot by EnsureRuntimeSchema (ensureSyncHealthSchema); mirrored here.
ALTER TABLE email_sync_meta
    ADD COLUMN IF NOT EXISTS sync_status     TEXT NOT NULL DEFAULT 'ok',
    ADD COLUMN IF NOT EXISTS last_error      TEXT,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;

-- Bridge liveness heartbeat: bumped on every authenticated /bridge/* call,
-- independent of per-device WiZ UDP reachability.
CREATE TABLE IF NOT EXISTS bridge_heartbeat (
    id        INTEGER     PRIMARY KEY DEFAULT 1,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Retry bookkeeping for device commands so transient failures can requeue.
ALTER TABLE device_commands
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;

-- Atomic once-per-day claim for the scheduled AI briefing (replaces a fragile
-- content-LIKE dedup that could double-fire across ticks/processes).
CREATE TABLE IF NOT EXISTS briefing_sent (
    day     DATE        PRIMARY KEY,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
