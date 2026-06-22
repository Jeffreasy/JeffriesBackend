package store

import "context"

// ensureBaseTables creates the core application tables that previously existed
// only in migrations/ (dead code). Every statement is idempotent (CREATE TABLE
// IF NOT EXISTS), so it is a no-op on an existing DB and a full build-out on an
// empty one. Order follows foreign-key dependencies. LaventeCare pipeline tables
// (lc_companies/contacts/leads/projects/action_items and their dependents) are
// created by the ensureLaventeCare* functions, not here.
func ensureBaseTables(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS rooms (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(100) NOT NULL,
    icon        VARCHAR(50)  NOT NULL DEFAULT 'room',
    floor_number INTEGER     NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS devices (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    room_id            UUID REFERENCES rooms(id) ON DELETE SET NULL,
    ip_address         VARCHAR(45),
    mac_address        VARCHAR(17),
    matter_node_id     INTEGER NOT NULL DEFAULT 0,
    matter_endpoint_id INTEGER NOT NULL DEFAULT 1,
    name               VARCHAR(150) NOT NULL,
    device_type        VARCHAR(50)  NOT NULL,
    manufacturer       VARCHAR(100),
    model              VARCHAR(100),
    firmware_version   VARCHAR(50),
    current_state      JSONB NOT NULL DEFAULT '{}',
    status             VARCHAR(20) NOT NULL DEFAULT 'offline',
    last_seen          TIMESTAMPTZ,
    commissioned_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scenes (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       VARCHAR(100) NOT NULL,
    icon       VARCHAR(50)  NOT NULL DEFAULT 'scene',
    color_hex  VARCHAR(7)   NOT NULL DEFAULT '#6366f1',
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS scene_actions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scene_id        UUID NOT NULL REFERENCES scenes(id) ON DELETE CASCADE,
    device_id       UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    target_state    JSONB   NOT NULL,
    execution_order INTEGER NOT NULL DEFAULT 0,
    transition_ms   INTEGER NOT NULL DEFAULT 1000
);

CREATE TABLE IF NOT EXISTS automations (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             VARCHAR(150) NOT NULL,
    description      TEXT,
    is_enabled       BOOLEAN NOT NULL DEFAULT true,
    trigger_config   JSONB NOT NULL,
    condition_config JSONB NOT NULL DEFAULT '[]',
    action_config    JSONB NOT NULL,
    last_triggered   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS device_events (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    time       TIMESTAMPTZ NOT NULL DEFAULT now(),
    device_id  UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    event_type VARCHAR(50) NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}'
);

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

CREATE TABLE IF NOT EXISTS schedule_meta (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     VARCHAR(100) NOT NULL UNIQUE,
    imported_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    file_name   VARCHAR(300) NOT NULL DEFAULT '',
    total_rows  INTEGER      NOT NULL DEFAULT 0
);

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

CREATE TABLE IF NOT EXISTS emails (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT        NOT NULL,
    gmail_id        TEXT        NOT NULL,
    thread_id       TEXT        NOT NULL,
    from_addr       TEXT        NOT NULL DEFAULT '',
    to_addr         TEXT        NOT NULL DEFAULT '',
    cc              TEXT,
    bcc             TEXT,
    subject         TEXT        NOT NULL DEFAULT '(geen onderwerp)',
    snippet         TEXT        NOT NULL DEFAULT '',
    datum           DATE        NOT NULL,
    ontvangen       BIGINT      NOT NULL DEFAULT 0,
    is_gelezen      BOOLEAN     NOT NULL DEFAULT false,
    is_ster         BOOLEAN     NOT NULL DEFAULT false,
    is_verwijderd   BOOLEAN     NOT NULL DEFAULT false,
    is_draft        BOOLEAN     NOT NULL DEFAULT false,
    label_ids       TEXT[]      NOT NULL DEFAULT '{}',
    categorie       TEXT        DEFAULT 'primary',
    heeft_bijlagen  BOOLEAN     NOT NULL DEFAULT false,
    bijlagen_count  INT         NOT NULL DEFAULT 0,
    search_text     TEXT        NOT NULL DEFAULT '',
    synced_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, gmail_id)
);

CREATE TABLE IF NOT EXISTS email_sync_meta (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT        NOT NULL UNIQUE,
    history_id      TEXT        NOT NULL DEFAULT '',
    last_full_sync  TIMESTAMPTZ,
    total_synced    INT         NOT NULL DEFAULT 0,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS notes (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id         TEXT NOT NULL,
  titel           TEXT,
  inhoud          TEXT NOT NULL DEFAULT '',
  tags            TEXT[] DEFAULT '{}',
  kleur           TEXT,
  is_pinned       BOOLEAN NOT NULL DEFAULT false,
  is_archived     BOOLEAN NOT NULL DEFAULT false,
  deadline        TIMESTAMPTZ,
  linked_event_id TEXT,
  prioriteit      TEXT,
  triage_flag     BOOLEAN DEFAULT false,
  aangemaakt      TIMESTAMPTZ NOT NULL DEFAULT now(),
  gewijzigd       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS note_links (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id    TEXT NOT NULL,
  source_id  UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_id  UUID NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  aangemaakt TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(source_id, target_id)
);

CREATE TABLE IF NOT EXISTS habits (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id             TEXT NOT NULL,
  naam                TEXT NOT NULL,
  emoji               TEXT NOT NULL DEFAULT '🎯',
  type                TEXT NOT NULL DEFAULT 'positief',
  beschrijving        TEXT,
  frequentie          TEXT NOT NULL DEFAULT 'dagelijks',
  aangepaste_dagen    INTEGER[],
  doel_aantal         INTEGER,
  rooster_filter      TEXT,
  is_kwantitatief     BOOLEAN NOT NULL DEFAULT false,
  doel_waarde         NUMERIC,
  eenheid             TEXT,
  doel_tijd           TEXT,
  xp_per_voltooiing   INTEGER NOT NULL DEFAULT 10,
  moeilijkheid        TEXT NOT NULL DEFAULT 'normaal',
  financie_categorie  TEXT,
  huidige_streak      INTEGER NOT NULL DEFAULT 0,
  langste_streak      INTEGER NOT NULL DEFAULT 0,
  totaal_voltooid     INTEGER NOT NULL DEFAULT 0,
  totaal_xp           INTEGER NOT NULL DEFAULT 0,
  kleur               TEXT,
  volgorde            INTEGER NOT NULL DEFAULT 0,
  is_actief           BOOLEAN NOT NULL DEFAULT true,
  is_pauze            BOOLEAN NOT NULL DEFAULT false,
  gepauzeer_om        TIMESTAMPTZ,
  aangemaakt          TIMESTAMPTZ NOT NULL DEFAULT now(),
  gewijzigd           TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS habit_logs (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id     TEXT NOT NULL,
  habit_id    UUID NOT NULL REFERENCES habits(id) ON DELETE CASCADE,
  datum       DATE NOT NULL,
  voltooid    BOOLEAN NOT NULL DEFAULT false,
  waarde      NUMERIC,
  is_incident BOOLEAN NOT NULL DEFAULT false,
  trigger_cat TEXT,
  notitie     TEXT,
  bron        TEXT NOT NULL DEFAULT 'web',
  xp_verdiend INTEGER NOT NULL DEFAULT 0,
  aangemaakt  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(habit_id, datum)
);

CREATE TABLE IF NOT EXISTS habit_badges (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      TEXT NOT NULL,
  badge_id     TEXT NOT NULL,
  habit_id     UUID REFERENCES habits(id) ON DELETE SET NULL,
  naam         TEXT NOT NULL,
  emoji        TEXT NOT NULL,
  beschrijving TEXT NOT NULL,
  xp_bonus     INTEGER NOT NULL DEFAULT 0,
  behaald_op   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE(user_id, badge_id)
);

CREATE TABLE IF NOT EXISTS chat_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chat_id     BIGINT      NOT NULL,
    role        TEXT        NOT NULL CHECK (role IN ('user', 'assistant')),
    content     TEXT        NOT NULL,
    agent_id    TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS ai_pending_actions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT        NOT NULL,
    agent_id    TEXT        NOT NULL,
    tool_name   TEXT        NOT NULL,
    args_json   TEXT        NOT NULL DEFAULT '{}',
    summary     TEXT        NOT NULL,
    code        TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'cancelled', 'failed', 'expired')),
    result      TEXT,
    error       TEXT,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
`)
	return err
}
