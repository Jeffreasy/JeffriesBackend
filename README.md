# Homeapp 🏠

> **Lokale Matter smart home backend** voor WiZ GU10 GU10 en andere Matter-apparaten.
> Draait volledig lokaal — geen cloud vereist.

## Stack

| Laag | Technologie |
|---|---|
| Matter Controller | `python-matter-server` (Docker) |
| Backend API | Python 3.12 + FastAPI |
| Database | PostgreSQL 16 + SQLAlchemy 2 async |
| Scheduler | APScheduler (automatiseringen) |
| Cache / Pub-Sub | Redis 7 |
| Container | Docker Compose |

## Snel starten

```bash
# 1. Kopieer en vul de environment variabelen in
cp .env.example .env

# 2. Start alle services
docker compose up -d

# 3. API docs
open http://localhost:8000/docs
```

## Project structuur

```
Homeapp/
├── backend/                  # FastAPI applicatie
│   ├── app/
│   │   ├── main.py           # App factory + lifespan
│   │   ├── api/v1/           # REST endpoints
│   │   │   └── endpoints/    # devices, rooms, scenes, automations
│   │   ├── core/             # config, logging, security
│   │   ├── db/               # session, repositories, migrations
│   │   ├── models/           # SQLAlchemy ORM models
│   │   ├── schemas/          # Pydantic request/response schemas
│   │   ├── services/
│   │   │   ├── matter/       # Matter WebSocket client
│   │   │   ├── scenes/       # Scene activation logic
│   │   │   └── automation/   # APScheduler engine
│   │   ├── events/           # Matter event handlers → DB/WebSocket
│   │   └── utils/            # Helpers
│   ├── tests/
│   └── requirements.txt
├── infra/
│   ├── docker/               # Dockerfiles & Postgres init
│   └── nginx/                # Reverse proxy (productie)
├── matter-server/data/       # Persistente Matter fabric data
├── frontend/                 # Toekomstige frontend (React/Next.js)
├── docs/
│   ├── adr/                  # Architecture Decision Records
│   └── diagrams/
├── docker-compose.yml
└── .env.example
```

## API overzicht

| Methode | Route | Beschrijving |
|---|---|---|
| GET | `/api/v1/health` | Health check |
| GET/POST | `/api/v1/rooms` | Kamers beheren |
| GET | `/api/v1/devices` | Alle apparaten |
| POST | `/api/v1/devices/commission` | Nieuw apparaat toevoegen via Matter code |
| POST | `/api/v1/devices/{id}/command` | Apparaat besturen (dimmen, kleur, etc.) |
| GET/POST | `/api/v1/scenes` | Lichtscènes |
| POST | `/api/v1/scenes/{id}/activate` | Scène activeren |
| GET/POST | `/api/v1/automations` | Automatiseringsregels |

## WiZ GU10 Matter codes

Bewaar deze codes veilig — nodig voor commissioning:

| Lamp | Code |
|---|---|
| GU10 #1 | `2528-533-8501` |
| GU10 #2 | `2267-813-7135` |
| GU10 #3 | `1051-982-2124` |
| GU10 #4 | `2348-331-9533` |
| GU10 #5 | `1105-024-0832` |
| GU10 #6 | `3553-591-0097` |
