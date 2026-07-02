package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type SalaryHandler struct{ store *store.SalaryStore }

func NewSalaryHandler(s *store.SalaryStore) *SalaryHandler { return &SalaryHandler{store: s} }

// List returns all salary periods for a user.
// @Summary List salary periods
// @Description Returns all salary periods for the user
// @Tags Salary
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.Salary
// @Failure 400 {string} string "userId verplicht"
// @Failure 500 {string} string "Internal Server Error"
// @Router /salary [get]
func (h *SalaryHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId verplicht")
		return
	}
	list, err := h.store.List(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, list)
}

// GetByPeriode returns a specific salary period.
// @Summary Get salary by period
// @Description Returns salary details for a specific period
// @Tags Salary
// @Produce json
// @Param userId query string true "User ID"
// @Param periode query string true "Period (YYYY-MM)"
// @Success 200 {object} model.Salary
// @Failure 400 {string} string "userId en periode verplicht"
// @Failure 404 {string} string "Periode niet gevonden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /salary/periode [get]
func (h *SalaryHandler) GetByPeriode(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	periode := r.URL.Query().Get("periode")
	if userID == "" || periode == "" {
		Error(w, http.StatusBadRequest, "userId en periode verplicht")
		return
	}
	sal, err := h.store.GetByPeriode(r.Context(), userID, periode)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	if sal == nil {
		Error(w, http.StatusNotFound, "Periode niet gevonden")
		return
	}
	JSON(w, http.StatusOK, sal)
}

// Upsert inserts or updates a salary period.
// @Summary Upsert salary period
// @Description Inserts or updates salary details for a specific period
// @Tags Salary
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.Salary true "Salary Details"
// @Success 200 {object} map[string]bool "ok: true"
// @Failure 400 {string} string "Ongeldige JSON of ontbrekende velden"
// @Failure 500 {string} string "Internal Server Error"
// @Router /salary [post]
func (h *SalaryHandler) Upsert(w http.ResponseWriter, r *http.Request) {
	var sal model.Salary
	if err := json.NewDecoder(r.Body).Decode(&sal); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if sal.UserID == "" || sal.Periode == "" {
		Error(w, http.StatusBadRequest, "user_id en periode verplicht")
		return
	}
	if err := h.store.Upsert(r.Context(), sal); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]bool{"ok": true})
}
