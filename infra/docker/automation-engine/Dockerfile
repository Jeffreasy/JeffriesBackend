# ─── Automation Engine — Lightweight standalone container ─────────────────────
# Runs 24/7 lokaal op Docker Desktop.
# Verbindt met Convex (internet) voor automation config.
# Bestuurt WiZ lampen via unicast UDP over het lokale netwerk.
# NOOIT deployen naar Render/cloud — WiZ is alleen via LAN bereikbaar.
# ─────────────────────────────────────────────────────────────────────────────

FROM python:3.12-slim

WORKDIR /app

# Minimale systeem-dependencies (alleen libpq voor asyncpg)
RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc libpq-dev \
    && rm -rf /var/lib/apt/lists/*

# Installeer Python dependencies (zelfde requirements als de API)
COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

# Kopieer applicatie source
COPY . .

# Start de standalone engine (GEEN uvicorn, GEEN REST API)
CMD ["python", "-m", "automation_engine_main"]
