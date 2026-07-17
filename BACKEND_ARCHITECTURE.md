# JeffriesBackend — Architecture Map

> Deep-dive reference for the Go backend. Generated 2026-07-09 and fully re-checked/updated against source on 2026-07-17. Companion to `REVIEW.md`, `FULLSTACK_AUDIT_2026-07.md`, and the per-domain audits.

## 1. What it is

A **single-tenant** Go 1.25 backend (chi + pgx/PostgreSQL, no ORM) that powers one person's **"home-OS"** (WiZ smart lamps, personal finance, notes, habits, calendar, work roster) **and** the **LaventeCare CRM** (a solo consultancy's leads→projects→invoices book of business). Personal and CRM data is scoped to one configured `HOMEAPP_USER_ID`; the `user_id` columns are owner filters, not real multi-tenancy. The owner API uses `X-API-Key: APP_SECRET_KEY`. Public intake and the narrow LAN bridge have separate credentials and cannot authenticate to the owner API. A conversational **Telegram bot backed by xAI Grok** is the primary front-end.

**Two binaries:**

| Binary | Entrypoint | Role |
|---|---|---|
| **cmd/api** | `cmd/api/main.go` | HTTP REST API (chi, ~150 routes under `/api/v1`). Optionally co-hosts the engine in-process when `START_BACKGROUND_ENGINE=true` (the Render single-service setup). |
| **cmd/engine** | `cmd/engine/main.go` | Standalone automation engine (Telegram bot + crons + WiZ pollers). **OR**, if `BRIDGE_API_URL` is set, flips to a **DB-less LAN cloud-bridge** (`RunCloudBridge`) that returns before any `store.New`. |

## 2. Boot & wiring

**API boot order (`cmd/api/main.go`):**
1. `config.Load()` → one flat `config.Config` from ~60 env vars.
2. `slog` default handler set at `cfg.SlogLevel()`.
3. `cfg.Validate()` validates `DATABASE_URL` and `HOMEAPP_USER_ID`. Outside development it requires independent random secrets of at least 32 characters for `APP_SECRET_KEY` and `LAVENTECARE_SECRET_KEY`. Bridge/queue mode additionally requires `BRIDGE_API_KEY`; it may never equal the app secret. A configured intake secret is also at least 32 characters and distinct from every other trust-boundary secret.
4. `store.New(ctx, cfg.DatabaseURL)` → pgxpool (MinConns 2 / MaxConns 20) + Ping.
5. **`store.EnsureRuntimeSchema`** runs the idempotent `CREATE/ALTER … IF NOT EXISTS` sequence — **this IS the schema. `migrations/` is dead code.**
6. `engine.RunCleaner(backgroundCtx, db)` always starts for the API's DB-backed lifetime.
7. If `cfg.StartBackgroundEngine`: `engine.New(cfg, db)` + `eng.Run(backgroundCtx)` co-hosts the engine.
8. `server.New(cfg, db)` builds the DI graph + router.
9. `srv.ListenAndServe(ctx)` blocks through normal or signal-driven HTTP shutdown; the API then cancels and joins cleaner/engine workers before `db.Close()`.

**DI graph (`server.New`, order matters, one function):** global middleware first, then leaf deps (`wiz.NewClient`, deviceStore, commandStore), then ~22 handlers each given inline-constructed stores. Telegram client built only if `TelegramBotToken != ""`. Cross-handler wiring: `scheduleH.SetTodoistCleanup(syncH.ReconcileTodoist)` so a roster wipe reconciles Todoist. Finally `registerRoutes(...)`.

**Middleware chain (global, `server.go`):**
`RequestID → slogMiddleware → Recoverer → corsMiddleware → RateLimiter → MaxBytes(50 MiB)` → chi route match → **auth middleware** → handler.

**Auth boundary:**
- `/` and `/api/v1/health` are public. Swagger is public only in `development` and is not mounted in production.
- `POST /api/v1/laventecare/intake` accepts only its dedicated `Authorization: Bearer LAVENTECARE_INTAKE_SECRET`; an empty secret fails closed.
- `/api/v1/bridge/*` accepts only `BRIDGE_API_KEY`. The bridge key is never accepted by the owner API, and `APP_SECRET_KEY` is never accepted by bridge middleware.
- Every remaining `/api/v1/*` route is gated by a constant-time `X-API-Key` comparison against `APP_SECRET_KEY`; an empty configured key fails closed.

**Engine boot (`cmd/engine/main.go`):** `Load → Validate → BRANCH`. If `BRIDGE_API_URL` is set, `RunCloudBridge` runs DB-less. Otherwise: `store.New + EnsureRuntimeSchema → engine.New`, then `RunCleaner` and `eng.Run` execute concurrently and are joined before the pool closes. `Run` fans out goroutines gated by flags: `EngineCronsEnabled` (crons), `EngineAutomationsEnabled` (30s automation loop), `EngineCommandPollerEnabled` (device_commands drain), `EngineStatusPollEnabled` (only if automations loop is OFF), Telegram poller (if token set + enabled).

## 3. Layered package map

| Layer | Package | Role | Biggest / key files |
|---|---|---|---|
| **Entry** | `cmd/api`, `cmd/engine` | Composition roots for the two binaries | `main.go` ×2 |
| **Server** | `internal/server` | Middleware chain, DI graph, route table | `server.go`, `routes.go` |
| **Config** | `internal/config` | Single flat `Config` struct + `Validate()` | `config.go` |
| **Middleware** | `internal/middleware` | Per-IP token buckets (global plus an independent sensitive-operation bucket), body cap | `ratelimit.go`, `bodylimit.go` |
| **Handler** | `internal/handler` | Thin chi handlers: decode→validate→store→JSON | `laventecare.go` (~3120 LOC), `focus.go`, `habit.go`, `contacts.go`, `sync.go`, `pending.go`, `respond.go` |
| **Store** | `internal/store` | Hand-written pgx query structs; boot schema | `laventecare.go` (~5261 LOC), `laventecare_mailbox.go` (~2669), `runtime_schema*.go`, `habit.go`, `note.go`, `contacts.go` |
| **Engine** | `internal/engine` | Automation eval, crons, WiZ control, Telegram bot, AI tool executor | `executor.go` (~3929 LOC), `engine.go`, `cron_workers.go`, `telegram*.go`, `pending_actions.go`, `cloud_bridge.go` |
| **AI** | `internal/ai` | Pure/stateless: Grok client, prompt builder, tool registry, agent policies | `grok.go`, `prompt.go`, `tools.go`, `agents.go` |
| **Integrations** | `internal/google`, `bunq`, `todoist`, `mail`, `telegram`, `whatsapp`, `wiz` | Stateless external clients (no DB) | `oauth.go`, `gmail.go`, `calendar.go`, `bunq/client.go`, `microsoft_graph.go`, `wiz/client.go` |
| **Model** | `internal/model` | ~90 pure structs (json+db tags) | `model.go`, `homeapp.go`, `laventecare.go`, `contacts.go` |

**Key insight on package boundaries:** `internal/engine/executor.go` and `business_context.go` are conceptually the **AI subsystem's tool layer** — they live in `package engine` only because `ProcessAIPrompt` does. The `internal/ai` package is inert (definitions/prompts/policies/HTTP) and never performs a side effect; all effects live in `HomeBotExecutor` + stores.

## 4. Feature domains

| Domain | Handler | Store | Owns tables | Driven by |
|---|---|---|---|---|
| **Smart-home: devices/scenes** | `device.go`, `scene.go`, `room.go`, `bridge.go` | `device.go`, `scene.go`, `room.go`, `device_command.go` | `rooms`, `devices`, `scenes`, `scene_actions`, `device_commands`, `bridge_heartbeat` | HTTP + AI tool (`lampBedien`) + automations; WiZ UDP or `device_commands` queue |
| **Automations** | `automation.go` | `automation.go` (`model.AutomationRow`) | `automations` | Engine `loopAutomations` (30s tick) evaluating `ShouldFire` |
| **Habits** | `habit.go` | `habit.go` (streak engine, FOR UPDATE tx) | `habits`, `habit_logs`, `habit_badges` | HTTP + AI tools (`habitVoltooien`/`habitNotitie`, un-gated) |
| **Notes** | `note.go` | `note.go` (optimistic concurrency, wikilinks, revisions) | `notes`, `note_links`, `note_revisions` | HTTP + Telegram `/noteer` + AI tools |
| **Personal events / agenda** | `personal_event.go` | `personal_event.go` (pending-op state machine) | `personal_events` | HTTP (instant Google sync) + cron (retry path) + AI |
| **Schedule (nurse roster)** | `schedule.go` | `schedule.go` | `schedule`, `schedule_meta` | Cron (Google Calendar pull → Todoist push) + HTTP import |
| **Focus (kiosk dashboard)** | `focus.go` (read-only, one ~24-subquery SELECT over ~9 tables incl. `lc_*`) | raw `db.Pool` | reads only | HTTP `GET /focus/summary` |
| **Transactions / salary / payslips** | `transaction.go`, `salary.go`, `loonstrook.go` | `transaction.go`, `transaction_stats.go`, `salary.go`, `loonstrook.go` | `transactions`, `salary`, `loonstroken` | HTTP CSV/PDF import; AI finance tools |
| **Email (Gmail mirror)** | `email.go` | `email.go` | `emails`, `email_sync_meta` | Cron (incremental history sync) + HTTP + AI |
| **LaventeCare CRM** | `laventecare.go` (~3120 LOC) | `laventecare.go` + `laventecare_mailbox.go` (one `LaventeCareStore` split across two files) | `lc_companies/contacts/leads/projects/workstreams/action_items/quotes/quote_lines/invoices/invoice_lines/time_entries/activity_events/documents/dossier_documents/decisions/change_requests/sla_incidents/access_credentials/mail_templates/mail_outbox/mail_inbox` | HTTP + AI tools + crons (mail sync, digests) |
| **Contacts (unified)** | `contacts.go`, `contact_labels.go`, `contact_channels.go` | `contacts.go`, `contact_labels.go`, `contact_channels.go`, `contact_ai.go`, `whatsapp.go` | `contacts`, `contact_labels`, `contact_label_assignments`, `contact_channels`, `contact_facts`, `contact_important_dates`, `contact_interactions`, `contact_organizations`, `whatsapp_conversations/messages/summaries` | Owner-scoped HTTP + cron (LaventeCare mirror) + AI. `GET /contacts` returns an array per page: `limit` default/max 200, `offset` default 0/max 10000, deterministic name/id order; `q` matches name, e-mail, notes and labels. |
| **AI + Telegram** | `pending.go` (`/ai/pending`) | `ai_pending.go` (`PendingStore`), `ai_call_log.go`, `chat.go` | `ai_pending_actions`, `ai_call_log`, `chat_messages` | Telegram long-poll → Grok tool-calling loop |
| **Sync / Google** | `sync.go` | `sync_run.go`, drives schedule/email/personal_event stores | `sync_runs` (audit) | HTTP `POST /sync/*` + engine crons |
| **Payments / bunq** | `laventecare.go` (payment-request/refresh) | invoice fields + `laventecare_payment_attempt.go` | `lc_invoices`, `lc_payment_request_attempts` | AI tool → **pending action** → provider reconciliation/reservation → engine executor |
| **Todoist** | `sync.go` (`ReconcileTodoist`) | schedule rows | — | Cron push + post-roster-wipe hook |

**Settings / Bridge:** `settings.go` (Overview, GDPR Backup export, Telegram status, AI diagnostics, bunq introspect); `bridge.go` (`bridgeMw`-gated command claim/complete/status for the LAN WiZ bridge).

## 5. Cross-cutting mechanisms

**AI tool-calling loop (`ai/grok.go` → `engine`):**
Telegram msg (voice → Groq Whisper first) → `Engine.ProcessAIPrompt` → per-chat lock + load last-10 history → `BuildSystemPrompt(agent, liveContext, tools)` → `GrokClient.Chat` runs ≤5 rounds (`MaxToolRounds=5`). Each `tool_calls` finish dispatches to `executor.Execute(toolName, argsJSON)`; non-mutating results wrapped in `[UNTRUSTED TOOL DATA]` markers; at the 5-round cap `finalSynthesis` forces one tools-omitted completion so raw tool JSON never leaks. Two gobreaker circuit breakers (chat + web-search) trip on 3 consecutive 5xx. 11 agents exist but the model always answers directly and never names them.

**Pending-actions confirmation pattern (`engine/pending_actions.go`):**
`ConfirmingExecutor` wraps `HomeBotExecutor`. It re-checks `IsToolAllowed` at execution time (hard authz gate on the untrusted model-supplied tool name). For `Mutating + RequiresConfirmation` tools it creates an `ai_pending_actions` row with a 6-hex code (10-min expiry, deduped via `FindPendingByToolArgs`) and returns `confirmationRequired` **without executing**. User `/approve <code>` (or taps) → `ConfirmPendingActionByCode` → `Claim` (atomic pending→confirmed) → `executeClaimedPendingAction` runs against a **plain** `HomeBotExecutor` (no second gate — bypasses confirmation by design). Summaries are enriched (invoice UUID→number+amount+customer, appointment conflict warning) so the human approves a legible action.

**WiZ command path (direct-vs-queue + cloud bridge):**
- **Direct mode** (`LightCommandMode != queue`): engine/handlers call `wiz.Client.SetState/SetScene` over LAN UDP (port 38899, 3s timeout, no retry), then optimistically patch `devices.current_state`.
- **Queue mode**: writes a `device_commands` row; drained by **either** the in-process poller (`commands.go`, `EngineCommandPollerEnabled`) **or** the remote `CloudBridge` (`BRIDGE_API_URL` set) polling `/bridge/commands/claim` every 2s and running UDP locally. `ClaimPending` uses `FOR UPDATE SKIP LOCKED`, auto-requeues stale `processing` (>2min), and auto-fails pendings >10min to prevent a replay-storm on bridge reconnect. `RequeueOrFail` retries 3× then marks the device offline.

**Cron catalog (`RegisterHomeappCrons`, ~20 jobs; `execJob` = recover + 5min timeout):**
Gmail sync, schedule sync (Google Calendar → diensten + immediate Todoist push), personal-events sync, pending-calendar-op processor (retry/dead-letter), Todoist push, contacts reminders/nudges, Telegram daily briefing/digest, appointment reminders (~60min ahead), weekly roster-hours check (Sun 19:00–22:00), mail inbox sync, health/sync-failure alerts, housekeeping. Wrappers: `recordingCron` (writes `sync_runs` audit) and `wrapGoogleCron` (24h-deduped re-auth alert on `invalid_grant`). Proactive-alert dedup has two layers: in-memory `shouldFireAlert` (lost on restart) + persistent `cron_claim`/`briefing_sent` (claimed only **after** confirming there's content). Stubs: `decay-habit-streaks`, `triage-notes-weekly` are log-only.

**Single-tenant ownership:** there is no identity-derived user because this is an owner-only deployment. Personal, finance, sync, AI, Contacts and LaventeCare handlers use the configured `HOMEAPP_USER_ID` and ignore legacy client-supplied `userId`/`user_id` values. Stores still include `user_id` in reads and mutations as a defence-in-depth ownership predicate.

## 6. End-to-end flows

### 1. User Telegram message → router → AI agent → tool call → store write → reply
1. **Poll** — `telegram_lock.go:loopTelegramWithLock` holds `pg_try_advisory_lock(240603409)`; the winner runs `telegram.go:loopTelegram`, which `DeleteWebhook` + `SetMyCommands` then long-polls `telegram.Client.GetUpdatesContext`.
2. **Gate + dispatch** — `processUpdate` owner-checks against `cfg.TelegramChatID`, then `processText` (telegram_commands.go): `handlePendingConfirmationCommand` → slash switch → `expandTelegramCommand` (commandRegistry) → `detectLampCommand` → unknown-slash Levenshtein guard → `routeFreeText` picks an `agentID`.
3. **AI hand-off** — `telegram_ai.go:ProcessAIPrompt` takes the per-chat lock, loads last-10 history from `ChatStore.GetHistory` (`chat_messages`), builds live context, calls `ai.BuildSystemPrompt`.
4. **Grok loop** — `ai/grok.go:GrokClient.Chat` POSTs to `api.x.ai/v1/chat/completions` with the agent's tools (`ai.GetToolsForAgent`), looping up to `MaxToolRounds=5`.
5. **Tool execute** — each tool call routes to `engine/executor.go:HomeBotExecutor.Execute` (wrapped by `ConfirmingExecutor`); read-only tool calls the store and returns Dutch-safe JSON wrapped in `[UNTRUSTED TOOL DATA]`.
6. **Reply** — final text `normalizeAssistantText`-stripped, saved to `ChatStore`, sent via `telegram.Client.SendMessage`; `engine.go:logAICall` writes to `ai_call_log`.

### 2. "Turn on a lamp" in QUEUE mode: intent → device_commands → cloud bridge → WiZ UDP → status back
1. **Enqueue** — HTTP `POST /devices/{id}/command` (`handler/device.go`), OR AI `lampBedien` (`executor.go`), OR automation (`engine.go:applyAction`) → `enqueueDeviceCommand` → `DeviceCommandStore.Create` inserts a `device_commands` row (nullable `device_id` = broadcast) + optimistic `devices.current_state` patch.
2. **Claim** — remote `cloud_bridge.go:RunCloudBridge` polls `POST /bridge/commands/claim` every ~2s → `handler/bridge.go:ClaimCommands` → `DeviceCommandStore.ClaimPending` (`FOR UPDATE SKIP LOCKED`, requeues stale, auto-fails >10min), fires `commands.TouchBridge` into `bridge_heartbeat`.
3. **Execute UDP** — `commandToWizParams` → `wiz/client.go:SendCommand` (`setPilot`, UDP 38899, 3s).
4. **Report back** — `POST /bridge/commands/{id}/complete` → `CompleteCommand` (requeue ≤3 then mark offline); `POST /bridge/devices/{id}/status` → `UpdateDeviceStatus`.
5. **Surface** — `focus.go:Summary` + Telegram `/lampen` read `devices` + `bridge_heartbeat` MAX-timestamp for liveness.

### 3. Automation firing: tick → ShouldFire → action → lamp effect
`engine.go:loopAutomations` (30s tick) → `AutomationStore.List` + `ScheduleStore.ListByDate` (build `todayShiftTypes`) + `getDeviceMap` → per-rule `MinFireInterval`(55s) + persisted `LastFiredAt` guard → `trigger.go:ShouldFire` (HH:MM ±1min, weekday `(wd+6)%7`, shiftType, excludedShifts) → `executeAction`/`applyAction` (direct UDP or enqueue) → `firedAt[id]=now` + `AutomationStore.MarkFired`.

### 4. Gmail + Calendar sync: cron (or POST /sync) → google client → store upsert → surfaced
Cron (`recordingCron`+`wrapGoogleCron`) or `POST /sync/{gmail,calendar}` → `google/gmail.go:SyncGmail` (incremental history API keyed on stored `historyId`, 500-msg fallback on 404) / `calendar.go:SyncScheduleDetailed` → map to `model.*` → `EmailStore.BulkUpsert` (`emails`+`email_sync_meta`), `ScheduleStore.BulkUpsert`+`PruneMissingInDateRange`, `PersonalEventStore.UpsertSynced` (prune bails on zero-fetch) → `sync_runs` audit → post-schedule `pushTodoist` (`todoist.SyncDiensten`). `invalid_grant` → `ErrGoogleReauthRequired` → 24h-deduped Telegram alert.

### 5. LaventeCare invoice → bunq payment request (with the confirmation gate)
1. `POST /laventecare/invoices` → `LaventeCareStore.CreateInvoice`/`CreateInvoiceFromQuote`: `nextLCNumber` (`LC-FAC`), `payment_provider='bunq'`, flips `lc_time_entries`→`gefactureerd` in-tx. Row → `lc_invoices`.
2. `POST /invoices/{id}/payment-request` **does not call bunq** — de-dups then `PendingStore.Create` in `ai_pending_actions` (`tool_name=laventecareBetaalverzoekMaken`, 6-hex code). **The confirmation gate is here.**
3. `/approve <code>` (`ConfirmPendingActionByCode`) or `handler/pending.go:Confirm` → `PendingStore.Claim` (atomic pending→confirmed).
4. `executeClaimedPendingAction` → `createOrReconcileBunqPaymentRequest` first lists provider requests by invoice-number merchant reference, then atomically reserves the invoice's single create attempt in `lc_payment_request_attempts`.
5. `bunq.CreatePaymentRequest` uses a stable per-invoice `X-Bunq-Client-Request-Id`. Success persists provider data and marks the attempt `succeeded`; ambiguous writes become `unknown` and are never blindly retried. `POST /invoices/{id}/payment-refresh` reconciles provider state.

### 6. Daily "🧠 Dagbriefing": cron → context_briefing → AI → Telegram send
`cron_alerts.go:cronDailyAgendaDigest` (Amsterdam morning window, claims `briefing_sent`/`cron_claim` so a redeploy can't double-send, 5-min timeout) → `context_briefing.go:buildContextBriefing` aggregates `ScheduleStore`/`PersonalEventStore`/`NoteStore`/`EmailStore`/`LaventeCareStore.GetCockpit`, `recommendedContextActions` scores on one urgency scale, PII-minimized → seeded as Live Data into `ProcessAIPrompt` (`brain`/`laventecare` agent) → `GrokClient.Chat` → `SendProactiveNotification` (quiet-hours aware, reads `brain_preferences`). On error the claim is released for retry.

### 7. AI-proposed mutation requiring confirmation
`ConfirmingExecutor.Execute` re-checks `ai.IsToolAllowed` → for `Mutates && RequiresConfirmation` (per `ai/agents.go:Policies`) de-dups (`FindPendingByToolArgs`), builds enriched summary, `PendingStore.Create` (`ai_pending_actions`, 6-hex, 10-min) → returns `{confirmationRequired}` **without executing** → user `/approve` → `Claim` → `executeClaimedPendingAction` runs against a **plain `HomeBotExecutor`** (no second gate) → `MarkStatus`. Cancel: `/reject` → `CancelPendingAction`. Expiry sweep: cron `PendingStore.ExpireOld`. **Habits exception:** `habitVoltooien`/`habitNotitie` are the only mutating tools with `RequiresConfirmation:false` (write immediately).

## 7. Gotchas & footguns (ranked by how likely they mislead)

1. **`migrations/` is DEAD CODE.** Real schema = `store.EnsureRuntimeSchema` (~22 idempotent `ensure*` steps in `runtime_schema.go` + `runtime_schema_base.go`). Change a column by editing those, never by writing a migration.
2. **`internal/ai` performs ZERO side effects.** Reading only `internal/ai` gives a false picture — all mutations and confirmation gating live in `internal/engine` (`HomeBotExecutor` + `ConfirmingExecutor`). `IsMutatingTool`/`RequiresConfirmation` are just booleans; the real gate is `ConfirmingExecutor.Execute`, and `executeClaimedPendingAction` deliberately bypasses it.
3. **Two ways to run the engine, inverted defaults.** In-process (`START_BACKGROUND_ENGINE=true`, Render single-service) vs standalone `cmd/engine`. `START_BACKGROUND_ENGINE` is the real gate; legacy `AUTOMATION_ENGINE_ENABLED` remains a backwards-compatible default only when the newer variable is absent. `EngineCommandPollerEnabled`/`EngineStatusPollEnabled` default to `!queueLightCommands`, so in **queue mode the engine does NOT poll commands** — the bridge must.
4. **`BRIDGE_API_URL` flips `cmd/engine` into a DB-LESS mode** (`RunCloudBridge`) that returns before any `store.New`. Queue/bridge mode requires a bridge-only key of at least 32 characters. Validation rejects reuse of `APP_SECRET_KEY`, and bridge middleware accepts only `BRIDGE_API_KEY`.
5. **DR-index gap (a prior self-caused incident).** The `UNIQUE` indexes (`idx_schedule_user_event`, `idx_pe_user_event`, `idx_trx_user_rek_volgnr`, `idx_salary_user_periode`, `idx_loon_user_jr_per`, `idx_sync_user_source`) once lived only in `migrations/`; a restored DB had the tables but not the constraints, so every `ON CONFLICT` upsert raised `42P10` and silently left tables empty. They now live in `ensureBaseTables` — **do not remove them.**
6. **Weekday convention (`trigger.go`):** Go `time.Weekday` (0=Sun) → Python-style (0=Mon) via `(wd+6)%7`. `days==nil` = ALL days; `days==[]` (explicit empty) = NEVER.
7. **LLM day-of-week math is treated as unreliable** — `prompt.go` injects a literal 14-day Dutch weekday table; the model is ordered to look it up, never compute from ISO strings. Removing it reintroduces the weekday bug.
8. **bunq create is guarded at several layers.** Payment is a confirmed pending action; provider state is reconciled first; `lc_payment_request_attempts` permits one active create per invoice; the POST carries a stable client request id. Transport/5xx ambiguity becomes `unknown` and blocks a second create until reconciliation. Preserve all layers—provider-side idempotency semantics alone are not assumed.
9. **Todoist `due:{date}` gotcha (`todoist.go`):** the Sync API `due` object uses the `date` field for both date and datetime; the `datetime` field is IGNORED. Setting it wrong nulls `due` and silently strips the shift's date.
10. **Owner scope is configuration-derived, not request-derived.** Owner-data handlers ignore legacy `userId`/`user_id` query/body fields and use `HOMEAPP_USER_ID`. Some stale Swagger annotations may still advertise those legacy fields; do not reintroduce request-derived ownership while cleaning the generated contract.
11. **Financial immutability is enforced only in Go, not the DB.** `validateInvoiceStatusTransition` blocks reverting a `betaald` invoice or re-amounting `paid_cents`; `validateQuoteStatusTransition` blocks un-accepting. A direct SQL write bypasses all of it. `GenerateInvoiceDocument` does NOT persist `document_url`/`ubl_xml` for `concept` invoices (drafts are preview-only).
12. **`focus.go` `lcClosedStatuses` is a duplicated literal** that must stay in sync with `store/laventecare.go isClosedStatus`, or dashboard "active" counts drift. Any non-COUNT subquery added to its big SELECT must be `COALESCE`-wrapped or the whole Scan errors on empty tables.
13. **Max-rounds JSON leak:** after 5 tool rounds the last message is raw tool-result JSON; `finalSynthesis` issues one tools-omitted completion so it never reaches Telegram. Removing it reintroduces a real past leak.
14. **Telegram is long-poll, not webhook** — `loopTelegram` calls `DeleteWebhook` on start; a Postgres advisory lock (`240603409`, `telegram_lock.go`) ensures only one instance polls (avoids Telegram 409). Two concurrency guards: `aiSem` (engine-wide, max 3) and per-chat `lockChat`.
15. **`WriteTimeout` is 120s specifically because Gmail sync can take 90s** — lowering it re-introduces a double-work bug (server killed the conn while sync succeeded server-side).
16. **`chi.RealIP` is intentionally NOT used** (spoofable). Rate limiting derives client IP itself, trusting `X-Forwarded-For` only for `TrustedProxyCount` hops — **default 0**, so behind Render's edge you must set `TRUSTED_PROXY_COUNT=1` or every request keys on the proxy IP.
17. **`RunCleaner` is a joined long-running worker.** Both `cmd/api` (regardless of whether it co-hosts the engine) and DB-backed `cmd/engine` run it. Shutdown must cancel and join it before closing pgx; DB-less bridge mode deliberately has no cleaner.
18. **Optimistic lamp-state patching is duplicated in ~4 places** and must stay in sync or UI shows stale state for up to 5 min. `color_temp` semantics differ intentionally: commands carry `color_temp_mireds`, DB state carries `color_temp` in Kelvin and clears r/g/b+scene_id (white mode).
19. **WhatsApp privacy boundary is deliberate:** only `whatsapp_summaries` (metadata) ever reaches the AI; raw `whatsapp_messages` bodies stay local. Import auto-flags >2 distinct participants as a group.
20. **Contacts merge durability:** `MergeContacts` and `SyncLaventeCareContactMirror` share a per-user advisory lock. `laventeCareIdentityKey` (Go) must stay byte-identical to the SQL `identity_key` expression. Adding a child table to `contacts` requires adding it to `contactChildTables` or merges orphan that data. Deleting a LaventeCare-sourced contact returns `ErrManagedContact`.
21. **`SyncInbox` / inbound mail needs Azure `Mail.Read` application permission** — without it Graph returns 403 and the endpoint degrades to `200 {ok:false}`. `mail.Send` deliberately does draft-then-send to capture Graph id + conversationId.
22. **Financial magic defaults:** `hourly_rate_cents=7500` (€75), `vat_rate_bps=2100` (21%), `payment_terms_days=14`. Access credentials use AES-256-GCM with a key derived only from `LAVENTECARE_SECRET_KEY`. That vault key is mandatory outside development, at least 32 characters and distinct from app/bridge/intake secrets; there is no app-secret fallback.
23. **Two "contact" concepts:** LaventeCare has its own company-scoped `/laventecare/contacts` (`LCContact`) entirely separate from the unified `/contacts` module (`ContactStore`).
24. **`google.SharedOAuthClient` uses `sync.Once`** — credentials freeze at first call; a refresh-token change only takes effect on redeploy. Google **write** requests are never retried (idempotency relies on deterministic SHA-256 event IDs; 409 = success). Gmail full sync capped at 500 newest messages.
25. **Minor:** `PreferencesStore`/`ChatStore` take a raw `*pgxpool.Pool` while most stores take `*DB`. `SceneStore.GetAll` is N+1. `loonstroken` rows come back as untyped `map[string]any`. Transaction ordering normalizes leading zeros and pads to 50 digits before sorting. Amsterdam tz is loaded once in `init()` and silently falls back to UTC (cron windows would be 1–2h off).

---
*This map is a point-in-time snapshot re-verified on 2026-07-17. The `migrations/`-is-dead and DR-index facts are load-bearing; verify file:line before relying on any specific citation.*
