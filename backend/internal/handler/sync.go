package handler

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/todoist"
)

type SyncHandler struct {
	db  *store.DB
	cfg *config.Config
}

func NewSyncHandler(db *store.DB, cfg *config.Config) *SyncHandler {
	return &SyncHandler{db: db, cfg: cfg}
}

// SyncCalendar triggers a manual sync of Google Calendar and Personal Events.
// @Summary Sync Calendar
// @Description Triggers a manual sync of Google Calendar to fetch schedules and personal events
// @Tags Sync
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Success 200 {object} map[string]interface{} "ok, scheduleCount, personalCount, message"
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /sync/calendar [post]
func (h *SyncHandler) SyncCalendar(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}

	client := google.SharedOAuthClient(h.cfg.GoogleClientID, h.cfg.GoogleClientSecret, h.cfg.GoogleRefreshToken)

	// Start sync asynchronously to prevent timeout, or synchronously if it's fast enough.
	// We'll do it synchronously for simplicity so frontend gets immediate response.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pendingProcessed, pendingErr := processPendingCalendar(ctx, client, store.NewPersonalEventStore(h.db), userID)
	if pendingErr != nil {
		slog.Warn("pending calendar sync failed; continuing with calendar pull", "error", pendingErr)
	}

	scheduleSync, err := google.SyncScheduleDetailed(ctx, client, userID, h.cfg.SDBCalendarID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("schedule sync: %w", err))
		return
	}
	diensten := scheduleSync.Diensten

	// Update schedule in db
	scheduleStore := store.NewScheduleStore(h.db)
	var scheduleImports []model.ScheduleImport
	for _, d := range diensten {
		scheduleImports = append(scheduleImports, model.ScheduleImport{
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
		})
	}
	scheduleUpserted := 0
	var scheduleWriteErr error
	if len(scheduleImports) > 0 {
		scheduleUpserted, scheduleWriteErr = scheduleStore.BulkUpsert(ctx, userID, scheduleImports)
		if scheduleWriteErr != nil {
			slog.Error("Failed to upsert schedule", "error", scheduleWriteErr)
		}
	}
	schedulePruned := 0
	var pruneErr error
	if scheduleWriteErr == nil {
		schedulePruned, pruneErr = scheduleStore.PruneMissingInDateRange(
			ctx,
			userID,
			scheduleSync.PruneStartDatum,
			scheduleSync.PruneEindDatum,
			scheduleSync.FetchedEventIDs,
		)
		if pruneErr != nil {
			slog.Warn("Failed to prune stale schedule rows", "error", pruneErr)
		}
	}
	if scheduleWriteErr == nil {
		_ = scheduleStore.UpsertMeta(ctx, userID, "Google Calendar Sync", len(scheduleImports))
		// Push the refreshed schedule to Todoist (best-effort) so a shift change
		// lands now instead of waiting for the daily todoist cron.
		if h.cfg.TodoistAPIToken != "" {
			if _, terr := h.pushTodoist(ctx, userID); terr != nil {
				slog.Warn("calendar sync: todoist push failed (non-fatal)", "error", terr)
			}
		}
	}

	calendarIDs := []string{"primary"}
	if h.cfg.PersonalCalendarIDs != "" {
		calendarIDs = google.SplitCalendarIDs(h.cfg.PersonalCalendarIDs)
	}

	personalSync, err := google.SyncPersonalEventsDetailed(ctx, client, userID, calendarIDs, h.cfg.SDBCalendarID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("personal event sync: %w", err))
		return
	}
	personalEvents := personalSync.Events

	// Update personal events in db
	peStore := store.NewPersonalEventStore(h.db)
	ptr := func(s string) *string {
		if s == "" {
			return nil
		}
		return &s
	}
	peEvents := make([]model.PersonalEvent, 0, len(personalEvents))
	for _, pe := range personalEvents {
		peEvents = append(peEvents, model.PersonalEvent{
			UserID:               userID,
			EventID:              pe.EventID,
			Titel:                pe.Titel,
			StartDatum:           pe.StartDatum,
			StartTijd:            ptr(pe.StartTijd),
			EindDatum:            pe.EindDatum,
			EindTijd:             ptr(pe.EindTijd),
			Heledag:              pe.Heledag,
			Locatie:              ptr(pe.Locatie),
			Beschrijving:         ptr(pe.Beschrijving),
			Symbol:               ptr(pe.Symbol),
			BusinessContextType:  ptr(pe.BusinessContextType),
			BusinessContextID:    ptr(pe.BusinessContextID),
			BusinessContextTitle: ptr(pe.BusinessContextTitle),
			Status:               pe.Status,
			Kalender:             pe.Kalender,
		})
	}

	// Batch upsert in one transaction; fall back to per-row so one bad row
	// doesn't block the rest. prune is skipped if any row ultimately failed.
	personalWriteFailed := false
	if _, bulkErr := peStore.BulkUpsertSynced(ctx, peEvents); bulkErr != nil {
		slog.Warn("personal events batch upsert failed, falling back to per-row", "error", bulkErr)
		for _, pe := range peEvents {
			if e := peStore.UpsertSynced(ctx, pe); e != nil {
				personalWriteFailed = true
				slog.Error("Failed to upsert personal event", "error", e)
			}
		}
	}
	personalPruned := 0
	var personalPruneErr error
	if !personalWriteFailed {
		personalPruned, personalPruneErr = peStore.MarkMissingSyncedInDateRange(
			ctx,
			userID,
			personalSync.PruneStartDatum,
			personalSync.PruneEindDatum,
			personalSync.FetchedEventIDs,
			personalSync.SyncedKalenders,
		)
		if personalPruneErr != nil {
			slog.Warn("Failed to mark stale personal events deleted", "error", personalPruneErr)
		}
	}

	result := map[string]any{
		"ok":               true,
		"scheduleCount":    len(diensten),
		"scheduleUpserted": scheduleUpserted,
		"schedulePruned":   schedulePruned,
		"personalCount":    len(personalEvents),
		"personalPruned":   personalPruned,
		"pendingProcessed": pendingProcessed,
		"message":          "Kalender sync voltooid",
	}
	if pendingErr != nil {
		result["pendingError"] = pendingErr.Error()
	}
	if scheduleWriteErr != nil {
		result["scheduleWriteError"] = scheduleWriteErr.Error()
	}
	if pruneErr != nil {
		result["schedulePruneError"] = pruneErr.Error()
	}
	if personalWriteFailed {
		result["personalWriteError"] = "Een of meer persoonlijke afspraken konden niet worden opgeslagen."
	}
	if personalPruneErr != nil {
		result["personalPruneError"] = personalPruneErr.Error()
	}
	JSON(w, http.StatusOK, result)
}

func processPendingCalendar(ctx context.Context, client *google.OAuthClient, peStore *store.PersonalEventStore, userID string) (int, error) {
	pending, err := peStore.ListPendingCalendar(ctx, userID, 50)
	if err != nil {
		return 0, err
	}

	processed := 0
	failed := 0
	failures := []string{}
	for _, event := range pending {
		if !isPendingCalendarStatus(event.Status) {
			continue
		}
		if err := processPendingCalendarEvent(ctx, client, peStore, event); err != nil {
			failed++
			failures = append(failures, fmt.Sprintf("%s: %v", event.EventID, err))
			slog.Warn("manual pending calendar operation failed", "eventId", event.EventID, "status", event.Status, "error", err)
			continue
		}
		processed++
	}
	if failed > 0 && processed == 0 {
		return processed, fmt.Errorf("%d pending calendar operation(s) failed: %s", failed, strings.Join(failures, "; "))
	}
	return processed, nil
}

func processPendingCalendarEvent(ctx context.Context, client *google.OAuthClient, peStore *store.PersonalEventStore, event model.PersonalEvent) error {
	calendarID, googleEventID := google.ResolveCalendarTarget(event)
	nextStatus := resolvedPersonalEventStatus(event)

	switch event.Status {
	case store.PersonalEventStatusPendingCreate:
		createdID, err := google.CreatePersonalEvent(ctx, client, calendarID, event)
		if err != nil {
			return err
		}
		return peStore.ReplaceEventIDAndStatus(ctx, event.UserID, event.EventID, google.StoredCalendarEventID(calendarID, createdID), nextStatus)
	case store.PersonalEventStatusPendingUpdate:
		if err := google.UpdatePersonalEvent(ctx, client, calendarID, googleEventID, event); err != nil {
			return err
		}
		return peStore.UpdateStatus(ctx, event.UserID, event.EventID, nextStatus)
	case store.PersonalEventStatusPendingDelete:
		if err := google.DeletePersonalEvent(ctx, client, calendarID, googleEventID); err != nil {
			return err
		}
		return peStore.UpdateStatus(ctx, event.UserID, event.EventID, store.PersonalEventStatusDeleted)
	default:
		return nil
	}
}

func isPendingCalendarStatus(status string) bool {
	switch status {
	case store.PersonalEventStatusPendingCreate, store.PersonalEventStatusPendingUpdate, store.PersonalEventStatusPendingDelete:
		return true
	default:
		return false
	}
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

	loc, locErr := time.LoadLocation("Europe/Amsterdam")
	if locErr != nil {
		loc = time.UTC
	}
	end, err := time.ParseInLocation("2006-01-02 15:04", endDate+" "+endClock, loc)
	if err != nil {
		return store.PersonalEventStatusUpcoming
	}
	if end.Before(time.Now().In(loc)) {
		return store.PersonalEventStatusPast
	}
	return store.PersonalEventStatusUpcoming
}

// SyncGmail triggers a manual sync of Gmail messages.
// @Summary Sync Gmail
// @Description Triggers a manual sync of Gmail messages
// @Tags Sync
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "userId required"
// @Router /sync/gmail [post]
// SyncTodoist re-pushes the current schedule to Todoist on demand (the cron is
// daily). Useful right after a shift-type correction so the tasks refresh now
// instead of waiting a day. Mirrors cronTodoistSync.
// @Summary Trigger Todoist sync
// @Tags Sync
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Success 200 {object} map[string]int
// @Router /sync/todoist [post]
func (h *SyncHandler) SyncTodoist(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	if h.cfg.TodoistAPIToken == "" {
		Error(w, http.StatusBadRequest, "Todoist niet geconfigureerd")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	res, err := h.pushTodoist(ctx, userID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("todoist sync: %w", err))
		return
	}
	JSON(w, http.StatusOK, map[string]int{"created": res.Created, "updated": res.Updated, "closed": res.Closed, "deleted": res.Deleted, "failed": res.Failed})
}

// ReconcileTodoist re-pushes the current (possibly empty) stored schedule to
// Todoist. Wired as the ScheduleHandler post-wipe cleanup hook: after "Rooster
// wissen" the schedule is empty, so SyncDiensten closes/deletes every lingering
// [EID:…] shift task. Returns nil when Todoist is not configured (no-op).
func (h *SyncHandler) ReconcileTodoist(ctx context.Context, userID string) error {
	if h.cfg.TodoistAPIToken == "" {
		return nil
	}
	_, err := h.pushTodoist(ctx, userID)
	return err
}

// pushTodoist re-pushes the stored schedule to Todoist, shared by SyncTodoist and
// a best-effort call from SyncCalendar so shift changes reach Todoist immediately.
func (h *SyncHandler) pushTodoist(ctx context.Context, userID string) (*todoist.SyncResult, error) {
	rows, err := store.NewScheduleStore(h.db).List(ctx, userID)
	if err != nil {
		return nil, err
	}
	diensten := make([]todoist.Dienst, 0, len(rows))
	for _, rd := range rows {
		diensten = append(diensten, todoist.Dienst{
			EventID: rd.EventID, Titel: rd.Titel, StartDatum: rd.StartDatum,
			StartTijd: rd.StartTijd, EindTijd: rd.EindTijd, Locatie: rd.Locatie,
			ShiftType: rd.ShiftType, Duur: rd.Duur, Heledag: rd.Heledag, Status: rd.Status,
		})
	}
	client := todoist.NewClient(h.cfg.TodoistAPIToken, h.cfg.TodoistProjectID)
	ctx = context.WithValue(ctx, "today", time.Now().Format("2006-01-02"))
	return client.SyncDiensten(ctx, diensten)
}

func (h *SyncHandler) SyncGmail(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}

	if !googleOAuthConfigured(h.cfg) {
		Error(w, http.StatusServiceUnavailable, "Google OAuth is niet geconfigureerd.")
		return
	}

	client := google.SharedOAuthClient(h.cfg.GoogleClientID, h.cfg.GoogleClientSecret, h.cfg.GoogleRefreshToken)
	emailStore := store.NewEmailStore(h.db)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	meta, err := emailStore.GetSyncMeta(ctx, userID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("gmail sync meta: %w", err))
		return
	}

	storedBefore, err := emailStore.Count(ctx, userID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("gmail count: %w", err))
		return
	}

	historyID := ""
	if meta != nil {
		historyID = meta.HistoryID
	}
	if meta != nil && storedBefore == 0 {
		historyID = ""
	}

	result, parsedEmails, newHistoryID, err := google.SyncGmail(ctx, client, userID, historyID)
	if err != nil {
		_ = emailStore.MarkSyncFailed(ctx, userID, err.Error())
		InternalError(w, r, fmt.Errorf("gmail sync: %w", err))
		return
	}

	upserted, err := storeParsedEmails(ctx, emailStore, parsedEmails)
	if err != nil {
		InternalError(w, r, fmt.Errorf("gmail store: %w", err))
		return
	}

	if newHistoryID == "" && meta != nil {
		newHistoryID = meta.HistoryID
	}

	totalSynced, err := emailStore.Count(ctx, userID)
	if err != nil {
		InternalError(w, r, fmt.Errorf("gmail count update: %w", err))
		return
	}

	var lastFullSync *time.Time
	if meta != nil {
		lastFullSync = meta.LastFullSync
	}
	if result.Mode == "full" {
		now := time.Now().UTC()
		lastFullSync = &now
	}

	if err := emailStore.UpsertSyncMeta(ctx, userID, newHistoryID, lastFullSync, totalSynced); err != nil {
		InternalError(w, r, fmt.Errorf("gmail sync meta update: %w", err))
		return
	}

	JSON(w, http.StatusOK, map[string]any{
		"ok":          true,
		"mode":        result.Mode,
		"synced":      result.Synced,
		"upserted":    upserted,
		"historyId":   newHistoryID,
		"totalSynced": totalSynced,
		"message":     "Gmail sync voltooid",
	})
}

// GetSyncStatus returns the status of various sync targets.
// @Summary Get Sync Status
// @Description Returns the status of calendar, personal, and gmail sync targets
// @Tags Sync
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /sync/status [get]
func (h *SyncHandler) GetSyncStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		userID = h.cfg.HomeappUserID
	}

	googleConfigured := googleOAuthConfigured(h.cfg)
	scheduleMeta, _ := store.NewScheduleStore(h.db).GetMeta(ctx, userID)
	emailMeta, _ := store.NewEmailStore(h.db).GetSyncMeta(ctx, userID)

	var personalTotal, pendingPersonal int
	var personalUpdated sql.NullTime
	_ = h.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE status IN ($2, $3, $4)),
		        MAX(created_at)
		   FROM personal_events
		  WHERE user_id = $1`,
		userID,
		store.PersonalEventStatusPendingCreate,
		store.PersonalEventStatusPendingUpdate,
		store.PersonalEventStatusPendingDelete,
	).Scan(&personalTotal, &pendingPersonal, &personalUpdated)

	var scheduleLastSuccess any
	var scheduleRows int
	if scheduleMeta != nil {
		scheduleLastSuccess = scheduleMeta.ImportedAt.Format(time.RFC3339)
		scheduleRows = scheduleMeta.TotalRows
	}

	var gmailLastSuccess any
	var gmailLastFull any
	var gmailTotal int
	var gmailHistoryID string
	gmailSyncStatus := "unknown"
	gmailLastError := ""
	if emailMeta != nil {
		gmailLastSuccess = emailMeta.UpdatedAt.Format(time.RFC3339)
		if emailMeta.LastFullSync != nil {
			gmailLastFull = emailMeta.LastFullSync.Format(time.RFC3339)
		}
		gmailTotal = emailMeta.TotalSynced
		gmailHistoryID = emailMeta.HistoryID
		if emailMeta.SyncStatus != "" {
			gmailSyncStatus = emailMeta.SyncStatus
		}
		gmailLastError = emailMeta.LastError
	}

	personalLastSuccess := sqlTimeRFC3339(personalUpdated)

	// Recent sync-run history (best-effort — never fail /sync/status over it).
	recentRuns, runsErr := store.NewSyncRunStore(h.db).Recent(r.Context(), 20)
	if runsErr != nil {
		slog.Warn("sync status: recent runs query failed", "error", runsErr)
		recentRuns = nil
	}

	JSON(w, http.StatusOK, map[string]any{
		"userId":     userID,
		"recentRuns": recentRuns,
		"schedule": map[string]any{
			"status":        syncSourceStatus(h.cfg.GoogleCalendarEnabled, googleConfigured, scheduleLastSuccess),
			"enabled":       h.cfg.GoogleCalendarEnabled,
			"configured":    googleConfigured,
			"lastSuccessAt": scheduleLastSuccess,
			"totalRows":     scheduleRows,
		},
		"personal": map[string]any{
			"status":        syncSourceStatus(h.cfg.GoogleCalendarEnabled, googleConfigured, personalLastSuccess),
			"enabled":       h.cfg.GoogleCalendarEnabled,
			"configured":    googleConfigured,
			"lastSuccessAt": personalLastSuccess,
			"total":         personalTotal,
			"pending":       pendingPersonal,
		},
		"gmail": map[string]any{
			"status":          syncSourceStatus(true, googleConfigured, gmailLastSuccess),
			"syncStatus":      gmailSyncStatus,
			"lastError":       gmailLastError,
			"enabled":         h.cfg.GmailEnabled,
			"autoEnabled":     h.cfg.GmailEnabled,
			"manualAvailable": googleConfigured,
			"configured":      googleConfigured,
			"lastSuccessAt":   gmailLastSuccess,
			"lastFullSync":    gmailLastFull,
			"totalSynced":     gmailTotal,
			"historyId":       gmailHistoryID,
		},
	})
}

func storeParsedEmails(ctx context.Context, emailStore *store.EmailStore, parsed []google.ParsedEmail) (int, error) {
	if len(parsed) == 0 {
		return 0, nil
	}

	modelEmails := make([]model.Email, len(parsed))
	for i, pe := range parsed {
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

	return emailStore.BulkUpsert(ctx, modelEmails)
}

func syncSourceStatus(enabled, configured bool, lastSuccess any) string {
	if !enabled {
		return "disabled"
	}
	if !configured {
		return "missing_config"
	}
	if lastSuccess == nil {
		return "pending"
	}
	return "success"
}

func sqlTimeRFC3339(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time.Format(time.RFC3339)
}
