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
	"github.com/Jeffreasy/JeffriesBackend/internal/whatsapp"
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

// maxDayForMonth returns the highest valid day for a 1-based month. February
// allows 29 so a leap-day birthday (or one with an unknown year) is accepted.
func maxDayForMonth(month int) int {
	switch month {
	case 2:
		return 29
	case 4, 6, 9, 11:
		return 30
	default:
		return 31
	}
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
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
		case errors.Is(err, store.ErrManagedContact):
			Error(w, http.StatusConflict, "Dit contact wordt beheerd in LaventeCare en kan hier niet worden verwijderd.")
		default:
			InternalError(w, r, err)
		}
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type mergeContactBody struct {
	Into string `json:"into"` // survivor contact id; this contact ({id}) is folded into it
}

// Merge folds contact {id} into the `into` contact (survivor).
func (h *ContactHandler) Merge(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	fromID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	var body mergeContactBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	toID, err := uuid.Parse(strings.TrimSpace(body.Into))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig doel-contact (into).")
		return
	}
	merged, err := h.store.MergeContacts(r.Context(), userID, fromID, toID)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrCannotMergeSelf):
			Error(w, http.StatusBadRequest, "Kan een contact niet met zichzelf samenvoegen.")
		case errors.Is(err, pgx.ErrNoRows):
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
		default:
			InternalError(w, r, err)
		}
		return
	}
	JSON(w, http.StatusOK, merged)
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
	if body.Month < 1 || body.Month > 12 || body.Day < 1 || body.Day > maxDayForMonth(body.Month) {
		Error(w, http.StatusBadRequest, "Ongeldige datum (controleer dag/maand).")
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

// ─── WhatsApp import ─────────────────────────────────────────────────────────

type whatsappImportBody struct {
	ChatName       string `json:"chat_name"`
	SourceFilename string `json:"source_filename"`
	IsGroup        bool   `json:"is_group"`
	Text           string `json:"text"`
}

// WhatsAppImport parses an exported chat (.txt content in `text`) and stores it
// for the contact, returning the conversation + its metadata summary.
func (h *ContactHandler) WhatsAppImport(w http.ResponseWriter, r *http.Request) {
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
	var body whatsappImportBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		RespondDecodeError(w, err)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		Error(w, http.StatusBadRequest, "Geen chat-tekst meegestuurd.")
		return
	}
	parsed := whatsapp.Parse(body.Text)
	if len(parsed.Messages) == 0 {
		Error(w, http.StatusBadRequest, "Kon geen berichten herkennen in het geëxporteerde bestand.")
		return
	}
	chatName := strings.TrimSpace(body.ChatName)
	if chatName == "" {
		if len(parsed.Participants) > 0 {
			chatName = parsed.Participants[0]
		} else {
			chatName = "WhatsApp"
		}
	}
	conv, summary, err := h.store.ImportWhatsAppConversation(
		r.Context(), userID, contactID, chatName, body.SourceFilename, body.IsGroup, parsed.Messages,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			Error(w, http.StatusNotFound, "Contact niet gevonden.")
			return
		}
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusCreated, map[string]any{
		"conversation": conv,
		"summary":      summary,
		"participants": parsed.Participants,
		"imported":     len(parsed.Messages),
	})
}

// WhatsAppList returns a contact's imported conversations + their summaries.
func (h *ContactHandler) WhatsAppList(w http.ResponseWriter, r *http.Request) {
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
	convs, err := h.store.ListWhatsAppConversations(r.Context(), userID, contactID)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	summaries, err := h.store.ListWhatsAppSummaries(r.Context(), userID, &contactID, 50)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, map[string]any{"conversations": convs, "summaries": summaries})
}

// WhatsAppMessages returns the (local) messages of a conversation.
func (h *ContactHandler) WhatsAppMessages(w http.ResponseWriter, r *http.Request) {
	userID := contactUserID(r)
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId is verplicht")
		return
	}
	convID, err := uuid.Parse(chi.URLParam(r, "convID"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Ongeldig id.")
		return
	}
	msgs, err := h.store.ListWhatsAppMessages(r.Context(), userID, convID, 5000)
	if err != nil {
		InternalError(w, r, err)
		return
	}
	JSON(w, http.StatusOK, msgs)
}
