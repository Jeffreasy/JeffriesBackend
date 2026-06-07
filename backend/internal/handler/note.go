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

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type NoteHandler struct{ store *store.NoteStore }

func NewNoteHandler(s *store.NoteStore) *NoteHandler { return &NoteHandler{store: s} }

// List returns all notes for a user.
// @Summary List notes
// @Description Returns all notes for the user
// @Tags Notes
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} model.Note
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes [get]
func (h *NoteHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	notes, err := h.store.List(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, notes)
}

// Get returns a single note.
// @Summary Get a note
// @Description Returns a single note by its ID
// @Tags Notes
// @Produce json
// @Param id path string true "Note ID (UUID)"
// @Success 200 {object} model.Note
// @Failure 400 {string} string "invalid id"
// @Failure 404 {string} string "note not found"
// @Router /notes/{id} [get]
func (h *NoteHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	note, err := h.store.GetForUser(r.Context(), userID, id)
	if err != nil {
		Error(w, http.StatusNotFound, "note not found")
		return
	}
	JSON(w, http.StatusOK, note)
}

type noteCreateBody struct {
	Titel                *string  `json:"titel"`
	Inhoud               string   `json:"inhoud"`
	Tags                 []string `json:"tags"`
	Kleur                *string  `json:"kleur"`
	Deadline             *string  `json:"deadline"`
	LinkedEventID        *string  `json:"linkedEventId"`
	Prioriteit           *string  `json:"prioriteit"`
	Symbol               *string  `json:"symbol"`
	BusinessContextType  *string  `json:"businessContextType"`
	BusinessContextID    *string  `json:"businessContextId"`
	BusinessContextTitle *string  `json:"businessContextTitle"`
}

// Create adds a new note.
// @Summary Create a note
// @Description Creates a new note for the user
// @Tags Notes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param userId query string true "User ID"
// @Param request body noteCreateBody true "Note Details"
// @Success 201 {object} model.Note
// @Failure 400 {string} string "userId required or invalid JSON"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes [post]
func (h *NoteHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	var body noteCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	deadline, err := parseDeadline(body.Deadline)
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid deadline format: "+err.Error())
		return
	}
	linkedEventID, err := h.store.NormalizeLinkedEventID(r.Context(), userID, body.LinkedEventID)
	if err != nil {
		if errors.Is(err, store.ErrLinkedEventNotFound) {
			Error(w, http.StatusBadRequest, "linked event not found")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	n := model.Note{
		Titel:                body.Titel,
		Inhoud:               body.Inhoud,
		Tags:                 body.Tags,
		Kleur:                body.Kleur,
		Deadline:             deadline,
		LinkedEventID:        linkedEventID,
		Prioriteit:           body.Prioriteit,
		Symbol:               body.Symbol,
		BusinessContextType:  cleanOptionalString(body.BusinessContextType),
		BusinessContextID:    cleanOptionalString(body.BusinessContextID),
		BusinessContextTitle: cleanOptionalString(body.BusinessContextTitle),
	}
	created, err := h.store.Create(r.Context(), userID, n)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, created)
}

type noteUpdateBody struct {
	Titel                *string  `json:"titel"`
	Inhoud               *string  `json:"inhoud"`
	Tags                 []string `json:"tags"`
	Kleur                *string  `json:"kleur"`
	IsPinned             *bool    `json:"isPinned"`
	IsArchived           *bool    `json:"isArchived"`
	IsCompleted          *bool    `json:"isCompleted"`
	Deadline             *string  `json:"deadline"`
	LinkedEventID        *string  `json:"linkedEventId"`
	Prioriteit           *string  `json:"prioriteit"`
	Symbol               *string  `json:"symbol"`
	BusinessContextType  *string  `json:"businessContextType"`
	BusinessContextID    *string  `json:"businessContextId"`
	BusinessContextTitle *string  `json:"businessContextTitle"`
}

// Update patches a note.
// @Summary Update a note
// @Description Updates the details of an existing note
// @Tags Notes
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Note ID (UUID)"
// @Param request body noteUpdateBody true "Updated Note Fields"
// @Success 200 {object} model.Note
// @Failure 400 {string} string "invalid JSON or ID"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/{id} [patch]
func (h *NoteHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	var body noteUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		Error(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	fields := map[string]any{}
	if body.Titel != nil {
		fields["titel"] = *body.Titel
	}
	if body.Inhoud != nil {
		fields["inhoud"] = *body.Inhoud
	}
	if body.Tags != nil {
		fields["tags"] = body.Tags
	}
	if body.Kleur != nil {
		fields["kleur"] = *body.Kleur
	}
	if body.IsPinned != nil {
		fields["is_pinned"] = *body.IsPinned
	}
	if body.IsArchived != nil {
		fields["is_archived"] = *body.IsArchived
	}
	if body.IsCompleted != nil {
		fields["is_completed"] = *body.IsCompleted
		if *body.IsCompleted {
			now := time.Now()
			fields["completed_at"] = now
		} else {
			fields["completed_at"] = nil
		}
	}
	if body.Prioriteit != nil {
		fields["prioriteit"] = *body.Prioriteit
	}
	if body.Symbol != nil {
		if *body.Symbol == "" {
			fields["symbol"] = nil
		} else {
			fields["symbol"] = *body.Symbol
		}
	}
	if body.BusinessContextType != nil {
		fields["business_context_type"] = cleanOptionalString(body.BusinessContextType)
	}
	if body.BusinessContextID != nil {
		fields["business_context_id"] = cleanOptionalString(body.BusinessContextID)
	}
	if body.BusinessContextTitle != nil {
		fields["business_context_title"] = cleanOptionalString(body.BusinessContextTitle)
	}
	if body.LinkedEventID != nil {
		linkedEventID, err := h.store.NormalizeLinkedEventID(r.Context(), userID, body.LinkedEventID)
		if err != nil {
			if errors.Is(err, store.ErrLinkedEventNotFound) {
				Error(w, http.StatusBadRequest, "linked event not found")
				return
			}
			Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		fields["linked_event_id"] = linkedEventID
	}

	if body.Deadline != nil {
		if *body.Deadline == "" {
			fields["deadline"] = nil
		} else {
			deadline, err := parseDeadline(body.Deadline)
			if err != nil {
				Error(w, http.StatusBadRequest, "invalid deadline format: "+err.Error())
				return
			}
			fields["deadline"] = deadline
		}
	}

	if len(fields) == 0 {
		Error(w, http.StatusBadRequest, "no fields to update")
		return
	}

	updated, err := h.store.UpdateForUser(r.Context(), userID, id, fields)
	if err != nil {
		if errors.Is(err, store.ErrNoteNotFound) {
			Error(w, http.StatusNotFound, "note not found")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, updated)
}

// Delete removes a note.
// @Summary Delete a note
// @Description Permanently deletes a note by its ID
// @Tags Notes
// @Security ApiKeyAuth
// @Param id path string true "Note ID (UUID)"
// @Success 200 {object} map[string]string "status deleted"
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/{id} [delete]
func (h *NoteHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := h.store.DeleteForUser(r.Context(), userID, id); err != nil {
		if errors.Is(err, store.ErrNoteNotFound) {
			Error(w, http.StatusNotFound, "note not found")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Search performs full-text search across notes.
// @Summary Search notes
// @Description Performs a full-text search across all notes for a user
// @Tags Notes
// @Produce json
// @Param userId query string true "User ID"
// @Param q query string true "Search query"
// @Param limit query int false "Limit count" default(20)
// @Success 200 {array} model.Note
// @Failure 400 {string} string "userId and q required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/search [get]
func (h *NoteHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	query := r.URL.Query().Get("q")
	if userID == "" || query == "" {
		Error(w, http.StatusBadRequest, "userId and q required")
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	notes, err := h.store.Search(r.Context(), userID, query, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, notes)
}

// Tags returns all unique tags.
// @Summary Get all note tags
// @Description Returns all unique tags used across a user's notes
// @Tags Notes
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {array} string
// @Failure 400 {string} string "userId required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/tags [get]
func (h *NoteHandler) Tags(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	tags, err := h.store.AllTags(r.Context(), userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, tags)
}

// Backlinks returns notes that link to a specific note.
// @Summary Get note backlinks
// @Description Returns notes that reference the given note
// @Tags Notes
// @Produce json
// @Param id path string true "Note ID (UUID)"
// @Success 200 {array} model.Note
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/{id}/backlinks [get]
func (h *NoteHandler) Backlinks(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	links, err := h.store.GetBacklinksForUser(r.Context(), userID, id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, links)
}

// Revisions returns saved versions for a note.
// @Summary List note revisions
// @Description Returns recent saved versions for a note
// @Tags Notes
// @Produce json
// @Param id path string true "Note ID (UUID)"
// @Param userId query string true "User ID"
// @Param limit query int false "Limit count" default(20)
// @Success 200 {array} model.NoteRevision
// @Failure 400 {string} string "invalid id"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/{id}/revisions [get]
func (h *NoteHandler) Revisions(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	limit := 20
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	revisions, err := h.store.ListRevisions(r.Context(), userID, id, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, revisions)
}

// RestoreRevision restores a note from a saved version.
// @Summary Restore note revision
// @Description Replaces a note with a previous saved version
// @Tags Notes
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Note ID (UUID)"
// @Param revisionID path string true "Revision ID (UUID)"
// @Param userId query string true "User ID"
// @Success 200 {object} model.Note
// @Failure 400 {string} string "invalid id"
// @Failure 404 {string} string "note or revision not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /notes/{id}/revisions/{revisionID}/restore [post]
func (h *NoteHandler) RestoreRevision(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	revisionID, err := uuid.Parse(chi.URLParam(r, "revisionID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid revision id")
		return
	}
	restored, err := h.store.RestoreRevision(r.Context(), userID, id, revisionID)
	if err != nil {
		if errors.Is(err, store.ErrNoteNotFound) {
			Error(w, http.StatusNotFound, "note or revision not found")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, restored)
}

func parseDeadline(deadlineStr *string) (*time.Time, error) {
	if deadlineStr == nil || *deadlineStr == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, *deadlineStr)
	if err == nil {
		return &t, nil
	}
	t, err = time.Parse("2006-01-02T15:04:05", *deadlineStr)
	if err == nil {
		return &t, nil
	}
	t, err = time.Parse("2006-01-02T15:04", *deadlineStr)
	if err == nil {
		return &t, nil
	}
	t, err = time.Parse("2006-01-02", *deadlineStr)
	if err == nil {
		return &t, nil
	}
	return nil, err
}

func cleanOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
