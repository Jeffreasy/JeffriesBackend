package handler

import (
	"encoding/json"
	"net/http"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type PrivacyHandler struct {
	store       *store.PrivacyStore
	ownerUserID string
}

func NewPrivacyHandler(s *store.PrivacyStore, ownerUserID string) *PrivacyHandler {
	return &PrivacyHandler{store: s, ownerUserID: ownerUserID}
}

// Get returns the privacy settings for a user.
// @Summary Get privacy settings
// @Description Returns the privacy settings for the user
// @Tags Privacy
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {object} model.PrivacySettings
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /privacy [get]
func (h *PrivacyHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := h.ownerUserID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	p, err := h.store.Get(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, p)
}

// Update patches the privacy settings.
// @Summary Update privacy settings
// @Description Updates the privacy settings for the user
// @Tags Privacy
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param request body model.PrivacySettings true "Updated Privacy Settings"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "invalid JSON or missing userId"
// @Failure 500 {string} string "Internal Server Error"
// @Router /privacy [put]
func (h *PrivacyHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := h.ownerUserID
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	var body model.PrivacySettings
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if err := h.store.Update(r.Context(), userID, body); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
