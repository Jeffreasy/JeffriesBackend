package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	note, err := h.store.Get(r.Context(), id)
	if err != nil {
		Error(w, http.StatusNotFound, "note not found")
		return
	}
	JSON(w, http.StatusOK, note)
}

type noteCreateBody struct {
	Titel         *string  `json:"titel"`
	Inhoud        string   `json:"inhoud"`
	Tags          []string `json:"tags"`
	Kleur         *string  `json:"kleur"`
	Deadline      *string  `json:"deadline"`
	LinkedEventID *string  `json:"linkedEventId"`
	Prioriteit    *string  `json:"prioriteit"`
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
	n := model.Note{
		Titel:         body.Titel,
		Inhoud:        body.Inhoud,
		Tags:          body.Tags,
		Kleur:         body.Kleur,
		LinkedEventID: body.LinkedEventID,
		Prioriteit:    body.Prioriteit,
	}
	created, err := h.store.Create(r.Context(), userID, n)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, created)
}

type noteUpdateBody struct {
	Titel         *string  `json:"titel"`
	Inhoud        *string  `json:"inhoud"`
	Tags          []string `json:"tags"`
	Kleur         *string  `json:"kleur"`
	IsPinned      *bool    `json:"isPinned"`
	IsArchived    *bool    `json:"isArchived"`
	Deadline      *string  `json:"deadline"`
	LinkedEventID *string  `json:"linkedEventId"`
	Prioriteit    *string  `json:"prioriteit"`
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
	if body.Prioriteit != nil {
		fields["prioriteit"] = *body.Prioriteit
	}

	if len(fields) == 0 {
		Error(w, http.StatusBadRequest, "no fields to update")
		return
	}

	updated, err := h.store.Update(r.Context(), id, fields)
	if err != nil {
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
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "invalid id")
		return
	}
	links, err := h.store.GetBacklinks(r.Context(), id)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, links)
}
