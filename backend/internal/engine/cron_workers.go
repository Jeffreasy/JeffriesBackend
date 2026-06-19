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
	}

	// ── Gmail sync — every 5 minutes ─────────────────────────────────────────
	if cfg.GmailEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:       "sync-gmail",
			Interval:   5 * time.Minute,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(cronGmailSync(oauthClient, e.db, cfg)),
		})
	}

	// ── Google Calendar sync (work schedule) — daily ─────────────────────────
	if cfg.GoogleCalendarEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:       "sync-schedule-daily",
			Interval:   24 * time.Hour,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(cronScheduleSync(oauthClient, e.db, cfg)),
		})

		s.Register(CronJob{
			Name:       "sync-personal-events",
			Interval:   1 * time.Hour,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(cronPersonalEventsSync(oauthClient, e.db, cfg)),
		})

		s.Register(CronJob{
			Name:       "process-pending-calendar",
			Interval:   5 * time.Minute,
			RunOnStart: true,
			RunFunc:    e.wrapGoogleCron(e.cronPendingCalendar(oauthClient, cfg)),
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
		upserted := 0
		for _, e := range events {
			startTijd := strPtr(e.StartTijd)
			eindTijd := strPtr(e.EindTijd)
			locatie := strPtr(e.Locatie)
			beschrijving := strPtr(e.Beschrijving)
			symbol := strPtr(e.Symbol)
			businessContextType := strPtr(e.BusinessContextType)
			businessContextID := strPtr(e.BusinessContextID)
			businessContextTitle := strPtr(e.BusinessContextTitle)

			pe := model.PersonalEvent{
				UserID:               e.UserID,
				EventID:              e.EventID,
				Titel:                e.Titel,
				StartDatum:           e.StartDatum,
				StartTijd:            startTijd,
				EindDatum:            e.EindDatum,
				EindTijd:             eindTijd,
				Heledag:              e.Heledag,
				Locatie:              locatie,
				Beschrijving:         beschrijving,
				Symbol:               symbol,
				BusinessContextType:  businessContextType,
				BusinessContextID:    businessContextID,
				BusinessContextTitle: businessContextTitle,
				Status:               e.Status,
				Kalender:             e.Kalender,
			}
			err := evStore.UpsertSynced(ctx, pe)
			if err != nil {
				slog.Warn("personal event upsert failed", "eventId", e.EventID, "error", err)
				continue
			}
			upserted++
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
	evStore := store.NewPersonalEventStore(e.db)

	return func(ctx context.Context) error {
		slog.Info("📅 process-pending-calendar: starting")

		events, err := evStore.ListPendingCalendar(ctx, cfg.UserID, 50)
		if err != nil {
			return err
		}

		processed := 0
		deadLettered := 0
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
				// A token problem isn't the op's fault — abort the run and let
				// wrapGoogleCron fire the de-duped re-auth alert, without burning
				// this op's retry budget.
				if errors.Is(opErr, google.ErrGoogleReauthRequired) {
					return opErr
				}
				deadLetter, recErr := evStore.RecordPendingFailure(ctx, event.UserID, event.EventID, opErr.Error(), pendingCalendarMaxAttempts)
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

		if deadLettered > 0 {
			e.alertPendingCalendarFailures(ctx, deadLettered)
		}

		slog.Info("📅 process-pending-calendar: done", "pending", len(events), "processed", processed, "deadLettered", deadLettered)
		return nil
	}
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

		// Fetch schedule from PG
		schedStore := store.NewScheduleStore(db)
		rows, err := schedStore.List(ctx, cfg.UserID)
		if err != nil {
			return err
		}

		var diensten []todoist.Dienst
		for _, r := range rows {
			diensten = append(diensten, todoist.Dienst{
				EventID:    r.EventID,
				Titel:      r.Titel,
				StartDatum: r.StartDatum,
				StartTijd:  r.StartTijd,
				EindTijd:   r.EindTijd,
				Locatie:    r.Locatie,
				ShiftType:  r.ShiftType,
				Duur:       r.Duur,
				Heledag:    r.Heledag,
				Status:     r.Status,
			})
		}

		client := todoist.NewClient(cfg.TodoistAPIToken, cfg.TodoistProjectID)
		// Pass today's date via context
		ctx = context.WithValue(ctx, "today", time.Now().Format("2006-01-02"))
		_, err = client.SyncDiensten(ctx, diensten)
		return err
	}
}

// ── Telegram cron implementations ────────────────────────────────────────────

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

		// Save the user intent message in history first to keep context clean
		chatStore := store.NewChatStore(e.db.Pool)
		_ = chatStore.SaveMessage(ctx, chatID, "user", briefingQuery, nil)

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

		return nil
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
