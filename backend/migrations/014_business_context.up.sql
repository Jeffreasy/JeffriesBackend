-- 014_business_context.up.sql
-- Structured business context links for notes and personal agenda events.

ALTER TABLE notes
    ADD COLUMN IF NOT EXISTS business_context_type TEXT,
    ADD COLUMN IF NOT EXISTS business_context_id TEXT,
    ADD COLUMN IF NOT EXISTS business_context_title TEXT;

ALTER TABLE note_revisions
    ADD COLUMN IF NOT EXISTS business_context_type TEXT,
    ADD COLUMN IF NOT EXISTS business_context_id TEXT,
    ADD COLUMN IF NOT EXISTS business_context_title TEXT;

ALTER TABLE personal_events
    ADD COLUMN IF NOT EXISTS business_context_type TEXT,
    ADD COLUMN IF NOT EXISTS business_context_id TEXT,
    ADD COLUMN IF NOT EXISTS business_context_title TEXT;

CREATE INDEX IF NOT EXISTS idx_notes_user_business_context
    ON notes(user_id, business_context_type, business_context_id)
    WHERE business_context_type IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_note_revisions_user_business_context
    ON note_revisions(user_id, business_context_type, business_context_id)
    WHERE business_context_type IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_pe_user_business_context
    ON personal_events(user_id, business_context_type, business_context_id)
    WHERE business_context_type IS NOT NULL;
