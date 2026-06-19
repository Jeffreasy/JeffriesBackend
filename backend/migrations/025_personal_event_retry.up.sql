-- Retry/dead-letter bookkeeping for pending Google Calendar operations so a
-- permanently-failing op (bad calendar id, deleted target, malformed time) is
-- capped at PendingFailed instead of being retried on every 5-minute tick.
-- Applied at boot by EnsureRuntimeSchema (ensurePersonalEventRetrySchema).
ALTER TABLE personal_events
    ADD COLUMN IF NOT EXISTS attempts        INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error      TEXT,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;

-- The assistant historically staged AI-created appointments with kalender='AI',
-- which is not a real Google calendar id and 404'd on push. Resolve to the
-- primary-calendar alias.
UPDATE personal_events SET kalender = 'Main' WHERE kalender = 'AI';
