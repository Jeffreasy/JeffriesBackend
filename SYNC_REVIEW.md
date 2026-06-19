# SYNC LAYER FINDINGS

## [HIGH/bug] (robustness) AI-created appointments hardcode Kalender="AI", an invalid Google calendar id that 404s forever
- verdict: confirmed/high
- loc: backend/internal/engine/executor.go:2181 (afspraakAanmaken) + backend/internal/engine/cron_workers.go:632-644 (calendarTarget)
- desc: When the AI assistant creates an appointment via the afspraakAanmaken tool, it builds a model.PersonalEvent with Status=PendingCreate and Kalender:"AI" (executor.go:2181). The pending-calendar processor resolves the target calendar via calendarTarget(), which only rewrites the calendar id to "primary" when Kalender is empty or equals "Main" (case-insensitive). "AI" passes through unchanged, so CreatePersonalEvent POSTs to https://www.googleapis.com/calendar/v3/calendars/AI/events, which Google rejects with HTTP 404 notFound. This is the exact observed failure ("POST .../calendars/AI/events: HTTP 404 notFound", pendingProcessed:0). "AI" is a synthetic marker meaning "created by the assistant", not a real calendar id — it should target the user's primary calendar. The stored event id is also prefixed (ai-<uuid>) but that is the local event_id, not the cause; the cause is the calendar id.
- rec: Treat "AI" as an alias for the primary calendar. In calendarTarget() (cron_workers.go:632 AND the duplicate in handler/sync.go:304) extend the normalization to: if calendarID == "" || EqualFold(calendarID,"Main") || EqualFold(calendarID,"AI") -> "primary". Better: stop writing the routing marker into the Kalender column at all — in executor.go:2181 set Kalender to the real target ("Main"/"primary" or a configured personal calendar) and track "AI-origin" separately if needed. Backfill existing stuck rows (UPDATE personal_events SET kalender='Main' WHERE kalender='AI'). Note storedCalendarEventID will then prefix the saved id with the resolved calendar; keep the AI alias mapping in one shared helper so all three call sites agree.
- effort: low
- verify: Verified the full causal chain in code; the finding is accurate and the impact is real and permanent.

1. INJECTION (executor.go:2181): afspraakAanmaken builds model.PersonalEvent with Status=PendingCreate and Kalender:"AI" hardcoded. A repo-wide grep confirms this is the ONLY place "AI" is ever written into the kalender column. EventID is also prefixed "ai-"+uuid (line 2164), but as the finding c

## [HIGH/missing] (gaps) Pending calendar ops have no retry cap or dead-lettering — the "AI" 404 loops forever
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:399-454 (cronPendingCalendar), calendarTarget:632-644; root cause backend/internal/engine/executor.go:2164-2182 (afspraakMaken sets Kalender:"AI")
- desc: afspraakMaken writes a personal_events row with EventID="ai-<uuid>", Kalender="AI", status=PendingCreate. The pending processor's calendarTarget only maps ""/"Main" to "primary"; "AI" is passed through literally, so CreatePersonalEvent POSTs to /calendars/AI/events and Google returns 404. On any error the cron logs slog.Warn and `continue`s, leaving status=PendingCreate. There is NO attempts counter, NO next_attempt backoff, and NO terminal/dead-letter state anywhere on personal_events (verified: only device_commands got an `attempts` column in migration 024; personal_events has none). Result: the row is retried every 5 minutes indefinitely, generating a permanent 404 storm and a perpetually-stuck pending op — exactly the symptom described. The same loop applies to any PendingUpdate/PendingDelete that fails for a non-transient reason (bad calendar id, deleted target, malformed time).
- rec: Add `attempts INT NOT NULL DEFAULT 0`, `last_attempt_at`, `last_error` columns to personal_events. Increment on each failed op; after N attempts (e.g. 5) move the row to a terminal status (e.g. PendingFailed) so it stops being picked up by ListPendingCalendar, and record the error. Separately fix the proximate bug: map Kalender "AI" (and any non-real calendar alias) to "primary" in calendarTarget, or stop writing "AI" as the kalender in afspraakMaken.
- effort: medium
- verify: All claims verified against the cited code.

1. afspraakMaken (backend/internal/engine/executor.go:2164-2182) writes a personal_events row with EventID="ai-"+uuid, Kalender="AI", Status=PendingCreate. Confirmed verbatim.

2. calendarTarget (cron_workers.go:632-644) only maps ""/"Main" to "primary"; any other Kalender value (including "AI") is passed through literally as the Google calendarID.

3. 

## [HIGH/missing] (gaps) No way to surface or resolve stuck pending calendar ops to the user
- verdict: confirmed/high
- loc: backend/internal/handler/pending.go (only covers pending_actions table), backend/internal/handler/sync.go:467-551 (GetSyncStatus)
- desc: GetSyncStatus reports a single `pending` count for personal_events (count of PendingCreate/Update/Delete) but offers no detail, no error reason, and no action. The PendingActionHandler (List/Confirm/Cancel) operates only on the pending_actions table (AI tool confirmations), not on stuck calendar PendingCreate/Update/Delete rows. There is no endpoint to list which calendar ops are stuck, why (last_error), or to cancel/retry one. A user whose op is looping on a 404 has no UI/API path to see or clear it — they can only watch the `pending` counter stay non-zero forever.
- rec: Expose stuck pending calendar ops via an endpoint (list with event_id, status, attempts, last_error) and add cancel/force-retry actions that reset attempts or move the row to a cancelled/deleted status. Optionally fire a de-duplicated Telegram alert (reuse shouldFireAlert) when a calendar op exceeds the retry cap, mirroring alertGoogleReauthOnce.
- effort: medium
- verify: All load-bearing claims verified against code. (1) sync.go:478-490,529-536 — GetSyncStatus emits only an aggregate "pending": COUNT(*) FILTER (status IN PendingCreate/Update/Delete) for personal_events, with no detail, error reason, or action. (2) pending.go (List/Confirm/Cancel) routes exclusively through store.PendingStore + engine.ConfirmPendingAction/CancelPendingAction (the pending_actions / 

## [MEDIUM/missing] (robustness) Pending calendar operations have no retry cap or dead-letter — a bad op fails on every 5-minute tick forever
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:399-454 (cronPendingCalendar) + backend/internal/store/personal_event.go (personal_events schema has no attempts column)
- desc: process-pending-calendar runs every 5 minutes and reprocesses every row with status PendingCreate/Update/Delete. On failure it only logs slog.Warn and continues, leaving the status unchanged, so the same failing op is retried indefinitely (the "AI" 404 reproduces this: pendingProcessed:0 every run). This mirrors the device_commands permanent-fail issue that was already fixed: migration 024 added device_commands.attempts and store.RequeueOrFail() marks a command 'failed' after maxAttempts (default 3). personal_events received no equivalent — there is no attempts column, no dead-letter/'PendingFailed' status, and no exponential backoff. A single malformed pending op also wastes a Google API call and a log line on every tick indefinitely and inflates the pending count surfaced by GetSyncStatus.
- rec: Add retry bookkeeping to personal_events mirroring device_commands: ALTER TABLE personal_events ADD COLUMN attempts INT NOT NULL DEFAULT 0, last_error TEXT, last_attempt_at TIMESTAMPTZ (add to runtime_schema.go EnsureRuntimeSchema and a new migration). On failure, increment attempts and, once attempts >= cap (e.g. 5), move the row to a terminal 'PendingFailed' status (excluded from ListPendingCalendar) and surface it in the re-auth/health alert. Add backoff by skipping rows whose last_attempt_at is too recent. This stops the infinite-loop and makes the failure visible instead of silent.
- effort: medium
- verify: Every concrete claim in the finding checks out against the code.

INFINITE RETRY LOOP (real): cron_workers.go:399-454 (cronPendingCalendar) selects rows via ListPendingCalendar (personal_event.go:125-142, WHERE status IN PendingCreate/Update/Delete) and on any Google API error only does `slog.Warn(...)` + `continue` (lines 419-420, 429-430, 438-439). Status is never changed on failure, so the same

## [MEDIUM/bug] (robustness) Pending-calendar processing logic is triplicated with divergent error handling (telegram path swallows all errors)
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:399-454, backend/internal/handler/sync.go:242-293, backend/internal/engine/telegram_commands.go:285-320
- desc: The same create/update/delete pending pipeline is implemented three times with copy-pasted calendarTarget/storedCalendarEventID helpers (duplicated in cron_workers.go and sync.go). The three diverge: (1) cron_workers logs warnings and counts processed; (2) handler/sync.go aggregates failures and returns pendingError only when processed==0 (so a partial failure where at least one op succeeds hides the others' errors from the HTTP caller); (3) telegram_commands.go (line 302-317) ignores the create/update/delete error entirely (`if err == nil { ... }` with no else) — a 404 there leaves the row pending and reports nothing. All three share the "AI" bug. Divergent behavior means fixing the calendar-id bug in one place leaves the others broken, and the telegram daily sync silently masks failures.
- rec: Extract a single PendingCalendarProcessor (e.g. on PersonalEventStore or a new engine method) that takes the OAuthClient and returns per-op results (processed, failed, []error). Have cron, the HTTP handler, and telegram all call it. Centralize calendarTarget/storedCalendarEventID in one package (they already drifted between engine and handler). In the telegram path, capture and surface the error instead of dropping it.
- effort: medium
- verify: Verified against source. The core claims hold:

1) TRIPLICATION (confirmed): The create/update/delete pending switch is implemented three distinct times — inline in engine/cron_workers.go:415-447, extracted as processPendingCalendarEvent in handler/sync.go:269-293, and inline again in engine/telegram_commands.go:300-318.

2) DUPLICATED HELPERS (confirmed, and have actually drifted): calendarTarget

## [MEDIUM/bug] (robustness) Successful pending-create keeps Kalender="AI" on the row, so subsequent edits/deletes will 404 too
- verdict: partially-correct/high
- loc: backend/internal/engine/cron_workers.go:417-426 + backend/internal/store/personal_event.go:245 (ReplaceEventIDAndStatus)
- desc: Even after the calendar-id is fixed so creates succeed, ReplaceEventIDAndStatus only updates event_id and status; it never rewrites the kalender column. A row created by the assistant keeps kalender="AI" forever. storedCalendarEventID(calendarID, createdID) prefixes the stored id with the *resolved* calendar id ('Main'/real id) only if calendarID != primary; but the kalender column still says "AI". On a later afspraakBewerken/afspraakVerwijderen, calendarTarget reads kalender="AI" again and (without the alias fix) 404s, and (with the alias fix) treats it as primary while the event id may carry a different stored prefix — an inconsistency between the kalender column and the stored event_id prefix.
- rec: When a pending create succeeds, persist the resolved real calendar name in the kalender column (extend ReplaceEventIDAndStatus to also set kalender, or set it at creation time per finding #1). Ensure the kalender column and any event_id prefix are always derived from the same resolved calendar id.
- effort: low
- verify: Core mechanism CONFIRMED by reading the cited code. kalender="AI" is hardcoded at create time (executor.go:2181) and is never normalized to a real calendar anywhere. All three copies of calendarTarget (cron_workers.go:632-644, handler/sync.go:304-315, telegram_commands.go:291) only map ""/"Main" -> "primary"; the literal "AI" is never a resolution target anywhere in the codebase. ReplaceEventIDAnd

## [MEDIUM/bug] (robustness) Incremental Gmail sync silently falls back to full sync on ANY history error, including a normal 404-expired historyId
- verdict: confirmed/high
- loc: backend/internal/google/gmail.go:104-114 (SyncGmail) + gmail.go:116-125 (incrementalGmailSync)
- desc: SyncGmail treats every incremental error identically: it logs "incremental sync failed, falling back to full" and runs fullGmailSync, which only lists maxInitialSync=200 newest messages. Two problems: (1) An expired startHistoryId (Gmail returns HTTP 404 when the historyId is too old) is expected and correctly handled by full re-sync, but a transient 5xx/429/network blip on the history endpoint ALSO triggers a full re-list of 200 messages every 5 minutes, which is wasteful and can mask a persistent partial outage. (2) The fallback never clears emails outside the newest 200 and never reconciles deletions — full sync here is really "refresh newest 200", so older rows can silently drift from Gmail state. The fallback also does not distinguish invalid_grant (ErrGoogleReauthRequired propagates from getAccessToken inside fullGmailSync, which is fine, but a 404 vs 503 are conflated).
- rec: Inspect the incremental error: only fall back to full sync on a 404/expired-historyId (parse the HTTP status from the error or have GetJSON return a typed status). For transient 5xx/429, return the error so the cron retries next tick without a full re-list (and so MarkSyncFailed records the real cause). Consider bounding fallback frequency. Document that full sync only covers the newest maxInitialSync messages so the limitation is explicit.
- effort: medium
- verify: All core claims verified against the cited code.

(1) Indiscriminate fallback — CONFIRMED. backend/internal/google/gmail.go:105-113: SyncGmail runs incrementalGmailSync and on ANY non-nil error logs "incremental sync failed, falling back to full" then calls fullGmailSync. The error originates in OAuthClient.GetJSON (backend/internal/google/oauth.go:130-131), which returns a generic fmt.Errorf("GET

## [MEDIUM/bug] (robustness) Prune deletes ALL local rows in window when Google returns zero events (legit-empty vs transient-empty indistinguishable)
- verdict: confirmed/high
- loc: backend/internal/store/schedule.go:131-148 (PruneMissingInDateRange, keepEventIDs empty branch)
- desc: PruneMissingInDateRange, when keepEventIDs is empty, unconditionally DELETEs every schedule row in [start,eind]. fetchCalendarEvents returns (events, nil) with an empty slice both when the calendar genuinely has no events AND in edge cases (e.g. a misconfigured SDBCalendarID that still returns 200 with empty items, or singleEvents/orderBy returning nothing). Because the cron treats len==0 as success and FetchedEventIDs becomes empty, the next prune wipes all locally-stored shifts in the 4-month window. For personal events the equivalent MarkMissingSyncedInDateRange is guarded by len(syncedKalenders)>0 and kalender scoping, so it is less catastrophic, but schedule has no such guard. A transient state that yields an empty-but-200 response causes mass deletion of wanted rows.
- rec: Guard the prune: if the fetch returned zero events AND zero FetchedEventIDs, skip pruning (or require an explicit "calendar confirmed empty" signal) rather than deleting everything. At minimum, only prune when at least one event was fetched, or compare counts and refuse to prune if it would delete more than, say, all rows. Add a test for the empty-fetch case.
- effort: low
- verify: CONFIRMED, severity medium is correct. All core claims verified against the code.

1. Unconditional mass delete: backend/internal/store/schedule.go:136-148 — when len(keepEventIDs)==0, PruneMissingInDateRange runs `DELETE FROM schedule WHERE user_id=$1 AND start_datum>=$2 AND start_datum<=$3` with no further guard (no count check, no "calendar confirmed empty" signal).

2. Empty-but-success fetch:

## [MEDIUM/optimization] (optimize) Gmail metadata fetched with N per-message GETs instead of the batch endpoint
- verdict: confirmed/high
- loc: backend/internal/google/gmail.go:182-259 (fetchMessageBatch)
- desc: fetchMessageBatch issues one HTTP GET per message id (GET /messages/{id}?format=metadata) across 8 worker goroutines. Despite the name, it does not use Google's HTTP batch endpoint. On a full sync this is up to maxInitialSync=200 separate API round trips; on every incremental tick it is one GET per changed message. Each call carries auth + TLS overhead and counts against Gmail per-user quota (5 units/messages.get). At a 5-minute Gmail cadence this is the single largest API-call multiplier in the layer. The 8-worker fan-out also bursts concurrent requests against gmail.googleapis.com, which is exactly the pattern that triggers 429 rateLimitExceeded / userRateLimitExceeded.
- rec: Replace the per-id GET fan-out with Gmail's batch endpoint (POST https://gmail.googleapis.com/batch/gmail/v1 with a multipart/mixed body of up to ~50-100 sub-requests, or the equivalent google.golang.org/api/gmail/v1 BatchGet pattern). One batch HTTP request returns up to 100 message metadata responses, cutting 200 calls to ~2-4. If staying with the hand-rolled client, build the multipart body and parse the multipart response; reuse the same access token for the whole batch. Keep the worker pool only as a fallback. Add a small bounded retry with backoff on 429/5xx per sub-request.
- effort: medium
- verify: Every concrete technical claim checks out against the source.

CONFIRMED:
- fetchMessageBatch (backend/internal/google/gmail.go:182-259) issues one HTTP GET per message id (line 206: GET .../messages/{id}?format=metadata&metadataHeaders=...) inside a worker loop. Despite its name it does NOT use Google's batch endpoint — grep confirms no batch/gmail/v1 usage anywhere in the package.
- Fan-out is 8

## [MEDIUM/optimization] (optimize) Calendar re-fetches the full 120-day window every run; no syncToken incremental sync
- verdict: confirmed/high
- loc: backend/internal/google/calendar.go:268-299 (fetchCalendarEvents), 150-205 (SyncScheduleDetailed), 218-264 (SyncPersonalEventsDetailed)
- desc: Every schedule and personal-events run fetches the entire window now-30d..now+90d with singleEvents=true&orderBy=startTime&maxResults=250 and paginates the whole thing, then re-parses and re-upserts everything. nextSyncToken is never requested or stored (confirmed: no syncToken/updatedMin reference in calendar.go). The personal sync runs hourly across every configured calendar, so each hour pulls and rewrites the full window per calendar even when nothing changed. This wastes API calls (1+ page per calendar per run), DB write amplification (full re-upsert), and CPU on regex parsing of every event description.
- rec: Adopt incremental sync via Calendar's syncToken: store nextSyncToken per (user, calendarId) in a sync-meta table; on each run pass syncToken to fetch only changed/deleted events (Google returns 410 GONE when the token expires, on which you fall back to a full window fetch and capture a fresh token). Note syncToken is incompatible with singleEvents+orderBy+timeMin/timeMax, so use it for change detection and expand recurrences/window-filter locally, or keep a periodic (e.g. daily) full reconcile plus syncToken for the hourly deltas. As a cheaper interim step, pass updatedMin to skip unchanged events, and only upsert rows whose content hash changed to cut DB writes.
- effort: high
- verify: All load-bearing technical claims are accurate against the code.

WINDOW & PAGINATION (calendar.go:151-153, 219-221, 272-296): Both SyncScheduleDetailed and SyncPersonalEventsDetailed compute timeMin=now-30d / timeMax=now+90d (constants syncDaysBack=30, syncDaysForward=90 at :113-114 = 120-day window) and pass them to fetchCalendarEvents, which sets singleEvents=true, orderBy=startTime, maxResults

## [MEDIUM/optimization] (optimize) Six independent OAuthClient instances, each with its own token cache; HTTP handlers rebuild the client per request
- verdict: confirmed/high
- loc: backend/internal/google/oauth.go:34-100; instantiation at cron_workers.go:82, handler/sync.go:45, handler/sync.go:386, handler/pending.go:105, handler/personal_event.go:200, engine/telegram_ai.go:76
- desc: OAuthClient caches the access token in-instance (oauth.go:50-99) with a 60s skew. Because six separate clients are constructed, the token cache is fragmented: each refreshes its own access token. Worse, the three handler call sites (sync.go x2, pending.go, personal_event.go) call NewOAuthClient on every HTTP request, so the per-instance cache never survives a request — every manual /sync, every pending approval, every personal-event write does a fresh token refresh round trip to oauth2.googleapis.com before doing any real work. telegram_ai.go also builds one per call. This is needless extra latency and refresh-endpoint traffic, and multiplies the blast radius of an invalid_grant.
- rec: Construct one shared OAuthClient (or a small token-source singleton) at startup and inject it everywhere — the cron layer already holds a long-lived oauthClient (cron_workers.go:82); hang the same instance off SyncHandler/PendingHandler/PersonalEventHandler/Engine instead of calling NewOAuthClient per request. The existing mutex makes it concurrency-safe. This collapses ~6 token caches into 1, eliminates the per-request refresh, and means a single re-auth alert path. Alternatively switch to golang.org/x/oauth2 with a shared TokenSource (oauth2.ReuseTokenSource) which gives caching + refresh for free.
- effort: low
- verify: Every factual claim checks out against the code.

Token cache is per-instance: OAuthClient holds accessToken/expiresAt + mutex as struct fields (oauth.go:28-31). getAccessToken (oauth.go:50-99) returns the cached token only if accessToken != "" and now < expiresAt-60s (line 54). A freshly constructed client has an empty accessToken, so its first Do/GetJSON/SendJSON always POSTs to https://oauth2.g

## [MEDIUM/bug] (optimize) Cron scheduler never runs jobs on startup; daily schedule sync waits a full 24h after boot
- verdict: confirmed/high
- loc: backend/internal/engine/cron.go:62-81 (runJob); cron_workers.go:110-115 (sync-schedule-daily)
- desc: runJob creates time.NewTicker(job.Interval) and only executes RunFunc on the first tick. There is no immediate kick-off. So after a deploy/restart, the daily schedule sync (Interval 24h) does not run for 24 hours, the hourly personal sync waits an hour, and even sync-gmail waits 5 minutes. This is the structural reason a manual /sync endpoint had to exist — the data is stale for up to a day after every restart. It also means the 'daily' cadence is poorly matched to a work-schedule that users edit during the week: a shift added in Google Calendar can take ~24h to appear locally.
- rec: Run each job once immediately on start (or after a small jittered delay to avoid a thundering herd), then continue on the ticker. Minimal change in runJob: execute RunFunc once before entering the for/select, or use a ticker plus an initial timer. Separately, raise the schedule cadence from 24h to something like 1-3h (or unify it with the hourly personal sync) so /sync becomes a convenience rather than a necessity. Add small per-job startup jitter so all syncs don't fire simultaneously at boot.
- effort: low
- verify: Verified against source. cron.go:62-81 runJob creates time.NewTicker(job.Interval) and executes RunFunc only on `case <-ticker.C`. Go tickers do not fire at t=0, so the first run is one full interval after start; there is no immediate kick-off. main.go -> eng.Run -> e.cron.Run -> runJob is the only startup path, and no initial sync is triggered elsewhere. Thus after deploy/restart: sync-schedule-d

## [MEDIUM/optimization] (optimize) HTTP /sync handlers duplicate the cron sync logic instead of sharing it
- verdict: confirmed/high
- loc: backend/internal/handler/sync.go:38-240 (SyncCalendar) and 374-458 (SyncGmail) vs backend/internal/engine/cron_workers.go:160-397 (cronGmailSync, cronScheduleSync, cronPersonalEventsSync, cronPendingCalendar)
- desc: SyncCalendar/SyncGmail in the handler reimplement, almost line for line, the same fetch -> map ParsedEmail/ScheduleDienst/PersonalEventSync -> BulkUpsert -> prune -> UpsertSyncMeta pipeline that the crons run. The ParsedEmail->model.Email mapping exists twice (storeParsedEmails in sync.go:553 and the inline loop in cronGmailSync:191), and calendarTarget/splitCalendarIDs/resolvedPersonalEventStatus/storedCalendarEventID are duplicated verbatim in both handler/sync.go and engine/cron_workers.go. Beyond maintenance risk (the two paths can drift, e.g. lastFullSync handling already differs subtly), it means double the surface that must be optimized — any batch/syncToken improvement has to be applied in two places.
- rec: Extract the sync pipelines into shared service functions (e.g. a SyncService with RunGmailSync / RunCalendarSync taking ctx, db, shared OAuthClient, userID, cfg) and have both the crons and the HTTP handlers call them. The handler then only does request parsing + JSON response; the cron only does scheduling. This guarantees the batch/syncToken/shared-client optimizations above are written once and applied everywhere, and removes the duplicated helper functions.
- effort: medium
- verify: The core finding is real and verified against the cited code.

Verified duplication (backend/internal/handler/sync.go vs backend/internal/engine/cron_workers.go):
- calendarTarget, storedCalendarEventID, splitCalendarIDs are duplicated essentially verbatim (handler sync.go:304-337, cron cron_workers.go:632-666). The only trivial divergence is that the cron's calendarTarget introduces a local `pref

## [MEDIUM/bug] (optimize) Large-mailbox handling: full sync caps at 200 with no pagination, and incremental history has no pageToken loop
- verdict: confirmed/high
- loc: backend/internal/google/gmail.go:157-180 (fullGmailSync), 116-155 (incrementalGmailSync)
- desc: fullGmailSync requests messages?maxResults=200 once and never follows nextPageToken (confirmed: no pageToken handling in gmail.go), so a full sync (which also runs whenever incremental fails or when stored count is 0) silently captures at most the 200 most recent message ids and treats that as the whole mailbox. More importantly, incrementalGmailSync reads a single page of history and ignores any history nextPageToken — if many changes accumulated between ticks (or after downtime), changes past the first page are dropped, and because the new historyId is still advanced, those messages are never reconciled. The contrast with the Calendar fetch, which does paginate, shows the inconsistency.
- rec: Add nextPageToken pagination to both the history list (incrementalGmailSync) and the messages list (fullGmailSync), accumulating ids across pages before the batch metadata fetch. For the full path, decide an explicit bound (e.g. windowed by internalDate / a configurable cap) rather than an implicit 200-message truncation, and document it. Combined with the batch endpoint (finding 1), pagination of large result sets stays cheap because metadata is fetched in batches of ~100 rather than one-by-one. After downtime, paginating history avoids a silent gap in synced mail.
- effort: medium
- verify: Verified against source. All core claims hold.

1. No pagination anywhere in gmail.go: confirmed zero pageToken/nextPageToken references. Both gmailListResponse (gmail.go:16-20) and gmailHistoryResponse (gmail.go:22-25) lack a NextPageToken field entirely, so any token the API returns is silently dropped during unmarshalling.

2. fullGmailSync (gmail.go:157-180): issues messages?maxResults=200 (ma

## [MEDIUM/missing] (gaps) Gmail uses 5-minute polling instead of Pub/Sub push (users.watch)
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:101-107 (sync-gmail, 5*time.Minute); backend/internal/google/gmail.go (no watch/stop calls anywhere)
- desc: Gmail sync is a fixed 5-minute cron. There is no users.watch / Pub/Sub registration (verified: no `watch`, `pubsub`, or topic references in the google package). This means up to 5-minute latency on new mail, and every tick spends an API call on history even when nothing changed. Gmail's recommended pattern is users.watch -> Cloud Pub/Sub push -> a webhook that triggers an incremental history sync, which is both near-real-time and far cheaper on quota.
- rec: Register users.watch (renewed before its ~7-day expiry via a daily cron) pointing at a Pub/Sub topic, add a push webhook handler that calls incrementalGmailSync with the stored historyId, and keep the 5-min poll only as a low-frequency safety net (e.g. hourly). Persist the watch expiry to drive renewal.
- effort: high
- verify: Verified against the cited code and surrounding context. All factual claims hold:

1. Fixed 5-minute polling: cron_workers.go:101-107 registers "sync-gmail" with Interval: 5 * time.Minute, gated on cfg.GmailEnabled && oauthClient != nil. The scheduler (cron.go:62-81) drives it via a plain time.NewTicker(job.Interval) loop, so it fires unconditionally every 5 minutes — confirming fixed-interval pol

## [MEDIUM/bug] (gaps) Incremental Gmail sync falls back to a 200-message full sync on expired history — silent data loss
- verdict: confirmed/high
- loc: backend/internal/google/gmail.go:104-114 (SyncGmail fallback), 157-180 (fullGmailSync, maxInitialSync=200)
- desc: When incrementalGmailSync fails (the common case is Google returning 404 because startHistoryId is too old / expired — Gmail only retains history for ~a week or a bounded amount), SyncGmail silently falls back to fullGmailSync, which lists only maxInitialSync=200 most-recent messages. For a mailbox that changed more than 200 messages since the last successful sync, or simply with >200 messages of relevant state, older changes (reads, label changes, deletions beyond the newest 200) are never reconciled. The fallback is logged at Warn but the result is reported as a normal success, so health/observability shows green while state silently diverges. The full sync is also not paginated.
- rec: Distinguish a transient incremental error from an expired-history (404) signal and handle expired-history as a deliberate, paginated full resync of the relevant scope (or at minimum paginate fullGmailSync past 200). Surface 'fell back to full / history expired' in email_sync_meta so observability reflects it.
- effort: medium
- verify: Verified against the cited code and both call sites. All structural claims hold:

1. SyncGmail (backend/internal/google/gmail.go:104-114) falls back to fullGmailSync on ANY error from incrementalGmailSync, logged only at Warn. It does NOT distinguish error kinds — GetJSON (backend/internal/google/oauth.go:122-131) returns a generic error for every non-200 status, so an expired-history 404 and a tr

## [MEDIUM/missing] (gaps) Calendar sync re-fetches a fixed time window every run — no syncToken incremental sync, no webhook channels
- verdict: confirmed/high
- loc: backend/internal/google/calendar.go:268-299 (fetchCalendarEvents uses timeMin/timeMax+singleEvents, no syncToken), cron_workers.go:110-128 (daily schedule, hourly personal)
- desc: Both schedule and personal-event syncs always pull the full -30d..+90d window with timeMin/timeMax and paginate the whole thing every run, then diff locally to prune. There is no use of Google Calendar's nextSyncToken for incremental sync (verified: no syncToken/nextSyncToken references anywhere), and no events.watch push channels. This re-downloads the entire window hourly/daily regardless of whether anything changed, and cannot detect deletions outside the fixed window. It also means near-real-time external calendar edits aren't reflected until the next scheduled pull.
- rec: Store and reuse nextSyncToken per (calendar, user) for incremental syncs; on a 410 (token invalid) do a full window re-sync and capture a fresh token. For latency, register events.watch channels (with stored channelId/resourceId/expiry and a renewal cron) to trigger incremental syncs on change.
- effort: high
- verify: Verified against source (note: cron_workers.go is at backend/internal/engine/cron_workers.go, not backend/internal/google/ as cited — but the cited line numbers and content match).

fetchCalendarEvents (calendar.go:268-299): every run sends timeMin/timeMax + singleEvents=true + orderBy=startTime + maxResults=250 and paginates via pageToken (an intra-run page cursor, NOT a sync token). calendarList

## [MEDIUM/missing] (gaps) OAuth uses a single env-only refresh token — not DB-backed, not rotatable, no multi-account
- verdict: confirmed/high
- loc: backend/internal/config/config.go:145-147 (GOOGLE_REFRESH_TOKEN from env), backend/internal/google/oauth.go:23-40 (refreshToken field, never persisted), engine/cron_workers.go:80-83 and handler/sync.go:45 (one client from cfg)
- desc: The refresh token comes only from the GOOGLE_REFRESH_TOKEN env var and is held in-memory on the OAuthClient. When it expires/revokes (invalid_grant), the system can only alert the user to manually re-run scripts/gen-gmail-token.mjs and redeploy (see alertGoogleReauthOnce in engine.go:113-123) — there is no OAuth callback endpoint, no token persisted to or rotated in the DB, and no handling of a rotated refresh token (Google can return a new refresh_token, which is never captured because tokenResponse only reads access_token). The whole layer is hard-wired to one account: a single cfg.UserID / cfg.GoogleRefreshToken with no per-account credential storage, so it cannot sync more than one Google account.
- rec: Persist OAuth credentials (refresh token, scopes, account email) in a DB table keyed by user/account; add a standard OAuth authorization-code callback handler so re-auth is self-service (no redeploy); capture and store any rotated refresh_token from the token endpoint. Generalize the cron/handler to iterate accounts rather than a single cfg token to enable multi-account.
- effort: high
- verify: All cited facts verified against the code. (1) backend/internal/config/config.go:147 reads GOOGLE_REFRESH_TOKEN from env into cfg.GoogleRefreshToken. (2) backend/internal/google/oauth.go:23-40 holds refreshToken as a plain in-memory struct field set once in NewOAuthClient, never persisted. (3) tokenResponse (oauth.go:43-47) decodes only access_token/expires_in/token_type — a Google-rotated refresh

## [MEDIUM/missing] (gaps) No per-sync run history / observability for rows added, updated, or pruned over time
- verdict: confirmed/high
- loc: backend/internal/store/email.go:192-248 (email_sync_meta holds only last snapshot), engine/cron_workers.go:259-263/322/394 (counts only logged, never persisted), schedule UpsertMeta stores only total rows
- desc: email_sync_meta records only the latest snapshot (history_id, total_synced, updated_at, sync_status, last_error) — there is no time series of how many emails were added/updated per run, and no concept of pruned for email. Calendar schedule/personal syncs log parsed/upserted/pruned counts to slog but persist nothing comparable (ScheduleMeta stores only a single total_rows; personal events store nothing). There is no per-run audit row, so you cannot answer 'how many rows did the last 24h of syncs add/update/prune?', detect a sync that suddenly pruned everything, or chart sync volume/health trends.
- rec: Add a sync_runs table (source, started_at, duration_ms, mode, fetched, upserted, pruned, ok, error) written at the end of each cron sync (Gmail, schedule, personal, pending). Expose recent runs via GetSyncStatus so anomalies (e.g. a mass-prune or repeated incremental failures) are visible.
- effort: medium
- verify: All cited code verified at the corrected path backend/internal/engine/cron_workers.go (the finding's "engine/cron_workers.go" path is accurate relative to backend root). Claims hold:

1. email_sync_meta (migration 003_emails.up.sql:60, UNIQUE(user_id)) stores exactly one snapshot row per user. store/email.go:212-248 UpsertSyncMeta and MarkSyncFailed both use ON CONFLICT (user_id) DO UPDATE, overwr

## [MEDIUM/bug] (gaps) Pending-op execution is not idempotent — a create that succeeds remotely but fails the local status update duplicates the event
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:416-426 (PendingCreate then ReplaceEventIDAndStatus), backend/internal/handler/sync.go:274-279 (same pattern); CreatePersonalEvent backend/internal/google/calendar.go:301-317
- desc: For PendingCreate the flow is: POST to Google -> on success call ReplaceEventIDAndStatus to swap the local ai-<uuid> for the real Google id and clear pending status. If the Google create succeeds but the subsequent DB ReplaceEventIDAndStatus fails (transient DB error, context timeout, unique-violation edge), the row stays in PendingCreate and the next tick POSTs to Google AGAIN, creating a duplicate calendar event. CreatePersonalEvent sends no idempotency guarantee (Google Calendar supports a client-supplied event id / the events.insert with a stable id, or an iCalUID, to make creates idempotent), and the local DB upserts are keyed only on the final event_id so they don't dedupe pre-id creates. The same non-atomic 'do remote then update local' gap exists for update/delete, though those are naturally more idempotent.
- rec: Make creates idempotent by supplying a deterministic event id (or iCalUID) derived from the local pending row to Google so a retried insert is a no-op rather than a duplicate, and/or persist a 'remote-created, local-update-pending' intermediate state so a retry reconciles instead of re-creating.
- effort: medium
- verify: Verified against the cited code. The non-atomic "do remote then update local" gap is real at both locations: backend/internal/engine/cron_workers.go:417-426 and backend/internal/handler/sync.go:275-279 call google.CreatePersonalEvent (Google POST) followed by a separate evStore.ReplaceEventIDAndStatus (local DB swap of ai-<uuid> -> real Google id + clear pending). There is no surrounding transacti

## [LOW/bug] (robustness) Schedule/personal prune window starts at today but fetch window starts 30 days back — recently-deleted past events never pruned
- verdict: confirmed/high
- loc: backend/internal/google/calendar.go:151-204 (PruneStartDatum=now) + cron_workers.go:310-319 / store/schedule.go:131 / store/personal_event.go:207
- desc: fetchCalendarEvents pulls events from now-syncDaysBack(30d) to now+syncDaysForward(90d), and FetchedEventIDs covers that whole span. But PruneStartDatum is set to now.Format (today), so PruneMissingInDateRange / MarkMissingSyncedInDateRange only delete/mark rows with start_datum >= today. A shift in the last 30 days that the user deletes from Google Calendar stays in the local DB forever because it falls in the fetch+upsert window but outside the prune window. This is staleness, not data loss, but it means deletions of recent-past events are never reconciled.
- rec: Set PruneStartDatum to timeMin (now-syncDaysBack) so the prune window matches the fetch window, OR intentionally document that past events are never pruned. Given FetchedEventIDs already covers the full window, aligning PruneStartDatum to timeMin is safe (only events in the fetched window are candidates, and keepEventIDs protects everything still present).
- effort: low
- verify: Verified against source. The diagnosis is correct.

FETCH WINDOW: In both SyncScheduleDetailed and SyncPersonalEventsDetailed, timeMin = now.AddDate(0,0,-syncDaysBack) and timeMax = now.AddDate(0,0,syncDaysForward), with syncDaysBack=30, syncDaysForward=90 (C:/Users/jeffrey/Desktop/Projecten/JeffriesBackend/backend/internal/google/calendar.go:113-114,152-153,220-221). fetchCalendarEvents passes ti

## [LOW/bug] (robustness) Calendar event time parse errors are silently ignored, producing zero-time rows
- verdict: confirmed/high
- loc: backend/internal/google/calendar.go:572-581 (parseScheduleEvent) and 644-653 (parsePersonalEvent)
- desc: Both parsers ignore the error from time.Parse/ParseInLocation (startDt, _ := ...). If Google returns a DateTime/Date in an unexpected format (or an empty string when only one of Date/DateTime is set inconsistently), startDt/eindDt become the zero time (0001-01-01). The event is still emitted with StartDatum="0001-01-01", Duur computed from zero times, and nlDays[startDt.Weekday()] indexing into a bogus weekday. These rows are then upserted and, worse, because their start_datum is far in the past they fall outside the prune window (finding #6) and persist as garbage. Per-event parse failure should skip the event, not emit a zero-valued row.
- rec: Check the parse errors; on failure, log and skip the event (return nil from the parser, as is already done for nil Start/End). Validate that exactly one of Date/DateTime is populated. This also hardens against the DST-ambiguous all-day vs timed mismatch.
- effort: low
- verify: The finding is mechanically accurate and the impact chain is real. Verified at C:/Users/jeffrey/Desktop/Projecten/JeffriesBackend/backend/internal/google/calendar.go:

1. Both parsers discard parse errors: parseScheduleEvent (lines 573-574 ParseInLocation, 577-578 time.Parse) and parsePersonalEvent (645-646, 649-650) all use `startDt, _ := ...`. The only validation is the nil guard `ev.Start == ni

## [LOW/bug] (robustness) Personal-events partial write failure disables prune but partial upserts already mutated DB; HTTP partial failures under-reported
- verdict: partially-correct/high
- loc: backend/internal/handler/sync.go:128-212 + backend/internal/engine/cron_workers.go:344-392
- desc: In both the handler and the cron, personal events are upserted one-by-one in a loop; on any single upsert error the code sets personalWriteFailed/logs and continues. The prune (MarkMissingSyncedInDateRange) is then skipped if any write failed — good for safety — but the already-succeeded upserts are committed (no transaction), so the DB is left in a partially-updated state with no prune, and the next run repeats. In handler/sync.go the response reports a generic personalWriteError string but not which events failed. In cron the per-event failure is only a warning; the overall cron still returns nil (success), so MarkSyncFailed-style health is never recorded for personal events (unlike Gmail which calls MarkSyncFailed). A persistent bad row (e.g. titel exceeding VARCHAR(500), or kalender exceeding VARCHAR(50)) fails silently every hour.
- rec: Wrap the per-event upserts in a transaction (like ScheduleStore.BulkUpsert) so a batch is all-or-nothing, or at least record a personal-events sync_status/last_error analogous to email_sync_meta so persistent failures surface in GetSyncStatus. Include failing event ids in the handler response. Validate field lengths before upsert (kalender VARCHAR(50), titel VARCHAR(500)).
- effort: medium
- verify: All core claims verified against the cited code. (1) Per-event upserts with no transaction CONFIRMED: PersonalEventStore.UpsertSynced (personal_event.go:167-194) is a single Pool.Exec per call, looped in both handler (sync.go:129-197) and cron (cron_workers.go:346-380); contrast ScheduleStore.BulkUpsert/EmailStore.BulkUpsert which are batched. Already-succeeded upserts commit independently. (2) Pa

## [LOW/optimization] (robustness) OAuthClient retries nothing on transient Google 5xx/429 and shares a single mutable token across concurrent Gmail workers
- verdict: confirmed/high
- loc: backend/internal/google/oauth.go:102-162 (Do/GetJSON/SendJSON) + gmail.go:182-259 (8 concurrent workers)
- desc: Every Google call goes through Do() with no retry/backoff and no handling of HTTP 429 (rate limit) or 5xx — they surface as immediate errors. fetchMessageBatch fans out to 8 concurrent workers all calling GetJSON; on a token-expiry boundary getAccessToken is mutex-guarded (correct), but a 429 from any worker aborts that message and counts as failed, and during a full sync of 200 messages this can trip Gmail per-user rate limits with no backoff. There is also no overall request timeout on http.DefaultClient (only the ctx deadline from handlers; the cron passes a context that may not have a per-request timeout), so a hung connection can stall a worker until ctx cancellation.
- rec: Add bounded retry with exponential backoff + jitter for 429/5xx in Do() (respecting Retry-After), and a per-request timeout on the http client. Consider lowering messageWorkers or adding a rate limiter for full sync. These are robustness hardening, not correctness bugs, but they reduce spurious partial failures that currently just get logged as gmail partial failure.
- effort: medium
- verify: Verified against the cited code. (1) oauth.go:103-119 Do() makes a single http.DefaultClient.Do with no retry/backoff; GetJSON (122-135) and SendJSON (138-162) treat any non-2xx including 429/5xx as an immediate hard error with no Retry-After handling. CONFIRMED. (2) gmail.go:100 messageWorkers=8; fetchMessageBatch (182-259) fans out 8 goroutines each calling GetJSON; a 429/5xx on a single message

## [LOW/optimization] (optimize) Personal-events DB writes are per-row upserts in a loop instead of a batched transaction
- verdict: confirmed/high
- loc: backend/internal/engine/cron_workers.go:344-380 (cronPersonalEventsSync) and backend/internal/handler/sync.go:127-197 (SyncCalendar personal loop)
- desc: Unlike emails (EmailStore.BulkUpsert wraps all rows in one tx, email.go:92) and schedule (schedStore.BulkUpsert), personal events are written with evStore.UpsertSynced called once per event in a Go loop, each its own round trip / implicit transaction. Because the hourly personal sync re-fetches and re-writes the full 120-day window (finding 2), this is N separate DB statements every hour per calendar even when nothing changed. It amplifies DB load and connection-pool pressure proportional to event count.
- rec: Add a PersonalEventStore.BulkUpsertSynced that batches all events into a single transaction (mirroring EmailStore.BulkUpsert), or use pgx.Batch / COPY for the upsert set. Combined with the syncToken/content-hash change detection from finding 2, this drops steady-state writes to near zero. Have both the cron and the handler call the same batched method (ties into the dedup in finding 5).
- effort: low
- verify: All factual claims verified against the code.

(1) Per-row upserts confirmed: cronPersonalEventsSync (backend/internal/engine/cron_workers.go:346-380) and the SyncCalendar handler personal loop (backend/internal/handler/sync.go:129-197) both call UpsertSynced once per event inside a Go for-loop.

(2) UpsertSynced is a direct pool Exec with no transaction or batching: backend/internal/store/persona


# REFUTED
