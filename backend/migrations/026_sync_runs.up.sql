-- Per-run audit of background sync jobs (gmail, schedule, personal,
-- pending-calendar) so a history of outcomes, latency and failures is visible,
-- not just the latest-snapshot freshness. Applied at boot by EnsureRuntimeSchema
-- (ensureSyncRunsSchema).
CREATE TABLE IF NOT EXISTS sync_runs (
    id          BIGSERIAL   PRIMARY KEY,
    source      TEXT        NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INTEGER     NOT NULL DEFAULT 0,
    ok          BOOLEAN     NOT NULL DEFAULT true,
    error       TEXT
);

CREATE INDEX IF NOT EXISTS idx_sync_runs_source_started ON sync_runs (source, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sync_runs_started ON sync_runs (started_at DESC);
