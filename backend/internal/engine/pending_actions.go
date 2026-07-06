package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrPendingActionNotFound is returned by ConfirmPendingAction(ByCode) and
// CancelPendingAction when the code/id doesn't match any currently-pending
// action (unknown, already claimed/cancelled, or expired) — as opposed to a
// genuine unexpected error. Distinguishing this lets callers show a specific,
// actionable Dutch message instead of a raw "no rows in result set".
var ErrPendingActionNotFound = errors.New("pending actie niet gevonden, al bevestigd/geannuleerd, of verlopen")

// ConfirmingExecutor turns protected mutating tools into pending actions.
type ConfirmingExecutor struct {
	pool     *pgxpool.Pool
	userID   string
	agentID  string
	delegate ai.ToolExecutor
	pending  *store.PendingStore
}

func NewConfirmingExecutor(pool *pgxpool.Pool, userID, agentID string, delegate ai.ToolExecutor) *ConfirmingExecutor {
	return &ConfirmingExecutor{
		pool:     pool,
		userID:   userID,
		agentID:  agentID,
		delegate: delegate,
		pending:  store.NewPendingStore(pool),
	}
}

func (e *ConfirmingExecutor) Execute(ctx context.Context, toolName string, argsJSON string) string {
	// Hard authorization gate at execution time: the model-supplied tool name is
	// untrusted. GetToolsForAgent only filters which tools are advertised, so a
	// hallucinated/injected call to a tool outside this agent's policy must be
	// refused here rather than dispatched.
	if !ai.IsToolAllowed(e.agentID, toolName) {
		return jsonString(map[string]any{
			"error": fmt.Sprintf("Tool '%s' is niet toegestaan voor agent '%s'.", toolName, e.agentID),
		})
	}
	if ai.IsMutatingTool(toolName) && ai.RequiresConfirmation(toolName) {
		// Reuse an existing identical pending action instead of creating a
		// duplicate. Without this, a retried tool call, a repeated user
		// request, or a multi-step plan issuing the same mutating call twice
		// in one turn leaves several near-identical pending actions with
		// different confirmation codes cluttering /pending — confusing to
		// scan on a phone and risking confirmation of the wrong (stale) one.
		if existing, err := e.pending.FindPendingByToolArgs(ctx, e.userID, toolName, argsJSON); err == nil && existing != nil {
			return jsonString(map[string]any{
				"confirmationRequired": true,
				"pendingActionId":      existing.ID,
				"code":                 existing.Code,
				"toolName":             existing.ToolName,
				"summary":              existing.Summary,
				"expiresAt":            existing.ExpiresAt,
				"message":              fmt.Sprintf("Deze actie staat al klaar ter bevestiging. Gebruik /approve %s, /reject %s of open Settings.", existing.Code, existing.Code),
			})
		}

		summary := summarizePendingTool(toolName, argsJSON)
		switch toolName {
		case "laventecareBetaalverzoekMaken":
			summary = e.enrichPaymentRequestSummary(ctx, argsJSON, summary)
		case "afspraakMaken", "afspraakBewerken":
			summary = e.enrichAppointmentSummary(ctx, toolName, argsJSON, summary)
		}
		action, err := e.pending.Create(ctx, e.userID, e.agentID, toolName, argsJSON, summary)
		if err != nil {
			return fmt.Sprintf(`{"error":"Bevestigingsactie aanmaken mislukt: %s"}`, err.Error())
		}
		return jsonString(map[string]any{
			"confirmationRequired": true,
			"pendingActionId":      action.ID,
			"code":                 action.Code,
			"toolName":             action.ToolName,
			"summary":              action.Summary,
			"expiresAt":            action.ExpiresAt,
			"message":              fmt.Sprintf("Actie staat klaar ter bevestiging. Gebruik /approve %s, /reject %s of open Settings.", action.Code, action.Code),
		})
	}
	return e.delegate.Execute(ctx, toolName, argsJSON)
}

// ConfirmPendingAction claims and executes a pending action.
func ConfirmPendingAction(ctx context.Context, pool *pgxpool.Pool, userID, id string, googleClient *google.OAuthClient) (map[string]any, error) {
	pending := store.NewPendingStore(pool)
	action, err := pending.Claim(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if action == nil {
		return nil, ErrPendingActionNotFound
	}
	return executeClaimedPendingAction(ctx, pool, pending, userID, action, googleClient)
}

// ConfirmPendingActionByCode claims and executes a pending action by its short code.
func ConfirmPendingActionByCode(ctx context.Context, pool *pgxpool.Pool, userID, code string, googleClient *google.OAuthClient) (map[string]any, error) {
	pending := store.NewPendingStore(pool)
	action, err := pending.FindByCode(ctx, userID, code)
	if err != nil {
		return nil, err
	}
	if action == nil {
		return nil, ErrPendingActionNotFound
	}
	claimed, err := pending.Claim(ctx, action.ID, userID)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return nil, ErrPendingActionNotFound
	}
	return executeClaimedPendingAction(ctx, pool, pending, userID, claimed, googleClient)
}

// CancelPendingAction cancels a pending action by id.
func CancelPendingAction(ctx context.Context, pool *pgxpool.Pool, userID, id string) (map[string]any, error) {
	action, err := store.NewPendingStore(pool).Cancel(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if action == nil {
		return nil, ErrPendingActionNotFound
	}
	return pendingActionResult(action, nil, nil), nil
}

func executeClaimedPendingAction(ctx context.Context, pool *pgxpool.Pool, pending *store.PendingStore, userID string, action *store.PendingAction, googleClient *google.OAuthClient) (map[string]any, error) {
	executor := NewHomeBotExecutorWithGoogle(pool, userID, googleClient)
	result := executor.Execute(ctx, action.ToolName, action.ArgsJSON)
	if message := toolResultError(result); message != "" {
		// The tool failure (message) is the actionable error for the user —
		// keep returning that even if persisting it also fails, rather than
		// replacing it with a confusing DB-persistence error. But a failed
		// MarkStatus here leaves the row stuck at 'confirmed' with no
		// failure recorded (Claim already set 'confirmed' before this ran),
		// so it silently vanishes from /pending with zero trail — log it.
		if markErr := pending.MarkStatus(ctx, action.ID, userID, "failed", &result, &message); markErr != nil {
			slog.Warn("pending action mark-failed also failed", "actionID", action.ID, "toolError", message, "markStatusError", markErr)
		}
		return pendingActionResult(action, &result, &message), fmt.Errorf("%s", message)
	}
	if err := pending.MarkStatus(ctx, action.ID, userID, "confirmed", &result, nil); err != nil {
		return nil, err
	}
	return pendingActionResult(action, &result, nil), nil
}

func pendingActionResult(action *store.PendingAction, result, errMsg *string) map[string]any {
	status := action.Status
	if errMsg != nil {
		status = "failed"
	} else if result != nil {
		status = "confirmed"
	}
	return map[string]any{
		"ok":        errMsg == nil,
		"id":        action.ID,
		"agentId":   action.AgentID,
		"toolName":  action.ToolName,
		"summary":   action.Summary,
		"code":      action.Code,
		"status":    status,
		"expiresAt": action.ExpiresAt.Format(time.RFC3339),
		"result":    result,
		"error":     errMsg,
	}
}

func toolResultError(result string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return ""
	}
	value, ok := parsed["error"]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

// enrichPaymentRequestSummary turns the opaque invoice UUID in a bunq
// payment-request confirmation into the human-meaningful invoice number, amount
// and customer, so the user confirms a clear action rather than a UUID.
func (e *ConfirmingExecutor) enrichPaymentRequestSummary(ctx context.Context, argsJSON, fallback string) string {
	var args struct {
		InvoiceID string `json:"invoice_id"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fallback
	}
	id, err := uuid.Parse(strings.TrimSpace(args.InvoiceID))
	if err != nil {
		return fallback
	}
	inv, err := store.NewLaventeCareStore(&store.DB{Pool: e.pool}).GetInvoice(ctx, e.userID, id)
	if err != nil || inv == nil {
		return fallback
	}
	who := ""
	if inv.CompanyName != nil && strings.TrimSpace(*inv.CompanyName) != "" {
		who = " aan " + strings.TrimSpace(*inv.CompanyName)
	}
	return fmt.Sprintf("Betaalverzoek factuur %s (€%.2f)%s", inv.InvoiceNumber, float64(inv.TotalCents)/100, who)
}

// enrichAppointmentSummary appends a work-shift conflict warning (if any) to
// the confirmation summary shown BEFORE the user approves — not just to the
// eventual DB record — so a double-booking is visible at the moment they
// decide, not only discoverable afterward in the executed result.
func (e *ConfirmingExecutor) enrichAppointmentSummary(ctx context.Context, toolName, argsJSON, fallback string) string {
	var args struct {
		EventID      string `json:"eventId"`
		EventIDDB    string `json:"event_id"`
		StartDatum   string `json:"startDatum"`
		StartDatumDB string `json:"start_datum"`
		StartIso     string `json:"startIso"`
		StartTijd    string `json:"startTijd"`
		StartTijdDB  string `json:"start_tijd"`
		EindDatum    string `json:"eindDatum"`
		EindDatumDB  string `json:"eind_datum"`
		EindIso      string `json:"eindIso"`
		EindTijd     string `json:"eindTijd"`
		EindTijdDB   string `json:"eind_tijd"`
		Heledag      *bool  `json:"heledag"`
		AllDay       *bool  `json:"allDay"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return fallback
	}
	startDatum := firstNonEmpty(args.StartDatum, args.StartDatumDB, args.StartIso)
	eindDatum := firstNonEmpty(args.EindDatum, args.EindDatumDB, args.EindIso)
	startTijd := firstNonEmpty(args.StartTijd, args.StartTijdDB)
	eindTijd := firstNonEmpty(args.EindTijd, args.EindTijdDB)
	var heledag bool
	heledagSet := args.Heledag != nil || args.AllDay != nil
	if heledagSet {
		heledag = (args.Heledag != nil && *args.Heledag) || (args.AllDay != nil && *args.AllDay)
	}

	// afspraakBewerken supports partial edits — argsJSON only carries the
	// fields the model actually wants to change. Without merging in the
	// existing event, a time-only-untouched edit (e.g. only changing
	// locatie) would run the conflict check against empty start/end and
	// silently skip the pre-approval warning even though execution later
	// (against the merged record) would have caught it.
	if toolName == "afspraakBewerken" {
		eventID := firstNonEmpty(args.EventID, args.EventIDDB)
		if eventID != "" {
			eventStore := store.NewPersonalEventStore(&store.DB{Pool: e.pool})
			if existing, err := eventStore.GetByUserEventID(ctx, e.userID, eventID); err == nil {
				if startDatum == "" {
					startDatum = existing.StartDatum
				}
				if eindDatum == "" {
					eindDatum = existing.EindDatum
				}
				if startTijd == "" {
					startTijd = optionalPtrValue(existing.StartTijd)
				}
				if eindTijd == "" {
					eindTijd = optionalPtrValue(existing.EindTijd)
				}
				if !heledagSet {
					heledag = existing.Heledag
				}
			}
		}
	}
	if eindDatum == "" {
		eindDatum = startDatum
	}

	scheduleStore := store.NewScheduleStore(&store.DB{Pool: e.pool})
	conflict := findDienstConflict(ctx, scheduleStore, e.userID, startDatum, startTijd, eindDatum, eindTijd, heledag)
	if conflict == "" {
		return fallback
	}
	return fallback + " — ⚠️ " + conflict
}

func summarizePendingTool(toolName, argsJSON string) string {
	var args map[string]any
	_ = json.Unmarshal([]byte(argsJSON), &args)

	value := func(keys ...string) string {
		for _, key := range keys {
			if raw, ok := args[key]; ok && raw != nil {
				if s := strings.TrimSpace(fmt.Sprint(raw)); s != "" {
					return s
				}
			}
		}
		return ""
	}

	switch toolName {
	case "afspraakMaken":
		return cleanSummary("Afspraak maken", value("titel", "title"), value("startDatum", "startIso"), value("startTijd"))
	case "afspraakBewerken":
		return cleanSummary("Afspraak bewerken", value("eventId"), value("titel", "title"), value("startDatum", "startIso"))
	case "afspraakVerwijderen":
		return cleanSummary("Afspraak verwijderen", value("eventId"))
	case "markeerGelezen":
		return cleanSummary("Email gelezen-status wijzigen", value("gmailId", "emailId", "id"))
	case "markeerSter":
		return cleanSummary("Email ster wijzigen", value("gmailId", "emailId", "id"))
	case "verwijderEmail":
		return cleanSummary("Email verwijderen", value("gmailId", "emailId", "id"))
	case "emailVersturen":
		return cleanSummary("Email versturen", value("to"), value("subject"), bodyPreview(value("body")))
	case "emailBeantwoorden":
		return cleanSummary("Email beantwoorden", value("gmailId", "emailId", "id"), bodyPreview(value("body")))
	case "bulkMarkeerGelezen":
		return cleanSummary("Meerdere emails gelezen-status wijzigen", value("query"))
	case "bulkVerwijder":
		return cleanSummary("Meerdere emails verwijderen", value("query"))
	case "inboxOpruimen":
		return cleanSummary("Inbox opruimen", value("query"), value("action"))
	case "categorieWijzigen":
		return cleanSummary("Transactie categoriseren", value("transactionId", "id"), value("categorie"))
	case "bulkCategoriseren":
		return cleanSummary("Transacties bulk categoriseren", value("categorie"))
	case "notitieBewerken":
		return cleanSummary("Notitie bewerken", value("id"), value("titel"))
	case "notitieArchiveren":
		return cleanSummary("Notitie archiveren", value("id"))
	case "bulkArchiveerNotities":
		return cleanSummary("Meerdere notities archiveren", value("ids"))
	case "laventecareBetaalverzoekMaken":
		return cleanSummary("LaventeCare betaalverzoek maken", value("invoice_id"))
	case "laventecareKlantMaken":
		return cleanSummary("LaventeCare klant maken", value("naam"))
	case "laventecareKlantBijwerken":
		return cleanSummary("LaventeCare klant bijwerken", value("id"), value("naam", "status"))
	case "laventecareContactMaken":
		return cleanSummary("LaventeCare contact maken", value("naam"), value("company_id"))
	case "contactMaken":
		return cleanSummary("Contact aanmaken", value("display_name", "naam"))
	case "contactBijwerken":
		return cleanSummary("Contact bijwerken", value("contact_id"), value("display_name"))
	case "contactFeitOnthouden":
		return cleanSummary("Feit onthouden bij contact", value("contact_id"), bodyPreview(value("fact")))
	case "laventecareLeadMaken":
		return cleanSummary("LaventeCare lead maken", value("titel"))
	case "laventecareLeadBijwerken":
		return cleanSummary("LaventeCare lead bijwerken", value("id"), value("status"))
	case "laventecareLeadNaarProject":
		return cleanSummary("LaventeCare lead naar project", value("lead_id"), value("naam"))
	case "laventecareOpdrachtMaken":
		return cleanSummary("LaventeCare opdracht maken", value("titel"), value("type"))
	case "laventecareOpdrachtBijwerken":
		return cleanSummary("LaventeCare opdracht bijwerken", value("id"), value("status"))
	case "laventecareOpdrachtNaarProject":
		return cleanSummary("LaventeCare opdracht naar project", value("workstream_id"), value("naam"))
	case "laventecareProjectMaken":
		return cleanSummary("LaventeCare project maken", value("naam"))
	case "laventecareProjectBijwerken":
		return cleanSummary("LaventeCare project bijwerken", value("id"), value("fase", "status"))
	case "laventecareActieMaken":
		return cleanSummary("LaventeCare actie maken", value("title"))
	case "laventecareActieAfronden":
		return cleanSummary("LaventeCare actie afronden", value("id"), value("status"))
	case "laventecareBesluitMaken":
		return cleanSummary("LaventeCare besluit vastleggen", value("titel"), value("datum"))
	case "laventecareChangeRequestMaken":
		return cleanSummary("LaventeCare change request maken", value("titel"), value("impact"))
	case "laventecareSlaIncidentMaken":
		return cleanSummary("LaventeCare SLA-incident registreren", value("titel"), value("prioriteit"))
	default:
		return cleanSummary("AI-mutatie bevestigen", toolName)
	}
}

// bodyPreview renders a short, single-line preview of a mail body so the
// /approve confirmation actually shows what will be sent, not just the
// recipient/subject — the AI drafts real emails to real clients/leads, and
// approving blind on subject alone means a typo, wrong tone, or wrong price
// in the body would otherwise go out with only a subject-line rubber stamp.
func bodyPreview(body string) string {
	body = collapseWhitespace(body)
	if body == "" {
		return ""
	}
	return `"` + truncateRunes(body, 350) + `"`
}

func cleanSummary(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, ": ")
}

func jsonString(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Sprintf(`{"error":"JSON fout: %s"}`, err.Error())
	}
	return string(data)
}
