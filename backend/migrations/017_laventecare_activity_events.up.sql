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
