package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type ScheduleHandler struct {
	store *store.ScheduleStore
}

func NewScheduleHandler(s *store.ScheduleStore) *ScheduleHandler {
	return &ScheduleHandler{store: s}
}

// List returns all diensten for the authenticated user.
// @Summary List all schedules
// @Description Returns all schedule events for the user
// @Tags Schedule
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.Schedule
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /schedule [get]
func (h *ScheduleHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	diensten, err := h.store.List(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, diensten)
}

// ListByDate returns diensten for a specific date.
// @Summary List schedules by date
// @Description Returns schedule events for the user on a specific date
// @Tags Schedule
// @Produce json
// @Param date path string true "Date (YYYY-MM-DD)"
// @Param userId query string true "User ID"
// @Success 200 {array} model.Schedule
// @Failure 400 {string} string "userId en date verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /schedule/date/{date} [get]
func (h *ScheduleHandler) ListByDate(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	date := chi.URLParam(r, "date")
	if userID == "" || date == "" {
		Error(w, http.StatusBadRequest, "userId en date verplicht")
		return
	}
	diensten, err := h.store.ListByDate(r.Context(), userID, date)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, diensten)
}

// Import bulk upserts diensten from the frontend or calendar sync.
// @Summary Import schedule data
// @Description Bulk upserts schedule items from an external source or frontend
// @Tags Schedule
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]interface{} true "Import payload containing UserID, FileName, and Rows"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "Ongeldige JSON of ontbrekende velden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /schedule/import [post]
func (h *ScheduleHandler) Import(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID   string                 `json:"userId"`
		FileName string                 `json:"fileName"`
		Rows     []model.ScheduleImport `json:"rows"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "Ongeldige JSON")
		return
	}
	if body.UserID == "" || len(body.Rows) == 0 {
		Error(w, http.StatusBadRequest, "userId en rows verplicht")
		return
	}

	count, err := h.store.BulkUpsert(r.Context(), body.UserID, body.Rows)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	_ = h.store.UpsertMeta(r.Context(), body.UserID, body.FileName, len(body.Rows))

	JSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"imported": count,
		"total":    len(body.Rows),
	})
}

// GetMeta returns import metadata.
// @Summary Get schedule metadata
// @Description Returns the latest sync metadata for the schedule
// @Tags Schedule
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {object} model.ScheduleMeta
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /schedule/meta [get]
func (h *ScheduleHandler) GetMeta(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	meta, err := h.store.GetMeta(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meta == nil {
		JSON(w, http.StatusOK, map[string]any{"imported": false})
		return
	}
	JSON(w, http.StatusOK, meta)
}
