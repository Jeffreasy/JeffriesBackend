package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// ─── Label catalog ───────────────────────────────────────────────────────────

// ListLabels returns the user's label catalog with usage counts.
func (h *ContactHandler) ListLabels(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	labels, err := h.store.ListLabels(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, labels)
}

type labelBody struct {
	Name  string  `json:"name"`
	Color *string `json:"color"`
}

// CreateLabel adds a label (idempotent on name).
func (h *ContactHandler) CreateLabel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	var body labelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.Name) == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht.")
		return
	}
	color := ""
	if body.Color != nil {
		color = *body.Color
	}
	label, err := h.store.CreateLabel(r.Context(), userID, body.Name, color)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, label)
}

type labelUpdateBody struct {
	Name  *string `json:"name"`
	Color *string `json:"color"`
}

// UpdateLabel renames/recolours a label.
func (h *ContactHandler) UpdateLabel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body labelUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	label, err := h.store.UpdateLabel(r.Context(), userID, id, body.Name, body.Color)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrLabelNameTaken):
			Error(w, http.StatusConflict, "Er bestaat al een label met deze naam.")
		case errors.Is(err, pgx.ErrNoRows):
			Error(w, http.StatusNotFound, "Label niet gevonden.")
		default:
			InternalError(w, r, err)
		}
		return
	}
	JSON(w, http.StatusOK, label)
}

// DeleteLabel removes a label (assignments cascade).
func (h *ContactHandler) DeleteLabel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.DeleteLabel(r.Context(), userID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Label niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type mergeLabelBody struct {
	Into string `json:"into"`
}

// MergeLabels folds {labelID} into the label given by `into`.
func (h *ContactHandler) MergeLabels(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	fromID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body mergeLabelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	toID, err := uuid.Parse(strings.TrimSpace(body.Into))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig doel-label (into).")
		return
	}
	if err := h.store.MergeLabels(r.Context(), userID, fromID, toID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Een van de labels bestaat niet.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "merged"})
}

type bulkLabelBody struct {
	ContactIDs []string `json:"contact_ids"`
	Remove     bool     `json:"remove"`
}

// BulkLabel adds/removes one label across many contacts.
func (h *ContactHandler) BulkLabel(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body bulkLabelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	ids, err := parseUUIDList(body.ContactIDs)
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig contact_ids.")
		return
	}
	n, err := h.store.BulkAssignLabel(r.Context(), userID, labelID, ids, body.Remove)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Label niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"status": "ok", "affected": n})
}

// ─── Per-contact assignment ──────────────────────────────────────────────────

type assignLabelBody struct {
	LabelID string  `json:"label_id"`
	Name    string  `json:"name"`
	Color   *string `json:"color"`
}

// AssignLabel tags a contact with an existing label (label_id) or a new/looked-up
// label by name.
func (h *ContactHandler) AssignLabel(w http.ResponseWriter, r *http.Request) {
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
	var body assignLabelBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}

	if strings.TrimSpace(body.LabelID) != "" {
		labelID, err := uuid.Parse(strings.TrimSpace(body.LabelID))
		if err != nil {
			Error(w, http.StatusBadRequest, "Ongeldig label_id.")
			return
		}
		if err := h.store.AssignLabel(r.Context(), userID, contactID, labelID); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				Error(w, http.StatusNotFound, "Contact of label niet gevonden.")
				return
			}
			InternalError(w, r, err)
			return
		}
		JSON(w, http.StatusOK, map[string]string{"status": "assigned"})
		return
	}

	if strings.TrimSpace(body.Name) == "" {
		Error(w, http.StatusBadRequest, "label_id of name is verplicht.")
		return
	}
	color := ""
	if body.Color != nil {
		color = *body.Color
	}
	label, err := h.store.AssignLabelByName(r.Context(), userID, contactID, body.Name, color)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, label)
}

// RemoveLabel untags a contact.
func (h *ContactHandler) RemoveLabel(w http.ResponseWriter, r *http.Request) {
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
	labelID, err := uuid.Parse(chi.URLParam(r, "labelID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.RemoveLabel(r.Context(), userID, contactID, labelID); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

type setLabelsBody struct {
	LabelIDs []string `json:"label_ids"`
}

// SetLabels replaces a contact's entire label set.
func (h *ContactHandler) SetLabels(w http.ResponseWriter, r *http.Request) {
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
	var body setLabelsBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	ids, err := parseUUIDList(body.LabelIDs)
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig label_ids.")
		return
	}
	if err := h.store.SetContactLabels(r.Context(), userID, contactID, ids); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// parseUUIDList parses a slice of UUID strings, skipping empties.
func parseUUIDList(in []string) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}
