-- Homeapp Batch 1: Convex → PostgreSQL migration
-- Schedule, Salary, Transactions, Loonstroken, PersonalEvents, AuditLogs, etc.

-- ─── Schedule (Werkdiensten) ─────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS schedule (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      VARCHAR(100) NOT NULL,
    event_id     VARCHAR(200) NOT NULL,
    titel        VARCHAR(300) NOT NULL,
    start_datum  DATE         NOT NULL,
    start_tijd   VARCHAR(5)   NOT NULL DEFAULT '',
    eind_datum   DATE         NOT NULL,
    eind_tijd    VARCHAR(5)   NOT NULL DEFAULT '',
    werktijd     VARCHAR(30)  NOT NULL DEFAULT '',
    locatie      VARCHAR(200) NOT NULL DEFAULT '',
    team         VARCHAR(20)  NOT NULL DEFAULT '',
    shift_type   VARCHAR(30)  NOT NULL DEFAULT 'Dienst',
    prioriteit   INTEGER      NOT NULL DEFAULT 1,
    duur         NUMERIC(5,2) NOT NULL DEFAULT 0,
    weeknr       VARCHAR(20)  NOT NULL DEFAULT '',
    dag          VARCHAR(20)  NOT NULL DEFAULT '',
    status       VARCHAR(30)  NOT NULL DEFAULT 'Opkomend',
    beschrijving TEXT         NOT NULL DEFAULT '',
    heledag      BOOLEAN      NOT NULL DEFAULT false,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_schedule_user_event ON schedule(user_id, event_id);
CREATE INDEX IF NOT EXISTS idx_schedule_user_date ON schedule(user_id, start_datum);

CREATE TABLE IF NOT EXISTS schedule_meta (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     VARCHAR(100) NOT NULL UNIQUE,
    imported_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    file_name   VARCHAR(300) NOT NULL DEFAULT '',
    total_rows  INTEGER      NOT NULL DEFAULT 0
);

-- ─── Salary (Salarisberekening per maand) ────────────────────────────────────

CREATE TABLE IF NOT EXISTS salary (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             VARCHAR(100) NOT NULL,
    periode             VARCHAR(10)  NOT NULL,
    jaar                INTEGER      NOT NULL,
    maand               INTEGER      NOT NULL,
    aantal_diensten     INTEGER      NOT NULL DEFAULT 0,
    uurloon_ort         NUMERIC(8,2) NOT NULL DEFAULT 0,
    basis_loon          NUMERIC(10,2) NOT NULL DEFAULT 0,
    amt_zeerintensief   NUMERIC(10,2) NOT NULL DEFAULT 0,
    toeslag_balansvlf   NUMERIC(10,2) NOT NULL DEFAULT 0,
    ort_totaal          NUMERIC(10,2) NOT NULL DEFAULT 0,
    extra_uren_bedrag   NUMERIC(10,2) NOT NULL DEFAULT 0,
    toeslag_vakatie_uren NUMERIC(10,2) NOT NULL DEFAULT 0,
    reiskosten          NUMERIC(10,2) NOT NULL DEFAULT 0,
    eenmalig_totaal     NUMERIC(10,2) NOT NULL DEFAULT 0,
    bruto_betaling      NUMERIC(10,2) NOT NULL DEFAULT 0,
    pensioenpremie      NUMERIC(10,2) NOT NULL DEFAULT 0,
    loonheffing_schat   NUMERIC(10,2) NOT NULL DEFAULT 0,
    netto_prognose      NUMERIC(10,2) NOT NULL DEFAULT 0,
    ort_detail          JSONB,
    eenmalig_detail     JSONB,
    berekend_op         TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_salary_user_periode ON salary(user_id, periode);

-- ─── Transactions (Rabobank CSV import) ──────────────────────────────────────

CREATE TABLE IF NOT EXISTS transactions (
    id                    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id               VARCHAR(100) NOT NULL,
    rekening_iban         VARCHAR(34)  NOT NULL,
    volgnr                VARCHAR(50)  NOT NULL,
    datum                 DATE         NOT NULL,
    bedrag                NUMERIC(12,2) NOT NULL,
    saldo_na_trn          NUMERIC(12,2) NOT NULL DEFAULT 0,
    code                  VARCHAR(10)  NOT NULL DEFAULT '',
    tegenrekening_iban    VARCHAR(34),
    tegenpartij_naam      VARCHAR(200),
    omschrijving          TEXT         NOT NULL DEFAULT '',
    referentie            VARCHAR(200),
    reden_retour          VARCHAR(200),
    oorsp_bedrag          NUMERIC(12,2),
    oorsp_munt            VARCHAR(10),
    is_interne_overboeking BOOLEAN     NOT NULL DEFAULT false,
    categorie             VARCHAR(100),
    created_at            TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_trx_user_rek_volgnr ON transactions(user_id, rekening_iban, volgnr);
CREATE INDEX IF NOT EXISTS idx_trx_user_datum ON transactions(user_id, datum);
CREATE INDEX IF NOT EXISTS idx_trx_user_cat ON transactions(user_id, categorie);

-- ─── Loonstroken (PDF parsed payslips) ───────────────────────────────────────

CREATE TABLE IF NOT EXISTS loonstroken (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            VARCHAR(100) NOT NULL,
    jaar               INTEGER      NOT NULL,
    periode            INTEGER      NOT NULL,
    periode_label      VARCHAR(10)  NOT NULL,
    type               VARCHAR(20)  NOT NULL DEFAULT 'loonstrook',
    netto              NUMERIC(10,2) NOT NULL DEFAULT 0,
    bruto_betaling     NUMERIC(10,2) NOT NULL DEFAULT 0,
    bruto_inhouding    NUMERIC(10,2) NOT NULL DEFAULT 0,
    salaris_basis      NUMERIC(10,2) NOT NULL DEFAULT 0,
    ort_totaal         NUMERIC(10,2) NOT NULL DEFAULT 0,
    ort_detail         JSONB        NOT NULL DEFAULT '[]',
    amt_zeerintensief  NUMERIC(10,2),
    pensioenpremie     NUMERIC(10,2),
    loonheffing        NUMERIC(10,2),
    reiskosten         NUMERIC(10,2),
    vakantietoeslag    NUMERIC(10,2),
    eju_bedrag         NUMERIC(10,2),
    toeslag_balansvlf  NUMERIC(10,2),
    extra_uren_bedrag  NUMERIC(10,2),
    schaalnummer       VARCHAR(10)  NOT NULL DEFAULT '?',
    trede              VARCHAR(10)  NOT NULL DEFAULT '?',
    parttime_factor    NUMERIC(5,3) NOT NULL DEFAULT 0,
    uurloon            NUMERIC(8,2),
    componenten        JSONB        NOT NULL DEFAULT '[]',
    cumulatieven       JSONB,
    geimporteerd_op    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_loon_user_jr_per ON loonstroken(user_id, jaar, periode);

-- ─── Personal Events (Google Calendar) ───────────────────────────────────────

CREATE TABLE IF NOT EXISTS personal_events (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             VARCHAR(100) NOT NULL,
    event_id            VARCHAR(300) NOT NULL,
    titel               VARCHAR(500) NOT NULL,
    start_datum         DATE         NOT NULL,
    start_tijd          VARCHAR(5),
    eind_datum          DATE         NOT NULL,
    eind_tijd           VARCHAR(5),
    heledag             BOOLEAN      NOT NULL DEFAULT false,
    locatie             VARCHAR(500),
    beschrijving        TEXT,
    conflict_met_dienst VARCHAR(300),
    status              VARCHAR(30)  NOT NULL DEFAULT 'Aankomend',
    kalender            VARCHAR(50)  NOT NULL DEFAULT 'Main',
    created_at          TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_pe_user_event ON personal_events(user_id, event_id);
CREATE INDEX IF NOT EXISTS idx_pe_user_date ON personal_events(user_id, start_datum);

-- ─── Audit Logs ──────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS audit_logs (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    VARCHAR(100),
    actor      VARCHAR(30)  NOT NULL,
    source     VARCHAR(100) NOT NULL,
    action     VARCHAR(100) NOT NULL,
    entity     VARCHAR(100) NOT NULL,
    entity_id  VARCHAR(200),
    status     VARCHAR(30)  NOT NULL DEFAULT 'success',
    summary    TEXT         NOT NULL,
    metadata   JSONB,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_audit_user_created ON audit_logs(user_id, created_at);

-- ─── Sync Status ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS sync_status (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         VARCHAR(100) NOT NULL,
    source          VARCHAR(50)  NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'success',
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    last_success_at TIMESTAMPTZ,
    last_error_at   TIMESTAMPTZ,
    last_error      TEXT,
    result          JSONB,
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_sync_user_source ON sync_status(user_id, source);

-- ─── Privacy Settings ────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS privacy_settings (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    VARCHAR(100) NOT NULL UNIQUE,
    finance    BOOLEAN NOT NULL DEFAULT true,
    habits     BOOLEAN NOT NULL DEFAULT true,
    notes      BOOLEAN NOT NULL DEFAULT true,
    email      BOOLEAN NOT NULL DEFAULT true,
    account    BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ─── Brain Preferences ──────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS brain_preferences (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          VARCHAR(100) NOT NULL UNIQUE,
    detail_level     VARCHAR(20) NOT NULL DEFAULT 'normaal',
    tone             VARCHAR(20) NOT NULL DEFAULT 'warm',
    proactive_level  VARCHAR(20) NOT NULL DEFAULT 'normaal',
    focus_areas      JSONB       NOT NULL DEFAULT '[]',
    briefing_time    VARCHAR(5),
    quiet_hours_start VARCHAR(5),
    quiet_hours_end   VARCHAR(5),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
