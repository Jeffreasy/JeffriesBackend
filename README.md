# Homeapp 🏠

> **Smart home backend** voor WiZ GU10 lampen.
> Kan lokaal draaien, maar is ook voorbereid op Render + Render Postgres met een lokale LAN-bridge voor WiZ UDP.

## Stack

| Laag | Technologie |
|---|---|
| Backend API | Go 1.25 + chi router |
| Database | PostgreSQL 16 + pgx v5 |
| Automation Engine | Go goroutines (server-side) |
| WiZ Control | UDP direct LAN of Render command queue via HTTP bridge |
| Sync | PostgreSQL-native automations, devices, notes, Gmail/Calendar metadata |
| Container | Docker Compose |

## Snel starten

```bash
# 1. Kopieer en vul de environment variabelen in
cp .env.example .env

# 2. Start alle services
docker compose up -d

# 3. Health check
open http://localhost:8000/api/v1/health
```

Postgres wordt lokaal gepubliceerd op `127.0.0.1:15432` om conflicten met een host-Postgres op `5432` te vermijden. Binnen Docker gebruiken API en engine gewoon `postgres:5432`.

## Render + lokale lamp-bridge

Render kan de API, Telegram, sync jobs, automations en PostgreSQL online houden. WiZ-lampen blijven wel lokale UDP-devices op je thuisnetwerk. Daarom gebruikt de cloudmodus een queue met een lokale HTTP-bridge:

```text
Frontend/Telegram -> Render API -> device_commands -> lokale bridge via Render API -> WiZ UDP -> lampen
```

Aanbevolen Render API/background-engine env:

```bash
START_BACKGROUND_ENGINE=true
LIGHT_COMMAND_MODE=queue
ENGINE_CRONS_ENABLED=true
ENGINE_AUTOMATIONS_ENABLED=true
ENGINE_COMMAND_POLLER_ENABLED=false
TELEGRAM_BOT_ENABLED=true
DATABASE_URL=<Render internal Postgres URL>
```

Aanbevolen lokale bridge env op je pc:

```bash
BRIDGE_API_URL=https://jeffriesbackend.onrender.com/api/v1
BRIDGE_API_KEY=<zelfde waarde als APP_SECRET_KEY op Render>
BRIDGE_STATUS_POLL_ENABLED=true
ENGINE_CRONS_ENABLED=false
ENGINE_AUTOMATIONS_ENABLED=false
TELEGRAM_BOT_ENABLED=false
```

Start lokaal alleen de bridge met:

```bash
cd backend
go run ./cmd/engine
```

## Lokaal ontwikkelen (zonder Docker)

```bash
cd backend

# API server
go run ./cmd/api

# Automation engine (apart process)
go run ./cmd/engine
```

## Google OAuth refresh token

Voor Gmail/Calendar sync heeft de backend `GOOGLE_CLIENT_ID`, `GOOGLE_CLIENT_SECRET` en `GOOGLE_REFRESH_TOKEN` nodig. Genereer een nieuw refresh token vanuit de repo-root:

```bash
node scripts/gen-gmail-token.mjs
```

Zet de getoonde redirect URI in Google Cloud Console bij de OAuth client. Het refresh token daarna alleen in `.env` en Render env vars zetten, nooit committen.

## Project structuur

```
JeffriesBackend/
├── backend/                    # Go module
│   ├── cmd/
│   │   ├── api/main.go         # REST API entrypoint
│   │   └── engine/main.go      # Automation Engine entrypoint
│   ├── internal/
│   │   ├── config/             # Environment configuration
│   │   ├── server/             # HTTP server + routes + middleware
│   │   ├── handler/            # REST endpoint handlers
│   │   ├── model/              # Domain structs
│   │   ├── store/              # PostgreSQL queries (pgx)
│   │   ├── wiz/                # WiZ UDP client
│   │   └── engine/             # Automation engine + telegram poller
│   ├── migrations/             # SQL migration files
│   ├── go.mod
│   └── Makefile
├── infra/
│   └── docker/                 # Dockerfiles & Postgres init
├── GoogleScripts/              # Google Apps Script (salary sim)
├── docker-compose.yml
└── .env.example
```

## API overzicht

Voor een vollediger contract: zie `backend/docs/api-overview.md`, `backend/docs/swagger.json` en live `/api/v1/swagger/index.html`.

| Methode | Route | Beschrijving |
|---|---|---|
| GET | `/api/v1/health` | Health check |
| GET/POST | `/api/v1/rooms` | Kamers beheren (PostgreSQL) |
| GET | `/api/v1/devices` | Alle apparaten (PostgreSQL) |
| POST | `/api/v1/devices/register` | WiZ lamp registreren |
| POST | `/api/v1/devices/{id}/command` | Lamp besturen direct of via queue |
| GET/POST | `/api/v1/scenes` | Lichtscènes (PostgreSQL) |
| POST | `/api/v1/scenes/{id}/activate` | Scène activeren |
| * | `/api/v1/automations` | Automations beheren (PostgreSQL) |

## Build

```bash
cd backend

# Build binaries
make build

# Run tests
make test

# Static analysis
make vet
```

## WiZ GU10 Pairing Codes

| Lamp | Code |
|---|---|
| GU10 #1 | `2528-533-8501` |
| GU10 #2 | `2267-813-7135` |
| GU10 #3 | `1051-982-2124` |
| GU10 #4 | `2348-331-9533` |
| GU10 #5 | `1105-024-0832` |
| GU10 #6 | `3553-591-0097` |
