-- Completion status for notes. Completed notes remain queryable and visible in
-- journals, but can be separated from active work without archiving them.
ALTER TABLE notes ADD COLUMN IF NOT EXISTS is_completed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_notes_user_completed
    ON notes(user_id, is_completed)
    WHERE NOT is_archived;
