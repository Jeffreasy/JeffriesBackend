package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// ─── Channels ────────────────────────────────────────────────────────────────

type channelBody struct {
	Kind      string  `json:"kind"`
	Value     string  `json:"value"`
	Label     *string `json:"label"`
	IsPrimary bool    `json:"is_primary"`
}

// AddChannel adds an extra email/phone to a contact.
func (h *ContactHandler) AddChannel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body channelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.Value) == "" {
		Error(w, http.StatusBadRequest, "Waarde is verplicht.")
		return
	}
	created, err := h.store.AddChannel(r.Context(), userID, model.ContactChannel{
		ContactID: contactID,
		Kind:      body.Kind,
		Value:     body.Value,
		Label:     cleanOptionalString(body.Label),
		IsPrimary: body.IsPrimary,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, created)
}

// DeleteChannel removes a channel.
func (h *ContactHandler) DeleteChannel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	channelID, err := uuid.Parse(chi.URLParam(r, "channelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.DeleteChannel(r.Context(), userID, channelID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Kanaal niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Interactions ────────────────────────────────────────────────────────────

// ListInteractions returns a contact's touchpoint timeline (?limit=).
func (h *ContactHandler) ListInteractions(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	limit := 0
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}
	items, err := h.store.ListInteractions(r.Context(), userID, contactID, limit)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, items)
}

type interactionBody struct {
	Kind       string  `json:"kind"`
	Summary    *string `json:"summary"`
	OccurredAt *string `json:"occurred_at"` // RFC3339; defaults to now
}

// AddInteraction logs a touchpoint (advances last_contacted_at).
func (h *ContactHandler) AddInteraction(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	contactID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body interactionBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	in := model.ContactInteraction{
		ContactID: contactID,
		Kind:      body.Kind,
		Summary:   cleanOptionalString(body.Summary),
	}
	if body.OccurredAt != nil && strings.TrimSpace(*body.OccurredAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*body.OccurredAt))
		if err != nil {
			Error(w, http.StatusBadRequest, "Ongeldige occurred_at (verwacht RFC3339).")
			return
		}
		in.OccurredAt = t
	}
	created, err := h.store.AddInteraction(r.Context(), userID, in)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, created)
}

// DeleteInteraction removes a touchpoint (recomputes last_contacted_at).
func (h *ContactHandler) DeleteInteraction(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	interactionID, err := uuid.Parse(chi.URLParam(r, "interactionID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.DeleteInteraction(r.Context(), userID, interactionID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Interactie niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
