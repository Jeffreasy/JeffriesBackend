-- 015_laventecare_workstreams.up.sql
-- Flexible LaventeCare opdracht/workstream layer for small and medium engagements.

CREATE TABLE IF NOT EXISTS lc_workstreams (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            TEXT NOT NULL,
    lead_id            UUID REFERENCES lc_leads(id) ON DELETE SET NULL,
    project_id         UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    titel              TEXT NOT NULL,
    type               TEXT NOT NULL DEFAULT 'advies',
    status             TEXT NOT NULL DEFAULT 'nieuw',
    prioriteit         TEXT NOT NULL DEFAULT 'normaal',
    klant_naam         TEXT,
    bron               TEXT NOT NULL DEFAULT 'cockpit',
    source_id          TEXT,
    doel               TEXT,
    scope              TEXT,
    deliverable        TEXT,
    bevindingen        TEXT,
    volgende_stap      TEXT,
    deadline           TEXT,
    geschatte_minuten  INTEGER,
    waarde_indicatie   INTEGER,
    stack_tags         TEXT[] NOT NULL DEFAULT '{}',
    tags               TEXT[] NOT NULL DEFAULT '{}',
    completed_at       TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_user
    ON lc_workstreams (user_id);

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_user_status
    ON lc_workstreams (user_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_user_deadline
    ON lc_workstreams (user_id, deadline)
    WHERE deadline IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_lead
    ON lc_workstreams (lead_id, updated_at DESC)
    WHERE lead_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_project
    ON lc_workstreams (project_id, updated_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_user_source
    ON lc_workstreams (user_id, bron, source_id)
    WHERE source_id IS NOT NULL;

ALTER TABLE lc_action_items
    ADD COLUMN IF NOT EXISTS linked_workstream_id UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_actions_workstream
    ON lc_action_items (linked_workstream_id, updated_at DESC)
    WHERE linked_workstream_id IS NOT NULL;

ALTER TABLE lc_dossier_documents
    ADD COLUMN IF NOT EXISTS workstream_id UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_workstream
    ON lc_dossier_documents (workstream_id, created_at DESC)
    WHERE workstream_id IS NOT NULL;
