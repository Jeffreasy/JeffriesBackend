-- 016_laventecare_customer_dossiers.up.sql
-- Promote LaventeCare customers/contacts to first-class CRM dossier entities.

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

ALTER TABLE lc_workstreams
    ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_workstreams_company
    ON lc_workstreams (company_id, updated_at DESC)
    WHERE company_id IS NOT NULL;

ALTER TABLE lc_action_items
    ADD COLUMN IF NOT EXISTS linked_company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_actions_company
    ON lc_action_items (linked_company_id, updated_at DESC)
    WHERE linked_company_id IS NOT NULL;

ALTER TABLE lc_dossier_documents
    ADD COLUMN IF NOT EXISTS company_id UUID REFERENCES lc_companies(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_company
    ON lc_dossier_documents (company_id, created_at DESC)
    WHERE company_id IS NOT NULL;
