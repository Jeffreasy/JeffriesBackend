package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/todoist"
)

// RegisterHomeappCrons adds all migrated Convex cron jobs to the scheduler.
func RegisterHomeappCrons(s *CronScheduler, e *Engine, cfg CronConfig) {

	s.Register(CronJob{
		Name:     "schedule-weekly-check",
		Interval: 1 * time.Hour, // Evaluates hourly, but logic only executes on Sunday 19:00
		RunFunc:  cronScheduleWeeklyCheck(e.db, cfg),
	})
	// ── Simple DB-only crons ─────────────────────────────────────────────────

	s.Register(CronJob{
		Name:     "decay-habit-streaks",
		Interval: 24 * time.Hour,
		RunFunc: func(ctx context.Context) error {
			slog.Info("🔄 decay-habit-streaks: running (stub)")
			return nil
		},
	})

	s.Register(CronJob{
		Name:     "purge-deleted-emails",
		Interval: 24 * time.Hour,
		RunFunc: func(ctx context.Context) error {
			emailStore := store.NewEmailStore(e.db)
			purged, err := emailStore.PurgeDeleted(ctx, cfg.UserID, 7*24*time.Hour)
			if err != nil {
				return err
			}
			if purged > 0 {
				slog.Info("🗑️ purge-deleted-emails: done", "purged", purged)
			}
			return nil
		},
	})

	s.Register(CronJob{
		Name:     "triage-notes-weekly",
		Interval: 7 * 24 * time.Hour,
		RunFunc: func(ctx context.Context) error {
			slog.Info("📝 triage-notes-weekly: running (stub)")
			return nil
		},
	})

	// Prune terminal device commands so historical done/failed rows don't
	// accumulate forever (and stop inflating the failed-command alert count).
	s.Register(CronJob{
		Name:     "cleanup-device-commands",
		Interval: 24 * time.Hour,
		RunFunc: func(ctx context.Context) error {
			deleted, err := e.cmdStore.DeleteOldCompleted(ctx, 7*24*time.Hour)
			if err != nil {
				return err
			}
			if deleted > 0 {
				slog.Info("🗑️ cleanup-device-commands: done", "deleted", deleted)
			}
			return nil
		},
	})

	// Prune sync-run audit history so the table doesn't grow unbounded.
	s.Register(CronJob{
		Name:     "cleanup-sync-runs",
		Interval: 24 * time.Hour,
		RunFunc: func(ctx context.Context) error {
			deleted, err := store.NewSyncRunStore(e.db).DeleteOlderThan(ctx, 14*24*time.Hour)
			if err != nil {
				return err
			}
			if deleted > 0 {
				slog.Info("🗑️ cleanup-sync-runs: done", "deleted", deleted)
			}
			return nil
		},
	})

	// ── Google OAuth client (shared by Gmail + Calendar + HTTP handlers) ──────
	var oauthClient *google.OAuthClient
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.GoogleRefreshToken != "" {
		oauthClient = google.SharedOAuthClient(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRefreshToken)
	}

	// ── Telegram crons ───────────────────────────────────────────────────────
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		s.Register(CronJob{
			Name:     "telegram-scheduled-briefing",
			Interval: 15 * time.Minute,
			RunFunc:  cronTelegramBriefing(e, cfg),
		})

		s.Register(CronJob{
			Name:     "telegram-health-alerts",
			Interval: 1 * time.Hour,
			RunFunc:  cronTelegramHealthAlert(e, cfg),
		})

		s.Register(CronJob{
			Name:     "contacts-reminders",
			Interval: 1 * time.Hour,
			RunFunc:  cronContactsReminders(e, cfg),
		})

		s.Register(CronJob{
			Name:     "contacts-reconnect-nudge",
			Interval: 3 * time.Hour,
			RunFunc:  cronStaleContactsNudge(e, cfg),
		})

		s.Register(CronJob{
			Name:     "telegram-daily-agenda-digest",
			Interval: 15 * time.Minute,
			RunFunc:  cronDailyAgendaDigest(e, cfg),
		})

		s.Register(CronJob{
			Name:     "telegram-appointment-reminders",
			Interval: 10 * time.Minute,
			RunFunc:  cronAppointmentReminders(e, cfg),
		})
	}

	// ── Contacts: mirror LaventeCare business contacts into the unified module ──
	s.Register(CronJob{
		Name:       "contacts-laventecare-sync",
		Interval:   6 * time.Hour,
		RunOnStart: true,
		RunFunc: func(ctx context.Context) error {
			n, err := store.NewContactStore(e.db).BackfillLaventeCareContacts(ctx, cfg.UserID)
			if err != nil {
				slog.Warn("contacts-laventecare-sync: failed", "error", err)
				return nil
			}
			if n > 0 {
				slog.Info("contacts-laventecare-sync: mirrored business contacts", "count", n)
			}
			return nil
		},
	})

	// ── Gmail sync — every 5 minutes ─────────────────────────────────────────
	if cfg.GmailEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:       "sync-gmail",
			Interval:   5 * time.Minute,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(recordingCron(e.db, "gmail", cronGmailSync(oauthClient, e.db, cfg))),
		})
	}

	// ── Google Calendar sync (work schedule) — daily ─────────────────────────
	if cfg.GoogleCalendarEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:       "sync-schedule-daily",
			Interval:   24 * time.Hour,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(recordingCron(e.db, "schedule", cronScheduleSync(oauthClient, e.db, cfg))),
		})

		s.Register(CronJob{
			Name:       "sync-personal-events",
			Interval:   1 * time.Hour,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(recordingCron(e.db, "personal", cronPersonalEventsSync(oauthClient, e.db, cfg))),
		})

		s.Register(CronJob{
			Name:       "process-pending-calendar",
			Interval:   5 * time.Minute,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(recordingCron(e.db, "pending-calendar", e.cronPendingCalendar(oauthClient, cfg))),
		})
	}

	// ── Todoist sync — daily ─────────────────────────────────────────────────
	if cfg.TodoistEnabled && cfg.TodoistAPIToken != "" {
		s.Register(CronJob{
			Name:     "sync-todoist-daily",
			Interval: 24 * time.Hour,
			RunFunc:  cronTodoistSync(e.db, cfg),
		})
	}
}

// CronConfig holds external API flags and keys for cron registration.
type CronConfig struct {
	TelegramBotToken      string
	TelegramChatID        string
	GmailEnabled          bool
	GoogleCalendarEnabled bool
	TodoistEnabled        bool
	UserID                string

	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRefreshToken  string
	SDBCalendarID       string
	PersonalCalendarIDs string
	TodoistAPIToken     string
	TodoistProjectID    string
}

// ── Gmail sync ───────────────────────────────────────────────────────────────

func cronGmailSync(client *google.OAuthClient, db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	emailStore := store.NewEmailStore(db)

	return func(ctx context.Context) error {
		slog.Info("📧 sync-gmail: starting")

		// Load last historyId from email_sync_meta
		meta, err := emailStore.GetSyncMeta(ctx, cfg.UserID)
		historyID := ""
		if err == nil && meta != nil {
			historyID = meta.HistoryID
		}

		storedBefore, err := emailStore.Count(ctx, cfg.UserID)
		if err != nil {
			return err
		}
		if meta != nil && storedBefore == 0 {
			historyID = ""
		}

		result, parsedEmails, newHistID, err := google.SyncGmail(ctx, client, cfg.UserID, historyID)
		if err != nil {
			// Record current failure health so the briefing/status cannot report
			// the last successful count as if the sync were still healthy.
			_ = emailStore.MarkSyncFailed(ctx, cfg.UserID, err.Error())
			return err
		}

		// Convert google.ParsedEmail → model.Email and store in PG
		if len(parsedEmails) > 0 {
			modelEmails := make([]model.Email, len(parsedEmails))
			for i, pe := range parsedEmails {
				var cc, bcc, categorie *string
				if pe.CC != "" {
					cc = &pe.CC
				}
				if pe.BCC != "" {
					bcc = &pe.BCC
				}
				if pe.Categorie != "" {
					categorie = &pe.Categorie
				}

				syncedAt, _ := time.Parse(time.RFC3339, pe.SyncedAt)
				if syncedAt.IsZero() {
					syncedAt = time.Now().UTC()
				}

				modelEmails[i] = model.Email{
					UserID:        pe.UserID,
					GmailID:       pe.GmailID,
					ThreadID:      pe.ThreadID,
					FromAddr:      pe.From,
					ToAddr:        pe.To,
					CC:            cc,
					BCC:           bcc,
					Subject:       pe.Subject,
					Snippet:       pe.Snippet,
					Datum:         pe.Datum,
					Ontvangen:     pe.Ontvangen,
					IsGelezen:     pe.IsGelezen,
					IsSter:        pe.IsSter,
					IsVerwijderd:  pe.IsVerwijderd,
					IsDraft:       pe.IsDraft,
					LabelIDs:      pe.LabelIDs,
					Categorie:     categorie,
					HeeftBijlagen: pe.HeeftBijlagen,
					BijlagenCount: pe.BijlagenCount,
					SearchText:    pe.SearchText,
					SyncedAt:      syncedAt,
				}
			}

			upserted, err := emailStore.BulkUpsert(ctx, modelEmails)
			if err != nil {
				return err
			} else {
				slog.Info("📧 emails stored", "upserted", upserted)
			}
		}

		// Update sync meta
		totalSynced, err := emailStore.Count(ctx, cfg.UserID)
		if err != nil {
			return err
		}
		var lastFullSync *time.Time
		if result.Mode == "full" {
			now := time.Now().UTC()
			lastFullSync = &now
		} else if meta != nil {
			lastFullSync = meta.LastFullSync
		}

		if err := emailStore.UpsertSyncMeta(ctx, cfg.UserID, newHistID, lastFullSync, totalSynced); err != nil {
			slog.Warn("📧 sync meta update failed", "error", err)
		}

		slog.Info("📧 sync-gmail: done",
			"synced", result.Synced,
			"mode", result.Mode,
			"newHistoryId", newHistID,
		)
		return nil
	}
}

// ── Schedule sync ────────────────────────────────────────────────────────────

func cronScheduleSync(client *google.OAuthClient, db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		slog.Info("📅 sync-schedule: starting")

		scheduleSync, err := google.SyncScheduleDetailed(ctx, client, cfg.UserID, cfg.SDBCalendarID)
		if err != nil {
			return err
		}
		diensten := scheduleSync.Diensten

		// Convert to store format and bulk upsert
		schedStore := store.NewScheduleStore(db)
		items := make([]model.ScheduleImport, len(diensten))
		for i, d := range diensten {
			items[i] = model.ScheduleImport{
				EventID:      d.EventID,
				Titel:        d.Titel,
				StartDatum:   d.StartDatum,
				StartTijd:    d.StartTijd,
				EindDatum:    d.EindDatum,
				EindTijd:     d.EindTijd,
				Werktijd:     d.Werktijd,
				Locatie:      d.Locatie,
				Team:         d.Team,
				ShiftType:    d.ShiftType,
				Prioriteit:   d.Prioriteit,
				Duur:         d.Duur,
				Weeknr:       d.Weeknr,
				Dag:          d.Dag,
				Status:       d.Status,
				Beschrijving: d.Beschrijving,
				Heledag:      d.Heledag,
			}
		}

		count, err := schedStore.BulkUpsert(ctx, cfg.UserID, items)
		if err != nil {
			return err
		}

		pruned, err := schedStore.PruneMissingInDateRange(
			ctx,
			cfg.UserID,
			scheduleSync.PruneStartDatum,
			scheduleSync.PruneEindDatum,
			scheduleSync.FetchedEventIDs,
		)
		if err != nil {
			return err
		}
		_ = schedStore.UpsertMeta(ctx, cfg.UserID, "Google Calendar Sync", len(items))

		// Push to Todoist immediately (best-effort) so a shift change lands now
		// instead of waiting up to a day for the separate daily todoist cron.
		if cfg.TodoistEnabled && cfg.TodoistAPIToken != "" {
			if res, terr := pushScheduleToTodoist(ctx, db, cfg); terr != nil {
				slog.Warn("sync-schedule: todoist push failed (non-fatal)", "error", terr)
			} else if res != nil {
				slog.Info("sync-schedule: todoist pushed", "updated", res.Updated, "created", res.Created, "closed", res.Closed, "deleted", res.Deleted, "failed", res.Failed)
			}
		}

		slog.Info("📅 sync-schedule: done", "parsed", len(diensten), "upserted", count, "pruned", pruned)
		return nil
	}
}

// ── Personal events sync ─────────────────────────────────────────────────────

func cronPersonalEventsSync(client *google.OAuthClient, db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		slog.Info("📅 sync-personal-events: starting")

		calendarIDs := []string{"primary"}
		if cfg.PersonalCalendarIDs != "" {
			calendarIDs = google.SplitCalendarIDs(cfg.PersonalCalendarIDs)
		}

		personalSync, err := google.SyncPersonalEventsDetailed(ctx, client, cfg.UserID, calendarIDs, cfg.SDBCalendarID)
		if err != nil {
			return err
		}
		events := personalSync.Events

		evStore := store.NewPersonalEventStore(db)
		peEvents := make([]model.PersonalEvent, 0, len(events))
		for _, e := range events {
			peEvents = append(peEvents, model.PersonalEvent{
				UserID:               e.UserID,
				EventID:              e.EventID,
				Titel:                e.Titel,
				StartDatum:           e.StartDatum,
				StartTijd:            strPtr(e.StartTijd),
				EindDatum:            e.EindDatum,
				EindTijd:             strPtr(e.EindTijd),
				Heledag:              e.Heledag,
				Locatie:              strPtr(e.Locatie),
				Beschrijving:         strPtr(e.Beschrijving),
				Symbol:               strPtr(e.Symbol),
				BusinessContextType:  strPtr(e.BusinessContextType),
				BusinessContextID:    strPtr(e.BusinessContextID),
				BusinessContextTitle: strPtr(e.BusinessContextTitle),
				Status:               e.Status,
				Kalender:             e.Kalender,
			})
		}

		// Batch upsert in one transaction; fall back to per-row so a single bad
		// row (e.g. an over-length field) doesn't block the whole sync.
		upserted, err := evStore.BulkUpsertSynced(ctx, peEvents)
		if err != nil {
			slog.Warn("personal events batch upsert failed, falling back to per-row", "error", err)
			upserted = 0
			for _, pe := range peEvents {
				if e := evStore.UpsertSynced(ctx, pe); e != nil {
					slog.Warn("personal event upsert failed", "eventId", pe.EventID, "error", e)
					continue
				}
				upserted++
			}
		}

		pruned, err := evStore.MarkMissingSyncedInDateRange(
			ctx,
			cfg.UserID,
			personalSync.PruneStartDatum,
			personalSync.PruneEindDatum,
			personalSync.FetchedEventIDs,
			personalSync.SyncedKalenders,
		)
		if err != nil {
			return err
		}

		slog.Info("📅 sync-personal-events: done", "parsed", len(events), "upserted", upserted, "pruned", pruned)
		return nil
	}
}

// pendingCalendarMaxAttempts caps how often a single failing pending calendar
// operation is retried before it is dead-lettered (status PendingFailed), so a
// permanently-bad op (e.g. a deleted target, malformed time) can no longer loop
// forever on every 5-minute tick.
const pendingCalendarMaxAttempts = 5

func (e *Engine) cronPendingCalendar(client *google.OAuthClient, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		slog.Info("📅 process-pending-calendar: starting")
		processed, deadLettered, err := e.processPendingCalendarOps(ctx, client, cfg.UserID)
		if err != nil {
			return err // re-auth: wrapGoogleCron fires the de-duped alert
		}
		if deadLettered > 0 {
			e.alertPendingCalendarFailures(ctx, deadLettered)
		}
		slog.Info("📅 process-pending-calendar: done", "processed", processed, "deadLettered", deadLettered)
		return nil
	}
}

// processPendingCalendarOps pushes all pending personal-event ops to Google with
// retry-cap/dead-letter bookkeeping. Shared by the cron and the Telegram /sync
// path so error handling can't drift. It returns the processed and dead-lettered
// counts, and a non-nil error only for a re-auth (token) failure — which aborts
// the batch so the op's retry budget isn't burned on a token problem.
func (e *Engine) processPendingCalendarOps(ctx context.Context, client *google.OAuthClient, userID string) (processed, deadLettered int, err error) {
	evStore := store.NewPersonalEventStore(e.db)
	events, listErr := evStore.ListPendingCalendar(ctx, userID, 50)
	if listErr != nil {
		return 0, 0, listErr
	}

	for _, event := range events {
		calendarID, googleEventID := google.ResolveCalendarTarget(event)
		nextStatus := resolvedPersonalEventStatus(event)

		var opErr error
		switch event.Status {
		case store.PersonalEventStatusPendingCreate:
			createdID, cerr := google.CreatePersonalEvent(ctx, client, calendarID, event)
			if cerr != nil {
				opErr = cerr
				break
			}
			storedID := google.StoredCalendarEventID(calendarID, createdID)
			opErr = evStore.ReplaceEventIDAndStatus(ctx, event.UserID, event.EventID, storedID, nextStatus)
		case store.PersonalEventStatusPendingUpdate:
			if uerr := google.UpdatePersonalEvent(ctx, client, calendarID, googleEventID, event); uerr != nil {
				opErr = uerr
				break
			}
			opErr = evStore.UpdateStatus(ctx, event.UserID, event.EventID, nextStatus)
		case store.PersonalEventStatusPendingDelete:
			if derr := google.DeletePersonalEvent(ctx, client, calendarID, googleEventID); derr != nil {
				opErr = derr
				break
			}
			opErr = evStore.UpdateStatus(ctx, event.UserID, event.EventID, store.PersonalEventStatusDeleted)
		default:
			continue
		}

		if opErr != nil {
			if errors.Is(opErr, google.ErrGoogleReauthRequired) {
				return processed, deadLettered, opErr
			}
			// A permanent error (e.g. editing a Google-generated birthday event)
			// will fail identically every time, so dead-letter it on the first
			// attempt instead of burning the normal 5-attempt retry budget.
			maxAttempts := pendingCalendarMaxAttempts
			if google.IsPermanentCalendarError(opErr) {
				maxAttempts = 1
			}
			deadLetter, recErr := evStore.RecordPendingFailure(ctx, event.UserID, event.EventID, opErr.Error(), maxAttempts)
			if recErr != nil {
				slog.Warn("pending calendar failure bookkeeping failed", "eventId", event.EventID, "error", recErr)
			}
			if deadLetter {
				deadLettered++
				slog.Warn("pending calendar op dead-lettered after max attempts",
					"eventId", event.EventID, "status", event.Status, "error", opErr)
			} else {
				slog.Warn("pending calendar op failed (will retry)",
					"eventId", event.EventID, "status", event.Status, "error", opErr)
			}
			continue
		}
		processed++
	}

	return processed, deadLettered, nil
}

// alertPendingCalendarFailures sends a single de-duplicated notification (max
// once per 24h) when one or more pending calendar ops are dead-lettered.
func (e *Engine) alertPendingCalendarFailures(ctx context.Context, count int) {
	if !e.shouldFireAlert("pending-calendar-failed", 24*time.Hour) {
		return
	}
	msg := fmt.Sprintf("⚠️ Agenda-sync probleem\n\n%d agenda-bewerking(en) zijn na %d pogingen mislukt en op 'mislukt' gezet, zodat ze niet eindeloos opnieuw geprobeerd worden. Bekijk de openstaande afspraken in de app.", count, pendingCalendarMaxAttempts)
	if err := e.SendProactiveNotification(ctx, msg); err != nil {
		slog.Warn("alertPendingCalendarFailures: failed to send", "error", err)
	}
}

// ── Todoist sync ─────────────────────────────────────────────────────────────

func cronTodoistSync(db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		slog.Info("✅ sync-todoist: starting")
		_, err := pushScheduleToTodoist(ctx, db, cfg)
		return err
	}
}

// pushScheduleToTodoist pushes the stored schedule to Todoist. Shared by the
// daily cron, the schedule sync (so changes land immediately), and the
// /sync/todoist handler.
func pushScheduleToTodoist(ctx context.Context, db *store.DB, cfg CronConfig) (*todoist.SyncResult, error) {
	rows, err := store.NewScheduleStore(db).List(ctx, cfg.UserID)
	if err != nil {
		return nil, err
	}
	diensten := make([]todoist.Dienst, 0, len(rows))
	for _, r := range rows {
		diensten = append(diensten, todoist.Dienst{
			EventID: r.EventID, Titel: r.Titel, StartDatum: r.StartDatum,
			StartTijd: r.StartTijd, EindTijd: r.EindTijd, Locatie: r.Locatie,
			ShiftType: r.ShiftType, Duur: r.Duur, Heledag: r.Heledag, Status: r.Status,
		})
	}
	client := todoist.NewClient(cfg.TodoistAPIToken, cfg.TodoistProjectID)
	ctx = context.WithValue(ctx, "today", time.Now().Format("2006-01-02"))
	return client.SyncDiensten(ctx, diensten)
}

// ── Telegram cron implementations ────────────────────────────────────────────

// cronContactsReminders sends a once-a-morning Telegram nudge for upcoming
// contact birthdays/anniversaries (within a week). The morning window + a 20h
// shouldFireAlert cooldown keep it to one message per day even though the job
// ticks hourly.
func cronContactsReminders(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		if now.Hour() < 7 || now.Hour() >= 12 {
			return nil
		}
		dates, err := store.NewContactStore(e.db).UpcomingImportantDates(ctx, cfg.UserID, 7)
		if err != nil {
			slog.Warn("cronContactsReminders: query failed", "error", err)
			return nil
		}
		if len(dates) == 0 {
			return nil
		}
		// Claim the day only once we actually have something to send (an empty tick
		// mustn't burn the window) — persistent so a redeploy can't double-fire.
		if !e.claimCronWindow(ctx, "contacts-daily-reminder", now.Format("2006-01-02")) {
			return nil
		}
		var b strings.Builder
		b.WriteString("🎂 Belangrijke datums deze week:\n")
		for _, d := range dates {
			when := "vandaag"
			if d.DaysUntil == 1 {
				when = "morgen"
			} else if d.DaysUntil > 1 {
				when = fmt.Sprintf("over %d dagen", d.DaysUntil)
			}
			kind := "verjaardag"
			if d.Kind == "anniversary" {
				kind = "jubileum"
			} else if d.Kind != "birthday" && d.Label != nil && strings.TrimSpace(*d.Label) != "" {
				kind = strings.TrimSpace(*d.Label)
			}
			line := fmt.Sprintf("• %s — %s %s", d.ContactName, kind, when)
			if d.TurningAge != nil {
				line += fmt.Sprintf(" (wordt %d)", *d.TurningAge)
			}
			b.WriteString(line + "\n")
		}
		if err := e.SendProactiveNotification(ctx, b.String()); err != nil {
			slog.Warn("cronContactsReminders: send failed", "error", err)
		}
		return nil
	}
}

// cronStaleContactsNudge sends a weekly "wie moet ik weer eens spreken" nudge for
// close relationships (family/friends) not contacted in a long time. Fires once a
// week (Monday morning) among the eligible candidates; the 6-day cooldown keeps
// it to one message even though the job ticks every few hours.
func cronStaleContactsNudge(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		if now.Weekday() != time.Monday || now.Hour() < 8 || now.Hour() >= 12 {
			return nil
		}
		const staleDays = 90
		stale, err := store.NewContactStore(e.db).StaleContacts(ctx, cfg.UserID, staleDays, 50)
		if err != nil {
			slog.Warn("cronStaleContactsNudge: query failed", "error", err)
			return nil
		}
		// Only nudge for close relationships we've actually spoken to before, so
		// the list stays meaningful (never-contacted rows are excluded).
		type cand struct {
			name string
			days int
		}
		cands := []cand{}
		for _, sc := range stale {
			if sc.DaysSince == nil {
				continue
			}
			if !hasAnyRelationship(sc.RelationshipTypes, "family", "friend") {
				continue
			}
			cands = append(cands, cand{name: sc.DisplayName, days: *sc.DaysSince})
			if len(cands) >= 5 {
				break
			}
		}
		if len(cands) == 0 {
			return nil
		}
		// Claim the week only now that there's a real candidate list — persistent
		// (per ISO week) so a Monday-morning redeploy can't send it twice.
		isoYear, isoWeek := now.ISOWeek()
		if !e.claimCronWindow(ctx, "contacts-reconnect-nudge", fmt.Sprintf("%d-W%02d", isoYear, isoWeek)) {
			return nil
		}
		var b strings.Builder
		b.WriteString("👋 Wie je weer eens kunt spreken:\n")
		for _, c := range cands {
			months := c.days / 30
			var ago string
			if months >= 2 {
				ago = fmt.Sprintf("%d maanden", months)
			} else {
				ago = fmt.Sprintf("%d dagen", c.days)
			}
			b.WriteString(fmt.Sprintf("• %s — %s geleden gesproken\n", c.name, ago))
		}
		if err := e.SendProactiveNotification(ctx, b.String()); err != nil {
			slog.Warn("cronStaleContactsNudge: send failed", "error", err)
		}
		return nil
	}
}

// hasAnyRelationship reports whether the relationship-type slice contains any of
// the given types.
func hasAnyRelationship(types []string, needles ...string) bool {
	for _, h := range types {
		for _, n := range needles {
			if h == n {
				return true
			}
		}
	}
	return false
}

func cronTelegramBriefing(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)

		// 1. Check user preferences for BriefingTime
		prefStore := store.NewPreferencesStore(e.db.Pool)
		prefs, err := prefStore.Get(ctx, cfg.UserID)
		if err != nil {
			slog.Warn("cronTelegramBriefing: failed to get user preferences", "error", err)
			return nil
		}

		briefingTime := "08:00"
		if prefs.BriefingTime != nil && *prefs.BriefingTime != "" {
			briefingTime = *prefs.BriefingTime
		}

		// Parse BriefingTime hour and minute (format "HH:MM")
		parts := strings.Split(briefingTime, ":")
		if len(parts) != 2 {
			slog.Warn("cronTelegramBriefing: invalid briefing_time format", "time", briefingTime)
			return nil
		}
		targetHour, _ := strconv.Atoi(parts[0])
		targetMinute, _ := strconv.Atoi(parts[1])

		// Calculate difference in minutes
		targetMinutes := targetHour*60 + targetMinute
		currentMinutes := now.Hour()*60 + now.Minute()

		// Since the cron runs every 15 minutes, we trigger if current time is within [targetMinutes, targetMinutes + 14]
		// and we haven't sent a briefing yet today.
		if currentMinutes < targetMinutes || currentMinutes >= targetMinutes+15 {
			return nil
		}

		chatIDStr := cfg.TelegramChatID
		if chatIDStr == "" {
			return nil
		}
		chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
		if err != nil {
			slog.Warn("cronTelegramBriefing: invalid chat ID", "error", err)
			return nil
		}

		// 2. Atomically claim today's briefing. ON CONFLICT DO NOTHING makes the
		// once-per-day guard atomic, so it survives the multi-second LLM loop and
		// any concurrent tick/process (unlike the old non-atomic content-LIKE check).
		today := now.Format("2006-01-02")
		claim, err := e.db.Pool.Exec(ctx,
			`INSERT INTO briefing_sent (day) VALUES ($1) ON CONFLICT (day) DO NOTHING`, today)
		if err != nil {
			slog.Warn("cronTelegramBriefing: failed to claim briefing day", "error", err)
			return nil
		}
		if claim.RowsAffected() == 0 {
			slog.Debug("cronTelegramBriefing: briefing already sent today")
			return nil
		}

		slog.Info("📬 cronTelegramBriefing: sending scheduled briefing", "time", now.Format("15:04"))

		// 3. Trigger Grok AI to generate the briefing
		briefingQuery := "Geef mij een compacte dagbriefing voor vandaag. Combineer planning, werkrooster, afspraken, notities, habits, email, lampen en systeemstatus. Sluit af met maximaal drie concrete aandachtspunten."

		// ProcessAIPrompt persists the user turn itself (after loading prior
		// history) and serializes per chat, so it's safe even if a live
		// Telegram message arrives while this briefing is mid-flight.
		_, err = e.ProcessAIPrompt(ctx, chatID, briefingQuery, "brain", false)
		if err != nil {
			// Release the day's claim so a later tick can retry today.
			_, _ = e.db.Pool.Exec(ctx, `DELETE FROM briefing_sent WHERE day = $1`, today)
			slog.Error("cronTelegramBriefing: failed to process briefing prompt", "error", err)
			return err
		}

		return nil
	}
}

func cronTelegramHealthAlert(e *Engine, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		hour := now.Hour()

		// Only check during day hours (7:00 to 22:00) to respect rest
		if hour >= 23 || hour < 7 {
			return nil
		}

		slog.Debug("🏥 health alert check", "time", now.Format("15:04"))

		// Check for open pending actions that require confirmation
		pendingStore := store.NewPendingStore(e.db.Pool)
		actions, err := pendingStore.ListPending(ctx, cfg.UserID)
		if err == nil && len(actions) > 0 {
			var b strings.Builder
			b.WriteString("🔔 Herinnering: Je hebt nog openstaande acties die wachten op bevestiging:\n")
			for _, action := range actions {
				b.WriteString(fmt.Sprintf("• %s (code: %s)\n", action.Summary, action.Code))
			}
			b.WriteString("\nGebruik /approve [code] of /bevestigingen om ze te verwerken.")

			err = e.SendProactiveNotification(ctx, b.String())
			if err != nil {
				slog.Warn("cronTelegramHealthAlert: failed to send reminder", "error", err)
			}
		}

		// ── Bridge liveness ──────────────────────────────────────────────────
		// The local bridge touches its heartbeat on every /bridge/* call. If it
		// was alive before but has gone quiet, WiZ lights + local automations are
		// likely dead — surface it instead of the user noticing by hand. Only a
		// previously-seen (non-zero) bridge alerts, so a never-deployed bridge
		// doesn't nag.
		if e.cmdStore != nil {
			if lastSeen, lsErr := e.cmdStore.BridgeLastSeen(ctx); lsErr == nil && !lastSeen.IsZero() {
				stale := time.Since(lastSeen)
				if stale > 15*time.Minute && e.shouldFireAlert("bridge-stale", 6*time.Hour) {
					msg := fmt.Sprintf("🔌 Bridge offline\n\nDe lokale bridge heeft zich %s niet gemeld (laatste teken van leven: %s). WiZ-lampen en lokale automations reageren waarschijnlijk niet.\n\nControleer of het bridge-proces nog draait.",
						humanizeDuration(stale), lastSeen.In(amsterdam).Format("02-01 15:04"))
					if sendErr := e.SendProactiveNotification(ctx, msg); sendErr != nil {
						slog.Warn("cronTelegramHealthAlert: bridge-stale alert failed", "error", sendErr)
					}
				}
			}
		}

		// ── Sync health ──────────────────────────────────────────────────────
		// Alert on a streak of consecutive sync failures using the sync_runs data
		// already collected. invalid_grant/reauth streaks are skipped here because
		// the dedicated Google re-auth alert already covers them.
		if failures, fsErr := store.NewSyncRunStore(e.db).FailingSources(ctx, 3, 2*time.Hour); fsErr == nil {
			for _, f := range failures {
				le := strings.ToLower(f.LastError)
				if strings.Contains(le, "invalid_grant") || strings.Contains(le, "reauth") {
					continue
				}
				if !e.shouldFireAlert("sync-streak:"+f.Source, 6*time.Hour) {
					continue
				}
				msg := fmt.Sprintf("⚠️ Sync hapert: %s\n\nDe laatste %d runs faalden achter elkaar. Laatste fout:\n%s\n\nBekijk /sync/status voor details.",
					f.Source, f.Streak, truncateErr(f.LastError))
				if sendErr := e.SendProactiveNotification(ctx, msg); sendErr != nil {
					slog.Warn("cronTelegramHealthAlert: sync-streak alert failed", "source", f.Source, "error", sendErr)
				}
			}
		}

		return nil
	}
}

// humanizeDuration renders a duration as a compact Dutch "Nu Nm" / "Nm" string.
func humanizeDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%du %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// truncateErr bounds an error string for inclusion in a Telegram message.
func truncateErr(s string) string {
	if s == "" {
		return "(geen details)"
	}
	const max = 200
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

// recordSyncRun persists one sync-run audit row (best-effort, detached ctx so it
// isn't cancelled with an already-finishing request).
func recordSyncRun(ctx context.Context, db *store.DB, run store.SyncRun) {
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := store.NewSyncRunStore(db).Record(logCtx, run); err != nil {
		slog.Warn("sync_runs record failed", "source", run.Source, "error", err)
	}
}

// recordingCron wraps a sync job so every run is timed and audited in sync_runs,
// giving a queryable history of outcomes/latency instead of only the latest
// freshness snapshot. The wrapped error is passed through unchanged.
func recordingCron(db *store.DB, source string, inner func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		start := time.Now()
		err := inner(ctx)
		run := store.SyncRun{
			Source:     source,
			StartedAt:  start,
			DurationMs: int(time.Since(start).Milliseconds()),
			OK:         err == nil,
		}
		if err != nil {
			run.Error = err.Error()
		}
		recordSyncRun(ctx, db, run)
		return err
	}
}

// wrapGoogleCron runs a Google-backed sync job and, when it fails because the
// refresh token is expired/revoked (invalid_grant), fires a single
// de-duplicated re-auth reminder instead of logging the same error every tick.
func (e *Engine) wrapGoogleCron(inner func(ctx context.Context) error) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		err := inner(ctx)
		if err != nil && errors.Is(err, google.ErrGoogleReauthRequired) {
			e.alertGoogleReauthOnce(ctx)
		}
		return err
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func resolvedPersonalEventStatus(event model.PersonalEvent) string {
	endDate := event.EindDatum
	if endDate == "" {
		endDate = event.StartDatum
	}

	endClock := "23:59"
	if !event.Heledag && event.EindTijd != nil && *event.EindTijd != "" {
		endClock = *event.EindTijd
	}

	end, err := time.ParseInLocation("2006-01-02 15:04", endDate+" "+endClock, amsterdam)
	if err != nil {
		return store.PersonalEventStatusUpcoming
	}
	if end.Before(time.Now().In(amsterdam)) {
		return store.PersonalEventStatusPast
	}
	return store.PersonalEventStatusUpcoming
}
