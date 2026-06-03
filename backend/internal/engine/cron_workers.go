package engine

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/todoist"
)

// RegisterHomeappCrons adds all migrated Convex cron jobs to the scheduler.
func RegisterHomeappCrons(s *CronScheduler, db *store.DB, cfg CronConfig) {

	s.Register(CronJob{
		Name:     "schedule-weekly-check",
		Interval: 1 * time.Hour, // Evaluates hourly, but logic only executes on Sunday 19:00
		RunFunc:  cronScheduleWeeklyCheck(db, cfg),
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
			emailStore := store.NewEmailStore(db)
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

	// ── Google OAuth client (shared by Gmail + Calendar) ─────────────────────
	var oauthClient *google.OAuthClient
	if cfg.GoogleClientID != "" && cfg.GoogleClientSecret != "" && cfg.GoogleRefreshToken != "" {
		oauthClient = google.NewOAuthClient(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRefreshToken)
	}

	// ── Telegram crons ───────────────────────────────────────────────────────
	if cfg.TelegramBotToken != "" && cfg.TelegramChatID != "" {
		s.Register(CronJob{
			Name:     "telegram-scheduled-briefing",
			Interval: 15 * time.Minute,
			RunFunc:  cronTelegramBriefing(cfg),
		})

		s.Register(CronJob{
			Name:     "telegram-health-alerts",
			Interval: 1 * time.Hour,
			RunFunc:  cronTelegramHealthAlert(cfg),
		})
	}

	// ── Gmail sync — every 5 minutes ─────────────────────────────────────────
	if cfg.GmailEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:     "sync-gmail",
			Interval: 5 * time.Minute,
			RunFunc:  cronGmailSync(oauthClient, db, cfg),
		})
	}

	// ── Google Calendar sync (work schedule) — daily ─────────────────────────
	if cfg.GoogleCalendarEnabled && oauthClient != nil {
		s.Register(CronJob{
			Name:     "sync-schedule-daily",
			Interval: 24 * time.Hour,
			RunFunc:  cronScheduleSync(oauthClient, db, cfg),
		})

		s.Register(CronJob{
			Name:     "sync-personal-events",
			Interval: 1 * time.Hour,
			RunFunc:  cronPersonalEventsSync(oauthClient, db, cfg),
		})

		s.Register(CronJob{
			Name:     "process-pending-calendar",
			Interval: 5 * time.Minute,
			RunFunc:  cronPendingCalendar(oauthClient, db, cfg),
		})
	}

	// ── Todoist sync — daily ─────────────────────────────────────────────────
	if cfg.TodoistEnabled && cfg.TodoistAPIToken != "" {
		s.Register(CronJob{
			Name:     "sync-todoist-daily",
			Interval: 24 * time.Hour,
			RunFunc:  cronTodoistSync(db, cfg),
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

		result, parsedEmails, newHistID, err := google.SyncGmail(ctx, client, cfg.UserID, historyID)
		if err != nil {
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
				slog.Warn("📧 email bulk upsert failed", "error", err)
			} else {
				slog.Info("📧 emails stored", "upserted", upserted)
			}
		}

		// Update sync meta
		totalSynced := len(parsedEmails)
		if meta != nil {
			totalSynced += meta.TotalSynced
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

		diensten, err := google.SyncSchedule(ctx, client, cfg.UserID, cfg.SDBCalendarID)
		if err != nil {
			return err
		}

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

		slog.Info("📅 sync-schedule: done", "parsed", len(diensten), "upserted", count)
		return nil
	}
}

// ── Personal events sync ─────────────────────────────────────────────────────

func cronPersonalEventsSync(client *google.OAuthClient, db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		slog.Info("📅 sync-personal-events: starting")

		calendarIDs := []string{"primary"}
		if cfg.PersonalCalendarIDs != "" {
			calendarIDs = splitCalendarIDs(cfg.PersonalCalendarIDs)
		}

		events, err := google.SyncPersonalEvents(ctx, client, cfg.UserID, calendarIDs, cfg.SDBCalendarID)
		if err != nil {
			return err
		}

		evStore := store.NewPersonalEventStore(db)
		upserted := 0
		for _, e := range events {
			startTijd := strPtr(e.StartTijd)
			eindTijd := strPtr(e.EindTijd)
			locatie := strPtr(e.Locatie)
			beschrijving := strPtr(e.Beschrijving)

			pe := model.PersonalEvent{
				UserID:       e.UserID,
				EventID:      e.EventID,
				Titel:        e.Titel,
				StartDatum:   e.StartDatum,
				StartTijd:    startTijd,
				EindDatum:    e.EindDatum,
				EindTijd:     eindTijd,
				Heledag:      e.Heledag,
				Locatie:      locatie,
				Beschrijving: beschrijving,
				Status:       e.Status,
				Kalender:     e.Kalender,
			}
			err := evStore.UpsertSynced(ctx, pe)
			if err != nil {
				slog.Warn("personal event upsert failed", "eventId", e.EventID, "error", err)
				continue
			}
			upserted++
		}

		slog.Info("📅 sync-personal-events: done", "parsed", len(events), "upserted", upserted)
		return nil
	}
}

func cronPendingCalendar(client *google.OAuthClient, db *store.DB, cfg CronConfig) func(ctx context.Context) error {
	evStore := store.NewPersonalEventStore(db)

	return func(ctx context.Context) error {
		slog.Info("📅 process-pending-calendar: starting")

		events, err := evStore.ListPendingCalendar(ctx, cfg.UserID, 50)
		if err != nil {
			return err
		}

		processed := 0
		for _, event := range events {
			calendarID, googleEventID := calendarTarget(event)
			nextStatus := resolvedPersonalEventStatus(event)

			switch event.Status {
			case store.PersonalEventStatusPendingCreate:
				createdID, err := google.CreatePersonalEvent(ctx, client, calendarID, event)
				if err != nil {
					slog.Warn("pending calendar create failed", "eventId", event.EventID, "error", err)
					continue
				}
				storedID := storedCalendarEventID(calendarID, createdID)
				if err := evStore.ReplaceEventIDAndStatus(ctx, event.UserID, event.EventID, storedID, nextStatus); err != nil {
					slog.Warn("pending calendar create status update failed", "eventId", event.EventID, "googleEventId", storedID, "error", err)
					continue
				}
			case store.PersonalEventStatusPendingUpdate:
				if err := google.UpdatePersonalEvent(ctx, client, calendarID, googleEventID, event); err != nil {
					slog.Warn("pending calendar update failed", "eventId", event.EventID, "error", err)
					continue
				}
				if err := evStore.UpdateStatus(ctx, event.UserID, event.EventID, nextStatus); err != nil {
					slog.Warn("pending calendar update status failed", "eventId", event.EventID, "error", err)
					continue
				}
			case store.PersonalEventStatusPendingDelete:
				if err := google.DeletePersonalEvent(ctx, client, calendarID, googleEventID); err != nil {
					slog.Warn("pending calendar delete failed", "eventId", event.EventID, "error", err)
					continue
				}
				if err := evStore.UpdateStatus(ctx, event.UserID, event.EventID, store.PersonalEventStatusDeleted); err != nil {
					slog.Warn("pending calendar delete status failed", "eventId", event.EventID, "error", err)
					continue
				}
			default:
				continue
			}
			processed++
		}

		slog.Info("📅 process-pending-calendar: done", "pending", len(events), "processed", processed)
		return nil
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

func cronTelegramBriefing(cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		hour := now.Hour()

		if hour < 6 || hour > 9 {
			return nil
		}

		slog.Info("📬 telegram briefing check", "time", now.Format("15:04"))
		// TODO: build briefing message from schedule + events
		return nil
	}
}

func cronTelegramHealthAlert(cfg CronConfig) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		now := time.Now().In(amsterdam)
		hour := now.Hour()

		if hour >= 23 || hour < 7 {
			return nil
		}

		slog.Debug("🏥 health alert check", "time", now.Format("15:04"))
		return nil
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func calendarTarget(event model.PersonalEvent) (calendarID, googleEventID string) {
	calendarID = strings.TrimSpace(event.Kalender)
	if calendarID == "" || strings.EqualFold(calendarID, "Main") {
		calendarID = "primary"
	}

	googleEventID = event.EventID
	if calendarID != "primary" {
		prefix := calendarID + ":"
		googleEventID = strings.TrimPrefix(googleEventID, prefix)
	}
	return calendarID, googleEventID
}

func storedCalendarEventID(calendarID, googleEventID string) string {
	if calendarID == "" || calendarID == "primary" {
		return googleEventID
	}
	return calendarID + ":" + googleEventID
}

func splitCalendarIDs(raw string) []string {
	parts := strings.Split(raw, ",")
	calendarIDs := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			calendarIDs = append(calendarIDs, part)
		}
	}
	if len(calendarIDs) == 0 {
		return []string{"primary"}
	}
	return calendarIDs
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
