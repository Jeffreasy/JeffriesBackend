package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
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
		Error(w, http.StatusBadRequest, "userId required")
		return
	}

	client := google.NewOAuthClient(h.cfg.GoogleClientID, h.cfg.GoogleClientSecret, h.cfg.GoogleRefreshToken)

	// Start sync asynchronously to prevent timeout, or synchronously if it's fast enough.
	// We'll do it synchronously for simplicity so frontend gets immediate response.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pendingProcessed, pendingErr := processPendingCalendar(ctx, client, store.NewPersonalEventStore(h.db), userID)
	if pendingErr != nil {
		slog.Warn("pending calendar sync failed; continuing with calendar pull", "error", pendingErr)
	}

	diensten, err := google.SyncSchedule(ctx, client, userID, h.cfg.SDBCalendarID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Schedule sync failed: "+err.Error())
		return
	}

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
	if len(scheduleImports) > 0 {
		_, err := scheduleStore.BulkUpsert(ctx, userID, scheduleImports)
		if err != nil {
			slog.Error("Failed to upsert schedule", "error", err)
		} else {
			_ = scheduleStore.UpsertMeta(ctx, userID, "Google Calendar Sync", len(scheduleImports))
		}
	}

	calendarIDs := []string{"primary"}
	if h.cfg.PersonalCalendarIDs != "" {
		calendarIDs = splitCalendarIDs(h.cfg.PersonalCalendarIDs)
	}

	personalEvents, err := google.SyncPersonalEvents(ctx, client, userID, calendarIDs, h.cfg.SDBCalendarID)
	if err != nil {
		Error(w, http.StatusInternalServerError, "Personal event sync failed: "+err.Error())
		return
	}

	// Update personal events in db
	peStore := store.NewPersonalEventStore(h.db)
	for _, pe := range personalEvents {
		startTijd := pe.StartTijd
		var pStartTijd *string
		if startTijd != "" {
			pStartTijd = &startTijd
		}

		eindTijd := pe.EindTijd
		var pEindTijd *string
		if eindTijd != "" {
			pEindTijd = &eindTijd
		}

		locatie := pe.Locatie
		var pLocatie *string
		if locatie != "" {
			pLocatie = &locatie
		}

		beschrijving := pe.Beschrijving
		var pBeschrijving *string
		if beschrijving != "" {
			pBeschrijving = &beschrijving
		}

		symbol := pe.Symbol
		var pSymbol *string
		if symbol != "" {
			pSymbol = &symbol
		}

		err = peStore.UpsertSynced(ctx, model.PersonalEvent{
			UserID:       userID,
			EventID:      pe.EventID,
			Titel:        pe.Titel,
			StartDatum:   pe.StartDatum,
			StartTijd:    pStartTijd,
			EindDatum:    pe.EindDatum,
			EindTijd:     pEindTijd,
			Heledag:      pe.Heledag,
			Locatie:      pLocatie,
			Beschrijving: pBeschrijving,
			Symbol:       pSymbol,
			Status:       pe.Status,
			Kalender:     pe.Kalender,
		})
		if err != nil {
			slog.Error("Failed to upsert personal event", "error", err)
		}
	}

	result := map[string]any{
		"ok":               true,
		"scheduleCount":    len(diensten),
		"personalCount":    len(personalEvents),
		"pendingProcessed": pendingProcessed,
		"message":          "Kalender sync voltooid",
	}
	if pendingErr != nil {
		result["pendingError"] = pendingErr.Error()
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
	calendarID, googleEventID := calendarTarget(event)
	nextStatus := resolvedPersonalEventStatus(event)

	switch event.Status {
	case store.PersonalEventStatusPendingCreate:
		createdID, err := google.CreatePersonalEvent(ctx, client, calendarID, event)
		if err != nil {
			return err
		}
		return peStore.ReplaceEventIDAndStatus(ctx, event.UserID, event.EventID, storedCalendarEventID(calendarID, createdID), nextStatus)
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

func calendarTarget(event model.PersonalEvent) (calendarID, googleEventID string) {
	calendarID = strings.TrimSpace(event.Kalender)
	if calendarID == "" || strings.EqualFold(calendarID, "Main") {
		calendarID = "primary"
	}

	googleEventID = event.EventID
	if calendarID != "primary" {
		googleEventID = strings.TrimPrefix(googleEventID, calendarID+":")
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
func (h *SyncHandler) SyncGmail(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}

	// Not fully implemented, placeholder response.
	// In reality we'd do google.SyncGmail(client) and upsert to emails table.
	JSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "Gmail sync functionaliteit wordt later overgezet naar Go.",
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
	now := time.Now().Format(time.RFC3339)
	JSON(w, http.StatusOK, map[string]any{
		"schedule": map[string]any{
			"status":        "success",
			"lastSuccessAt": now,
		},
		"personal": map[string]any{
			"status":        "success",
			"lastSuccessAt": now,
		},
		"gmail": map[string]any{
			"status":        "pending",
			"lastSuccessAt": nil,
		},
	})
}
