-- PostgreSQL initialisation script
-- Runs once on first container start.

-- Enable useful extensions
CREATE EXTENSION IF NOT EXISTS "pgcrypto";  -- gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS "pg_trgm";   -- fuzzy text search on device/room names

-- TimescaleDB (uncomment if the timescaledb image is used instead of plain postgres):
-- CREATE EXTENSION IF NOT EXISTS timescaledb;
