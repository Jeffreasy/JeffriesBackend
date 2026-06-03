package store

import (
	"context"
	"fmt"
)

// EnsureRuntimeSchema applies narrowly scoped, idempotent schema repairs that
// the API needs before it can safely accept runtime work on Render.
func EnsureRuntimeSchema(ctx context.Context, db *DB) error {
	if err := ensureDeviceCommandSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure device command schema: %w", err)
	}
	if err := ensureSymbolSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure symbol schema: %w", err)
	}
	if err := ensureNoteRevisionSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure note revision schema: %w", err)
	}
	return nil
}

func ensureNoteRevisionSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS note_revisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id         UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    user_id         TEXT NOT NULL,
    titel           TEXT,
    inhoud          TEXT NOT NULL DEFAULT '',
    tags            TEXT[] DEFAULT '{}',
    kleur           TEXT,
    deadline        TIMESTAMPTZ,
    linked_event_id TEXT,
    prioriteit      TEXT,
    symbol          TEXT,
    aangemaakt      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_note_revisions_note_created
    ON note_revisions(note_id, aangemaakt DESC);

CREATE INDEX IF NOT EXISTS idx_note_revisions_user_created
    ON note_revisions(user_id, aangemaakt DESC);
`)
	return err
}

func ensureSymbolSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
ALTER TABLE notes ADD COLUMN IF NOT EXISTS symbol TEXT;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS is_completed BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE notes ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ;
ALTER TABLE personal_events ADD COLUMN IF NOT EXISTS symbol TEXT;

CREATE INDEX IF NOT EXISTS idx_notes_user_symbol
    ON notes(user_id, symbol)
    WHERE symbol IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_notes_user_completed
    ON notes(user_id, is_completed)
    WHERE NOT is_archived;

CREATE INDEX IF NOT EXISTS idx_pe_user_symbol
    ON personal_events(user_id, symbol)
    WHERE symbol IS NOT NULL;
`)
	return err
}

func ensureDeviceCommandSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS device_commands (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      TEXT        NOT NULL,
    device_id    UUID        REFERENCES devices(id) ON DELETE CASCADE,
    command      JSONB       NOT NULL DEFAULT '{}',
    status       TEXT        NOT NULL DEFAULT 'pending',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    claimed_at   TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE device_commands ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;
ALTER TABLE device_commands ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ DEFAULT now();

UPDATE device_commands
   SET updated_at = COALESCE(completed_at, claimed_at, created_at, now())
 WHERE updated_at IS NULL;

ALTER TABLE device_commands ALTER COLUMN updated_at SET DEFAULT now();
ALTER TABLE device_commands ALTER COLUMN updated_at SET NOT NULL;

DO $$
DECLARE
    status_constraint text;
BEGIN
    FOR status_constraint IN
        SELECT c.conname
          FROM pg_constraint c
          JOIN pg_class t ON t.oid = c.conrelid
         WHERE t.relname = 'device_commands'
           AND c.contype = 'c'
           AND pg_get_constraintdef(c.oid) ILIKE '%status%'
    LOOP
        EXECUTE format('ALTER TABLE device_commands DROP CONSTRAINT %I', status_constraint);
    END LOOP;

    ALTER TABLE device_commands
        ADD CONSTRAINT device_commands_status_check
        CHECK (status IN ('pending', 'processing', 'done', 'failed'));
END $$;

CREATE INDEX IF NOT EXISTS idx_device_commands_pending
    ON device_commands (status, created_at)
    WHERE status = 'pending';

CREATE INDEX IF NOT EXISTS idx_device_commands_processing
    ON device_commands (status, claimed_at)
    WHERE status = 'processing';
`)
	return err
}
