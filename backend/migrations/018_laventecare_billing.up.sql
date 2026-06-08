-- 018_laventecare_billing.up.sql
-- Commercial LaventeCare layer: quotes, time entries, invoices and payment-provider fields.

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
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, quote_number)
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
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (minutes > 0)
);

CREATE TABLE IF NOT EXISTS lc_invoices (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             TEXT NOT NULL,
    company_id          UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    project_id          UUID REFERENCES lc_projects(id) ON DELETE SET NULL,
    workstream_id       UUID REFERENCES lc_workstreams(id) ON DELETE SET NULL,
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
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, invoice_number)
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

CREATE INDEX IF NOT EXISTS idx_lc_invoice_lines_invoice
    ON lc_invoice_lines (invoice_id, sort_order);
