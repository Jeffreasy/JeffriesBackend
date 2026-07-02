package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

type EmailHandler struct{ store *store.EmailStore }

func NewEmailHandler(s *store.EmailStore) *EmailHandler { return &EmailHandler{store: s} }

// List returns paginated emails for a user.
// @Summary List emails
// @Description Returns paginated emails for the user, optionally filtered by category
// @Tags Emails
// @Produce json
// @Param user_id query string true "User ID"
// @Param limit query int false "Limit count" default(50)
// @Param offset query int false "Offset count" default(0)
// @Param categorie query string false "Category filter"
// @Success 200 {array} model.Email
// @Failure 400 {string} string "user_id is required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /emails [get]
func (h *EmailHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		Error(w, http.StatusBadRequest, "user_id is verplicht")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	categorie := r.URL.Query().Get("categorie")

	var emails any
	var err error

	if categorie != "" {
		emails, err = h.store.ListByCategorie(r.Context(), userID, categorie, limit, offset)
	} else {
		emails, err = h.store.List(r.Context(), userID, limit, offset)
	}

	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, emails)
}

// Search performs full-text search.
// @Summary Search emails
// @Description Performs a full-text search across user emails
// @Tags Emails
// @Produce json
// @Param user_id query string true "User ID"
// @Param q query string true "Search query"
// @Param limit query int false "Limit count" default(50)
// @Success 200 {array} model.Email
// @Failure 400 {string} string "user_id and q are required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /emails/search [get]
func (h *EmailHandler) Search(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	query := r.URL.Query().Get("q")
	if userID == "" || query == "" {
		Error(w, http.StatusBadRequest, "user_id en q zijn verplicht")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	emails, err := h.store.Search(r.Context(), userID, query, limit)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, emails)
}

// Stats returns email counts.
// @Summary Get email stats
// @Description Returns total and unread email counts for the user
// @Tags Emails
// @Produce json
// @Param user_id query string true "User ID"
// @Success 200 {object} map[string]int
// @Failure 400 {string} string "user_id is required"
// @Failure 500 {string} string "Internal Server Error"
// @Router /emails/stats [get]
func (h *EmailHandler) Stats(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		Error(w, http.StatusBadRequest, "user_id is verplicht")
		return
	}

	total, err := h.store.Count(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	unread, err := h.store.CountUnread(r.Context(), userID)
	if err != nil {
		InternalError(w, r, err)
		return
	}

	JSON(w, http.StatusOK, map[string]int{
		"total":  total,
		"unread": unread,
	})
}

// MarkRead marks an email as read/unread.
// @Summary Mark email read status
// @Description Updates the read status of a specific email
// @Tags Emails
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]interface{} true "Read Status Details (user_id, gmail_id, read)"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "invalid request body"
// @Failure 500 {string} string "Internal Server Error"
// @Router /emails/read [patch]
func (h *EmailHandler) MarkRead(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID  string `json:"user_id"`
		GmailID string `json:"gmail_id"`
		Read    bool   `json:"read"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}

	if err := h.store.MarkRead(r.Context(), body.UserID, body.GmailID, body.Read); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Delete soft-deletes an email.
// @Summary Delete email
// @Description Soft-deletes a specific email
// @Tags Emails
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]string true "Delete Details (user_id, gmail_id)"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "invalid request body"
// @Failure 500 {string} string "Internal Server Error"
// @Router /emails/delete [patch]
func (h *EmailHandler) Delete(w http.ResponseWriter, r *http.Request) {
	var body struct {
		UserID  string `json:"user_id"`
		GmailID string `json:"gmail_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}

	if err := h.store.MarkDeleted(r.Context(), body.UserID, body.GmailID); err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
