-- Version history for user notes.
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
