# ─── Automation Engine — Multi-stage Go build ─────────────────────────────────
# Runs 24/7 locally on Docker Desktop.
# Connects to Convex (internet) for automation config.
# Controls WiZ bulbs via unicast UDP over the local network.
# NEVER deploy to Render/cloud — WiZ is only reachable via LAN.

FROM golang:alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /engine ./cmd/engine

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /engine /engine

CMD ["/engine"]
