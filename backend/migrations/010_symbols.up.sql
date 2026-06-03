-- 010_symbols.up.sql
-- First-class symbols for notes and personal calendar events.

ALTER TABLE notes ADD COLUMN IF NOT EXISTS symbol TEXT;
ALTER TABLE personal_events ADD COLUMN IF NOT EXISTS symbol TEXT;

CREATE INDEX IF NOT EXISTS idx_notes_user_symbol
    ON notes(user_id, symbol)
    WHERE symbol IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_pe_user_symbol
    ON personal_events(user_id, symbol)
    WHERE symbol IS NOT NULL;
