# Jeffries Homeapp API Overview

This backend exposes a Go/chi REST API for the Homeapp frontend. The API runs on Render and uses Render Postgres. Browser traffic should normally reach it through the frontend's Next.js proxy.

## Live Runtime

| Item | Value |
|---|---|
| Render backend | `https://jeffriesbackend.onrender.com` |
| API base path | `/api/v1` |
| Frontend proxy base | `/api/backend` |
| Swagger UI | `/api/v1/swagger/index.html` |
| Swagger JSON | `/api/v1/swagger/doc.json` |

## Source Of Truth

| Purpose | File |
|---|---|
| Mounted routes | `backend/internal/server/routes.go` |
| Handlers and Swagger annotations | `backend/internal/handler/*.go` |
| Generated Swagger JSON | `backend/docs/swagger.json` |
| Generated Swagger YAML | `backend/docs/swagger.yaml` |
| Runtime schema patches | `backend/internal/store/runtime_schema.go` |
| Device command queue migration | `backend/migrations/009_device_command_claiming.up.sql` |
| Note/event symbols migration | `backend/migrations/010_symbols.up.sql` |
| Note revision history migration | `backend/migrations/011_note_revisions.up.sql` |

When route behavior and Swagger disagree, fix the handler annotation or route mount and regenerate Swagger. Do not treat generated docs as the only source of truth.

## Auth

- `GET /` and `HEAD /` are public health/root checks.
- `GET /api/v1/health` is public.
- `GET /api/v1/swagger/*` is public documentation.
- All other `/api/v1` endpoints are private and require `X-API-Key` when called directly.
- The frontend proxy injects the key server-side, so browser code must not send secrets.

## Main Route Groups

| Group | Routes |
|---|---|
| System | `GET /health` |
| Rooms | `GET/POST /rooms`, `GET/PATCH/DELETE /rooms/{roomID}` |
| Devices | `GET /devices`, `GET/PATCH/DELETE /devices/{deviceID}`, `POST /devices/register`, `POST /devices/{deviceID}/command` |
| Bridge | `POST /bridge/commands/claim`, `POST /bridge/commands/{commandID}/complete`, `POST /bridge/devices/{deviceID}/status` |
| Scenes | `GET/POST /scenes`, `GET/DELETE /scenes/{sceneID}`, `POST /scenes/{sceneID}/activate` |
| Automations | `GET/POST /automations`, `PUT/DELETE /automations/{id}`, `POST /automations/{id}/toggle`, `DELETE /automations/group` |
| Schedule | `GET /schedule`, `GET /schedule/meta`, `GET /schedule/date/{date}`, `POST /schedule/import` |
| Transactions | `GET /transactions`, `GET /transactions/stats`, `POST /transactions/import`, `PATCH /transactions/{txID}` |
| Salary | `GET/POST /salary`, `GET /salary/periode` |
| Loonstroken | `GET /loonstroken`, `POST /loonstroken/import` |
| Personal events | `GET/POST /personal-events`, `GET /personal-events/upcoming`, `GET /personal-events/date/{date}`, `PATCH /personal-events/{eventID}/status` |
| Emails | `GET /emails`, `GET /emails/search`, `GET /emails/stats`, `PATCH /emails/read`, `PATCH /emails/delete` |
| Privacy | `GET /privacy`, `PUT /privacy` |
| Notes | `GET/POST /notes`, `GET /notes/search`, `GET /notes/tags`, `GET/PATCH/DELETE /notes/{id}`, `GET /notes/{id}/backlinks`, `GET /notes/{id}/revisions`, `POST /notes/{id}/revisions/{revisionID}/restore` |
| Habits | `GET/POST /habits`, `GET /habits/for-date`, `GET /habits/stats`, `GET /habits/heatmap`, `GET /habits/badges`, `GET/PATCH/DELETE /habits/{id}`, habit action posts |
| LaventeCare | cockpit, documents, leads, projects, actions, signal conversion, document seeding |
| Settings | `GET /settings/overview`, `GET /settings/backup`, `GET /settings/telegram/status` |
| Sync | `GET /sync/status`, `POST /sync/calendar`, `POST /sync/gmail` |

## Query Param Conventions

- Most migrated user-owned routes use `userId`.
- Email routes use `user_id`.
- List endpoints for rooms, devices, scenes use `skip` and `limit`.
- Notes and email search use `q`.
- Date routes use `YYYY-MM-DD`.
- Finance filters live on `/transactions` and `/transactions/stats`.

## Shared Symbol Fields

- `model.Note.symbol` and `model.PersonalEvent.symbol` store an app icon key from the frontend symbol registry.
- Personal events also mirror the icon in Google Calendar descriptions as `[symbol:<key>]`, so calendar sync can preserve the choice.
- Existing event category metadata still uses `[categorie:<id>]`; category and symbol are intentionally separate.

## Note Revision History

- Meaningful note edits snapshot the previous title, content, tags, color, deadline, linked event, priority, and symbol into `note_revisions`.
- Pin/archive-only updates do not create a revision.
- `GET /notes/{id}/revisions?userId=...&limit=20` returns recent versions for the editor.
- `POST /notes/{id}/revisions/{revisionID}/restore?userId=...` restores a version and first snapshots the current state so the restore can be undone.

## Note Completion

- Completion is separate from archive: completed notes remain queryable and usable in journals.
- `PATCH /notes/{id}` with `isCompleted: true|false` updates `is_completed`; the server manages `completed_at`.
- Completion-only updates do not create a note revision.

## Render And WiZ Command Flow

Render cannot send UDP to local WiZ lights directly. In cloud mode:

```text
frontend or Telegram -> Render API -> device_commands table -> local HTTP bridge -> WiZ UDP
```

Important queue fields:

- `status`: `pending`, `processing`, `done`, `failed`
- `claimed_at`: set when the local bridge atomically claims work
- `updated_at`: updated by claim, completion, and failure transitions

The bridge claims work through `POST /bridge/commands/claim`. The backend still uses `FOR UPDATE SKIP LOCKED` and requeues stale `processing` commands older than two minutes.

## Integration Status

- `GET /settings/overview` reports integration flags from runtime config instead of assuming every integration is active.
- `GET /settings/telegram/status` checks Telegram `getMe` and `getWebhookInfo`; Render uses long polling when no webhook URL is configured.
- `GET /sync/status` reports real schedule meta, personal event counts/pending operations, and Gmail sync meta.
- `POST /sync/gmail?userId=...` performs a real Gmail API sync and stores messages in PostgreSQL.

## Regenerate Swagger

Run this after changing handlers or route annotations:

```bash
cd C:\Users\jeffrey\Desktop\Projecten\JeffriesBackend\backend
go run github.com/swaggo/swag/cmd/swag@v1.16.6 init -g cmd/api/main.go
```

Then regenerate frontend clients:

```bash
cd C:\Users\jeffrey\Desktop\Projecten\JeffriesHomeapp
npx orval
```

