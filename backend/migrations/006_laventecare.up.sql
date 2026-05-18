-- 006_laventecare.up.sql
-- LaventeCare CRM tables (migrated from Convex)

-- ─── Companies ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS lc_companies (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    naam       TEXT NOT NULL,
    website    TEXT,
    sector     TEXT,
    omvang     TEXT,
    status     TEXT NOT NULL DEFAULT 'prospect',
    fit_score  INTEGER,
    tags       TEXT[],
    bron       TEXT,
    notities   TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lc_companies_user ON lc_companies (user_id);
CREATE INDEX idx_lc_companies_user_status ON lc_companies (user_id, status);

-- ─── Contacts ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS lc_contacts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT NOT NULL,
    company_id  UUID REFERENCES lc_companies(id) ON DELETE SET NULL,
    naam        TEXT NOT NULL,
    email       TEXT,
    telefoon    TEXT,
    rol         TEXT,
    is_beslisser BOOLEAN NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_lc_contacts_user ON lc_contacts (user_id);
CREATE INDEX idx_lc_contacts_user_email ON lc_contacts (user_id, email);
CREATE INDEX idx_lc_contacts_company ON lc_contacts (company_id);

-- ─── Leads ───────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_leads_user ON lc_leads (user_id);
CREATE INDEX idx_lc_leads_user_status ON lc_leads (user_id, status);
CREATE INDEX idx_lc_leads_user_source ON lc_leads (user_id, bron, source_id);
CREATE INDEX idx_lc_leads_user_next_action ON lc_leads (user_id, volgende_actie_datum);

-- ─── Projects ────────────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_projects_user ON lc_projects (user_id);
CREATE INDEX idx_lc_projects_user_fase ON lc_projects (user_id, fase);
CREATE INDEX idx_lc_projects_company ON lc_projects (company_id);

-- ─── Action Items ────────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_actions_user ON lc_action_items (user_id);
CREATE INDEX idx_lc_actions_user_status ON lc_action_items (user_id, status);
CREATE INDEX idx_lc_actions_user_due ON lc_action_items (user_id, due_date);
CREATE INDEX idx_lc_actions_user_source ON lc_action_items (user_id, source, source_id);

-- ─── Documents ───────────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_documents_user ON lc_documents (user_id);
CREATE UNIQUE INDEX idx_lc_documents_user_key ON lc_documents (user_id, document_key);

-- ─── Decisions ───────────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_decisions_user ON lc_decisions (user_id);
CREATE INDEX idx_lc_decisions_project ON lc_decisions (project_id);

-- ─── Change Requests ─────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_changes_user ON lc_change_requests (user_id);
CREATE INDEX idx_lc_changes_project ON lc_change_requests (project_id);

-- ─── SLA Incidents ───────────────────────────────────────────────────────────
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

CREATE INDEX idx_lc_sla_user ON lc_sla_incidents (user_id);
CREATE INDEX idx_lc_sla_project ON lc_sla_incidents (project_id);
CREATE INDEX idx_lc_sla_user_status ON lc_sla_incidents (user_id, status);
