-- Batch 1: Privacy Settings + Notes
-- Migration 004

-- ─── Privacy Settings ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS privacy_settings (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    TEXT NOT NULL UNIQUE,
  finance    BOOLEAN NOT NULL DEFAULT false,
  habits     BOOLEAN NOT NULL DEFAULT false,
  notes      BOOLEAN NOT NULL DEFAULT false,
  email      BOOLEAN NOT NULL DEFAULT false,
  account    BOOLEAN NOT NULL DEFAULT false,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Notes ───────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS notes (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         TEXT NOT NULL,
  titel           TEXT,
  inhoud          TEXT NOT NULL DEFAULT '',
  tags            TEXT[] DEFAULT '{}',
  kleur           TEXT,
  is_pinned       BOOLEAN NOT NULL DEFAULT false,
  is_archived     BOOLEAN NOT NULL DEFAULT false,
  deadline        TIMESTAMPTZ,
  linked_event_id TEXT,
  prioriteit      TEXT,
  triage_flag     BOOLEAN DEFAULT false,
  aangemaakt      TIMESTAMPTZ NOT NULL DEFAULT now(),
  gewijzigd       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_notes_user        ON notes(user_id);
CREATE INDEX IF NOT EXISTS idx_notes_user_pinned  ON notes(user_id, is_pinned) WHERE NOT is_archived;
CREATE INDEX IF NOT EXISTS idx_notes_user_deadline ON notes(user_id, deadline) WHERE deadline IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_notes_search       ON notes USING GIN (to_tsvector('dutch', COALESCE(titel,'') || ' ' || inhoud));

-- ─── Note Links ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS note_links (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    TEXT NOT NULL,
  source_id  UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_id  UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  aangemaakt TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(source_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_note_links_source ON note_links(source_id);
CREATE INDEX IF NOT EXISTS idx_note_links_target ON note_links(target_id);
