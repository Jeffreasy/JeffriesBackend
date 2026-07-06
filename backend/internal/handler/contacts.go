package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// ContactHandler serves the unified Contacts/Relationships module.
type ContactHandler struct{ store *store.ContactStore }

func NewContactHandler(s *store.ContactStore) *ContactHandler { return &ContactHandler{store: s} }

func contactUserID(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("userId"))
}

// parseOptionalUUID turns an optional string into an optional UUID. A nil or
// empty pointer yields (nil, nil); a malformed value yields an error.
func parseOptionalUUID(s *string) (*uuid.UUID, error) {
	if s == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil, nil
	}
	id, err := uuid.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// List returns contacts for a user; optional ?q=, ?type=, ?includeArchived=, ?limit=.
func (h *ContactHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	opts := store.ListContactsOptions{
		Query:            r.URL.Query().Get("q"),
		RelationshipType: r.URL.Query().Get("type"),
		IncludeArchived:  strings.EqualFold(r.URL.Query().Get("includeArchived"), "true"),
	}
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			opts.Limit = parsed
		}
	}
	contacts, err := h.store.List(r.Context(), userID, opts)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, contacts)
}

// Get returns a single contact with dates and facts.
func (h *ContactHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	c, err := h.store.Get(r.Context(), userID, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, c)
}

type contactCreateBody struct {
	DisplayName       string   `json:"display_name"`
	RelationshipTypes []string `json:"relationship_types"`
	Notes             *string  `json:"notes"`
	Email             *string  `json:"email"`
	Phone             *string  `json:"phone"`
	Address           *string  `json:"address"`
	OrganizationID    *string  `json:"organization_id"`
	BusinessRole      *string  `json:"business_role"`
}

// Create adds a new contact.
func (h *ContactHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	var body contactCreateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	name := strings.TrimSpace(body.DisplayName)
	if name == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht.")
		return
	}
	orgID, err := parseOptionalUUID(body.OrganizationID)
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig organization_id.")
		return
	}
	c := model.Contact{
		DisplayName:       name,
		RelationshipTypes: normalizeTags(body.RelationshipTypes),
		Notes:             cleanOptionalString(body.Notes),
		Email:             cleanOptionalString(body.Email),
		Phone:             cleanOptionalString(body.Phone),
		Address:           cleanOptionalString(body.Address),
		OrganizationID:    orgID,
		BusinessRole:      cleanOptionalString(body.BusinessRole),
	}
	created, err := h.store.Create(r.Context(), userID, c)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, created)
}

type contactUpdateBody struct {
	DisplayName       *string   `json:"display_name"`
	RelationshipTypes *[]string `json:"relationship_types"`
	Notes             *string   `json:"notes"`
	Email             *string   `json:"email"`
	Phone             *string   `json:"phone"`
	Address           *string   `json:"address"`
	OrganizationID    *string   `json:"organization_id"`
	BusinessRole      *string   `json:"business_role"`
	Archived          *bool     `json:"archived"`
	TouchLastContact  *bool     `json:"touch_last_contact"`
}

// Update applies a partial update.
func (h *ContactHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body contactUpdateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}

	u := store.ContactUpdate{
		DisplayName:  body.DisplayName,
		Notes:        body.Notes,
		Email:        body.Email,
		Phone:        body.Phone,
		Address:      body.Address,
		BusinessRole: body.BusinessRole,
		Archived:     body.Archived,
	}
	if body.RelationshipTypes != nil {
		normalized := normalizeTags(*body.RelationshipTypes)
		u.RelationshipTypes = &normalized
	}
	// organization_id: absent = leave; empty string = clear; value = set.
	if body.OrganizationID != nil {
		if strings.TrimSpace(*body.OrganizationID) == "" {
			u.ClearOrganization = true
		} else {
			orgID, err := parseOptionalUUID(body.OrganizationID)
			if err != nil {
				Error(w, http.StatusBadRequest, "Ongeldig organization_id.")
				return
			}
			u.OrganizationID = orgID
		}
	}
	if body.TouchLastContact != nil && *body.TouchLastContact {
		u.TouchLastContact = true
	}

	updated, err := h.store.Update(r.Context(), userID, id, u)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, updated)
}

// Delete removes a contact.
func (h *ContactHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.Delete(r.Context(), userID, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Important dates ─────────────────────────────────────────────────────────

type importantDateBody struct {
	Kind      string  `json:"kind"`
	Label     *string `json:"label"`
	Month     int     `json:"month"`
	Day       int     `json:"day"`
	Year      *int    `json:"year"`
	Recurring *bool   `json:"recurring"`
}

// AddDate adds an important date to a contact.
func (h *ContactHandler) AddDate(w http.ResponseWriter, r *http.Request) {
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
	var body importantDateBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if body.Month < 1 || body.Month > 12 || body.Day < 1 || body.Day > 31 {
		Error(w, http.StatusBadRequest, "Ongeldige datum (maand 1-12, dag 1-31).")
		return
	}
	recurring := true
	if body.Recurring != nil {
		recurring = *body.Recurring
	}
	d := model.ContactImportantDate{
		ContactID: contactID,
		Kind:      strings.TrimSpace(body.Kind),
		Label:     cleanOptionalString(body.Label),
		Month:     body.Month,
		Day:       body.Day,
		Year:      body.Year,
		Recurring: recurring,
	}
	created, err := h.store.AddImportantDate(r.Context(), userID, d)
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

// DeleteDate removes an important date.
func (h *ContactHandler) DeleteDate(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	dateID, err := uuid.Parse(chi.URLParam(r, "dateID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.DeleteImportantDate(r.Context(), userID, dateID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Datum niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ─── Facts ───────────────────────────────────────────────────────────────────

type factBody struct {
	Fact   string  `json:"fact"`
	Source *string `json:"source"`
}

// AddFact records a fact about a contact.
func (h *ContactHandler) AddFact(w http.ResponseWriter, r *http.Request) {
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
	var body factBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.Fact) == "" {
		Error(w, http.StatusBadRequest, "Feit mag niet leeg zijn.")
		return
	}
	source := "manual"
	if body.Source != nil && strings.TrimSpace(*body.Source) != "" {
		source = strings.TrimSpace(*body.Source)
	}
	f := model.ContactFact{
		ContactID: contactID,
		Fact:      strings.TrimSpace(body.Fact),
		Source:    source,
	}
	created, err := h.store.AddFact(r.Context(), userID, f)
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

// DeleteFact removes a fact.
func (h *ContactHandler) DeleteFact(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	factID, err := uuid.Parse(chi.URLParam(r, "factID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	if err := h.store.DeleteFact(r.Context(), userID, factID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Feit niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}
