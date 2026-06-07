-- 013_laventecare_dossier_documents.up.sql
-- Generated LaventeCare PDF dossier history.

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

CREATE INDEX IF NOT EXISTS idx_lc_dossier_docs_user_document
    ON lc_dossier_documents (user_id, document_key, created_at DESC);
