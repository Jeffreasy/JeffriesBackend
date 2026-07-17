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
APP_ENV=production
APP_SECRET_KEY=<unieke random secret van minimaal 32 tekens>
LAVENTECARE_SECRET_KEY=<andere unieke random secret van minimaal 32 tekens>
BRIDGE_API_KEY=<derde unieke random secret van minimaal 32 tekens>
START_BACKGROUND_ENGINE=true
LIGHT_COMMAND_MODE=queue
ENGINE_CRONS_ENABLED=true
ENGINE_AUTOMATIONS_ENABLED=true
ENGINE_COMMAND_POLLER_ENABLED=false
TELEGRAM_BOT_ENABLED=true
TELEGRAM_BOT_TOKEN=<Telegram bot token>
TELEGRAM_CHAT_ID=<jouw Telegram chat id>
TELEGRAM_WEBAPP_URL=https://jeffries-homeapp.vercel.app
DATABASE_URL=<Render internal Postgres URL>
```

Aanbevolen lokale bridge env op je pc:

```bash
BRIDGE_API_URL=https://jeffriesbackend.onrender.com/api/v1
BRIDGE_API_KEY=<moet exact matchen met BRIDGE_API_KEY op Render; nooit APP_SECRET_KEY>
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

## Productiegeheimen

De backend start buiten `development` niet met zwakke of ontbrekende kerngeheimen. Genereer elke waarde onafhankelijk en gebruik minimaal 32 willekeurige tekens:

- `APP_SECRET_KEY` beveiligt de eigenaar-API.
- `LAVENTECARE_SECRET_KEY` is verplicht in productie en versleutelt de access-vault; deze waarde mag nooit gelijk zijn aan een ander geheim.
- `BRIDGE_API_KEY` is verplicht zodra queue/bridge-modus actief is. De cloud-API en lokale bridge delen deze ene bridgewaarde, maar deze mag nooit gelijk zijn aan `APP_SECRET_KEY`.
- `LAVENTECARE_INTAKE_SECRET` is optioneel. Zonder waarde blijft de publieke intake-route gesloten; met een waarde gebruikt de route uitsluitend dit Bearer-geheim.

Een ontbrekende `DATABASE_URL` is altijd een configuratiefout. Bewaar alle echte waarden alleen in `.env`/Render secrets en commit ze nooit.

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

## LaventeCare billing + bunq

LaventeCare ondersteunt intern offertes, urenregistratie en factuurconcepten. Die workflows werken direct met PostgreSQL. Voor live bunq-betaalverzoeken is extra configuratie nodig op Render:

```bash
BUNQ_ENVIRONMENT=sandbox
BUNQ_API_KEY=<bunq api key>
BUNQ_USER_ID=<bunq user id>
BUNQ_MONETARY_ACCOUNT_ID=<bunq monetary account id>
BUNQ_DEVICE_DESCRIPTION="JeffriesHomeapp Render"
```

De backend houdt facturen klaar met `payment_provider=bunq`, `merchant_reference` en provider-id/betaallink. Live RequestInquiry-aanmaak blijft achter de bevestigingslaag. Een stabiele bunq client-request-id, een lokale één-poging-reservering per factuur en voorafgaande provider-reconciliatie voorkomen een tweede POST bij gelijktijdige of onzekere uitvoering.

## Integratie-status

De frontend kan de echte runtime-status uitlezen via:

| Endpoint | Doel |
|---|---|
| `GET /api/v1/settings/overview` | Integratievlaggen, queue-status, modules en tellingen |
| `GET /api/v1/settings/telegram/status` | Telegram bot/token/owner/webhook/long-polling status |
| `GET /api/v1/sync/status` | Rooster, persoonlijke agenda en Gmail sync metadata |
| `POST /api/v1/sync/calendar` | Handmatige Google Calendar sync + pending afspraak push voor de geconfigureerde eigenaar |
| `POST /api/v1/sync/gmail` | Handmatige Gmail sync voor de geconfigureerde eigenaar |

`GET /api/v1/contacts` retourneert per pagina direct een JSON-array. `limit` is standaard 200 en maximaal 200; `offset` is standaard 0 en maximaal 10000. De sortering is stabiel op naam (met id als tie-breaker). `q` zoekt hoofdletterongevoelig in naam, e-mail, notities en toegewezen labelnamen. De frontend moet pagina's ophalen totdat een pagina minder dan `limit` resultaten bevat.

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

Voor een vollediger contract: zie `backend/docs/api-overview.md` en `backend/docs/swagger.json`. De live Swagger-UI op `/api/v1/swagger/index.html` is uitsluitend beschikbaar in `development`; productie retourneert 404.

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
