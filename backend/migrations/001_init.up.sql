-- Homeapp database schema
-- Exact replica of the SQLAlchemy models

CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

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

CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip_address);

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

CREATE INDEX IF NOT EXISTS idx_device_events_time ON device_events(time);
CREATE INDEX IF NOT EXISTS idx_device_events_device ON device_events(device_id);
