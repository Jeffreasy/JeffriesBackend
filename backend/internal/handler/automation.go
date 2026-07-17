package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type AutomationHandler struct {
	store  *store.AutomationStore
	userID string
}

func NewAutomationHandler(s *store.AutomationStore, userID string) *AutomationHandler {
	return &AutomationHandler{store: s, userID: userID}
}

// List returns all automations for a user.
// @Summary List automations
// @Description Returns all home automations for the user
// @Tags Automations
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.AutomationRow
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations [get]
func (h *AutomationHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	autos, err := h.store.List(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, autos)
}

// Create adds a new automation.
// @Summary Create automation
// @Description Creates a new home automation rule
// @Tags Automations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param request body model.AutomationRow true "Automation Details"
// @Success 201 {object} model.AutomationRow
// @Failure 400 {string} string "userId required or invalid JSON"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations [post]
func (h *AutomationHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	var body model.AutomationRow
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	body.UserID = userID
	created, err := h.store.Create(r.Context(), body)
	if err != nil {
		if err == pgx.ErrNoRows {
			// The store returns ErrNoRows for a duplicate name — surface it as a
			// 409 conflict, not a misleading 500.
			Error(w, http.StatusConflict, "Er bestaat al een automatisering met deze naam")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, created)
}

// Update modifies an existing automation.
// @Summary Update automation
// @Description Modifies a home automation rule
// @Tags Automations
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Automation ID (UUID)"
// @Param request body model.AutomationRow true "Automation Details"
// @Success 200 {object} model.AutomationRow
// @Failure 400 {string} string "invalid id or JSON"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations/{id} [put]
func (h *AutomationHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body model.AutomationRow
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	updated, err := h.store.Update(r.Context(), h.userID, id, body)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, updated)
}

// Toggle flips the enabled flag.
// @Summary Toggle automation
// @Description Enables or disables an automation
// @Tags Automations
// @Security ApiKeyAuth
// @Param id path string true "Automation ID (UUID)"
// @Success 200 {object} map[string]string "status toggled"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations/{id}/toggle [post]
func (h *AutomationHandler) Toggle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.Toggle(r.Context(), h.userID, id); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "toggled"})
}

// Delete removes an automation.
// @Summary Delete automation
// @Description Deletes an automation by its ID
// @Tags Automations
// @Security ApiKeyAuth
// @Param id path string true "Automation ID (UUID)"
// @Success 200 {object} map[string]string "status deleted"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations/{id} [delete]
func (h *AutomationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.Delete(r.Context(), h.userID, id); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// DeleteByGroup removes all automations for a user's group.
// @Summary Delete automations by group
// @Description Deletes all automations within a specific group
// @Tags Automations
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param group query string true "Group name"
// @Success 200 {object} map[string]string "status deleted"
// @Failure 400 {string} string "userId and group required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /automations/group [delete]
func (h *AutomationHandler) DeleteByGroup(w http.ResponseWriter, r *http.Request) {
	userID := h.userID
	group := r.URL.Query().Get("group")
	if userID == "" || group == "" {
		Error(w, http.StatusBadRequest, "userId en group zijn verplicht")
		return
	}
	if err := h.store.DeleteByGroup(r.Context(), userID, group); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
