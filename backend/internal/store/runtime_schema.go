package store

import (
	"context"
	"fmt"
)

// EnsureRuntimeSchema applies narrowly scoped, idempotent schema repairs that
// the API needs before it can safely accept runtime work on Render.
func EnsureRuntimeSchema(ctx context.Context, db *DB) error {
	// Base tables first. These were historically created only by migrations/ (now
	// dead code) and merely ALTERed/referenced at runtime, so a fresh or restored
	// DB boot-looped on "relation does not exist". Creating them here (idempotent)
	// makes an empty DB self-bootable; the ensure* repairs below then layer on
	// later columns/indexes. Verified by TestEnsureRuntimeSchema_FreshDB.
	if err := ensureBaseTables(ctx, db); err != nil {
		return fmt.Errorf("ensure base tables: %w", err)
	}
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
	if err := ensureLaventeCareBillingSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare billing schema: %w", err)
	}
	if err := ensureLaventeCareMailboxSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare mailbox schema: %w", err)
	}
	if err := ensureLaventeCareBusinessCoreSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure laventecare business core schema: %w", err)
	}
	if err := ensureBusinessContextSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure business context schema: %w", err)
	}
	if err := ensureAICallLogSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure ai call log schema: %w", err)
	}
	if err := ensureSyncHealthSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure sync health schema: %w", err)
	}
	if err := ensurePersonalEventRetrySchema(ctx, db); err != nil {
		return fmt.Errorf("ensure personal event retry schema: %w", err)
	}
	if err := ensureSyncRunsSchema(ctx, db); err != nil {
		return fmt.Errorf("ensure sync runs schema: %w", err)
	}
	return nil
}

// ensureSyncRunsSchema creates the sync_runs audit table: one row per background
// sync execution (gmail, schedule, personal, pending-calendar) so a history of
// outcomes/latency/failures is queryable, not just the latest snapshot. Mirrors
// migrations/026_sync_runs.up.sql.
func ensureSyncRunsSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS sync_runs (
    id          BIGSERIAL   PRIMARY KEY,
    source      TEXT        NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    duration_ms INTEGER     NOT NULL DEFAULT 0,
    ok          BOOLEAN     NOT NULL DEFAULT true,
    error       TEXT
);

CREATE INDEX IF NOT EXISTS idx_sync_runs_source_started ON sync_runs (source, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_sync_runs_started ON sync_runs (started_at DESC);
`)
	return err
}

// ensurePersonalEventRetrySchema adds retry/dead-letter bookkeeping to
// personal_events so a permanently-failing pending calendar op can be capped
// instead of retried forever, and backfills the legacy "AI" calendar alias
// (which 404'd on push) to "Main". Mirrors migrations/025_personal_event_retry.up.sql.
func ensurePersonalEventRetrySchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
ALTER TABLE personal_events
    ADD COLUMN IF NOT EXISTS attempts        INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_error      TEXT,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;

UPDATE personal_events SET kalender = 'Main' WHERE kalender = 'AI';
`)
	return err
}

// ensureSyncHealthSchema adds current-sync-health columns, the bridge heartbeat
// table, and device-command retry bookkeeping. Mirrors migrations/024_sync_health.up.sql.
func ensureSyncHealthSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
ALTER TABLE email_sync_meta
    ADD COLUMN IF NOT EXISTS sync_status     TEXT NOT NULL DEFAULT 'ok',
    ADD COLUMN IF NOT EXISTS last_error      TEXT,
    ADD COLUMN IF NOT EXISTS last_attempt_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS bridge_heartbeat (
    id        INTEGER     PRIMARY KEY DEFAULT 1,
    last_seen TIMESTAMPTZ NOT NULL DEFAULT now()
);

ALTER TABLE device_commands
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0;

CREATE TABLE IF NOT EXISTS briefing_sent (
    day     DATE        PRIMARY KEY,
    sent_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}

// ensureAICallLogSchema creates the ai_call_log table used for AI token/cost/
// latency observability. Mirrors migrations/023_ai_call_log.up.sql.
func ensureAICallLogSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS ai_call_log (
    id                BIGSERIAL PRIMARY KEY,
    user_id           TEXT        NOT NULL,
    agent_id          TEXT        NOT NULL DEFAULT '',
    model             TEXT        NOT NULL DEFAULT '',
    kind              TEXT        NOT NULL DEFAULT 'chat',
    prompt_tokens     INTEGER     NOT NULL DEFAULT 0,
    completion_tokens INTEGER     NOT NULL DEFAULT 0,
    total_tokens      INTEGER     NOT NULL DEFAULT 0,
    rounds            INTEGER     NOT NULL DEFAULT 0,
    duration_ms       INTEGER     NOT NULL DEFAULT 0,
    tools_used        TEXT        NOT NULL DEFAULT '',
    finish_reason     TEXT        NOT NULL DEFAULT '',
    ok                BOOLEAN     NOT NULL DEFAULT TRUE,
    error             TEXT        NOT NULL DEFAULT '',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_ai_call_log_created_at ON ai_call_log (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_call_log_user_created ON ai_call_log (user_id, created_at DESC);
`)
	return err
}

func ensureLaventeCareBusinessCoreSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
ALTER TABLE lc_companies
    ADD COLUMN IF NOT EXISTS kvk_number TEXT,
    ADD COLUMN IF NOT EXISTS vat_number TEXT,
    ADD COLUMN IF NOT EXISTS billing_email TEXT,
    ADD COLUMN IF NOT EXISTS billing_address TEXT,
    ADD COLUMN IF NOT EXISTS billing_reference TEXT,
    ADD COLUMN IF NOT EXISTS payment_terms_days INTEGER NOT NULL DEFAULT 14,
    ADD COLUMN IF NOT EXISTS contract_status TEXT NOT NULL DEFAULT 'geen_contract',
    ADD COLUMN IF NOT EXISTS service_level TEXT NOT NULL DEFAULT 'basis',
    ADD COLUMN IF NOT EXISTS preferred_channel TEXT,
    ADD COLUMN IF NOT EXISTS portal_url TEXT,
    ADD COLUMN IF NOT EXISTS default_login_url TEXT,
    ADD COLUMN IF NOT EXISTS onboarding_status TEXT NOT NULL DEFAULT 'niet_gestart',
    ADD COLUMN IF NOT EXISTS data_processing_status TEXT NOT NULL DEFAULT 'niet_nodig';

ALTER TABLE lc_contacts
    ADD COLUMN IF NOT EXISTS preferred_channel TEXT,
    ADD COLUMN IF NOT EXISTS decision_role TEXT;

CREATE TABLE IF NOT EXISTS lc_access_credentials (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                TEXT NOT NULL,
    company_id             UUID NOT NULL REFERENCES lc_companies(id) ON DELETE CASCADE,
    contact_id             UUID REFERENCES lc_contacts(id) ON DELETE SET NULL,
    project_id             UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id          UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    title                  TEXT NOT NULL,
    login_url              TEXT,
    username               TEXT,
    role                   TEXT,
    environment            TEXT NOT NULL DEFAULT 'pilot',
    status                 TEXT NOT NULL DEFAULT 'actief',
    owner_contact          TEXT,
    secret_label           TEXT NOT NULL DEFAULT 'wachtwoord',
    secret_value_encrypted TEXT,
    secret_hint            TEXT,
    sharing_policy         TEXT NOT NULL DEFAULT 'veilig_kanaal',
    last_checked_at        TIMESTAMPTZ,
    expires_at             TIMESTAMPTZ,
    revoked_at             TIMESTAMPTZ,
    notes                  TEXT,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at             TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_access_credentials_user
    ON lc_access_credentials (user_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_access_credentials_company
    ON lc_access_credentials (company_id, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_access_credentials_status
    ON lc_access_credentials (user_id, status, updated_at DESC);
`)
	return err
}

func ensureLaventeCareMailboxSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_mail_templates (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          TEXT NOT NULL,
    template_key     TEXT NOT NULL,
    name             TEXT NOT NULL,
    category         TEXT NOT NULL DEFAULT 'general',
    status           TEXT NOT NULL DEFAULT 'active',
    subject_template TEXT NOT NULL,
    body_html        TEXT NOT NULL,
    body_text        TEXT,
    default_cc       TEXT[] NOT NULL DEFAULT '{}',
    default_bcc      TEXT[] NOT NULL DEFAULT '{}',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, template_key)
);

CREATE TABLE IF NOT EXISTS lc_mail_outbox (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             TEXT NOT NULL,
    template_id         UUID REFERENCES lc_mail_templates(id) ON DELETE SET NULL,
    company_id          UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    contact_id          UUID REFERENCES lc_contacts(id) ON DELETE SET NULL,
    project_id          UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id       UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    quote_id            UUID REFERENCES lc_quotes(id) ON DELETE SET NULL,
    invoice_id          UUID REFERENCES lc_invoices(id) ON DELETE SET NULL,
    to_email            TEXT NOT NULL,
    to_name             TEXT,
    cc                  TEXT[] NOT NULL DEFAULT '{}',
    bcc                 TEXT[] NOT NULL DEFAULT '{}',
    subject             TEXT NOT NULL,
    body_html           TEXT NOT NULL,
    body_text           TEXT,
    status              TEXT NOT NULL DEFAULT 'concept',
    provider            TEXT NOT NULL DEFAULT 'microsoft_graph',
    provider_message_id TEXT,
    conversation_id     TEXT,
    error_message       TEXT,
    sent_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_mail_templates_user_status
    ON lc_mail_templates (user_id, status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_mail_outbox_user_created
    ON lc_mail_outbox (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_mail_outbox_user_status
    ON lc_mail_outbox (user_id, status, created_at DESC);

-- conversation_id added after the initial outbox release; threads a sent mail to its
-- replies (shared with lc_mail_inbox.conversation_id). Idempotent for existing DBs.
ALTER TABLE lc_mail_outbox ADD COLUMN IF NOT EXISTS conversation_id TEXT;

CREATE INDEX IF NOT EXISTS idx_lc_mail_outbox_company
    ON lc_mail_outbox (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_mail_outbox_conversation
    ON lc_mail_outbox (user_id, conversation_id)
    WHERE conversation_id IS NOT NULL;

-- Inbound mail (received via Microsoft Graph). Idempotent on the Graph message id;
-- conversation_id threads a reply chain together with the outbox.
CREATE TABLE IF NOT EXISTS lc_mail_inbox (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    message_id      TEXT NOT NULL,
    conversation_id TEXT,
    company_id      UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    contact_id      UUID REFERENCES lc_contacts(id) ON DELETE SET NULL,
    from_email      TEXT NOT NULL,
    from_name       TEXT,
    subject         TEXT,
    body_preview    TEXT,
    web_link        TEXT,
    has_attachments BOOLEAN NOT NULL DEFAULT false,
    is_read         BOOLEAN NOT NULL DEFAULT false,
    received_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_lc_mail_inbox_user_message
    ON lc_mail_inbox (user_id, message_id);

CREATE INDEX IF NOT EXISTS idx_lc_mail_inbox_user_received
    ON lc_mail_inbox (user_id, received_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_mail_inbox_conversation
    ON lc_mail_inbox (user_id, conversation_id);

CREATE INDEX IF NOT EXISTS idx_lc_mail_inbox_company
    ON lc_mail_inbox (company_id, received_at DESC)
    WHERE company_id IS NOT NULL;
`)
	return err
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

-- Base pipeline tables. These historically lived only in migrations/006 and were
-- merely ALTERed (never CREATEd) at runtime, so a fresh/restored DB boot-looped
-- on "relation does not exist". Mirror migrations/006 here (idempotent) so an
-- empty DB is self-bootable; the ALTERs below then add later columns.
CREATE TABLE IF NOT EXISTS lc_leads (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              TEXT NOT NULL,
    company_id           UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    contact_id           UUID REFERENCES lc_contacts(id) ON DELETE SET NULL,
    titel                TEXT NOT NULL,
    bron                 TEXT NOT NULL DEFAULT 'cockpit',
    source_id            TEXT,
    status               TEXT NOT NULL DEFAULT 'nieuw',
    fit_score            INTEGER,
    pijnpunt             TEXT,
    prioriteit           TEXT DEFAULT 'normaal',
    volgende_stap        TEXT,
    volgende_actie_datum TEXT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_projects (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          TEXT NOT NULL,
    company_id       UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    lead_id          UUID REFERENCES lc_leads(id) ON DELETE SET NULL,
    naam             TEXT NOT NULL,
    fase             TEXT NOT NULL DEFAULT 'intake',
    status           TEXT NOT NULL DEFAULT 'actief',
    waarde_indicatie INTEGER,
    start_datum      TEXT,
    deadline         TEXT,
    samenvatting     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_action_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id           TEXT NOT NULL,
    source            TEXT NOT NULL DEFAULT 'handmatig',
    source_id         TEXT,
    title             TEXT NOT NULL,
    summary           TEXT,
    action_type       TEXT NOT NULL DEFAULT 'opvolgen',
    status            TEXT NOT NULL DEFAULT 'open',
    priority          TEXT NOT NULL DEFAULT 'normaal',
    due_date          TEXT,
    linked_lead_id    UUID REFERENCES lc_leads(id) ON DELETE SET NULL,
    linked_project_id UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_lc_leads_user ON lc_leads (user_id);
CREATE INDEX IF NOT EXISTS idx_lc_leads_user_status ON lc_leads (user_id, status);
CREATE INDEX IF NOT EXISTS idx_lc_leads_user_source ON lc_leads (user_id, bron, source_id);
CREATE INDEX IF NOT EXISTS idx_lc_projects_user ON lc_projects (user_id);
CREATE INDEX IF NOT EXISTS idx_lc_projects_user_fase ON lc_projects (user_id, fase);
CREATE INDEX IF NOT EXISTS idx_lc_projects_company ON lc_projects (company_id);
CREATE INDEX IF NOT EXISTS idx_lc_actions_user ON lc_action_items (user_id);
CREATE INDEX IF NOT EXISTS idx_lc_actions_user_status ON lc_action_items (user_id, status);
CREATE INDEX IF NOT EXISTS idx_lc_actions_user_due ON lc_action_items (user_id, due_date);
CREATE INDEX IF NOT EXISTS idx_lc_actions_user_source ON lc_action_items (user_id, source, source_id);

-- Dependent pipeline tables (also migrations/006-only). lc_decisions /
-- lc_change_requests / lc_sla_incidents FK lc_projects, so they must follow it.
CREATE TABLE IF NOT EXISTS lc_documents (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      TEXT NOT NULL,
    document_key TEXT NOT NULL,
    titel        TEXT NOT NULL,
    categorie    TEXT NOT NULL,
    fase         TEXT,
    versie       TEXT NOT NULL DEFAULT '2026-04',
    source_path  TEXT,
    samenvatting TEXT NOT NULL,
    tags         TEXT[],
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- Required by SeedDocuments' ON CONFLICT (user_id, document_key) upsert.
CREATE UNIQUE INDEX IF NOT EXISTS idx_lc_documents_user_key ON lc_documents (user_id, document_key);

CREATE TABLE IF NOT EXISTS lc_decisions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    project_id UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    titel      TEXT NOT NULL,
    besluit    TEXT NOT NULL,
    reden      TEXT NOT NULL,
    impact     TEXT,
    status     TEXT NOT NULL DEFAULT 'genomen',
    datum      TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_change_requests (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    project_id      UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    titel           TEXT NOT NULL,
    impact          TEXT NOT NULL,
    planning_impact TEXT,
    budget_impact   TEXT,
    status          TEXT NOT NULL DEFAULT 'nieuw',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_sla_incidents (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          TEXT NOT NULL,
    project_id       UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    titel            TEXT NOT NULL,
    prioriteit       TEXT NOT NULL DEFAULT 'P3',
    status           TEXT NOT NULL DEFAULT 'open',
    kanaal           TEXT NOT NULL DEFAULT 'telegram',
    gemeld_op        TIMESTAMPTZ NOT NULL DEFAULT now(),
    reactie_deadline TIMESTAMPTZ,
    samenvatting     TEXT,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

func ensureLaventeCareBillingSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS lc_quotes (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    company_id      UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    project_id      UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id   UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    quote_number    TEXT NOT NULL,
    titel           TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'concept',
    issue_date      DATE NOT NULL DEFAULT CURRENT_DATE,
    valid_until     DATE,
    currency        TEXT NOT NULL DEFAULT 'EUR',
    subtotal_cents  INTEGER NOT NULL DEFAULT 0,
    vat_rate_bps    INTEGER NOT NULL DEFAULT 2100,
    vat_cents       INTEGER NOT NULL DEFAULT 0,
    total_cents     INTEGER NOT NULL DEFAULT 0,
    accepted_at     TIMESTAMPTZ,
    notes           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_quote_lines (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    quote_id          UUID NOT NULL REFERENCES lc_quotes(id) ON DELETE CASCADE,
    user_id           TEXT NOT NULL,
    description       TEXT NOT NULL,
    quantity          INTEGER NOT NULL DEFAULT 1,
    unit_amount_cents INTEGER NOT NULL DEFAULT 0,
    total_cents       INTEGER NOT NULL DEFAULT 0,
    sort_order        INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS lc_time_entries (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            TEXT NOT NULL,
    company_id         UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    project_id         UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id      UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    activity_event_id  UUID REFERENCES lc_activity_events(id) ON DELETE SET NULL,
    invoice_id         UUID,
    source_type        TEXT NOT NULL DEFAULT 'manual',
    source_id          TEXT,
    description        TEXT NOT NULL,
    entry_date         DATE NOT NULL DEFAULT CURRENT_DATE,
    minutes            INTEGER NOT NULL,
    hourly_rate_cents  INTEGER NOT NULL DEFAULT 7500,
    billable           BOOLEAN NOT NULL DEFAULT true,
    status             TEXT NOT NULL DEFAULT 'concept',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_invoices (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             TEXT NOT NULL,
    company_id          UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    project_id          UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id       UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    quote_id            UUID REFERENCES lc_quotes(id) ON DELETE SET NULL,
    invoice_number      TEXT NOT NULL,
    status              TEXT NOT NULL DEFAULT 'concept',
    issue_date          DATE NOT NULL DEFAULT CURRENT_DATE,
    due_date            DATE,
    currency            TEXT NOT NULL DEFAULT 'EUR',
    subtotal_cents      INTEGER NOT NULL DEFAULT 0,
    vat_rate_bps        INTEGER NOT NULL DEFAULT 2100,
    vat_cents           INTEGER NOT NULL DEFAULT 0,
    total_cents         INTEGER NOT NULL DEFAULT 0,
    paid_cents          INTEGER NOT NULL DEFAULT 0,
    payment_provider    TEXT NOT NULL DEFAULT 'bunq',
    provider_request_id TEXT,
    merchant_reference  TEXT,
    payment_url         TEXT,
    sent_at             TIMESTAMPTZ,
    paid_at             TIMESTAMPTZ,
    notes               TEXT,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS lc_invoice_lines (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id           UUID NOT NULL REFERENCES lc_invoices(id) ON DELETE CASCADE,
    user_id              TEXT NOT NULL,
    source_time_entry_id UUID REFERENCES lc_time_entries(id) ON DELETE SET NULL,
    description          TEXT NOT NULL,
    quantity_minutes     INTEGER NOT NULL DEFAULT 0,
    unit_amount_cents    INTEGER NOT NULL DEFAULT 0,
    vat_rate_bps         INTEGER NOT NULL DEFAULT 2100,
    total_cents          INTEGER NOT NULL DEFAULT 0,
    sort_order           INTEGER NOT NULL DEFAULT 0
);

ALTER TABLE lc_quotes
    ADD COLUMN IF NOT EXISTS workstream_id UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS accepted_at TIMESTAMPTZ;

ALTER TABLE lc_time_entries
    ADD COLUMN IF NOT EXISTS activity_event_id UUID REFERENCES lc_activity_events(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS invoice_id UUID,
    ADD COLUMN IF NOT EXISTS source_type TEXT NOT NULL DEFAULT 'manual',
    ADD COLUMN IF NOT EXISTS source_id TEXT,
    ADD COLUMN IF NOT EXISTS billable BOOLEAN NOT NULL DEFAULT true;

ALTER TABLE lc_invoices
    ADD COLUMN IF NOT EXISTS workstream_id UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS quote_id UUID REFERENCES lc_quotes(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS payment_provider TEXT NOT NULL DEFAULT 'bunq',
    ADD COLUMN IF NOT EXISTS provider_request_id TEXT,
    ADD COLUMN IF NOT EXISTS merchant_reference TEXT,
    ADD COLUMN IF NOT EXISTS payment_url TEXT,
    ADD COLUMN IF NOT EXISTS document_url TEXT,
    ADD COLUMN IF NOT EXISTS document_generated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS ubl_xml TEXT,
    ADD COLUMN IF NOT EXISTS ubl_generated_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_checked_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS payment_status TEXT,
    ADD COLUMN IF NOT EXISTS payment_last_error TEXT,
    ADD COLUMN IF NOT EXISTS reminder_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS last_reminder_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS sent_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS paid_at TIMESTAMPTZ;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'lc_time_entries_invoice_fk'
    ) THEN
        ALTER TABLE lc_time_entries
            ADD CONSTRAINT lc_time_entries_invoice_fk
            FOREIGN KEY (invoice_id) REFERENCES lc_invoices(id) ON DELETE SET NULL
            DEFERRABLE INITIALLY DEFERRED;
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'lc_quotes_user_quote_number_key'
    ) THEN
        ALTER TABLE lc_quotes
            ADD CONSTRAINT lc_quotes_user_quote_number_key UNIQUE (user_id, quote_number);
    END IF;

    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'lc_invoices_user_invoice_number_key'
    ) THEN
        ALTER TABLE lc_invoices
            ADD CONSTRAINT lc_invoices_user_invoice_number_key UNIQUE (user_id, invoice_number);
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_lc_quotes_user_created
    ON lc_quotes (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_quotes_company
    ON lc_quotes (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_quote_lines_quote
    ON lc_quote_lines (quote_id, sort_order);

CREATE INDEX IF NOT EXISTS idx_lc_time_entries_user_date
    ON lc_time_entries (user_id, entry_date DESC);

CREATE INDEX IF NOT EXISTS idx_lc_time_entries_company
    ON lc_time_entries (company_id, entry_date DESC)
    WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_time_entries_invoice
    ON lc_time_entries (invoice_id)
    WHERE invoice_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_user_created
    ON lc_invoices (user_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_lc_invoices_company
    ON lc_invoices (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_quote
    ON lc_invoices (quote_id, created_at DESC)
    WHERE quote_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_payment_checked
    ON lc_invoices (user_id, payment_checked_at DESC)
    WHERE payment_checked_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_lc_invoices_document_generated
    ON lc_invoices (user_id, document_generated_at DESC)
    WHERE document_generated_at IS NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_lc_invoices_quote_unique_active
    ON lc_invoices (user_id, quote_id)
    WHERE quote_id IS NOT NULL AND status <> 'geannuleerd';

CREATE INDEX IF NOT EXISTS idx_lc_invoice_lines_invoice
    ON lc_invoice_lines (invoice_id, sort_order);
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
    ADD COLUMN IF NOT EXISTS linked_workstream_id UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS due_time TEXT;

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
