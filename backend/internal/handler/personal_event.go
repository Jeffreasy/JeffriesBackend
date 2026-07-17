package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type PersonalEventHandler struct {
	store *store.PersonalEventStore
	cfg   *config.Config
}

func NewPersonalEventHandler(s *store.PersonalEventStore, cfg *config.Config) *PersonalEventHandler {
	return &PersonalEventHandler{store: s, cfg: cfg}
}

// List returns all personal events.
// @Summary List personal events
// @Description Returns all personal calendar events for the user
// @Tags Personal Events
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.PersonalEvent
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /personal-events [get]
func (h *PersonalEventHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := h.cfg.HomeappUserID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	// Optional from/to (YYYY-MM-DD) bound the result; default stays the full list.
	from, to, ranged, rerr := parseOptionalDateRange(r)
	if rerr != nil {
		Error(w, http.StatusBadRequest, rerr.Error())
		return
	}
	var events []model.PersonalEvent
	var err error
	if ranged {
		events, err = h.store.ListRange(r.Context(), userID, from, to)
	} else {
		events, err = h.store.List(r.Context(), userID)
	}
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, events)
}

// ListByDate returns events for a specific date.
// @Summary List events by date
// @Description Returns personal events for a specific date
// @Tags Personal Events
// @Produce json
// @Param userId query string true "User ID"
// @Param date path string true "Date (YYYY-MM-DD)"
// @Success 200 {array} model.PersonalEvent
// @Failure 400 {string} string "userId en date verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /personal-events/date/{date} [get]
func (h *PersonalEventHandler) ListByDate(w http.ResponseWriter, r *http.Request) {
	userID := h.cfg.HomeappUserID
	date := chi.URLParam(r, "date")
	if userID == "" || date == "" {
		Error(w, http.StatusBadRequest, "userId en date verplicht")
		return
	}
	events, err := h.store.ListByDate(r.Context(), userID, date)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, events)
}

// ListUpcoming returns upcoming events.
// @Summary List upcoming events
// @Description Returns the next 50 upcoming personal events
// @Tags Personal Events
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.PersonalEvent
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /personal-events/upcoming [get]
func (h *PersonalEventHandler) ListUpcoming(w http.ResponseWriter, r *http.Request) {
	userID := h.cfg.HomeappUserID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	events, err := h.store.ListUpcoming(r.Context(), userID, 50)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, events)
}

// Upsert adds or updates an event.
// @Summary Upsert personal event
// @Description Adds or updates a personal event from calendar sync
// @Tags Personal Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.PersonalEvent true "Event Details"
// @Success 200 {object} map[string]bool "ok: true"
// @Failure 400 {string} string "Ongeldige JSON of ontbrekende velden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /personal-events [post]
func (h *PersonalEventHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	var e model.PersonalEvent
	if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if e.EventID == "" {
		Error(w, http.StatusBadRequest, "event_id verplicht")
		return
	}
	e.UserID = h.cfg.HomeappUserID
	if e.StartDatum != "" && e.EindDatum != "" && e.EindDatum < e.StartDatum {
		Error(w, http.StatusBadRequest, "einddatum ligt vóór startdatum")
		return
	}
	if !e.Heledag && e.StartDatum == e.EindDatum && e.StartTijd != nil && e.EindTijd != nil &&
		*e.StartTijd != "" && *e.EindTijd != "" && *e.EindTijd < *e.StartTijd {
		Error(w, http.StatusBadRequest, "eindtijd ligt vóór starttijd")
		return
	}
	if err := h.store.Upsert(r.Context(), e); err != nil {
		InternalError(w, r, err)
		return
	}
	result := map[string]any{"ok": true}
	if isPendingCalendarStatus(e.Status) {
		for key, value := range h.tryProcessPendingCalendarEventNow(r.Context(), e.UserID, e.EventID) {
			result[key] = value
		}
	}
	JSON(w, http.StatusOK, result)
}

// UpdateStatus updates the event status.
// @Summary Update event status
// @Description Updates the internal status of a personal event
// @Tags Personal Events
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param eventID path string true "Event ID"
// @Param request body map[string]string true "Status Details"
// @Success 200 {object} map[string]bool "ok: true"
// @Failure 400 {string} string "status verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /personal-events/{eventID}/status [patch]
func (h *PersonalEventHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	userID := h.cfg.HomeappUserID
	eventID := chi.URLParam(r, "eventID")
	var body struct {
		Status string `json:"status"`
	}
	if userID == "" || eventID == "" {
		Error(w, http.StatusBadRequest, "userId en eventID verplicht")
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		// A malformed/oversized body is a decode problem, not a missing status —
		// don't report every decode failure as "status verplicht".
		RespondDecodeError(w, err)
		return
	}
	if body.Status == "" {
		Error(w, http.StatusBadRequest, "status verplicht")
		return
	}
	if err := h.store.UpdateStatus(r.Context(), userID, eventID, body.Status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Afspraak niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	result := map[string]any{"ok": true}
	if isPendingCalendarStatus(body.Status) {
		for key, value := range h.tryProcessPendingCalendarEventNow(r.Context(), userID, eventID) {
			result[key] = value
		}
	}
	JSON(w, http.StatusOK, result)
}

func (h *PersonalEventHandler) tryProcessPendingCalendarEventNow(parent context.Context, userID, eventID string) map[string]any {
	result := map[string]any{
		"instantSync": false,
		"pending":     true,
	}
	if h.cfg == nil || h.cfg.GoogleClientID == "" || h.cfg.GoogleClientSecret == "" || h.cfg.GoogleRefreshToken == "" {
		result["syncMessage"] = "Google Calendar sync is niet geconfigureerd; actie blijft in wachtrij."
		return result
	}

	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()

	event, err := h.store.GetByUserEventID(ctx, userID, eventID)
	if err != nil {
		// Raw store/Google errors never reach the client (N12) — the queue-based
		// cron retries the pending action regardless.
		slog.Warn("instant calendar sync: pending event fetch failed", "userId", userID, "eventId", eventID, "error", err)
		result["syncError"] = "Google Calendar-sync mislukt — actie staat in de wachtrij."
		return result
	}
	if !isPendingCalendarStatus(event.Status) {
		result["pending"] = false
		result["syncMessage"] = "Geen pending Google Calendar actie gevonden."
		return result
	}

	client := google.SharedOAuthClient(h.cfg.GoogleClientID, h.cfg.GoogleClientSecret, h.cfg.GoogleRefreshToken)
	if err := processPendingCalendarEvent(ctx, client, h.store, event); err != nil {
		slog.Warn("instant calendar sync failed", "userId", userID, "eventId", eventID, "error", err)
		result["syncError"] = "Google Calendar-sync mislukt — actie staat in de wachtrij."
		// Tell the caller whether this can ever succeed on retry, so the UI can
		// give an honest message instead of a reassuring "blijft in de wachtrij"
		// for something that is guaranteed to fail identically every time (e.g.
		// editing a Google-generated birthday event).
		result["permanent"] = google.IsPermanentCalendarError(err)
		return result
	}

	result["pendingProcessed"] = 1
	result["instantSync"] = true
	result["pending"] = false
	return result
}
