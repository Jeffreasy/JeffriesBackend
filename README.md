# Homeapp 🏠

> **Lokale smart home backend** voor WiZ GU10 lampen.
> Draait volledig lokaal — geen cloud vereist voor lamp control.

## Stack

| Laag | Technologie |
|---|---|
| Backend API | Go 1.25 + chi router |
| Database | PostgreSQL 16 + pgx v5 |
| Automation Engine | Go goroutines (server-side) |
| WiZ Control | UDP (direct LAN) |
| Convex | Cloud sync (automations, devices, telegram) |
| Container | Docker Compose |

## Snel starten

```bash
# 1. Kopieer en vul de environment variabelen in
cp .env.example .env

# 2. Start alle services
docker compose up -d

# 3. API docs
open http://localhost:8000/api/v1/health
```

## Lokaal ontwikkelen (zonder Docker)

```bash
cd backend

# API server
go run ./cmd/api

# Automation engine (apart process)
go run ./cmd/engine
```

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
│   │   ├── convex/             # Convex HTTP API client
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

| Methode | Route | Beschrijving |
|---|---|---|
| GET | `/api/v1/health` | Health check |
| GET/POST | `/api/v1/rooms` | Kamers beheren (PostgreSQL) |
| GET | `/api/v1/devices` | Alle apparaten (Convex) |
| POST | `/api/v1/devices/register` | WiZ lamp registreren |
| POST | `/api/v1/devices/{id}/command` | Lamp besturen (UDP) |
| GET/POST | `/api/v1/scenes` | Lichtscènes (PostgreSQL) |
| POST | `/api/v1/scenes/{id}/activate` | Scène activeren |
| * | `/api/v1/automations` | 410 Gone (via Convex) |

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
