package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// LaventeCareHandler handles LaventeCare CRM endpoints.
type LaventeCareHandler struct {
	store   *store.LaventeCareStore
	pending *store.PendingStore
	userID  string
}

// NewLaventeCareHandler creates a new LaventeCareHandler.
func NewLaventeCareHandler(s *store.LaventeCareStore, pending *store.PendingStore, userID string) *LaventeCareHandler {
	return &LaventeCareHandler{store: s, pending: pending, userID: userID}
}

func parseOptionalUUIDQuery(r *http.Request, key string) (*uuid.UUID, error) {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// Cockpit returns the aggregated LaventeCare dashboard.
// @Summary Get LaventeCare Cockpit
// @Description Returns the aggregated CRM dashboard data
// @Tags LaventeCare
// @Produce json
// @Success 200 {object} model.LCCockpit
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/cockpit [get]
func (h *LaventeCareHandler) Cockpit(w http.ResponseWriter, r *http.Request) {
	cockpit, err := h.store.GetCockpit(r.Context(), h.userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, cockpit)
}

// Billing returns LaventeCare quotes, time entries, invoices and summary.
// @Summary Get LaventeCare Billing
// @Description Returns the commercial LaventeCare workflow: quotes, hours and invoices
// @Tags LaventeCare
// @Produce json
// @Param companyId query string false "Company ID (UUID)"
// @Param limit query int false "Limit count" default(40)
// @Success 200 {object} model.LCBilling
// @Failure 400 {string} string "Invalid companyId"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/billing [get]
func (h *LaventeCareHandler) Billing(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 40)
	companyID, err := parseOptionalUUIDQuery(r, "companyId")
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid companyId")
		return
	}
	billing, err := h.store.GetBilling(r.Context(), h.userID, limit, companyID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, billing)
}

// CreateQuote creates a LaventeCare quote.
// @Summary Create LaventeCare Quote
// @Description Creates a quote draft that can later become an invoice
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCQuoteCreate true "Quote"
// @Success 201 {object} model.LCQuote
// @Failure 400 {string} string "Invalid request body or missing fields"
// @Failure 404 {string} string "Related customer object not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/quotes [post]
func (h *LaventeCareHandler) CreateQuote(w http.ResponseWriter, r *http.Request) {
	var input model.LCQuoteCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Titel == "" || len(input.Lines) == 0 {
		Error(w, http.StatusBadRequest, "titel en minimaal 1 regel zijn verplicht")
		return
	}
	quote, err := h.store.CreateQuote(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant, opdracht of project niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, quote)
}

// UpdateQuoteStatus updates a LaventeCare quote status.
// @Summary Update Quote Status
// @Description Updates a quote status such as verzonden or geaccepteerd
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Quote ID (UUID)"
// @Param request body map[string]string true "Status"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Quote not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/quotes/{id}/status [patch]
func (h *LaventeCareHandler) UpdateQuoteStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid quote ID")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil || input.Status == "" {
		Error(w, http.StatusBadRequest, "Status is verplicht")
		return
	}
	if err := h.store.UpdateQuoteStatus(r.Context(), h.userID, id, input.Status); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Offerte niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateInvoiceFromQuote creates an invoice draft from an accepted LaventeCare quote.
// @Summary Create Invoice From Quote
// @Description Converts an accepted quote to one invoice draft and returns an existing active invoice if it was already converted
// @Tags LaventeCare
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Quote ID (UUID)"
// @Success 201 {object} model.LCInvoice
// @Failure 400 {string} string "Quote is not accepted or has no lines"
// @Failure 404 {string} string "Quote not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/quotes/{id}/invoice [post]
func (h *LaventeCareHandler) CreateInvoiceFromQuote(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid quote ID")
		return
	}
	invoice, err := h.store.CreateInvoiceFromQuote(r.Context(), h.userID, id)
	if err != nil {
		switch err {
		case store.ErrQuoteNotAccepted:
			Error(w, http.StatusBadRequest, "Offerte moet eerst geaccepteerd zijn")
		case store.ErrQuoteHasNoLines:
			Error(w, http.StatusBadRequest, "Offerte heeft geen factuurregels")
		case pgx.ErrNoRows:
			Error(w, http.StatusNotFound, "Offerte niet gevonden")
		default:
			Error(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	JSON(w, http.StatusCreated, invoice)
}

// CreateTimeEntry creates a billable LaventeCare time entry.
// @Summary Create Time Entry
// @Description Logs billable or non-billable work time for a customer/project/workstream
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCTimeEntryCreate true "Time Entry"
// @Success 201 {object} model.LCTimeEntry
// @Failure 400 {string} string "Invalid request body or missing fields"
// @Failure 404 {string} string "Related customer object not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/time-entries [post]
func (h *LaventeCareHandler) CreateTimeEntry(w http.ResponseWriter, r *http.Request) {
	var input model.LCTimeEntryCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Description == "" || input.Minutes <= 0 {
		Error(w, http.StatusBadRequest, "description en minutes zijn verplicht")
		return
	}
	entry, err := h.store.CreateTimeEntry(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant, opdracht of project niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, entry)
}

// CreateInvoice creates a LaventeCare invoice draft.
// @Summary Create Invoice
// @Description Creates an invoice from manual lines or selected time entries
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCInvoiceCreate true "Invoice"
// @Success 201 {object} model.LCInvoice
// @Failure 400 {string} string "Invalid request body or missing lines"
// @Failure 404 {string} string "Related customer object not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/invoices [post]
func (h *LaventeCareHandler) CreateInvoice(w http.ResponseWriter, r *http.Request) {
	var input model.LCInvoiceCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(input.Lines) == 0 && len(input.TimeEntryIDs) == 0 {
		Error(w, http.StatusBadRequest, "minimaal 1 regel of urenregel is verplicht")
		return
	}
	invoice, err := h.store.CreateInvoice(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Factuurbron niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, invoice)
}

// UpdateInvoiceStatus updates an invoice/payment status.
// @Summary Update Invoice Status
// @Description Updates invoice status and optional bunq/payment metadata
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID (UUID)"
// @Param request body model.LCInvoiceStatusUpdate true "Invoice Status"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Invoice not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/invoices/{id}/status [patch]
func (h *LaventeCareHandler) UpdateInvoiceStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid invoice ID")
		return
	}
	var input model.LCInvoiceStatusUpdate
	if err := DecodeJSON(r, &input); err != nil || input.Status == "" {
		Error(w, http.StatusBadRequest, "Status is verplicht")
		return
	}
	if err := h.store.UpdateInvoiceStatus(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Factuur niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// CreateInvoicePaymentRequestAction queues a confirmed bunq payment request for an invoice.
// @Summary Queue Invoice Payment Request
// @Description Creates a pending confirmation action that creates a bunq RequestInquiry after approval
// @Tags LaventeCare
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Invoice ID (UUID)"
// @Success 202 {object} map[string]interface{} "pending action"
// @Failure 400 {string} string "Invalid invoice"
// @Failure 404 {string} string "Invoice not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/invoices/{id}/payment-request [post]
func (h *LaventeCareHandler) CreateInvoicePaymentRequestAction(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid invoice ID")
		return
	}
	invoice, err := h.store.GetInvoice(r.Context(), h.userID, id)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Factuur niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if invoice.Status == "betaald" || invoice.Status == "geannuleerd" {
		Error(w, http.StatusBadRequest, "Voor betaalde of geannuleerde facturen kan geen betaalverzoek worden gemaakt")
		return
	}
	if invoice.TotalCents <= 0 {
		Error(w, http.StatusBadRequest, "Factuurbedrag moet groter zijn dan 0")
		return
	}
	if invoice.ProviderRequestID != nil || invoice.PaymentURL != nil {
		JSON(w, http.StatusOK, map[string]any{
			"confirmationRequired": false,
			"alreadyCreated":       true,
			"invoice":              invoice,
			"message":              "Factuur heeft al een gekoppeld betaalverzoek.",
		})
		return
	}
	if h.pending == nil {
		Error(w, http.StatusInternalServerError, "Bevestigingswachtrij niet beschikbaar")
		return
	}

	args, err := json.Marshal(map[string]string{"invoice_id": id.String()})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	toolName := "laventecareBetaalverzoekMaken"
	summary := cleanPendingSummary(fmt.Sprintf(
		"LaventeCare betaalverzoek maken: %s - %s - %s",
		invoice.InvoiceNumber,
		formatCents(invoice.TotalCents),
		derefString(invoice.CompanyName, "geen klant"),
	))
	existing, err := h.pending.FindPendingByToolArgs(r.Context(), h.userID, toolName, string(args))
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if existing != nil {
		JSON(w, http.StatusAccepted, map[string]any{
			"confirmationRequired": true,
			"pendingActionId":      existing.ID,
			"code":                 existing.Code,
			"toolName":             existing.ToolName,
			"summary":              existing.Summary,
			"expiresAt":            existing.ExpiresAt,
			"message":              fmt.Sprintf("Betaalverzoek stond al klaar. Bevestig via Settings of Telegram met /approve %s.", existing.Code),
		})
		return
	}
	action, err := h.pending.Create(r.Context(), h.userID, "laventecare", toolName, string(args), summary)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusAccepted, map[string]any{
		"confirmationRequired": true,
		"pendingActionId":      action.ID,
		"code":                 action.Code,
		"toolName":             action.ToolName,
		"summary":              action.Summary,
		"expiresAt":            action.ExpiresAt,
		"message":              fmt.Sprintf("Betaalverzoek staat klaar. Bevestig via Settings of Telegram met /approve %s.", action.Code),
	})
}

func derefString(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func cleanPendingSummary(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func formatCents(cents int) string {
	return fmt.Sprintf("EUR %d.%02d", cents/100, cents%100)
}

// ListCompanies returns LaventeCare companies/customer dossiers.
// @Summary List Companies
// @Description Returns LaventeCare customer/company dossiers
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Param q query string false "Search by name or website"
// @Success 200 {array} model.LCCompany
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/companies [get]
func (h *LaventeCareHandler) ListCompanies(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	query := r.URL.Query().Get("q")
	companies, err := h.store.ListCompanies(r.Context(), h.userID, limit, query)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, companies)
}

// CreateCompany creates a LaventeCare customer/company dossier.
// @Summary Create Company
// @Description Creates a reusable customer/company dossier
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCCompanyCreate true "Company Details"
// @Success 201 {object} model.LCCompany
// @Failure 400 {string} string "Invalid request body or missing name"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/companies [post]
func (h *LaventeCareHandler) CreateCompany(w http.ResponseWriter, r *http.Request) {
	var input model.LCCompanyCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Naam == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht")
		return
	}

	company, err := h.store.CreateCompany(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, company)
}

// UpdateCompany modifies a LaventeCare customer/company dossier.
// @Summary Update Company
// @Description Updates a reusable customer/company dossier
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Company ID (UUID)"
// @Param request body model.LCCompanyUpdate true "Company Update"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Company not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/companies/{id} [patch]
func (h *LaventeCareHandler) UpdateCompany(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid company ID")
		return
	}
	var input model.LCCompanyUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.store.UpdateCompany(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListContacts returns LaventeCare contacts.
// @Summary List Contacts
// @Description Returns contacts, optionally filtered by company
// @Tags LaventeCare
// @Produce json
// @Param companyId query string false "Company ID (UUID)"
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCContact
// @Failure 400 {string} string "Invalid companyId"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/contacts [get]
func (h *LaventeCareHandler) ListContacts(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	var companyID *uuid.UUID
	if raw := r.URL.Query().Get("companyId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid companyId")
			return
		}
		companyID = &id
	}
	contacts, err := h.store.ListContacts(r.Context(), h.userID, companyID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, contacts)
}

// CreateContact creates a LaventeCare contact.
// @Summary Create Contact
// @Description Creates a reusable contact for a customer/company
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCContactCreate true "Contact Details"
// @Success 201 {object} model.LCContact
// @Failure 400 {string} string "Invalid request body or missing name"
// @Failure 404 {string} string "Company not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/contacts [post]
func (h *LaventeCareHandler) CreateContact(w http.ResponseWriter, r *http.Request) {
	var input model.LCContactCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Naam == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht")
		return
	}
	contact, err := h.store.CreateContact(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, contact)
}

// UpdateContact modifies a LaventeCare contact.
// @Summary Update Contact
// @Description Updates a reusable contact
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Contact ID (UUID)"
// @Param request body model.LCContactUpdate true "Contact Update"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Contact not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/contacts/{id} [patch]
func (h *LaventeCareHandler) UpdateContact(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid contact ID")
		return
	}
	var input model.LCContactUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.store.UpdateContact(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Contact niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListLeads returns all active leads.
// @Summary List Leads
// @Description Returns all active CRM leads
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCLead
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads [get]
func (h *LaventeCareHandler) ListLeads(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	leads, err := h.store.ListLeads(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, leads)
}

// CreateLead creates a new lead.
// @Summary Create Lead
// @Description Creates a new CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCLeadCreate true "Lead Details"
// @Success 201 {object} model.LCLead
// @Failure 400 {string} string "Invalid request body or missing title"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads [post]
func (h *LaventeCareHandler) CreateLead(w http.ResponseWriter, r *http.Request) {
	var input model.LCLeadCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Titel == "" {
		Error(w, http.StatusBadRequest, "Titel is verplicht")
		return
	}

	lead, err := h.store.CreateLead(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, lead)
}

// UpdateLead modifies lead fields.
// @Summary Update Lead
// @Description Modifies an existing CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Lead ID (UUID)"
// @Param request body model.LCLeadUpdate true "Updated Lead Details"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Lead not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads/{id} [patch]
func (h *LaventeCareHandler) UpdateLead(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid lead ID")
		return
	}

	var input model.LCLeadUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.UpdateLead(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Lead niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConvertLeadToProject converts a lead to a project.
// @Summary Convert Lead to Project
// @Description Converts an existing CRM lead into a project
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Lead ID (UUID)"
// @Param request body model.LCConvertLeadToProject true "Conversion Details"
// @Success 201 {object} model.LCProject
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Lead not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/leads/{id}/convert [post]
func (h *LaventeCareHandler) ConvertLeadToProject(w http.ResponseWriter, r *http.Request) {
	leadID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid lead ID")
		return
	}

	var input model.LCConvertLeadToProject
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	input.LeadID = leadID

	project, err := h.store.ConvertLeadToProject(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Lead niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, project)
}

// ListProjects returns all projects.
// @Summary List Projects
// @Description Returns all CRM projects
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCProject
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/projects [get]
func (h *LaventeCareHandler) ListProjects(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	projects, err := h.store.ListProjects(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, projects)
}

// CreateProject creates a new active project.
// @Summary Create Project
// @Description Creates a new CRM project directly
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCProjectCreate true "Project Details"
// @Success 201 {object} model.LCProject
// @Failure 400 {string} string "Invalid request body or missing name"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/projects [post]
func (h *LaventeCareHandler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var input model.LCProjectCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Naam == "" {
		Error(w, http.StatusBadRequest, "Naam is verplicht")
		return
	}
	companyID, _, err := h.store.ResolveCompanyReference(
		r.Context(),
		h.userID,
		input.CompanyID,
		input.CompanyName,
		input.Website,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	projectToCreate := model.LCProject{
		Naam:            input.Naam,
		CompanyID:       companyID,
		Fase:            input.Fase,
		Status:          input.Status,
		WaardeIndicatie: input.WaardeIndicatie,
		StartDatum:      input.StartDatum,
		Deadline:        input.Deadline,
		Samenvatting:    input.Samenvatting,
	}

	project, err := h.store.CreateProject(r.Context(), h.userID, projectToCreate)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, project)
}

// UpdateProject modifies project fields.
// @Summary Update Project
// @Description Modifies an existing CRM project
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Project ID (UUID)"
// @Param request body model.LCProjectUpdate true "Updated Project Details"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Project not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/projects/{id} [patch]
func (h *LaventeCareHandler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid project ID")
		return
	}

	var input model.LCProjectUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.UpdateProject(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Project niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListWorkstreams returns flexible LaventeCare opdrachten/workstreams.
// @Summary List Workstreams
// @Description Returns LaventeCare opdrachten for small and medium workstreams
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Param includeClosed query bool false "Include closed/completed workstreams"
// @Success 200 {array} model.LCWorkstream
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/workstreams [get]
func (h *LaventeCareHandler) ListWorkstreams(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	includeClosed := r.URL.Query().Get("includeClosed") == "true"
	workstreams, err := h.store.ListWorkstreams(r.Context(), h.userID, limit, includeClosed)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, workstreams)
}

// CreateWorkstream creates a flexible LaventeCare opdracht/workstream.
// @Summary Create Workstream
// @Description Creates a LaventeCare opdracht for flexible small/medium engagements
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCWorkstreamCreate true "Workstream Details"
// @Success 201 {object} model.LCWorkstream
// @Failure 400 {string} string "Invalid request body or missing title"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/workstreams [post]
func (h *LaventeCareHandler) CreateWorkstream(w http.ResponseWriter, r *http.Request) {
	var input model.LCWorkstreamCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Titel == "" {
		Error(w, http.StatusBadRequest, "Titel is verplicht")
		return
	}

	workstream, err := h.store.CreateWorkstream(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, workstream)
}

// UpdateWorkstream modifies a LaventeCare opdracht/workstream.
// @Summary Update Workstream
// @Description Modifies a LaventeCare opdracht
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Workstream ID (UUID)"
// @Param request body model.LCWorkstreamUpdate true "Updated Workstream Details"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Workstream not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/workstreams/{id} [patch]
func (h *LaventeCareHandler) UpdateWorkstream(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid workstream ID")
		return
	}

	var input model.LCWorkstreamUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.store.UpdateWorkstream(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Opdracht niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ConvertWorkstreamToProject promotes a LaventeCare opdracht to a project.
// @Summary Convert Workstream to Project
// @Description Converts a flexible opdracht into a full delivery project
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Workstream ID (UUID)"
// @Param request body model.LCConvertWorkstreamToProject true "Conversion Details"
// @Success 201 {object} model.LCProject
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Workstream not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/workstreams/{id}/convert-project [post]
func (h *LaventeCareHandler) ConvertWorkstreamToProject(w http.ResponseWriter, r *http.Request) {
	workstreamID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid workstream ID")
		return
	}

	var input model.LCConvertWorkstreamToProject
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	input.WorkstreamID = workstreamID

	project, err := h.store.ConvertWorkstreamToProject(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Opdracht niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, project)
}

// ListActions returns open action items.
// @Summary List Action Items
// @Description Returns all open CRM action items
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(8)
// @Success 200 {array} model.LCActionItem
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions [get]
func (h *LaventeCareHandler) ListActions(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 8)
	actions, err := h.store.ListActions(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, actions)
}

// CreateAction creates a new action item.
// @Summary Create Action Item
// @Description Creates a new CRM action item
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCActionCreate true "Action Details"
// @Success 201 {object} model.LCActionItem
// @Failure 400 {string} string "Invalid request body or missing title"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions [post]
func (h *LaventeCareHandler) CreateAction(w http.ResponseWriter, r *http.Request) {
	var input model.LCActionCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.Title == "" {
		Error(w, http.StatusBadRequest, "Title is verplicht")
		return
	}

	action, err := h.store.CreateAction(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, action)
}

// UpdateActionStatus changes an action item's status.
// @Summary Update Action Status
// @Description Changes the status of an action item
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Action ID (UUID)"
// @Param request body map[string]string true "Status Details (e.g. {status: 'done'})"
// @Success 200 {object} map[string]string "status ok"
// @Failure 400 {string} string "Invalid request body or ID"
// @Failure 404 {string} string "Action not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/actions/{id}/status [patch]
func (h *LaventeCareHandler) UpdateActionStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid action ID")
		return
	}

	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil || input.Status == "" {
		Error(w, http.StatusBadRequest, "Status is verplicht")
		return
	}

	if err := h.store.UpdateActionStatus(r.Context(), h.userID, id, input.Status); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Actie niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListDocuments returns the document catalog.
// @Summary List Documents
// @Description Returns all CRM documents in the catalog
// @Tags LaventeCare
// @Produce json
// @Success 200 {array} model.LCDocument
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/documents [get]
func (h *LaventeCareHandler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	docs, err := h.store.ListDocuments(r.Context(), h.userID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, docs)
}

// ListDossierDocuments returns recently generated dossier documents.
// @Summary List Dossier Documents
// @Description Returns generated PDF dossier document history, optionally filtered by lead or project
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(20)
// @Param leadId query string false "Lead ID (UUID)"
// @Param projectId query string false "Project ID (UUID)"
// @Success 200 {array} model.LCDossierDocument
// @Failure 400 {string} string "Invalid query parameter"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/dossier-documents [get]
func (h *LaventeCareHandler) ListDossierDocuments(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 20)
	var leadID *uuid.UUID
	var projectID *uuid.UUID
	var workstreamID *uuid.UUID
	var companyID *uuid.UUID

	if raw := r.URL.Query().Get("leadId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid leadId")
			return
		}
		leadID = &id
	}

	if raw := r.URL.Query().Get("projectId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid projectId")
			return
		}
		projectID = &id
	}

	if raw := r.URL.Query().Get("workstreamId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid workstreamId")
			return
		}
		workstreamID = &id
	}

	if raw := r.URL.Query().Get("companyId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid companyId")
			return
		}
		companyID = &id
	}

	docs, err := h.store.ListDossierDocuments(r.Context(), h.userID, limit, leadID, projectID, workstreamID, companyID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, docs)
}

// CreateDossierDocument logs a generated PDF as a lead/project dossier document.
// @Summary Create Dossier Document
// @Description Logs a generated PDF URL against a lead or project dossier
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCDossierDocumentCreate true "Dossier Document Details"
// @Success 201 {object} model.LCDossierDocument
// @Failure 400 {string} string "Invalid request body or missing required field"
// @Failure 404 {string} string "Lead or project not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/dossier-documents [post]
func (h *LaventeCareHandler) CreateDossierDocument(w http.ResponseWriter, r *http.Request) {
	var input model.LCDossierDocumentCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.DocumentKey == "" || input.Titel == "" || input.PDFURL == "" {
		Error(w, http.StatusBadRequest, "document_key, titel en pdf_url zijn verplicht")
		return
	}

	doc, err := h.store.CreateDossierDocument(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Lead of project niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, doc)
}

// ListActivityEvents returns recent customer timeline events.
// @Summary List Activity Events
// @Description Returns recent LaventeCare customer dossier activity events
// @Tags LaventeCare
// @Produce json
// @Param companyId query string false "Company ID (UUID)"
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCActivityEvent
// @Failure 400 {string} string "Invalid companyId"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/activity [get]
func (h *LaventeCareHandler) ListActivityEvents(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	var companyID *uuid.UUID
	if raw := r.URL.Query().Get("companyId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			Error(w, http.StatusBadRequest, "Invalid companyId")
			return
		}
		companyID = &id
	}

	events, err := h.store.ListActivityEvents(r.Context(), h.userID, limit, companyID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, events)
}

// CreateActivityEvent logs a manual customer dossier timeline event.
// @Summary Create Activity Event
// @Description Logs a customer contact moment, note, decision or project update in the customer dossier timeline
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCActivityEventCreate true "Activity Event"
// @Success 201 {object} model.LCActivityEvent
// @Failure 400 {string} string "Invalid request body or missing required field"
// @Failure 404 {string} string "Related customer object not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/activity [post]
func (h *LaventeCareHandler) CreateActivityEvent(w http.ResponseWriter, r *http.Request) {
	var input model.LCActivityEventCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.CompanyID == uuid.Nil {
		Error(w, http.StatusBadRequest, "company_id is verplicht")
		return
	}
	if input.Title == "" {
		Error(w, http.StatusBadRequest, "title is verplicht")
		return
	}

	event, err := h.store.CreateActivityEvent(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Klant of gekoppeld object niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, event)
}

// ConvertSignalToLead creates a lead from a business signal.
// @Summary Convert Signal to Lead
// @Description Converts a business signal into a CRM lead
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCConvertSignalToLead true "Signal Details"
// @Success 201 {object} map[string]interface{}
// @Failure 400 {string} string "Invalid request body"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/signals/convert-lead [post]
func (h *LaventeCareHandler) ConvertSignalToLead(w http.ResponseWriter, r *http.Request) {
	var input model.LCConvertSignalToLead
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	pijnpunt := input.Subtitle + "\n" + input.ActionHint
	prioriteit := input.Urgency

	lead, err := h.store.CreateLead(r.Context(), h.userID, model.LCLeadCreate{
		Titel:      input.Title,
		Bron:       input.Source,
		SourceID:   &input.SourceID,
		Pijnpunt:   &pijnpunt,
		Prioriteit: &prioriteit,
	})
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, map[string]any{
		"lead":   lead,
		"reused": false,
	})
}

// SeedDocuments upserts the full LaventeCare knowledge document catalog.
// @Summary Seed Documents
// @Description Bulk upserts knowledge documents into the catalog
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body []model.LCDocument true "Documents"
// @Success 200 {object} model.LCSeedResult
// @Failure 400 {string} string "Invalid request body or empty"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/documents/seed [post]
func (h *LaventeCareHandler) SeedDocuments(w http.ResponseWriter, r *http.Request) {
	var docs []model.LCDocument
	if err := DecodeJSON(r, &docs); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if len(docs) == 0 {
		Error(w, http.StatusBadRequest, "Geen documenten ontvangen")
		return
	}

	inserted, updated, err := h.store.SeedDocuments(r.Context(), h.userID, docs)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, model.LCSeedResult{
		Total:    len(docs),
		Inserted: inserted,
		Updated:  updated,
	})
}
