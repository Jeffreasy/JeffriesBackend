package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

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

// parseOptionalDateRange reads optional from/to (YYYY-MM-DD) query params.
// Missing bounds get open defaults; both empty means "no range requested".
func parseOptionalDateRange(r *http.Request) (from, to string, ranged bool, err error) {
	from = strings.TrimSpace(r.URL.Query().Get("from"))
	to = strings.TrimSpace(r.URL.Query().Get("to"))
	if from == "" && to == "" {
		return "", "", false, nil
	}
	for _, v := range []string{from, to} {
		if v == "" {
			continue
		}
		if _, perr := time.Parse("2006-01-02", v); perr != nil {
			return "", "", false, errors.New("Ongeldige from/to-datum (verwacht YYYY-MM-DD).")
		}
	}
	if from == "" {
		from = "0001-01-01"
	}
	if to == "" {
		to = "9999-12-31"
	}
	if to < from {
		return "", "", false, errors.New("to-datum ligt vóór from-datum.")
	}
	return from, to, true, nil
}

// List returns diensten for the authenticated user. Optional from/to query
// params (YYYY-MM-DD) restrict the range; without them the full list is
// returned (backward-compatible).
// @Summary List all schedules
// @Description Returns schedule events for the user, optionally bounded by from/to (YYYY-MM-DD)
// @Tags Schedule
// @Produce json
// @Param userId query string true "User ID"
// @Param from query string false "Start date (YYYY-MM-DD)"
// @Param to query string false "End date (YYYY-MM-DD)"
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
	from, to, ranged, rerr := parseOptionalDateRange(r)
	if rerr != nil {
		Error(w, http.StatusBadRequest, rerr.Error())
		return
	}
	var diensten []model.Schedule
	var err error
	if ranged {
		diensten, err = h.store.ListRange(r.Context(), userID, from, to)
	} else {
		diensten, err = h.store.List(r.Context(), userID)
	}
	if err != nil {
		InternalError(w, r, err)
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
		InternalError(w, r, err)
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
		RespondDecodeError(w, err)
		return
	}
	if body.UserID == "" || len(body.Rows) == 0 {
		Error(w, http.StatusBadRequest, "userId en rows verplicht")
		return
	}

	count, err := h.store.BulkUpsert(r.Context(), body.UserID, body.Rows)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	_ = h.store.UpsertMeta(r.Context(), body.UserID, body.FileName, len(body.Rows))

	JSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"imported": count,
		"total":    len(body.Rows),
	})
}

// Clear deletes ALL schedule rows (and the import metadata) for a user — the
// backend half of "Rooster wissen" (N2: that button POSTed an empty import,
// which the backend rejected; there was no delete path at all).
// @Summary Clear schedule
// @Description Deletes all schedule rows and import metadata for the user
// @Tags Schedule
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Success 204 "No Content"
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /schedule [delete]
func (h *ScheduleHandler) Clear(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	if _, err := h.store.DeleteAll(r.Context(), userID); err != nil {
		InternalError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
		InternalError(w, r, err)
		return
	}
	if meta == nil {
		JSON(w, http.StatusOK, map[string]any{"imported": false})
		return
	}
	JSON(w, http.StatusOK, meta)
}
