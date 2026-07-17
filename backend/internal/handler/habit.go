package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type HabitHandler struct {
	store  *store.HabitStore
	userID string
}

func NewHabitHandler(s *store.HabitStore, userID string) *HabitHandler {
	return &HabitHandler{store: s, userID: userID}
}

var amsterdamLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// todayAmsterdam returns today's date (YYYY-MM-DD) in Europe/Amsterdam, matching
// the store layer — so a completion logged late at night (00:00–02:00 CET/CEST on
// a UTC server) isn't stamped on the previous calendar day and breaking the streak.
func todayAmsterdam() string {
	return time.Now().In(amsterdamLoc).Format("2006-01-02")
}

// List returns all active habits for a user.
// @Summary List habits
// @Description Returns all active habits for the user
// @Tags Habits
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.Habit
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits [get]
func (h *HabitHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	habits, err := h.store.List(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, habits)
}

// Get returns a single habit.
// @Summary Get a habit
// @Description Returns a single habit by its ID
// @Tags Habits
// @Produce json
// @Param id path string true "Habit ID (UUID)"
// @Success 200 {object} model.Habit
// @Failure 400 {string} string "invalid id"
// @Failure 404 {string} string "habit not found"
// @Router /habits/{id} [get]
func (h *HabitHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	habit, err := h.store.Get(r.Context(), h.userID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden.")
			return
		}
		// A DB timeout is not "niet gevonden" — surface it as a real server error.
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, habit)
}

// guardHabitLoggable answers 404/409/500 (and returns false) when a habit can't
// accept new logs: missing, archived or paused (L1).
func (h *HabitHandler) guardHabitLoggable(w http.ResponseWriter, r *http.Request, id uuid.UUID) (model.Habit, bool) {
	habit, err := h.store.Get(r.Context(), h.userID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return model.Habit{}, false
		}
		InternalError(w, r, err)
		return model.Habit{}, false
	}
	if !habit.IsActief {
		Error(w, http.StatusConflict, "Deze habit is gearchiveerd.")
		return model.Habit{}, false
	}
	if habit.IsPauze {
		Error(w, http.StatusConflict, "Deze habit is gepauzeerd.")
		return model.Habit{}, false
	}
	return habit, true
}

// Create adds a new habit.
// @Summary Create a habit
// @Description Creates a new habit for the user
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param request body model.Habit true "Habit Details"
// @Success 201 {object} model.Habit
// @Failure 400 {string} string "invalid JSON or missing userId"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits [post]
func (h *HabitHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	var habit model.Habit
	if err := json.NewDecoder(r.Body).Decode(&habit); err != nil {
		RespondDecodeError(w, err)
		return
	}
	created, err := h.store.Create(r.Context(), userID, habit)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, created)
}

// Update patches a habit.
// @Summary Update a habit
// @Description Updates the details of an existing habit
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Param request body map[string]interface{} true "Updated Habit Fields"
// @Success 200 {object} model.Habit
// @Failure 400 {string} string "invalid JSON or ID"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id} [patch]
func (h *HabitHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if len(body) == 0 {
		Error(w, http.StatusBadRequest, "Geen velden om bij te werken.")
		return
	}
	updated, err := h.store.Update(r.Context(), h.userID, id, body)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, updated)
}

// Archive soft-deletes a habit.
// @Summary Archive a habit
// @Description Soft-deletes a habit so it no longer appears in active views
// @Tags Habits
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Success 200 {object} map[string]string "status archived"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/archive [post]
func (h *HabitHandler) Archive(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.Archive(r.Context(), h.userID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "archived"})
}

// Delete permanently removes a habit.
// @Summary Delete a habit
// @Description Permanently deletes a habit
// @Tags Habits
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Success 200 {object} map[string]string "status deleted"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id} [delete]
func (h *HabitHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.Delete(r.Context(), h.userID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// TogglePause flips the pause state.
// @Summary Toggle pause habit
// @Description Pauses or unpauses an active habit
// @Tags Habits
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Success 200 {object} map[string]string "status toggled"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/pause [post]
func (h *HabitHandler) TogglePause(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.TogglePause(r.Context(), h.userID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "toggled"})
}

// Reorder updates the order of multiple habits.
// @Summary Reorder habits
// @Description Bulk updates the sorting order of multiple habits
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]interface{} true "Reorder Items"
// @Success 200 {object} map[string]string "status reordered"
// @Failure 400 {string} string "invalid JSON"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/reorder [post]
func (h *HabitHandler) Reorder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Items []struct {
			ID       uuid.UUID `json:"id"`
			Volgorde int       `json:"volgorde"`
		} `json:"items"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if err := h.store.Reorder(r.Context(), h.userID, body.Items); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "reordered"})
}

// ─── Logs ────────────────────────────────────────────────────────────────────

// Toggle creates or toggles a habit log for a given date.
// @Summary Log habit completion
// @Description Creates or toggles a daily completion log for a habit
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Param userId query string true "User ID"
// @Param request body map[string]interface{} true "Log details (datum, waarde, notitie)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "invalid JSON or id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/toggle [post]
func (h *HabitHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	habitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig habit-id.")
		return
	}
	var body struct {
		Datum    string   `json:"datum"`
		Waarde   *float64 `json:"waarde"`
		Notitie  *string  `json:"notitie"`
		Voltooid *bool    `json:"voltooid"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	datum := body.Datum
	if datum == "" {
		datum = todayAmsterdam()
	}

	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	if _, ok := h.guardHabitLoggable(w, r, habitID); !ok {
		return
	}
	// Optional explicit voltooid: absent = true (backward compatible),
	// false = echte untoggle ("Heropenen") — voltooid=false, xp 0 for that day.
	voltooid := true
	if body.Voltooid != nil {
		voltooid = *body.Voltooid
	}
	log := model.HabitLog{
		UserID:   userID,
		HabitID:  habitID,
		Datum:    datum,
		Voltooid: voltooid,
		Waarde:   body.Waarde,
		Notitie:  body.Notitie,
		Bron:     "web",
	}
	result, err := h.store.UpsertLog(r.Context(), log)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, result)
}

// incidentBackfillDays caps how far back an incident may be logged.
const incidentBackfillDays = 30

// parseIncidentDatum validates an optional incident date: parseable YYYY-MM-DD,
// not in the future (Amsterdam time) and — when maxAgeDays > 0 — at most that
// many days back. An empty value defaults to today (Amsterdam).
func parseIncidentDatum(raw string, maxAgeDays int) (string, error) {
	raw = strings.TrimSpace(raw)
	today := todayAmsterdam()
	if raw == "" {
		return today, nil
	}
	parsed, err := time.ParseInLocation("2006-01-02", raw, amsterdamLoc)
	if err != nil {
		return "", errors.New("Ongeldige datum (verwacht YYYY-MM-DD).")
	}
	datum := parsed.Format("2006-01-02")
	if datum > today {
		return "", errors.New("Datum mag niet in de toekomst liggen.")
	}
	if maxAgeDays > 0 {
		todayParsed, perr := time.ParseInLocation("2006-01-02", today, amsterdamLoc)
		if perr == nil {
			floor := todayParsed.AddDate(0, 0, -maxAgeDays).Format("2006-01-02")
			if datum < floor {
				return "", fmt.Errorf("Datum mag maximaal %d dagen terug liggen.", maxAgeDays)
			}
		}
	}
	return datum, nil
}

// Incident logs a negative habit incident.
// @Summary Log habit incident
// @Description Logs a negative incident for a habit, optionally on a past date (max 30 days back)
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Param userId query string true "User ID"
// @Param request body map[string]interface{} true "Incident details (trigger, notitie, datum YYYY-MM-DD)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "invalid JSON, id or datum"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/incident [post]
func (h *HabitHandler) Incident(w http.ResponseWriter, r *http.Request) {
	habitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig habit-id.")
		return
	}
	var body struct {
		Trigger *string `json:"trigger"`
		Notitie *string `json:"notitie"`
		Datum   string  `json:"datum"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}

	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	datum, derr := parseIncidentDatum(body.Datum, incidentBackfillDays)
	if derr != nil {
		Error(w, http.StatusBadRequest, derr.Error())
		return
	}
	if _, ok := h.guardHabitLoggable(w, r, habitID); !ok {
		return
	}
	// A second incident on the same day would silently overwrite the first
	// trigger/notitie — refuse it so the UI can tell the user what's going on.
	if existing, gerr := h.store.GetLog(r.Context(), h.userID, habitID, datum); gerr == nil && existing.IsIncident {
		Error(w, http.StatusConflict, "Er is al een incident gelogd op deze dag.")
		return
	} else if gerr != nil && !errors.Is(gerr, pgx.ErrNoRows) {
		InternalError(w, r, gerr)
		return
	}
	log := model.HabitLog{
		UserID:     userID,
		HabitID:    habitID,
		Datum:      datum,
		IsIncident: true,
		TriggerCat: body.Trigger,
		Notitie:    body.Notitie,
		Bron:       "web",
	}
	// UpsertIncident preserves an existing completion on the same day (only the
	// incident fields are written) — see store.UpsertIncident (R2).
	result, err := h.store.UpsertIncident(r.Context(), log)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Habit niet gevonden — mogelijk al verwijderd.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, result)
}

// DeleteIncident removes the incident log for a habit on a specific date and
// recalculates streaks/badges — the undo path for a mis-tapped incident.
// @Summary Delete habit incident
// @Description Removes the incident log for a habit on a date (default today, Amsterdam)
// @Tags Habits
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Param datum query string false "Date (YYYY-MM-DD, default today)"
// @Success 204 "No Content"
// @Failure 400 {string} string "invalid id or datum"
// @Failure 404 {string} string "no incident logged on that date"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/incident [delete]
func (h *HabitHandler) DeleteIncident(w http.ResponseWriter, r *http.Request) {
	habitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig habit-id.")
		return
	}
	// No backfill floor on deletion: removing an old mis-logged incident is safe.
	datum, derr := parseIncidentDatum(r.URL.Query().Get("datum"), 0)
	if derr != nil {
		Error(w, http.StatusBadRequest, derr.Error())
		return
	}
	if err := h.store.DeleteIncidentLog(r.Context(), h.userID, habitID, datum); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Geen incident gevonden op "+datum+".")
			return
		}
		InternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─── Stats & Heatmap ─────────────────────────────────────────────────────────

// Stats returns aggregate stats.
// @Summary Get habit stats
// @Description Returns aggregated statistics across all habits
// @Tags Habits
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/stats [get]
func (h *HabitHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	stats, err := h.store.Stats(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, stats)
}

// Heatmap returns daily completion data.
// @Summary Get habit heatmap
// @Description Returns daily completion percentages for the last year
// @Tags Habits
// @Produce json
// @Param userId query string true "User ID"
// @Param days query int false "Number of days" default(365)
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/heatmap [get]
func (h *HabitHandler) Heatmap(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	days := 365
	if d := r.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}
	data, err := h.store.HeatmapData(r.Context(), userID, days)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"days": data})
}

// Badges returns all badges.
// @Summary Get user badges
// @Description Returns earned achievements and badges
// @Tags Habits
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.HabitBadge
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/badges [get]
func (h *HabitHandler) Badges(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	badges, err := h.store.ListBadges(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, badges)
}

// ForDate returns habits with their log entry for a given date.
// @Summary Get habits for date
// @Description Returns all active habits combined with their log status for a specific date
// @Tags Habits
// @Produce json
// @Param userId query string true "User ID"
// @Param datum query string false "Date (YYYY-MM-DD)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/for-date [get]
func (h *HabitHandler) ForDate(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	datum := r.URL.Query().Get("datum")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	if datum == "" {
		datum = todayAmsterdam()
	}

	habits, err := h.store.ListDueForDate(r.Context(), userID, datum)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	logs, err := h.store.ListLogsForDate(r.Context(), userID, datum)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	logMap := make(map[uuid.UUID]model.HabitLog)
	for _, l := range logs {
		logMap[l.HabitID] = l
	}

	// Period completion counts for x_per_week/x_per_maand habits: "2/3 deze
	// week". One range query covers both the ISO week and the month around datum.
	periodCounts, perr := h.periodCompletionCounts(r, userID, datum, habits)
	if perr != nil {
		InternalError(w, r, perr)
		return
	}

	type habitWithLog struct {
		model.Habit
		Log *model.HabitLog `json:"log"`
		// PeriodVoltooidCount is only set for x_per_week/x_per_maand habits: the
		// number of completions in the ISO week / month containing datum
		// (Amsterdam calendar). The frontend treats the habit as satisfied when
		// this reaches doel_aantal.
		PeriodVoltooidCount *int `json:"period_voltooid_count,omitempty"`
	}
	result := make([]habitWithLog, 0, len(habits))
	for _, hab := range habits {
		entry := habitWithLog{Habit: hab}
		if l, ok := logMap[hab.ID]; ok {
			entry.Log = &l
		}
		if count, ok := periodCounts[hab.ID]; ok {
			c := count
			entry.PeriodVoltooidCount = &c
		}
		result = append(result, entry)
	}

	JSON(w, http.StatusOK, map[string]any{
		"datum":  datum,
		"habits": result,
	})
}

// periodCompletionCounts counts voltooid-logs in the ISO week (x_per_week) or
// month (x_per_maand) containing datum, per habit. Returns an empty map when no
// habit needs it.
func (h *HabitHandler) periodCompletionCounts(r *http.Request, userID, datum string, habits []model.Habit) (map[uuid.UUID]int, error) {
	counts := map[uuid.UUID]int{}
	needsWeek, needsMonth := false, false
	for _, hab := range habits {
		switch hab.Frequentie {
		case "x_per_week":
			needsWeek = true
		case "x_per_maand":
			needsMonth = true
		}
	}
	if !needsWeek && !needsMonth {
		return counts, nil
	}
	weekStart, weekEnd, err := store.PeriodBoundsForDate(datum, true)
	if err != nil {
		return nil, err
	}
	monthStart, monthEnd, err := store.PeriodBoundsForDate(datum, false)
	if err != nil {
		return nil, err
	}
	rangeStart, rangeEnd := weekStart, weekEnd
	if monthStart < rangeStart {
		rangeStart = monthStart
	}
	if monthEnd > rangeEnd {
		rangeEnd = monthEnd
	}
	logs, err := h.store.ListLogsRange(r.Context(), userID, rangeStart, rangeEnd)
	if err != nil {
		return nil, err
	}
	freq := make(map[uuid.UUID]string, len(habits))
	for _, hab := range habits {
		if hab.Frequentie == "x_per_week" || hab.Frequentie == "x_per_maand" {
			freq[hab.ID] = hab.Frequentie
			counts[hab.ID] = 0
		}
	}
	for _, l := range logs {
		f, ok := freq[l.HabitID]
		if !ok || !l.Voltooid {
			continue
		}
		switch f {
		case "x_per_week":
			if l.Datum >= weekStart && l.Datum <= weekEnd {
				counts[l.HabitID]++
			}
		case "x_per_maand":
			if l.Datum >= monthStart && l.Datum <= monthEnd {
				counts[l.HabitID]++
			}
		}
	}
	return counts, nil
}
