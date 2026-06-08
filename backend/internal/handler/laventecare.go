package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/mail"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
)

// LaventeCareHandler handles LaventeCare CRM endpoints.
type LaventeCareHandler struct {
	store      *store.LaventeCareStore
	pending    *store.PendingStore
	userID     string
	cfg        *config.Config
	mailSender *mail.Sender
}

// NewLaventeCareHandler creates a new LaventeCareHandler.
func NewLaventeCareHandler(s *store.LaventeCareStore, pending *store.PendingStore, userID string, cfg *config.Config) *LaventeCareHandler {
	return &LaventeCareHandler{
		store:      s,
		pending:    pending,
		userID:     userID,
		cfg:        cfg,
		mailSender: mail.NewSender(cfg),
	}
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

// Mailbox returns LaventeCare mail templates and outbound message history.
// @Summary Get LaventeCare Mailbox
// @Description Returns templated mail workspace and outbox history
// @Tags LaventeCare
// @Produce json
// @Success 200 {object} model.LCMailbox
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/mailbox [get]
func (h *LaventeCareHandler) Mailbox(w http.ResponseWriter, r *http.Request) {
	mailbox, err := h.store.GetMailbox(
		r.Context(),
		h.userID,
		queryInt(r, "limit", 40),
		h.cfg.LaventeCareMailConfigured(),
		h.cfg.MicrosoftSenderEmail,
	)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, mailbox)
}

func (h *LaventeCareHandler) CreateMailTemplate(w http.ResponseWriter, r *http.Request) {
	var input model.LCMailTemplateCreate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.SubjectTemplate == "" || input.BodyHTML == "" {
		Error(w, http.StatusBadRequest, "subject_template en body_html zijn verplicht")
		return
	}
	template, err := h.store.CreateMailTemplate(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, template)
}

func (h *LaventeCareHandler) UpdateMailTemplate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "Invalid template ID")
		return
	}
	var input model.LCMailTemplateUpdate
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if err := h.store.UpdateMailTemplate(r.Context(), h.userID, id, input); err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Template niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// SuggestMailContent creates an AI-assisted variable proposal for a LaventeCare mail template.
// @Summary Suggest LaventeCare mail content
// @Description Builds a safe draft context from LaventeCare, agenda, rooster and notes. It does not create or send mail.
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCMailAISuggestionRequest true "Mail AI suggestion request"
// @Success 200 {object} model.LCMailAISuggestion
// @Failure 400 {string} string "Invalid request body"
// @Failure 404 {string} string "Template or context object not found"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/mailbox/ai-suggest [post]
func (h *LaventeCareHandler) SuggestMailContent(w http.ResponseWriter, r *http.Request) {
	var input model.LCMailAISuggestionRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.TemplateID == uuid.Nil {
		Error(w, http.StatusBadRequest, "template_id is verplicht")
		return
	}

	contextBundle, err := h.store.BuildMailAIContext(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Template of gekoppelde context niet gevonden")
			return
		}
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	suggestion := mailAISuggestionFallback(contextBundle, input)
	if strings.TrimSpace(h.cfg.GrokAPIKey) == "" {
		JSON(w, http.StatusOK, suggestion)
		return
	}

	payload, err := json.Marshal(contextBundle)
	if err != nil {
		JSON(w, http.StatusOK, suggestion)
		return
	}

	systemPrompt := `Je bent de LaventeCare mail-assistent van Jeffrey Lavente.
Maak uitsluitend een JSON-object voor een professioneel klantmail-concept.
Gebruik alleen de aangeleverde context. Verzin geen afspraken, bedragen, betaalurls, contactgegevens of toezeggingen.
Vul alleen korte, bruikbare templatevariabelen. Schrijf in helder Nederlands, zakelijk warm, concreet en zonder markdown.
Antwoord exact met JSON in dit schema:
{
  "variables": {"placeholder": "waarde"},
  "subject_hint": "optionele onderwerpregel",
  "briefing": "korte interne samenvatting voor Jeffrey",
  "sources": [{"type":"note|agenda|schedule|action|activity|billing|dossier|laventecare","title":"bron","date":"optioneel","summary":"waarom gebruikt"}],
  "confidence": "hoog|normaal|laag"
}`
	userPrompt := fmt.Sprintf(`Template intent: %s
Toon: %s

Context JSON:
%s

Maak een voorstel voor templatevariabelen. Variabelen moeten aansluiten op de placeholders in subject/body van de template en op gangbare LaventeCare-velden zoals next_step, meeting.summary, meeting.actions, project.update, project.risk, quote.summary, invoice.payment_url, delivery.done, support.summary, change.summary. Houd alles controleerbaar en kort.`,
		strings.TrimSpace(input.Intent), strings.TrimSpace(input.Tone), string(payload))

	client := ai.NewGrokClientWithOptions(h.cfg.GrokAPIKey, h.cfg.GrokModel, h.cfg.GrokReasoningEffort)
	result := client.Chat(r.Context(), systemPrompt, userPrompt, nil, nil, nil)
	if result.OK {
		if parsed, err := parseMailAISuggestion(result.Antwoord, suggestion); err == nil {
			JSON(w, http.StatusOK, parsed)
			return
		}
	}

	JSON(w, http.StatusOK, suggestion)
}

// SendTemplatedMail creates a rendered outbound mail and optionally sends it via Microsoft Graph.
// @Summary Create or send LaventeCare templated mail
// @Description Renders a LaventeCare mail template with customer context. If send=true, sends through Microsoft Graph.
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCMailSendRequest true "Mail send request"
// @Success 201 {object} model.LCMailOutboxItem
// @Failure 400 {string} string "Invalid request body"
// @Failure 404 {string} string "Template or related customer object not found"
// @Failure 503 {string} string "Mail provider not configured"
// @Router /laventecare/mailbox/send-template [post]
func (h *LaventeCareHandler) SendTemplatedMail(w http.ResponseWriter, r *http.Request) {
	var input model.LCMailSendRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if input.TemplateID == uuid.Nil {
		Error(w, http.StatusBadRequest, "template_id is verplicht")
		return
	}

	item, err := h.store.CreateMailFromTemplate(r.Context(), h.userID, input)
	if err != nil {
		if err == pgx.ErrNoRows {
			Error(w, http.StatusNotFound, "Template of gekoppelde klant niet gevonden")
			return
		}
		Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if !input.Send {
		JSON(w, http.StatusCreated, item)
		return
	}
	if !h.cfg.LaventeCareMailConfigured() {
		_ = h.store.MarkMailOutboxFailed(r.Context(), h.userID, item.ID, "Microsoft Graph mail is niet geconfigureerd")
		failed, _ := h.store.GetMailOutboxItem(r.Context(), h.userID, item.ID)
		if failed != nil {
			JSON(w, http.StatusServiceUnavailable, failed)
			return
		}
		Error(w, http.StatusServiceUnavailable, "Microsoft Graph mail is niet geconfigureerd")
		return
	}

	result, err := h.mailSender.Send(r.Context(), mail.SendInput{
		To:      []string{item.ToEmail},
		CC:      item.CC,
		BCC:     item.BCC,
		Subject: item.Subject,
		HTML:    item.BodyHTML,
		Text:    derefModelString(item.BodyText),
	})
	if err != nil {
		_ = h.store.MarkMailOutboxFailed(r.Context(), h.userID, item.ID, err.Error())
		failed, _ := h.store.GetMailOutboxItem(r.Context(), h.userID, item.ID)
		if failed != nil {
			JSON(w, http.StatusBadGateway, failed)
			return
		}
		Error(w, http.StatusBadGateway, err.Error())
		return
	}

	if err := h.store.MarkMailOutboxSent(r.Context(), h.userID, item.ID, result.ProviderMessageID); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item.CompanyID != nil {
		body := fmt.Sprintf("Aan: %s\nTemplate: %s", item.ToEmail, derefModelString(item.TemplateName))
		_, _ = h.store.CreateActivityEvent(r.Context(), h.userID, model.LCActivityEventCreate{
			CompanyID:    *item.CompanyID,
			ContactID:    item.ContactID,
			ProjectID:    item.ProjectID,
			WorkstreamID: item.WorkstreamID,
			EventType:    "email",
			Channel:      "email",
			Title:        "Mail verstuurd: " + item.Subject,
			Body:         &body,
		})
	}
	sent, err := h.store.GetMailOutboxItem(r.Context(), h.userID, item.ID)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, sent)
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

// ListDecisions returns recent LaventeCare decisions.
// @Summary List LaventeCare Decisions
// @Description Returns recent decision log records
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCDecision
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/decisions [get]
func (h *LaventeCareHandler) ListDecisions(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	decisions, err := h.store.ListDecisions(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, decisions)
}

// CreateDecision stores a LaventeCare decision log record.
// @Summary Create LaventeCare Decision
// @Description Creates a decision log record for governance/audit trail
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCDecision true "Decision"
// @Success 201 {object} model.LCDecision
// @Failure 400 {string} string "Invalid request body or missing fields"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/decisions [post]
func (h *LaventeCareHandler) CreateDecision(w http.ResponseWriter, r *http.Request) {
	var input model.LCDecision
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Titel) == "" || strings.TrimSpace(input.Besluit) == "" {
		Error(w, http.StatusBadRequest, "titel en besluit zijn verplicht")
		return
	}
	if strings.TrimSpace(input.Reden) == "" {
		input.Reden = "Niet gespecificeerd"
	}
	decision, err := h.store.CreateDecision(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, decision)
}

// UpdateDecisionStatus updates the status of a LaventeCare decision record.
// @Summary Update LaventeCare Decision Status
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Decision ID"
// @Param request body map[string]string true "Status update"
// @Success 200 {object} map[string]string
// @Failure 400 {string} string "Invalid request"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/decisions/{id}/status [patch]
func (h *LaventeCareHandler) UpdateDecisionStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "ongeldig besluit id")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Status) == "" {
		Error(w, http.StatusBadRequest, "status is verplicht")
		return
	}
	if err := h.store.UpdateDecisionStatus(r.Context(), h.userID, id, input.Status); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListChangeRequests returns open LaventeCare change requests.
// @Summary List LaventeCare Change Requests
// @Description Returns open change requests
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCChangeRequest
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/changes [get]
func (h *LaventeCareHandler) ListChangeRequests(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	changes, err := h.store.ListChangeRequests(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, changes)
}

// CreateChangeRequest stores a LaventeCare change request.
// @Summary Create LaventeCare Change Request
// @Description Creates a scope/planning/budget change request
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body model.LCChangeRequest true "Change request"
// @Success 201 {object} model.LCChangeRequest
// @Failure 400 {string} string "Invalid request body or missing fields"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/changes [post]
func (h *LaventeCareHandler) CreateChangeRequest(w http.ResponseWriter, r *http.Request) {
	var input model.LCChangeRequest
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Titel) == "" || strings.TrimSpace(input.Impact) == "" {
		Error(w, http.StatusBadRequest, "titel en impact zijn verplicht")
		return
	}
	change, err := h.store.CreateChangeRequest(r.Context(), h.userID, input)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, change)
}

// UpdateChangeRequestStatus updates the lifecycle status of a change request.
// @Summary Update LaventeCare Change Request Status
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Change request ID"
// @Param request body map[string]string true "Status update"
// @Success 200 {object} map[string]string
// @Failure 400 {string} string "Invalid request"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/changes/{id}/status [patch]
func (h *LaventeCareHandler) UpdateChangeRequestStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "ongeldig change id")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Status) == "" {
		Error(w, http.StatusBadRequest, "status is verplicht")
		return
	}
	if err := h.store.UpdateChangeRequestStatus(r.Context(), h.userID, id, input.Status); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ListSlaIncidents returns open LaventeCare SLA/support incidents.
// @Summary List LaventeCare SLA Incidents
// @Description Returns open SLA/support incidents
// @Tags LaventeCare
// @Produce json
// @Param limit query int false "Limit count" default(30)
// @Success 200 {array} model.LCSlaIncident
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/sla-incidents [get]
func (h *LaventeCareHandler) ListSlaIncidents(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 30)
	incidents, err := h.store.ListSlaIncidents(r.Context(), h.userID, limit)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, incidents)
}

// CreateSlaIncident stores a LaventeCare SLA/support incident.
// @Summary Create LaventeCare SLA Incident
// @Description Creates an incident for support/SLA tracking
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param request body map[string]interface{} true "SLA incident"
// @Success 201 {object} model.LCSlaIncident
// @Failure 400 {string} string "Invalid request body or missing fields"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/sla-incidents [post]
func (h *LaventeCareHandler) CreateSlaIncident(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ProjectID       *uuid.UUID `json:"project_id"`
		Titel           string     `json:"titel"`
		Prioriteit      string     `json:"prioriteit"`
		Status          string     `json:"status"`
		Kanaal          string     `json:"kanaal"`
		GemeldOp        *string    `json:"gemeld_op"`
		ReactieDeadline *string    `json:"reactie_deadline"`
		Samenvatting    *string    `json:"samenvatting"`
	}
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Titel) == "" {
		Error(w, http.StatusBadRequest, "titel is verplicht")
		return
	}
	gemeldOp, err := parseLaventeCareTime(input.GemeldOp)
	if err != nil {
		Error(w, http.StatusBadRequest, "gemeld_op is ongeldig")
		return
	}
	deadline, err := parseLaventeCareTime(input.ReactieDeadline)
	if err != nil {
		Error(w, http.StatusBadRequest, "reactie_deadline is ongeldig")
		return
	}
	incident := model.LCSlaIncident{
		ProjectID:       input.ProjectID,
		Titel:           input.Titel,
		Prioriteit:      input.Prioriteit,
		Status:          input.Status,
		Kanaal:          input.Kanaal,
		ReactieDeadline: deadline,
		Samenvatting:    input.Samenvatting,
	}
	if gemeldOp != nil {
		incident.GemeldOp = *gemeldOp
	}
	created, err := h.store.CreateSlaIncident(r.Context(), h.userID, incident)
	if err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusCreated, created)
}

// UpdateSlaIncidentStatus updates the lifecycle status of an SLA/support incident.
// @Summary Update LaventeCare SLA Incident Status
// @Tags LaventeCare
// @Accept json
// @Produce json
// @Security ApiKeyAuth
// @Param id path string true "Incident ID"
// @Param request body map[string]string true "Status update"
// @Success 200 {object} map[string]string
// @Failure 400 {string} string "Invalid request"
// @Failure 500 {string} string "Internal Server Error"
// @Router /laventecare/sla-incidents/{id}/status [patch]
func (h *LaventeCareHandler) UpdateSlaIncidentStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		Error(w, http.StatusBadRequest, "ongeldig incident id")
		return
	}
	var input struct {
		Status string `json:"status"`
	}
	if err := DecodeJSON(r, &input); err != nil {
		Error(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(input.Status) == "" {
		Error(w, http.StatusBadRequest, "status is verplicht")
		return
	}
	if err := h.store.UpdateSlaIncidentStatus(r.Context(), h.userID, id, input.Status); err != nil {
		Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func parseLaventeCareTime(value *string) (*time.Time, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	raw := strings.TrimSpace(*value)
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid time")
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

func mailAISuggestionFallback(contextBundle *model.LCMailAIContext, input model.LCMailAISuggestionRequest) model.LCMailAISuggestion {
	variables := map[string]string{}
	for key, value := range contextBundle.ExistingVars {
		mailAIAddVariable(variables, key, value)
	}

	if contextBundle.Company != nil {
		mailAIAddVariable(variables, "company.naam", contextBundle.Company.Naam)
		mailAIAddVariable(variables, "company.website", derefModelString(contextBundle.Company.Website))
		mailAIAddVariable(variables, "company.sector", derefModelString(contextBundle.Company.Sector))
		mailAIAddVariable(variables, "company.volgende_actie", derefModelString(contextBundle.Company.VolgendeActie))
	}
	if contextBundle.Contact != nil {
		mailAIAddVariable(variables, "contact.naam", contextBundle.Contact.Naam)
		mailAIAddVariable(variables, "contact.email", derefModelString(contextBundle.Contact.Email))
		mailAIAddVariable(variables, "contact.rol", derefModelString(contextBundle.Contact.Rol))
	}
	if contextBundle.Project != nil {
		mailAIAddVariable(variables, "project.naam", mailAIMapString(contextBundle.Project, "naam"))
		mailAIAddVariable(variables, "project.status", mailAIMapString(contextBundle.Project, "status"))
		mailAIAddVariable(variables, "project.update", mailAIMapString(contextBundle.Project, "samenvatting"))
		mailAIAddVariable(variables, "project.risk", "Geen expliciete risico's gevonden in de gekoppelde context.")
	}
	if contextBundle.Workstream != nil {
		mailAIAddVariable(variables, "meeting.topic", mailAIMapString(contextBundle.Workstream, "titel"))
		mailAIAddVariable(variables, "quote.summary", mailAIJoinNonEmpty([]string{
			mailAIMapString(contextBundle.Workstream, "doel"),
			mailAIMapString(contextBundle.Workstream, "scope"),
			mailAIMapString(contextBundle.Workstream, "deliverable"),
		}, " "))
		mailAIAddVariable(variables, "project.update", mailAIJoinNonEmpty([]string{
			mailAIMapString(contextBundle.Workstream, "bevindingen"),
			mailAIMapString(contextBundle.Workstream, "volgende_stap"),
		}, " "))
		mailAIAddVariable(variables, "next_step", mailAIMapString(contextBundle.Workstream, "volgende_stap"))
	}
	if contextBundle.Quote != nil {
		mailAIAddVariable(variables, "quote.number", mailAIMapString(contextBundle.Quote, "quote_number"))
		mailAIAddVariable(variables, "quote.summary", mailAIJoinNonEmpty([]string{
			mailAIMapString(contextBundle.Quote, "titel"),
			mailAIMapString(contextBundle.Quote, "total"),
			mailAIMapString(contextBundle.Quote, "notes"),
		}, " - "))
	}
	if contextBundle.Invoice != nil {
		mailAIAddVariable(variables, "invoice.number", mailAIMapString(contextBundle.Invoice, "invoice_number"))
		mailAIAddVariable(variables, "invoice.amount", mailAIMapString(contextBundle.Invoice, "total"))
		mailAIAddVariable(variables, "invoice.due_date", mailAIMapString(contextBundle.Invoice, "due_date"))
		mailAIAddVariable(variables, "invoice.payment_url", mailAIMapString(contextBundle.Invoice, "payment_url"))
	}

	if variables["meeting.summary"] == "" {
		mailAIAddVariable(variables, "meeting.summary", mailAIItemsLine(contextBundle.Activity, 2))
	}
	if variables["meeting.actions"] == "" {
		mailAIAddVariable(variables, "meeting.actions", mailAIItemsLine(contextBundle.Actions, 3))
	}
	if variables["delivery.done"] == "" {
		mailAIAddVariable(variables, "delivery.done", mailAIItemsLine(contextBundle.Dossier, 2))
	}
	if variables["support.summary"] == "" {
		mailAIAddVariable(variables, "support.summary", mailAIItemsLine(contextBundle.Notes, 2))
	}
	if variables["next_step"] == "" && len(contextBundle.Actions) > 0 {
		mailAIAddVariable(variables, "next_step", contextBundle.Actions[0].Title)
	}
	if variables["next_step"] == "" {
		mailAIAddVariable(variables, "next_step", "Ik hoor graag welke vervolgstap voor jou het beste past.")
	}

	sources := mailAISourcesFromContext(contextBundle)
	confidence := "laag"
	if contextBundle.Company != nil || contextBundle.Contact != nil {
		confidence = "normaal"
	}
	if len(sources) >= 3 && (contextBundle.Project != nil || contextBundle.Workstream != nil) {
		confidence = "hoog"
	}

	subjectHint := ""
	if contextBundle.Template != nil {
		target := "LaventeCare"
		if contextBundle.Company != nil {
			target = contextBundle.Company.Naam
		}
		subjectHint = fmt.Sprintf("%s - %s", contextBundle.Template.Name, target)
	}
	briefing := fmt.Sprintf("Contextvoorstel op basis van %d bron(nen). Controleer bedragen, deadlines en klantafspraken voordat je verzendt.", len(sources))
	if strings.TrimSpace(input.Intent) != "" {
		briefing = briefing + " Intent: " + strings.TrimSpace(input.Intent) + "."
	}

	return model.LCMailAISuggestion{
		Variables:   variables,
		SubjectHint: cleanMailAIStringPtr(&subjectHint),
		Briefing:    briefing,
		Sources:     sources,
		Confidence:  confidence,
		GeneratedAt: time.Now().UTC(),
	}
}

func parseMailAISuggestion(raw string, fallback model.LCMailAISuggestion) (model.LCMailAISuggestion, error) {
	payload := extractMailAIJSON(raw)
	var parsed model.LCMailAISuggestion
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return fallback, err
	}
	if parsed.GeneratedAt.IsZero() {
		parsed.GeneratedAt = time.Now().UTC()
	}
	merged := fallback
	for key, value := range parsed.Variables {
		mailAIAddVariable(merged.Variables, key, value)
	}
	if parsed.SubjectHint != nil && strings.TrimSpace(*parsed.SubjectHint) != "" {
		merged.SubjectHint = cleanMailAIStringPtr(parsed.SubjectHint)
	}
	if strings.TrimSpace(parsed.Briefing) != "" {
		merged.Briefing = strings.TrimSpace(parsed.Briefing)
	}
	if len(parsed.Sources) > 0 {
		merged.Sources = parsed.Sources
	}
	merged.Confidence = mailAIConfidence(parsed.Confidence, fallback.Confidence)
	merged.GeneratedAt = parsed.GeneratedAt
	return merged, nil
}

func extractMailAIJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		return raw[start : end+1]
	}
	return raw
}

func mailAISourcesFromContext(contextBundle *model.LCMailAIContext) []model.LCMailAISource {
	sources := []model.LCMailAISource{}
	addItems := func(items []model.LCMailAIContextItem, max int) {
		for _, item := range items {
			if len(sources) >= 10 || max <= 0 {
				return
			}
			max--
			sources = append(sources, model.LCMailAISource{
				Type:    item.Type,
				Title:   item.Title,
				Date:    item.Date,
				Summary: mailAIJoinNonEmpty([]string{item.Status, item.Priority, item.Summary}, " - "),
			})
		}
	}
	addItems(contextBundle.Actions, 3)
	addItems(contextBundle.Agenda, 2)
	addItems(contextBundle.Notes, 3)
	addItems(contextBundle.Activity, 2)
	addItems(contextBundle.Billing, 2)
	addItems(contextBundle.Dossier, 1)
	addItems(contextBundle.Schedule, 1)
	if len(sources) == 0 {
		title := "LaventeCare context"
		if contextBundle.Company != nil {
			title = contextBundle.Company.Naam
		}
		sources = append(sources, model.LCMailAISource{Type: "laventecare", Title: title, Summary: "Geen extra notities of agenda-items gevonden."})
	}
	return sources
}

func mailAIItemsLine(items []model.LCMailAIContextItem, max int) string {
	parts := []string{}
	for _, item := range items {
		if len(parts) >= max {
			break
		}
		parts = append(parts, mailAIJoinNonEmpty([]string{item.Title, item.Date, item.Summary}, " - "))
	}
	return mailAIJoinNonEmpty(parts, "; ")
}

func mailAIAddVariable(values map[string]string, key, value string) {
	key = strings.ToLower(strings.TrimSpace(key))
	value = strings.TrimSpace(value)
	if key == "" || value == "" {
		return
	}
	values[key] = value
}

func mailAIMapString(values map[string]any, key string) string {
	raw, ok := values[key]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case *string:
		return derefModelString(value)
	case []string:
		return strings.Join(value, ", ")
	default:
		return strings.TrimSpace(fmt.Sprint(value))
	}
}

func mailAIJoinNonEmpty(values []string, separator string) string {
	parts := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && value != "<nil>" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, separator)
}

func mailAIConfidence(value, fallback string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "hoog", "normaal", "laag":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return fallback
	}
}

func cleanMailAIStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func derefModelString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
