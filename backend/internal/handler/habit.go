package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type HabitHandler struct{ store *store.HabitStore }

func NewHabitHandler(s *store.HabitStore) *HabitHandler { return &HabitHandler{store: s} }

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
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	habits, err := h.store.List(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	habit, err := h.store.Get(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "habit not found")
		return
	}
	JSON(w, http.StatusOK, habit)
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
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	var habit model.Habit
	if err := json.NewDecoder(r.Body).Decode(&habit); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	created, err := h.store.Create(r.Context(), userID, habit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if len(body) == 0 {
		Error(w, http.StatusBadRequest, "no fields to update")
		return
	}
	updated, err := h.store.Update(r.Context(), id, body)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.Archive(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.Delete(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.TogglePause(r.Context(), id); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := h.store.Reorder(r.Context(), body.Items); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
		Error(w, http.StatusBadRequest, "invalid habit id")
		return
	}
	var body struct {
		Datum   string   `json:"datum"`
		Waarde  *float64 `json:"waarde"`
		Notitie *string  `json:"notitie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	datum := body.Datum
	if datum == "" {
		datum = time.Now().Format("2006-01-02")
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	log := model.HabitLog{
		UserID:   userID,
		HabitID:  habitID,
		Datum:    datum,
		Voltooid: true,
		Waarde:   body.Waarde,
		Notitie:  body.Notitie,
		Bron:     "web",
	}
	result, err := h.store.UpsertLog(r.Context(), log)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, result)
}

// Incident logs a negative habit incident.
// @Summary Log habit incident
// @Description Logs a negative incident for a habit
// @Tags Habits
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Habit ID (UUID)"
// @Param userId query string true "User ID"
// @Param request body map[string]interface{} true "Incident details (trigger, notitie)"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "invalid JSON or id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /habits/{id}/incident [post]
func (h *HabitHandler) Incident(w http.ResponseWriter, r *http.Request) {
	habitID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid habit id")
		return
	}
	var body struct {
		Trigger *string `json:"trigger"`
		Notitie *string `json:"notitie"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	log := model.HabitLog{
		UserID:     userID,
		HabitID:    habitID,
		Datum:      time.Now().Format("2006-01-02"),
		IsIncident: true,
		TriggerCat: body.Trigger,
		Notitie:    body.Notitie,
		Bron:       "web",
	}
	result, err := h.store.UpsertLog(r.Context(), log)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, result)
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
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	stats, err := h.store.Stats(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
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
		Error(w, http.StatusInternalServerError, err.Error())
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
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	badges, err := h.store.ListBadges(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
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
	userID := r.URL.Query().Get("userId")
	datum := r.URL.Query().Get("datum")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	if datum == "" {
		datum = time.Now().Format("2006-01-02")
	}

	habits, err := h.store.ListDueForDate(r.Context(), userID, datum)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	logs, err := h.store.ListLogsForDate(r.Context(), userID, datum)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	logMap := make(map[uuid.UUID]model.HabitLog)
	for _, l := range logs {
		logMap[l.HabitID] = l
	}

	type habitWithLog struct {
		model.Habit
		Log *model.HabitLog `json:"log"`
	}
	result := make([]habitWithLog, 0, len(habits))
	for _, hab := range habits {
		entry := habitWithLog{Habit: hab}
		if l, ok := logMap[hab.ID]; ok {
			entry.Log = &l
		}
		result = append(result, entry)
	}

	JSON(w, http.StatusOK, map[string]any{
		"datum":  datum,
		"habits": result,
	})
}
