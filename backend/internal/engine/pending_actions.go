package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
	if ai.IsMutatingTool(toolName) && ai.RequiresConfirmation(toolName) {
		summary := summarizePendingTool(toolName, argsJSON)
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
	return executeClaimedPendingAction(ctx, pool, pending, userID, action, googleClient)
}

// ConfirmPendingActionByCode claims and executes a pending action by its short code.
func ConfirmPendingActionByCode(ctx context.Context, pool *pgxpool.Pool, userID, code string, googleClient *google.OAuthClient) (map[string]any, error) {
	pending := store.NewPendingStore(pool)
	action, err := pending.FindByCode(ctx, userID, code)
	if err != nil {
		return nil, err
	}
	claimed, err := pending.Claim(ctx, action.ID, userID)
	if err != nil {
		return nil, err
	}
	return executeClaimedPendingAction(ctx, pool, pending, userID, claimed, googleClient)
}

// CancelPendingAction cancels a pending action by id.
func CancelPendingAction(ctx context.Context, pool *pgxpool.Pool, userID, id string) (map[string]any, error) {
	action, err := store.NewPendingStore(pool).Cancel(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	return pendingActionResult(action, nil, nil), nil
}

func executeClaimedPendingAction(ctx context.Context, pool *pgxpool.Pool, pending *store.PendingStore, userID string, action *store.PendingAction, googleClient *google.OAuthClient) (map[string]any, error) {
	executor := NewHomeBotExecutorWithGoogle(pool, userID, googleClient)
	result := executor.Execute(ctx, action.ToolName, action.ArgsJSON)
	if message := toolResultError(result); message != "" {
		_ = pending.MarkStatus(ctx, action.ID, "failed", &result, &message)
		return pendingActionResult(action, &result, &message), fmt.Errorf("%s", message)
	}
	if err := pending.MarkStatus(ctx, action.ID, "confirmed", &result, nil); err != nil {
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
		return cleanSummary("Email versturen", value("to"), value("subject"))
	case "emailBeantwoorden":
		return cleanSummary("Email beantwoorden", value("gmailId", "emailId", "id"))
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
	case "laventecareLeadMaken":
		return cleanSummary("LaventeCare lead maken", value("titel"))
	case "laventecareLeadBijwerken":
		return cleanSummary("LaventeCare lead bijwerken", value("id"), value("status"))
	case "laventecareLeadNaarProject":
		return cleanSummary("LaventeCare lead naar project", value("lead_id"), value("naam"))
	case "laventecareProjectMaken":
		return cleanSummary("LaventeCare project maken", value("naam"))
	case "laventecareProjectBijwerken":
		return cleanSummary("LaventeCare project bijwerken", value("id"), value("fase", "status"))
	case "laventecareActieMaken":
		return cleanSummary("LaventeCare actie maken", value("title"))
	case "laventecareActieAfronden":
		return cleanSummary("LaventeCare actie afronden", value("id"), value("status"))
	default:
		return cleanSummary("AI-mutatie bevestigen", toolName)
	}
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
