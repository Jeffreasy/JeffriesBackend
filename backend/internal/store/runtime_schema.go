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
	if err := ensureBrainPreferencesSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure brain preferences schema: %w", err)
	}
	if err := ensureLaventeCareCustomerSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare customer schema: %w", err)
	}
	if err := ensureLaventeCareDossierDocumentSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare dossier document schema: %w", err)
	}
	if err := ensureLaventeCareWorkstreamSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare workstream schema: %w", err)
	}
	if err := ensureLaventeCareActivitySchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare activity schema: %w", err)
	}
	if err := ensureBusinessContextSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure business context schema: %w", err)
	}
	return nil
}

func ensureLaventeCareCustomerSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_companies (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    naam            TEXT NOT NULL,
    website         TEXT,
    sector          TEXT,
    status          TEXT NOT NULL DEFAULT 'prospect',
    relatie_type    TEXT NOT NULL DEFAULT 'prospect',
    notities        TEXT,
    laatste_contact TIMESTAMPTZ,
    volgende_actie  TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_contacts (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    naam       TEXT NOT NULL,
    email      TEXT,
    telefoon   TEXT,
    rol        TEXT,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    notities   TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE lc_companies
    ADD COLUMN IF NOT EXISTS relatie_type TEXT NOT NULL DEFAULT 'prospect',
    ADD COLUMN IF NOT EXISTS laatste_contact TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS volgende_actie TEXT;

ALTER TABLE lc_contacts
    ADD COLUMN IF NOT EXISTS is_primary BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS notities TEXT;

UPDATE lc_companies
SET relatie_type = CASE
    WHEN status IN ('klant', 'partner', 'leverancier', 'prospect') THEN status
    ELSE 'prospect'
END
WHERE relatie_type IS NULL OR relatie_type = '';

CREATE INDEX IF NOT EXISTS idx_lc_companies_user
    ON lc_companies (user_id);

CREATE INDEX IF NOT EXISTS idx_lc_companies_user_status
    ON lc_companies (user_id, status);

CREATE INDEX IF NOT EXISTS idx_lc_companies_user_name_lower
    ON lc_companies (user_id, LOWER(TRIM(naam)));

CREATE INDEX IF NOT EXISTS idx_lc_companies_user_website_lower
    ON lc_companies (user_id, LOWER(TRIM(website)))
    WHERE website IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_contacts_user
    ON lc_contacts (user_id);

CREATE INDEX IF NOT EXISTS idx_lc_contacts_company
    ON lc_contacts (company_id);

ALTER TABLE lc_leads
    ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS contact_id UUID REFERENCES lc_contacts(id) ON DELETE SET NULL;

ALTER TABLE lc_projects
    ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

ALTER TABLE lc_action_items
    ADD COLUMN IF NOT EXISTS linked_company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_actions_company
    ON lc_action_items (linked_company_id, updated_at DESC)
    WHERE linked_company_id IS NOT NULL;
`)
	return err
}

func ensureLaventeCareActivitySchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_activity_events (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    company_id      UUID NOT NULL REFERENCES lc_companies(id) ON DELETE CASCADE,
    contact_id      UUID REFERENCES lc_contacts(id) ON DELETE SET NULL,
    lead_id         UUID REFERENCES lc_leads(id) ON DELETE SET NULL,
    project_id      UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id   UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    action_item_id  UUID REFERENCES lc_action_items(id) ON DELETE SET NULL,
    event_type      TEXT NOT NULL DEFAULT 'notitie',
    channel         TEXT NOT NULL DEFAULT 'manual',
    title           TEXT NOT NULL,
    body            TEXT,
    occurred_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_activity_events_user_occurred
    ON lc_activity_events (user_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_activity_events_company_occurred
    ON lc_activity_events (company_id, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_activity_events_project_occurred
    ON lc_activity_events (project_id, occurred_at DESC)
    WHERE project_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_activity_events_workstream_occurred
    ON lc_activity_events (workstream_id, occurred_at DESC)
    WHERE workstream_id IS NOT NULL;
`)
	return err
}

func ensureBusinessContextSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
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
`)
	return err
}

func ensureLaventeCareWorkstreamSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_workstreams (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            TEXT NOT NULL,
    company_id         UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
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

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_company
    ON lc_workstreams (company_id, updated_at DESC)
    WHERE company_id IS NOT NULL;

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
`)
	return err
}

func ensureLaventeCareDossierDocumentSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_dossier_documents (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id        TEXT NOT NULL,
    document_key   TEXT NOT NULL,
    titel          TEXT NOT NULL,
    template_label TEXT,
    context_type   TEXT NOT NULL DEFAULT 'manual',
    context_id     TEXT,
    context_title  TEXT,
    lead_id        UUID REFERENCES lc_leads(id) ON DELETE SET NULL,
    project_id     UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    company_id     UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    pdf_url        TEXT NOT NULL,
    theme          TEXT NOT NULL DEFAULT 'screen',
    delivery       TEXT NOT NULL DEFAULT 'inline',
    notes          TEXT,
    generated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_user_created
    ON lc_dossier_documents (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_lead
    ON lc_dossier_documents (lead_id, created_at DESC)
    WHERE lead_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_project
    ON lc_dossier_documents (project_id, created_at DESC)
    WHERE project_id IS NOT NULL;

ALTER TABLE lc_dossier_documents
    ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_company
    ON lc_dossier_documents (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_user_document
    ON lc_dossier_documents (user_id, document_key, created_at DESC);
`)
	return err
}

func ensureBrainPreferencesSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
ALTER TABLE brain_preferences ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE brain_preferences ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now();
ALTER TABLE brain_preferences ALTER COLUMN created_at SET DEFAULT now();
ALTER TABLE brain_preferences ALTER COLUMN updated_at SET DEFAULT now();

CREATE OR REPLACE FUNCTION homeapp_jsonb_to_text_array(value JSONB)
RETURNS TEXT[] LANGUAGE SQL IMMUTABLE AS $$
    SELECT COALESCE(array_agg(elem), ARRAY[]::TEXT[])
      FROM jsonb_array_elements_text(
          CASE WHEN jsonb_typeof(value) = 'array' THEN value ELSE '[]'::JSONB END
      ) AS elem
$$;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
          FROM information_schema.columns
         WHERE table_name = 'brain_preferences'
           AND column_name = 'focus_areas'
           AND udt_name = 'jsonb'
    ) THEN
        ALTER TABLE brain_preferences ALTER COLUMN focus_areas DROP DEFAULT;
        ALTER TABLE brain_preferences
            ALTER COLUMN focus_areas TYPE TEXT[]
            USING homeapp_jsonb_to_text_array(focus_areas);
        ALTER TABLE brain_preferences ALTER COLUMN focus_areas SET DEFAULT '{}';
    END IF;
END $$;

DROP FUNCTION IF EXISTS homeapp_jsonb_to_text_array(JSONB);
`)
	return err
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
