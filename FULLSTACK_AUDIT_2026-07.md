# Full-stack audit — 2026-07-01

_Read-only multi-agent audit van de volledige codebase: backend (JeffriesBackend, ~45k LOC Go) + frontend (JeffriesHomeapp, ~65k LOC TS/React). 8 parallelle audit-agents (finance/bunq, core/auth/infra, engine, store/model, handlers/integraties, frontend core/auth, frontend components/hooks, frontend domein-logica). Alle HIGH-bevindingen zijn daarna handmatig geverifieerd tegen de broncode._

**Totaal:** 🔴 0 kritiek · 🟠 8 hoog · 🟡 24 medium · ⚪ ~30 laag

> Context: single-tenant persoonlijke app (één eigenaar). Backend op Render (API + engine in-process), frontend op Vercel met Clerk-auth. Tijdzone Europe/Amsterdam is bedrijfskritisch. `backend/migrations/` is dood — schema draait via `internal/store/runtime_schema*.go`. Severity is beoordeeld in die context (geen multi-tenant, één gebruiker), maar "hoog" = kan geld, data of beschikbaarheid direct raken.

---

## Executive verdict

De codebase is volwassen en op de meeste plekken netjes verdedigd: auth-dekking is compleet en constant-time, CORS is strikt allowlist-only, geld staat end-to-end in integer-centen, de rate-limiter is spoof-resistent, de Telegram-poller heeft een advisory-lock tegen dubbele pollers, en de pending-action-lifecycle is atomair. De eerder gefixte klassen (Focus privacy-mask reactief, notes optimistic-concurrency, Amsterdam-datums) houden grotendeels stand. Er zijn **geen kritieke gaten** meer zoals in de LaventeCare-audit.

De acht hoge bevindingen clusteren rond vier thema's:

1. **Eén live secret in git** — een werkende Todoist API-token staat hardcoded in `GoogleScripts/Code.gs`, voor altijd in de historie. Roteren.
2. **Geld-integriteit** — het bunq-betaalverzoek is niet-atomair en niet-idempotent (bevestigd door twee agents): een timeout of crash op het verkeerde moment stuurt de klant een tweede live betaalverzoek. En de salaris-prognose past elk tarief-wijzigingsmaand het oude CAO-tarief toe door een UTC-vs-lokaal `Date`-vergelijking.
3. **Reproduceerbaarheid (DR)** — de performance-indexes (o.a. GIN full-text op e-mails en notities, de chat-history index) bestaan alléén in de dode migrations-map. Een verse of herstelde database mist ze allemaal: de grootste tabellen worden seq-scanned op elke mailbox-/notitie-/Telegram-actie.
4. **Regressies die stil falen** — `mailContactByEmail` 500t sinds de business-core kolommen (mail naar een bekend contact is kapot), de habits-hook schrijft vinkjes naar de UTC-datum (streak-corruptie tussen middernacht en ~02:00), en de Finance CSV-export corrumpeert 100% van de rijen door ongequote komma-decimalen in een komma-gescheiden bestand.

Daaronder ligt een brede medium-laag: informatielekken in error-responses, een service worker en IndexedDB-cache die gevoelige data (loonstroken, facturen, mailbodies, credential-hints) onversleuteld en zonder purge-bij-uitloggen op het apparaat bewaren, prompt-injection-paden vanuit e-mail naar ongeconfirmeerde AI-tools, en diverse "silent failure" UX-gaten (categorie-edit, loonstrook-import, wekkerpack) waar een mislukte mutatie geen enkele feedback geeft.

Aanbevolen volgorde: (1) token roteren, (2) bunq idempotent maken, (3) runtime-schema de ontbrekende indexes laten aanmaken, (4) de drie stille regressies fixen, dan de medium-laag.

---

## 🟠 HOOG (8)

### H1 · Live Todoist API-token committed in git (OVERSLAGEN)

`GoogleScripts/Code.gs:65`
`const TOKEN = '4309998c3e3588535556645b55f67769ea65430c'` is een echte 40-hex Todoist personal token, getrackt in HEAD. De comment zegt "vervang na 1x uitvoeren" — dat is nooit gebeurd. Iedereen met repo-toegang (en de historie, permanent) krijgt volledige lees/schrijf-toegang tot het Todoist-account. **Actie:** token intrekken bij Todoist, nieuwe genereren, en de waarde uit de source verwijderen (historie-exposure accepteren of scrubben). Dit was het enige echte secret in een volledige entropy-scan van getrackte files.

### H2 · [GEFIXT] bunq-betaalverzoek is niet-atomair en niet-idempotent (dup-payment) — *bevestigd door 2 agents + handmatig*
`backend/internal/engine/executor.go:2978-3002` · `internal/bunq/client.go:260,352`
Het `RequestInquiry` wordt bij bunq aangemaakt (2978) vóórdat de factuur `provider_request_id`/`payment_url` krijgt (2993). Faalt de persist, crasht het proces, of geeft de POST client-side een timeout ná dat bunq hem verwerkte (20s client-timeout ≠ niet-uitgevoerd), dan ziet de guard op 2970 een schone factuur en stuurt een retry/re-approve de klant een **tweede live betaalverzoek**. Er is geen idempotency: `X-Bunq-Client-Request-Id` is per call vers-random en bunq dedupt er niet op. **Actie:** persist een idempotency-key/`provider_request_id` vóór de bunq-call (of query bunq op `merchant_reference` vóór create), en maak create+persist atomair.

### H3 · [GEFIXT] `mailContactByEmail` scan/kolom-mismatch — mail naar bekend contact 500t — *handmatig bevestigd*
`backend/internal/store/laventecare_mailbox.go:1146-1163` vs `laventecare.go:4733`
Beide SELECTs leveren 11 kolommen (`id … notities, created_at, updated_at`), maar de rijen worden verzameld met `scanContact`, dat 13 destinations scant (incl. `PreferredChannel`, `DecisionRole`). pgx v5 faalt op destination-count-mismatch. `CreateMailFromTemplate` → `buildMailRenderContext` → `mailContactByEmail` wordt onvoorwaardelijk aangeroepen, dus **templated mail sturen naar een e-mail die matcht met een `lc_contacts`-rij crasht met een scanfout.** Regressie: de business-core kolommen werden aan `scanContact` toegevoegd en `ListContacts`/`GetContact` werden bijgewerkt, deze twee queries zijn gemist. **Actie:** voeg `preferred_channel, decision_role` toe aan beide SELECT-lijsten (of gebruik een smallere scanner).

### H4 · [GEFIXT] DR-index-gap: performance-indexes bestaan alléén in de dode migrations-map — *handmatig bevestigd*
`backend/internal/store/runtime_schema.go` / `runtime_schema_base.go` vs `backend/migrations/*.sql`
`runtime_schema` maakt tabellen + de unieke/upsert-indexes, maar een verse/herstelde DB krijgt nooit de niet-unieke indexes die alleen in `migrations/` staan. Geverifieerd ontbrekend in runtime-schema: `idx_emails_search` (GIN full-text — `EmailStore.Search` doet `to_tsvector('dutch', …)`; grootste tabel, seq-scan op elke mailbox-view), `idx_notes_search` (GIN, matcht `NoteStore.Search` exact), `idx_chat_messages_chat_id (chat_id, created_at DESC)` (`GetHistory` draait op elke Telegram-boodschap tegen een nooit-geprunede tabel), plus de user/datum/next-action indexes op notes, habits, schedule, transactions, personal_events, devices, note_links, audit, lc_contacts, lc_leads, lc_decisions, lc_change_requests, lc_sla_incidents. De boot-test controleert alleen de 6 upsert-kritieke unieke indexes, dus dit gat is onzichtbaar in CI. **Actie:** verplaats alle index-DDL naar runtime_schema (idempotent `CREATE INDEX IF NOT EXISTS`) en breid de boot-test uit.

### H5 · [GEFIXT] LaventeCare PDF-route mist owner-check — *handmatig bevestigd*
`JeffriesHomeapp/app/api/laventecare/pdf/[documentKey]/route.tsx:26-86`
De backend-proxy (`app/api/backend/[...path]/route.ts:79-81`) wijst elke niet-owner Clerk-user af, en de dossierpagina gate't op `isOwner()`. Deze PDF-route doet géén `auth()`/owner-verificatie — hij vertrouwt volledig op de middleware-check "een geldige Clerk-sessie" en rendert het document direct uit URL-params. Het beleid van de repo zelf is expliciet "vertrouw niet op de out-of-band Clerk sign-up restrictie", en `/sign-up` is publiek. Elke gebruiker die een Clerk-sessie op deze instance bemachtigt kan zo de volledige LaventeCare-documentcatalogus downloaden. **Actie:** dezelfde owner-allowlist-check als de proxy vooraan deze route zetten.

### H6 · [GEFIXT] Habits "vandaag" gebruikt UTC-datum i.p.v. Amsterdam — streak-corruptie — *handmatig bevestigd*
`JeffriesHomeapp/hooks/useHabits.ts:301` (+ payload op 416, 433)
`datum: datum ?? new Date().toISOString().slice(0, 10)` bouwt zowel de for-date query als de toggle/increment-payload. `DailyChecklist` and `app/habits/page.tsx` (initieel `selectedDate=""`) vallen beide in de default. Tussen 00:00 en ~01:59 Amsterdam (CEST) is de UTC-datum nog gisteren: de checklist toont gisteren's habits en een vinkje wordt naar het log van **gisteren** geschreven → streaks/perfect-days corrupt. De juiste helper (`todayStr()` in `HabitsUtils.ts:51`) bestaat maar wordt hier niet gebruikt — een gemiste plek van precies de klasse die de "Amsterdam-datums" audit elders fixte. **Actie:** vervang de default door de Amsterdam-helper.

### H7 · [GEFIXT] Salaris-prognose past CAO-tarief een maand te laat toe (UTC vs lokaal `Date`) — *handmatig bevestigd*
`JeffriesHomeapp/lib/salaryForecast.ts:60-67`
`getTarief` bouwt `peilDatum` als lokaal-middernacht `new Date(jaar, maand-1, 1)` maar parst `entry.vanaf` met `new Date("YYYY-MM-DD")` = UTC-middernacht. In Amsterdam ligt lokaal-middernacht 1–2u vóór UTC-middernacht, dus `peilDatum >= vanaf` is false in de maand waarin het nieuwe tarief ingaat. Elke grensmaand (bijv. 2025-08, 2026-01) rekent bruto/netto, ORT-euro's en de reiskosten-km (0,16 vs 0,20) met het **oude** tarief. Loonstrook-kalibratie maskeert dit alleen voor maanden die al een loonstrook hebben — dus nooit voor de vooruitkijkende prognose waarvoor deze functie bestaat (Focus "Netto"-tegel, SalarisView-prognoses). **Actie:** vergelijk als string (`"YYYY-MM"`) of parse `vanaf` also lokaal.

### H8 · [GEFIXT] Finance CSV-export corrumpeert elke rij (ongequote komma-decimaal) — *handmatig bevestigd*
`JeffriesHomeapp/components/finance/FinanceUtils.ts:16` (knop: `app/finance/page.tsx:476`)
`tx.bedrag.toFixed(2).replace(".", ",")` schrijft bijv. `-25,00` **ongequote** tussen komma's in een komma-gescheiden bestand met 6-koloms header (`Datum,Tegenpartij,Omschrijving,Bedrag,Code,Categorie`). Elke datarij heeft daardoor 7 velden: een consumer leest Bedrag als `-25`, Code als `00` en Categorie als de code. 100% van de geëxporteerde rijen is stil corrupt. **Actie:** quote het bedrag-veld (`"${…}"`) of gebruik `;` als delimiter (consistent met de Rabobank-parser die `;` al ondersteunt).

---

## 🟡 MEDIUM (24)

**Backend — stabiliteit & crashes**
- **[GEFIXT] Geen panic-recovery rond cron-jobs** — `engine/cron.go:96-103`. `execJob` draait `job.RunFunc` zonder `defer recover()`; elke job in eigen goroutine. Een panic in een sync-worker (Gmail/Calendar/Todoist/bunq/briefing) killt het hele proces (API + engine in-process op Render). Automation-tick, command-poller en Telegram hebben wél recovers — cron is het gat.
- **[GEFIXT] Nil-pointer crash in `/sync` Gmail-goroutine** — `engine/telegram_commands.go:620`. `meta.LastFullSync` derefereert `meta` zonder nil-guard, terwijl `GetSyncMeta` `(nil, nil)` teruggeeft als er geen `email_sync_meta`-rij is (verse DB of `GMAIL_ENABLED=false`). `/sync` + geslaagde Gmail-sync → panic in goroutine zonder recover → hele backend crasht.
- **[GEFIXT] `afspraakBewerken` maakt van een un-pushed create een gedoemde update** — `engine/executor.go:2461`.
  Bewerk je een AI-afspraak vóór de 5-min pending-cron hem pusht ("maak afspraak 10:00" → "nee, 11:00"), dan PATCHt de worker `/events/ai-<uuid>` in Google → 404 → 5 retries → dead-letter + "agenda-sync probleem"-alert. De afspraak bereikt Google nooit, terwijl beide tool-calls `ok:true` gaven.

**Backend — geld & data-integriteit**
- **[GEFIXT] `CreateInvoice` kan uren dubbel factureren onder retry-race** — `store/laventecare.go:3196-3333`. `invoiceLinesFromTimeEntries` leest entries (filter `invoice_id IS NULL`) vóór `tx.Begin` en zonder `FOR UPDATE`; de in-tx claim hercontroleert `invoice_id IS NULL` niet. Twee overlappende creates (dubbelklik / AI-retry) bouwen beide regels uit dezelfde uren; beide facturen committen ze.
- **[GEFIXT] TOCTOU + exacte-string dedupe laat twee pending payment-acties dubbelvuren** — `engine/executor.go:2960` + `store/ai_pending.go:120`. `FindPendingByToolArgs` vergelijkt `args_json` als exacte string; de HTTP-handler marshalt een map, the AI-pad slaat raw argsJSON op (spacing/casing kan verschillen) → twee pending-acties voor dezelfde factuur kunnen naast elkaar bestaan en beide de bunq-guard passeren.
- **[GEFIXT] Telegram "Huidig saldo" is netto-flow, niet banksaldo** — `engine/telegram_dashboards.go:330,380`. `GetStats.saldo = SUM(bedrag)` over álle transacties (incl. interne overboekingen) sinds de dataset begon (2018), gelabeld als "Huidig saldo" en door de AI-tool als "huidig totaalsaldo" gebruikt. Het web-dashboard gebruikt correct `saldo_na_trn` van de laatste transactie per IBAN. De twee verschillen met het openingssaldo.
- **[GEFIXT] Telegram "vorige maand" AddDate-normalisatie toont verkeerde maand** — `engine/telegram_dashboards.go:444`. `now.AddDate(0,-1,0)` op 31 juli → "juni 31" = 1 juli; op 29-31 maart (niet-schrikkeljaar) → maart. Op die dagen rendert "vorige maand" stil de cijfers van de huidige maand.


**Backend — AI / prompt-injection**
- **[GEFIXT] E-mail/CRM-content bereikt de LLM zonder untrusted-framing, en muterende tools skippen bevestiging** — `ai/prompt.go:64` + `ai/agents.go`. Alleen de "Live Data"-JSON is als untrusted gemarkeerd; tool-results (`leesEmail` met aanvaller-gecontroleerd subject/snippet) worden als raw `role:"tool"` toegevoegd. `lampBedien`, `notitieAanmaken/Pinnen`, `habitAanmaken/Voltooien`, `laventecareBesluit/ChangeRequest/SlaIncident` muteren zónder confirmation-queue. Een crafted e-mailonderwerp tijdens inbox-triage kan ongeconfirmeerde writes triggeren.
- **[GEFIXT] LLM-output gemerged in klantmail-variabelen zonder URL-validatie** — `handler/laventecare.go:2696`. Elke key/value uit de `variables`-JSON van het model (incl. `invoice.payment_url`, `cta.url`, `pilot.login_url`) wordt in de suggestie gemerged; enige controle is een system-prompt-instructie. Met aanvaller-beïnvloedbare context (inbound mail previews) kan een injected instructie een phishing-URL in een kant-en-klaar klant-concept planten. Menselijke review is de enige gate.

**Backend — timeouts & info-leaks**
- **[GEFIXT] Google/Todoist-clients op `http.DefaultClient` (geen timeout); cron zonder per-run deadline** — `google/oauth.go:123,170` + `todoist/todoist.go:328,359` + `engine/cron.go:96`. Cron-jobs draaien met de langlevende engine-context; één stalled TCP-connectie wedged die cron-goroutine permanent tot restart. mail/telegram/grok hebben wél timeouts.
- **[GEFIXT] Raw interne errors doorgegeven aan HTTP-clients** — `handler/respond.go` + ~alle handlers. `Error(w, 500, err.Error())` verscheept raw pgx/SQLSTATE-tekst, constraint-namen en via `google.APIError.Error()` de volledige Google-URL + body. De AI-laag sanitizeert dit (`classifyStoreError`), de HTTP-laag niet.
- **[GEFIXT] Ongeauthenticeerde `/health` lekt raw DB-error-detail** — `handler/health.go:36`. `"detail": err.Error()` van `db.Ping` op de publieke `/` en `/api/v1/health`; pgx-connectiefouten bevatten host/user uit de DSN.


**Frontend — data-op-apparaat & sessie**
- **Service worker cachet geauthenticeerde PDF-/dossier-responses zonder purge** — `app/sw.ts:15-22`. Alleen `/api/backend/` is `NetworkOnly`; de rest valt in serwist's `defaultCache` (NetworkFirst voor `/api/*`, pages, RSC). `GET /api/laventecare/pdf/[key]` en de SSR-dossierview belanden in Cache Storage; geen sign-out-hook purget ze ooit.
- **IndexedDB query-cache bewaart loonstroken, facturen, mailbodies, credential-hints — 24u, onversleuteld, geen purge** — `app/providers.tsx:15-19`. `PERSIST_DENY_PREFIXES` sluit alleen notes/events/schedule/sync uit; `/loonstroken`, `/salary`, `laventecare` cockpit/billing/mailbox worden gedehydrateerd naar idb-keyval en overleven browser-herstart op een gedeeld apparaat.
- **Verlopen Clerk-sessie → cryptische JSON-parse-error, geen 401-recovery** — `proxy.ts:14-19` + `lib/api.ts:51-67`. De middleware redirect (302) elke `/api/backend/*`-fetch naar de sign-in-pagina; `res.ok` is true en `res.json()` gooit `SyntaxError: Unexpected token '<'`. Geen hook vangt 401 op → de 24/7 focus-tablet (refetch elke 30s) degradeert naar cryptische fouten tot handmatige reload.

**Frontend — stille mutatie-faal**
- **[GEFIXT] Checklist-checkbox schrijft volledige note-inhoud zónder concurrency-token** — `components/notes/NoteCard.tsx:80-93`. `toggleCheckbox` → `update(id, { inhoud })` zonder `expectedGewijzigd` (alleen de editor-save passeert de token). Met 24h react-query-cache + mutaties via Telegram/andere apparaten overschrijft een vinkje op een stale kaart nieuwere server-content — precies de clobber die de 409-token moest voorkomen.
- **[GEFIXT] Loonstrook-import invalideert een niet-bestaande query-key** — `components/salary/LoonstrookUploader.tsx:98`. `invalidateQueries(["getLoonstroken"])` maar de Orval-key is `["/loonstroken", params]`. No-op: na "Import geslaagd!" blijft de lijst stale tot refocus/reload.
- **[GEFIXT] Wekker-pack partial failure: oude pack al verwijderd, error nooit gevangen** — `hooks/useAutomations.ts:104-126` + `app/automations/page.tsx:68-76`. `addDienstWekkerPack` verwijdert eerst de oude, dan `allSettled` creates, en gooit bij deelfalen. De pagina heeft `try…finally` zonder catch en `onSave` wordt unawaited gevuurd. Deelfalen → half opgebouwde wekker (origineel weg) met nul feedback.
- **[GEFIXT] Transactie-categorie-edit faalt stil** — `hooks/useTransactions.ts:201-209`. `updateCategorie` await't de PATCH zonder try/catch en update lokale state alleen bij succes; de editor sluit direct. Bij rejectie: geen toast, geen revert — een hercategorisatie persisteert stil niet.
- **[GEFIXT] Settings backup/export is een dode stub met eeuwige spinner** — `app/settings/page.tsx:130,169-181,636`. `const backupData = undefined as any; // TODO`. Klik op "JSON export" zet `backupRequested=true`; het download-effect returnt vroeg en reset nooit → knop blijft disabled met permanente `Loader2`, geen bestand.


**Frontend — datum/tijd & business-logica**
- **[GEFIXT] Privacy-scope defaults zijn inconsistent (`?? true` vs `?? false`) + clobber-on-unloaded** — `app/settings/page.tsx:301` vs `hooks/usePrivacy.ts:55`. Settings toont "Maskeren" voor alle scopes, de pagina's tonen alles zichtbaar. Klik vóór de GET resolvet → de geraden all-`true` set wordt via de upsert-all PUT weggeschreven, echte server-state overschreven.
- **[GEFIXT] Weeknummer-berekening breekt op ISO-jaargrenzen** — `lib/schedule.ts:313-317` + `lib/unified.ts:30-35`. De jan4-formule negeert dat eind-dec bij volgend jaar W01 hoort: `2025-12-29 → "2025-53"`, `2027-01-01 → "2027-00"`. Elke jaarwisseling splitst `generateUnifiedTimeline` één week in twee groepen en mismatcht de ContractWidget-weekbalans. `WeekJournal.tsx` heeft al een correcte ISO-implementatie — drift binnen de repo.
- **[GEFIXT] All-day roosterrijen genereren valse "harde" conflicten** — `lib/conflictDetection.ts:46-67`. `detectConflict` skipt `dienst.heledag`-rijen niet; een heledag-rij (lege tijden → "00:00"–"23:59") markeert elk getimed persoonlijk event die dag als **hard** conflict, wat de conflict-tellers op rooster/agenda/focus opblaast. Ook `getNextDienst` neemt heledag-markers als "volgende dienst".
- **[GEFIXT] Cockpit business-signalen gebruiken verkeerd event-status-vocabulaire** — `store/laventecare.go:4207`. Filtert `status <> 'cancelled'`, maar `'cancelled'` wordt nergens naar `personal_events` geschreven (echt: `VERWIJDERD`/`PendingDelete`). Verwijderde events blijven als "agenda"-signaal opduiken — dezelfde drift die vorige week op /focus gefixt werd.


---

## ⚪ LAAG (selectie van ~30)

**Backend**
- Engine niet gracefully gestopt bij shutdown; DB-pool sluit onder lopende cron/AI-writes — `cmd/api/main.go:54` + `server/server.go:110`.
- Onauth. Swagger UI + volledige API-spec publiek — `server/routes.go:48`.
- `HomeappUserID[:12]` slice kan panieken bij korte config-waarde (in unrecovered goroutine) — `engine/engine.go:214`.
- Rate-limits hardcoded; gedocumenteerde `API_RATE_LIMIT_*` env-knoppen zijn dood; shared-bucket-DoS-risico als `TRUSTED_PROXY_COUNT` niet in dashboard staat — `middleware/ratelimit.go:96`.
- `HOMEAPP_GAS_SECRET` heeft realistische default in source + valse "configured"-signaal — `config/config.go:114`. `BUNQ_CALLBACK_SECRET` is dode config (geen callback-route bestaat) — `config/config.go:79`.
- Containers draaien als root (geen `USER` in Dockerfiles); dode `migrations/` in image gekopieerd.
- Bridge status-poller breekt onder de aanbevolen distinct-key-hardening (`/devices` accepteert alleen APP_SECRET_KEY) — `engine/cloud_bridge.go:171`.
- bunq: volledige re-onboarding elke 15 min (sessies nooit herbruikt, ~96 device-registraties/dag) — `bunq/client.go:236`; account-listing niet gepagineerd — `client.go:104`.
- `GetHistory`/`getBacklinks`/`Incidents30d`/`PerfectDays` negeren `rows.Err()` / slikken errors (stille truncatie / 0-rapportage) — `store/chat.go:54`, `note.go:799`, `habit.go:747`.
- `PerfectDays`-incidentvenster gebruikt server-`CURRENT_DATE` (UTC) i.p.v. Amsterdam — off-by-one rond middernacht.
- Habit-reads/mutations niet user-scoped (alleen UUID) — `handler/habit.go` + `store/habit.go`; automation Update/Toggle/Delete idem.
- "PDF only"-attachment-policy omzeilbaar via lege `content_type` — `handler/laventecare.go:453`.
- `/settings/ai/diagnostics` vuurt twee betaalde Grok-calls per request (geen cache/cooldown) — `handler/settings.go:345`.
- Hue/Saturation niet geclamped vóór HSV-conversie → negatieve RGB opgeslagen — `handler/device.go:402`.
- Todoist "today" via untyped context-key met stale `"2026-01-01"`-fallback — `handler/sync.go:364`.
- Grok multi-round token-usage onder-gerapporteerd (€-rollup telt alleen laatste call) — `ai/grok.go:230`.
- `ExpireOld` (pending-acties) is dode code — expired rows blijven `status='pending'` — `store/ai_pending.go:227`.
- DST-dubbel/gemiste-fire voor 02:00-03:00 automations — `engine/trigger.go:37`. Dagelijkse briefing + interval-only cleanup-crons gemist bij deploy-in-window / frequente deploys — `engine/cron_workers.go`.
- Lamp on/off-detectie matcht "uit"/"aan" als bare substring → *"zet de buitenlamp aan"* zet de lamp **uit** — `engine/telegram_commands.go:1029`.
- Geïmporteerde transactie-datums ongevalideerd (DateStyle-afhankelijke DD-MM swap); `volgnr` lexicaal vergeleken ("9">"10") — `store/transaction.go`, `transaction_stats.go:283`. O(n²) insertion-sort op reverse-gesorteerde input in `/transactions/stats` — `transaction_stats.go:389`.
- Geen security headers (nosniff/CSP/HSTS) — moot voor JSON-API, relevant voor de publieke Swagger-HTML.

**Frontend**
- `usePrivacy` lokale override shadowt server-settings permanent (localStorage-key nooit verwijderd) — `hooks/usePrivacy.ts:49`.
- Spatiebalk-lampschakelaar vuurt terwijl een button focus heeft (alleen INPUT/TEXTAREA/SELECT uitgezonderd) — `hooks/useGlobalShortcuts.ts:22` (bevestigd door 2 agents).
- `loadMore` zonder in-flight-guard → dubbele rijen/keys — `hooks/useTransactions.ts:165`, idem `useEmails.ts:127`.
- In-place `.sort()` muteert de react-query-cache-array — `hooks/useDevices.ts:15`, `useRooms.ts:12`.
- SW-registratie kan overgeslagen worden als `load` al vuurde (geen `readyState`-fallback) — `components/pwa/PwaRegistry.tsx:29`.
- Rabobank-CSV: quoted veld met embedded newline breekt parsing; geen `;`-delimiter-fallback — `lib/rabobank-csv.ts:108`.
- Agenda NextShiftCard mist volgende-ochtend-conflicten van nachtdiensten (wél gefixt op /rooster) — `app/agenda/page.tsx:584`.
- `findLatestLoonstrook` kan met een *toekomstige* loonstrook kalibreren — `lib/salaryForecast.ts:216`.
- Dode localStorage-storage-laag (`loadSchedule`/`saveSchedule`/`loadAutomations`) + stale-remnant-risico; drie verschillende "contract-norm"-definities op aangrenzende views — `lib/schedule.ts`, `lib/automations.ts`, `components/schedule/MonthBalanceChart.tsx`.
- Stale public carve-out `/api/schedule(.*)` in de middleware voor een route die niet bestaat — `proxy.ts:8`.
- Proxy userId-override dekt alleen camelCase `userId`, niet `user_id` in body/query — `app/api/backend/[...path]/route.ts:84`.
- CSP staat stale/brede origins toe (`http://localhost:8000`, `api.x.ai`, `unsafe-eval`) — `vercel.json:15`.

---

## Wat geverifieerd goed is (geen actie nodig)

- **Auth-dekking compleet & constant-time**: elke business-route onder `/api/v1` in de `authMw`-group met `subtle.ConstantTimeCompare`; config weigert te booten met leeg/default `APP_SECRET_KEY` buiten development; geen ongeauth. webhook-surface (Telegram long-polling met advisory-lock, bunq poll-based, mail pull-based).
- **CORS strikt**: exact-origin allowlist met `Vary: Origin`, credentials alleen voor listed origins, lege lijst = deny-all.
- **Geld end-to-end in integer-centen**: `TotalCents`/`PaidCents`, VAT per regel afgerond, nul/negatief afgewezen op drie lagen; import-dedupe via `ON CONFLICT` met de juiste unieke indexes (de historische 42P10-boot-crash is gefixt en getest).
- **Pending-action-lifecycle atomair**: `Claim` = conditional `UPDATE … WHERE status='pending' … RETURNING`; dubbel-approve van dezelfde actie kan niet dubbel-executen; execution-time `IsToolAllowed`-gate.
- **Notes optimistic-concurrency & offline** houden stand (editor-pad): `expected_gewijzigd` → `FOR UPDATE` → `ErrNoteConflict`; onMutate-snapshot → onError-rollback → onSettled-invalidate. (Uitzondering: de NoteCard-checkbox, M-finding.)
- **Geen SQL-injectie**: alle user-waarden via positional params; de twee dynamische SET-builders gaan door een kolom-allowlist / hardcoded keys.
- **XSS-oppervlak schoon**: geen `dangerouslySetInnerHTML`; alle mail-HTML in `sandbox=""` iframes met escaped interpolatie en allowlisted CTA-hrefs.
- **Frontend-secret-handling**: `X-API-Key` alleen server-side geïnjecteerd; geen `NEXT_PUBLIC_*` secrets; owner-allowlist fail-closed in de backend-proxy.
- **Focus privacy-mask nu reactief** (oude lead opgelost) via `useSyncExternalStore` + custom event; Settings privacy-center werkt (modulo de default-drift M-finding).
- **Google-integratie**: token-cache met skew onder mutex, 429/5xx-retry met jitter, idempotente calendar-creates via deterministische event-ID's; inbound-mail degradeert netjes bij ontbrekende Mail.Read-grant.

---

## Aanbevolen volgorde

1. **H1** — Todoist-token roteren (5 min, hoogste urgentie: live credential in historie).
2. **H2** — bunq idempotent + atomair maken (financieel risico naar klanten).
3. **H4** — index-DDL naar runtime_schema verplaatsen + boot-test uitbreiden (DR + performance).
4. **H3, H6, H8** — de drie stille regressies (mail-500, habit-streaks, CSV-corruptie) — kleine, geïsoleerde fixes.
5. **H5, H7** — PDF-owner-check + salaris-tarief-vergelijking.
6. Medium-laag: begin met de crash-klasse (cron-recovery, `/sync` nil-guard) en de data-op-apparaat-cluster (SW/IndexedDB purge bij sign-out), dan de AI-injection-hardening.

_Alle bevindingen zijn read-only vastgesteld; er is niets gewijzigd. Bestandsverwijzingen zijn `pad:regel` t.o.v. de repo-root van elke app._
