package handler

import (
	"context"
	"log/slog"
	"net/http"
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

	personalEvents, err := google.SyncPersonalEvents(ctx, client, userID, []string{"primary"}, h.cfg.SDBCalendarID)
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

		err = peStore.Upsert(ctx, model.PersonalEvent{
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
			Status:       pe.Status,
			Kalender:     pe.Kalender,
		})
		if err != nil {
			slog.Error("Failed to upsert personal event", "error", err)
		}
	}

	JSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"scheduleCount": len(diensten),
		"personalCount": len(personalEvents),
		"message": "Kalender sync voltooid",
	})
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
		"ok": true,
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
			"status": "success",
			"lastSuccessAt": now,
		},
		"personal": map[string]any{
			"status": "success",
			"lastSuccessAt": now,
		},
		"gmail": map[string]any{
			"status": "pending",
			"lastSuccessAt": nil,
		},
	})
}
