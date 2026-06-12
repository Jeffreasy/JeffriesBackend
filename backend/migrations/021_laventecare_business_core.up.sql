-- 021_laventecare_business_core.up.sql
-- Business-core hardening: richer customer master data and secure access records.

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
