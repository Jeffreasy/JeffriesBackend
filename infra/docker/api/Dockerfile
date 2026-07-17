# ─── Homeapp API — Multi-stage Go build ───────────────────────────────────────
# Produces a ~15MB Alpine image with a single static binary.

FROM golang:1.25.12-alpine AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Build
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /api ./cmd/api

# ─── Runtime ──────────────────────────────────────────────────────────────────
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata && addgroup -S app && adduser -S -G app app

COPY --from=builder /api /api

EXPOSE 8000

USER app

CMD ["/api"]
