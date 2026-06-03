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
	return nil
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
