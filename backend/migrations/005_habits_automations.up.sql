-- Batch 2: Habits + Automations
-- Migration 005

-- ─── Habits ──────────────────────────────────────────────────────────────────
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

CREATE INDEX IF NOT EXISTS idx_habits_user        ON habits(user_id);
CREATE INDEX IF NOT EXISTS idx_habits_user_actief ON habits(user_id, is_actief);

-- ─── Habit Logs ──────────────────────────────────────────────────────────────
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

CREATE INDEX IF NOT EXISTS idx_habit_logs_user       ON habit_logs(user_id);
CREATE INDEX IF NOT EXISTS idx_habit_logs_habit       ON habit_logs(habit_id);
CREATE INDEX IF NOT EXISTS idx_habit_logs_habit_datum ON habit_logs(habit_id, datum);
CREATE INDEX IF NOT EXISTS idx_habit_logs_user_datum  ON habit_logs(user_id, datum);

-- ─── Habit Badges ────────────────────────────────────────────────────────────
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

CREATE INDEX IF NOT EXISTS idx_habit_badges_user ON habit_badges(user_id);

-- ─── Automations ─────────────────────────────────────────────────────────────
DROP TABLE IF EXISTS automations CASCADE;
CREATE TABLE IF NOT EXISTS automations (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        TEXT NOT NULL,
  name           TEXT NOT NULL,
  enabled        BOOLEAN NOT NULL DEFAULT true,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_fired_at  TIMESTAMPTZ,
  group_name     TEXT,
  trigger_config JSONB NOT NULL DEFAULT '{}',
  action_config  JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_automations_user ON automations(user_id);
