-- 020_laventecare_mailbox.up.sql
-- LaventeCare mailbox foundation: reusable templates and auditable outbound messages.

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

CREATE INDEX IF NOT EXISTS idx_lc_mail_outbox_company
    ON lc_mail_outbox (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;
