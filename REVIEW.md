# Code review — JeffriesBackend + JeffriesHomeapp

> Datum: 2026-06-19 · Scope: volledige stack (Go backend + Next.js frontend)
> Methode: multi-agent review — 13 subsysteem-mappers, 7 review-dimensies, adversariële verificatie van elke bevinding (82 agents). 52 bevindingen bevestigd, 10 weerlegd.
> Severities zijn de na-verificatie gecorrigeerde waarden. `userId`-IDOR is afgeschaald naar defense-in-depth omdat Clerk sign-up beperkt is tot de eigenaar (bevestigd door de auteur).

## Inhoud

- [Wat het project is](#wat-het-project-is)
- [Algemeen oordeel](#algemeen-oordeel)
- [Architectuur in het kort](#architectuur-in-het-kort)
- [Security](#-security)
- [Correctheid / bugs](#-correctheid--bugs)
- [Ops / infra](#-ops--infra)
- [Kwaliteit / onderhoud](#-kwaliteit--onderhoud)
- [Wat juist góed zit (weerlegde bevindingen)](#-wat-juist-góed-zit-weerlegde-bevindingen)
- [Aanbevolen volgorde](#aanbevolen-volgorde)

---

## Wat het project is

Een persoonlijk "home-OS" + zakelijke CRM in twee repos:

- **JeffriesBackend** — Go 1.25 / chi / pgx v5, ~45k regels. Twee binaries: `api` (Render) en `engine` (lokaal in Docker). Domeinen: WiZ-verlichting, automation-engine, Telegram-AI-bot (Grok/Groq), Google-sync (Gmail/Calendar), Todoist, bunq-bankieren, en **LaventeCare** — een complete IT-consultancy CRM met facturatie, dossiers, mailbox (MS Graph) en UBL e-facturen.
- **JeffriesHomeapp** — Next.js 16 / React 19 PWA, ~63k regels TS. Clerk-auth, TanStack Query, Zustand, Tailwind 4, Serwist (offline), orval-client, react-pdf.

Drie niet-vanzelfsprekende feiten die je moet kennen:

1. **Single-tenant.** Vrijwel alle backend-data hangt aan één hardcoded eigenaar (`HOMEAPP_USER_ID`, default `user_3Ax561ZvuSkGtWpKFooeY65HNtY`, ook zichtbaar in `docker-compose.yml`). Er zijn geen echte user-accounts in de backend.
2. **`migrations/` is dood.** De 22 SQL-migraties worden door geen enkele code uitgevoerd; het echte schema wordt bij elke boot opgebouwd door `EnsureRuntimeSchema` in `backend/internal/store/runtime_schema.go`.
3. **Twee API-clients naast elkaar** in de frontend: een orval-gegenereerde én een handgeschreven `lib/api.ts` (1676 regels). Ze overlappen en kunnen stil uit elkaar lopen.

## Algemeen oordeel

Voor een solo-project verrassend volwassen en breed: nette laag-scheiding (handler/store/engine/model), geparametriseerde SQL (**geen** SQL-injectie gevonden), circuit breaker rond Grok, advisory-lock voor de Telegram-poller, AES-GCM voor klant-credentials, doordacht queue/bridge-model dat cloud-API en lokale UDP-lampen scheidt.

De zwakke plekken zijn vooral **structureel**: het autorisatie-model (één gedeelde sleutel + client-bepaalde `userId`), de schema-bron-van-waarheid, en het ontbreken van vangnetten (tests, CI, backups, type-check-gate).

## Architectuur in het kort

```
Browser (PWA, React 19 + TanStack Query)
        │  data-plane
        ▼
Vercel · Next.js 16  ── Clerk-gate (proxy.ts) + key-injecterende [...path] proxy
        │  injecteert X-API-Key server-side
        ▼
Render · Go API (chi) ── één gedeelde X-API-Key · single-tenant · ~110 routes ──► PostgreSQL (schema via runtime_schema.go)
        │                                                         ▲
        │  outbound                                               │ queue/bridge (HTTPS poll)
        ▼                                                         │
Telegram · Grok/Groq · Google · bunq · MS Graph · Todoist   Engine (lokaal, Docker) ──UDP──► WiZ lampen
```

Twee binaries delen `config`/`store`/`engine`. De `api` kan de engine optioneel in-process draaien (`START_BACKGROUND_ENGINE`); de standalone `engine`-binary draait lokaal en bestuurt de lampen via UDP, of haalt in "queue mode" commando's bij Render op via de bridge.

---

## 🔴 Security

### S1 · IDOR — client-bepaalde `userId` zonder ownership-check  · *defense-in-depth (sign-up restricted)*
**Locatie:** `JeffriesHomeapp/app/api/backend/[...path]/route.ts:55-71`; `backend/internal/handler/transaction.go`, `privacy.go`, `email.go`, `loonstrook.go`, `personal_event.go`

De proxy haalt de Clerk-`userId` alleen op voor het pad `notes` (`shouldInjectUserId`). Voor alle andere user-scoped endpoints stuurt de client zelf `?userId=...` mee; proxy én backend nemen die over zonder ownership-check (`r.URL.Query().Get("userId")`). De eigenaar-id staat hardcoded in de repo/compose.

**Reconciliatie:** de middleware draait wél (in Next 16 ís `proxy.ts` de middleware-conventie — geverifieerd via `.next/server/functions-config-manifest.json`), en de hele `/api/v1`-groep zit achter `authMw` (`backend/internal/server/routes.go:51`). Dit is dus géén anonieme exposure. Omdat sign-up tot de eigenaar beperkt is, blijft dit defense-in-depth.

**Fix:** leid `userId` server-side af in de proxy voor álle user-scoped paden (niet alleen `notes`); strip/overschrijf client-`userId` in query én body; voeg backend-ownership-checks toe (`WHERE id=$1 AND user_id=$2`).

### S2 · Eén gedeelde sleutel + zwakke defaults  · **high**
**Locatie:** `backend/internal/server/server.go:163-176`; `backend/internal/config/config.go:95,117`; `backend/internal/store/laventecare.go:4624-4654`

- API-key-check gebruikt `key != secretKey` — niet constant-time (timing-zijkanaal); `crypto/subtle` komt nergens voor.
- `APP_SECRET_KEY` defaultt naar `"change-me"` zonder startup-guard → API staat open als de env-var ontbreekt.
- Drie trust-grenzen op één secret: `BRIDGE_API_KEY` defaultt naar `APP_SECRET_KEY` (config.go:117), en dezelfde key wordt (zonder `LAVENTECARE_SECRET_KEY`) de AES-sleutel voor klant-credentials. De bridge stuurt die key bovendien als bearer-header over internet.

**Fix:** `subtle.ConstantTimeCompare`; fataal weigeren te booten bij lege/`change-me`-key in productie; bridge en credential-encryptie elk een eigen, losgekoppeld secret geven.

### S3 · Rate-limiter omzeilbaar via gespoofte forwarded-headers  · **high**
**Locatie:** `backend/internal/server/server.go:37` (chi `RealIP`); `backend/internal/middleware/ratelimit.go:60-87`

`RealIP` overschrijft `RemoteAddr` met client-gestuurde `X-Forwarded-For`/`X-Real-IP`/`True-Client-IP` zonder trusted-proxy-allowlist; de limiter keyt daarop → unieke vervalste header per request = verse bucket. Dit is de énige brute-force-bescherming op de gedeelde sleutel.

**Fix:** forwarded-headers alleen vertrouwen van Render's edge, of `RealIP` droppen en op de echte TCP-peer keyen.

### S4 · CORS reflecteert elke origin bij lege config  · **medium**
**Locatie:** `backend/internal/server/server.go:138-161`; `config.go:229-245`

`if originSet[origin] || len(origins) == 0` reflecteert élke origin terug mét `Allow-Credentials: true` zodra `CORS_ORIGINS` leeg is (en `envSliceOr` kan stil leeg worden bij `"[]"`/`" "`). Default is veilig; impact nu beperkt omdat auth via header (niet cookie) gaat.

**Fix:** bij lege set juist weigeren; `Allow-Credentials` weghalen.

### S5 · `NEXT_PUBLIC_API_KEY`-fallback kan backend-key in client-bundle lekken  · **medium**
**Locatie:** `JeffriesHomeapp/app/api/backend/[...path]/route.ts:28`; `app/laventecare/documenten/[documentKey]/page.tsx:71`

`backendApiKey()` valt terug op `process.env.NEXT_PUBLIC_API_KEY`. Niet gezet vandaag, maar zodra iemand die env-var zet, inlinet Next de backend-key in elke browser. **Fix:** verwijder de `NEXT_PUBLIC_API_KEY`-tak in beide functies.

### S6 · Overige (low/medium)
- Zwakke, raadbare `BACKEND_API_KEY` in `.env.local` (correct gitignored, geen repo-leak) — roteren naar lange random waarde. *(medium)*
- Customer-credential-encryptie afgeleid van `APP_SECRET_KEY` (zie S2); `LAVENTECARE_SECRET_KEY` verplicht maken. *(low)*
- `DATABASE_URL`-default met `sslmode=disable`. *(low)*
- Geen request-body-size-limit op JSON-decoding (resource-exhaustion). *(low)*
- GAS-/Telegram-bridge-/bunq-callback-secrets worden door géén endpoint geverifieerd → dode security-indicatoren in de settings-UI; ofwel bedraden+verifiëren, ofwel verwijderen. *(low)*
- PDF-/dossier-routes zijn niet geauthenticeerd en embedden deels-gecontroleerde URL's. *(low)*

---

## 🟠 Correctheid / bugs

### B1 · Maandfilter financiën kapot voor ~5 maanden/jaar  · **high**
**Locatie:** `JeffriesHomeapp/hooks/useTransactions.ts:109-112, 159-162`

`datumTot = "${maandFilter}-31"` → `2026-02-31`, `2026-04-31`… Postgres (`DATE`-kolom) weigert die hard → 500 → frontend slikt het en toont een lege lijst. Februari/april/juni/september/november geven dus **nul** transacties (initiële fetch én loadMore).
**Fix:** echte laatste dag via `new Date(y, m, 0).getDate()`, of exclusieve volgende-maand-grens.

### B2 · CSV-import verschijnt pas na reload  · **high**
**Locatie:** `JeffriesHomeapp/components/finance/CsvUploader.tsx:27-90` + `hooks/useTransactions.ts`

`useTransactions` is géén TanStack-Query-hook maar lokale `useState`; `CsvUploader` muteert een eigen geïsoleerde instance, de finance-pagina krijgt geen invalidatie/refetch.
**Fix:** migreer naar `useQuery`/`useInfiniteQuery` + `invalidateQueries`, of geef `CsvUploader` een `onImported`-callback.

### B3 · DST/timezone-bug in week-berekening  · **medium**
**Locatie:** `backend/internal/engine/cron_schedule.go:60-64`

`Truncate(24h)` rekent in UTC i.p.v. Europe/Amsterdam → de "volgende week"-einddatum is *elke week* één dag te ver, waardoor de Telegram-uren-waarschuwing structureel over-telt.
**Fix:** expliciete `time.Date(y,m,d,0,0,0,0, amsterdam)` (zoals `cron_schedule.go:35`).

### B4 · bunq-idempotentiegat — dubbel betaalverzoek bij retry  · *uit integraties-map*
`bunq.CreatePaymentRequest` stuurt een verse `X-Bunq-Client-Request-Id` per poging → een retry kan een dubbel betaalverzoek aanmaken. **Fix:** stabiele idempotency-key per logisch verzoek.

### B5 · Mail send-then-mark race  · *uit integraties-map*
`MarkMailOutboxSending → mail.Send → MarkMailOutboxSent`: faalt de laatste stap na een echte Graph-send, dan blijft de rij op `sending`/`sent_unconfirmed` → risico op dubbele verzending bij retry.

### B6 · Onbegrensde Telegram-goroutines + stille fouten  · *uit engine/telegram-map*
`loopTelegram` spawnt `go func(u)` per update zonder worker-pool (elk draait volledige `ProcessAIPrompt`). Plus pervasief `_ = client.Send(...)`. **Fix:** worker-pool/semafoor; fouten loggen.

### B7 · Overige (low)
- Context-cancellation-gat bij in-process engine-shutdown (`cmd/api/main.go:49-52`).
- `context.WithValue` met bare string-key (`cron_workers.go:466`) — faalt `go vet` SA1029.
- `firedMu` beschermt `e.firedAt`, maar dezelfde map gaat ongelockt by-reference `ShouldFire` in.
- Telegram-offset wordt vóór de async-handler verhoogd → updates kunnen verloren gaan bij restart.
- Stille `json.Unmarshal`-fouten bij decoden van automation-trigger/action-config.
- `pgx.ErrNoRows` met `==` i.p.v. `errors.Is` (12×).
- `useEmails`: unread-teller-drift bij her-markeren + geen in-flight-cancellatie (hook nu nog ongebruikt).
- `usePrivacy`: localStorage-override per-scope maar niet per-user; Salary "huidige maand" gebruikt browser-tz i.p.v. Europe/Amsterdam.

---

## 🟠 Ops / infra

### O1 · Schema-bron-van-waarheid is dubbel en gevaarlijk  · **high**
**Locatie:** `backend/cmd/api/main.go:39`, `cmd/engine/main.go:48`; `backend/internal/store/runtime_schema.go`; `backend/migrations/*.sql`

`migrations/*.sql` wordt door geen code uitgevoerd; `EnsureRuntimeSchema` is de echte applier — maar die doet alleen `ALTER TABLE` op tabellen die **alleen in de migraties** worden aangemaakt (`notes`, `transactions`, `lc_leads`…). Op een verse DB faalt de boot dus tenzij iemand de migraties handmatig draaide. De Dockerfile kopieert `migrations/` zelfs naar de image en `.env.example:22` adverteert `MIGRATIONS_DIR=/migrations` — beide dood.
**Fix:** kies één bron — óf golang-migrate met `embed.FS` (voor `EnsureRuntimeSchema` draaien), óf verwijder `migrations/`+`MIGRATIONS_DIR` en verklaar `runtime_schema.go` canoniek.

### O2 · Geen backups voor bank- + zorg-data  · **medium**
Geen `pg_dump`/PITR/WAL in de repo, geen `render.yaml`. **Fix:** Render daily backups/PITR aanzetten + lokale `pg_dump`-job; documenteren. Behandel `transactions`/`salary`/`loonstroken`/`lc_access_credentials` als beschermde tabellen.

### O3 · Geen CI / type-check-gate uit  · **medium**
Geen `.github/workflows` → lint, type-check en Playwright draaien nooit automatisch. `next.config.ts` zet `typescript.ignoreBuildErrors: true` (nu schoon via `tsc --noEmit`, maar toekomstige type-fouten shippen). Lint-regels op "warn".
**Fix:** minimale CI (`tsc --noEmit`, `go test ./...`, `eslint`, Playwright); `ignoreBuildErrors` weghalen.

### O4 · Overige (medium/low)
- `docker-compose.yml` mapt postgres hard op `5432:5432` terwijl README `15432` (`DB_PUBLISHED_PORT`) belooft → host-conflict. *(medium)*
- Geen healthcheck/graceful-shutdown op de `api`/`engine`-compose-services. *(medium)*
- Destructieve `*.down.sql` (drop `transactions`/`salary`/`loonstroken`) zonder guard — inert vandaag maar een footgun. *(medium)*
- Docker base-images met zwevende tags (`golang:alpine`, `node:22-alpine`), geen digest → niet-reproduceerbaar. *(low)*
- "Engine NOOIT op Render" alleen comment + default-flag, geen harde guard. *(low)*

---

## 🟣 Kwaliteit / onderhoud

### Q1 · Twee parallelle API-clients (orval + handgeschreven `lib/api.ts`)  · **medium**
~1600 regels dubbel onderhoud + drift-risico; LaventeCare-types bestaan op drie plekken. Geen actieve runtime-bug (parallelle/dode code). **Fix:** consolideer op de gegenereerde client; verwijder dubbele types.

### Q2 · LaventeCare god-modules  · **medium**
`backend/internal/store/laventecare.go` = **4681 regels / 177 functies** (groter dan de 9 volgende store-files samen); handler 2712, model 1099; `JeffriesHomeapp/app/laventecare/page.tsx` 1765. **Fix:** file-split per sub-domein (`laventecare_companies.go`, `_billing.go`, …) — zero API-wijziging.

### Q3 · Tests bijna afwezig  · **high**
1224 test-regels voor ~45k backend-regels; de hele handler-laag, `apiKeyMiddleware` en `config.Load` hebben **nul** tests. Frontend: 2 Playwright-specs voor 127 componenten. **Fix:** begin met `httptest` table-driven handler-tests + een test op het `apiKeyMiddleware`-rejectiepad + `config.Load`-defaults.

### Q4 · OpenAPI-spec stale → orval-client mist endpoints  · **medium**
`backend/docs/swagger.json` is van 06-03, routes van 06-12 → de gegenereerde client mist o.a. `/focus/summary`, `/ai/pending/*`, `/bridge/*` en ~38 LaventeCare-endpoints (draaien daarom via de handmatige client). **Fix:** `swag init` regenereren + CI-diff-check; daarna orval opnieuw draaien.

### Q5 · Overige (low/medium)
- `useLaventeCare` herhaalt 38× exact dezelfde volledige-tree-invalidatie (over-fetcht o.a. een dure AI-endpoint bij élke mutatie) → mutation-factory + scoped keys. *(medium)*
- `uuid.Parse(chi.URLParam(...))`-boilerplate 49× zonder helper; `queryInt` staat in `room.go` i.p.v. `respond.go`. *(medium)*
- ~40 rauwe SQL-queries in `focus.go`/`settings.go` (laag-schending). *(medium)*
- Hardcoded production-achtige defaults/secrets in `config.Load()`. *(medium)*
- Dode/debug-code: `backend/test_executor.go`, `parse_cors.go`, `_archive/convex/client.go`; lege `tmp_convex/`, `_archive/` in frontend. *(low)*
- Inconsistente react-query queryKey-conventies; 14 onderling-afhankelijke boolean feature-flags voor de engine. *(low)*

---

## ✅ Wat juist góed zit (weerlegde bevindingen)

De verificatie-ronde wierp 10 bevindingen om — nuttig, want ze bevestigen sterke punten:

- **Geen SQL-injectie.** Alle dynamische queries gebruiken `$N`-placeholders en whitelisted kolomnamen.
- **Clerk-middleware werkt.** In Next 16 ís `proxy.ts` de middleware-conventie; route-protectie is actief (geverifieerd via `functions-config-manifest.json` + gecompileerde bundle). Hernoemen naar `middleware.ts` zou de bescherming juist breken.
- **Geen cross-user cache-leak.** De idb-keyval-cache is niet user-scoped, maar omdat de backend single-tenant is, is er geen cross-user-leak (wel hygiëne: stale tot 24u, geen `queryClient.clear()` bij logout).
- **Geen dubbele WiZ-command-uitvoering** bij ack-failure.
- **`useDevices`/`useFocusData`** query-key- en next-item-claims waren onjuist.
- **TS type-check is nú schoon** (`tsc --noEmit` = 0 fouten), ondanks `ignoreBuildErrors`.
- Migratie-set is asymmetrisch (22 up, 3 down) maar dat is bewust/acceptabel voor forward-only.

---

## Aanbevolen volgorde (hoogste leverage eerst)

1. **Maandfilter-fix (B1)** + **CSV-invalidatie (B2)** — kapotte features, kleine fixes.
2. **Schema-bron-van-waarheid kiezen (O1)** — grootste latente ops-risico.
3. **Boot-guard + constant-time key + losgekoppelde secrets (S2)** — ook al is sign-up restricted, dit beschermt tegen misconfig.
4. **Backups + minimale CI (O2, O3)** — vangnetten voor bank/zorg-data.
5. **Rate-limiter trusted-proxy-fix (S3)** en **CORS lege-set-fix (S4)**.
6. **Defense-in-depth IDOR (S1)** — `userId` server-side afleiden + backend-ownership.
7. Opruimen: één API-client (Q1), LaventeCare-files splitsen (Q2), swagger regenereren (Q4), dode code weg.

---

*Gegenereerd door een multi-agent review (82 agents, 7 dimensies, adversariële verificatie). Severities zijn na-verificatie gecorrigeerd; elke bevinding is tegen de actuele code geverifieerd.*
