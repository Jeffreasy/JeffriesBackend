package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

type SettingsHandler struct {
	db       *store.DB
	telegram *telegram.Client
	cfg      *config.Config
}

func NewSettingsHandler(db *store.DB, telegram *telegram.Client, cfg *config.Config) *SettingsHandler {
	return &SettingsHandler{db: db, telegram: telegram, cfg: cfg}
}

// Overview returns a summary of the entire system state for the Settings page.
// @Summary Get system overview
// @Description Returns aggregate data and statistics across all modules for the settings dashboard
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /settings/overview [get]
func (h *SettingsHandler) Overview(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Queries to get the required counts
	var totalDevices, onlineDevices, onDevices, totalRooms, unassignedDevices int
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices`).Scan(&totalDevices)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE status = 'online'`).Scan(&onlineDevices)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE current_state->>'on' = 'true'`).Scan(&onDevices)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM rooms`).Scan(&totalRooms)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM devices WHERE room_id IS NULL`).Scan(&unassignedDevices)

	var activeAutomations, totalAutomations int
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM automations`).Scan(&totalAutomations)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM automations WHERE enabled = true`).Scan(&activeAutomations)

	var pendingCommands, processingCommands, failedCommands int
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'pending'`).Scan(&pendingCommands)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'processing'`).Scan(&processingCommands)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'failed'`).Scan(&failedCommands)

	var bridgeLastSeen *time.Time
	_ = h.db.Pool.QueryRow(ctx, `SELECT MAX(last_seen) FROM devices`).Scan(&bridgeLastSeen)
	bridgeOnline := bridgeLastSeen != nil && time.Since(bridgeLastSeen.UTC()) <= 10*time.Minute
	bridgeStatus := "Offline"
	if bridgeOnline {
		bridgeStatus = "Active"
	}

	var totalSchedule, upcomingSchedule int
	var importedAt *time.Time
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM schedule`).Scan(&totalSchedule)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM schedule WHERE start_datum >= CURRENT_DATE`).Scan(&upcomingSchedule)
	_ = h.db.Pool.QueryRow(ctx, `SELECT MAX(imported_at) FROM schedule_meta`).Scan(&importedAt)

	var totalEmails, unreadEmails int
	var lastFullSync *time.Time
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM emails`).Scan(&totalEmails)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM emails WHERE NOT is_gelezen`).Scan(&unreadEmails)
	_ = h.db.Pool.QueryRow(ctx, `SELECT MAX(synced_at) FROM emails`).Scan(&lastFullSync)

	var upcomingPersonalEvents int
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM personal_events WHERE eind_datum >= CURRENT_DATE`).Scan(&upcomingPersonalEvents)

	var totalNotes, activeHabits int
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM notes WHERE NOT is_archived`).Scan(&totalNotes)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM habits WHERE is_actief = true`).Scan(&activeHabits)

	// Build response object
	resp := map[string]any{
		"account": map[string]any{
			"name":  "Jeffries Home",
			"email": "jeffrey@jeffreasy.nl", // Dummy email placeholder since Clerk handles this on frontend
		},
		"devices": map[string]any{
			"total":   totalDevices,
			"online":  onlineDevices,
			"offline": totalDevices - onlineDevices,
			"on":      onDevices,
		},
		"rooms": map[string]any{
			"total":             totalRooms,
			"unassignedDevices": unassignedDevices,
		},
		"automations": map[string]any{
			"active": activeAutomations,
			"total":  totalAutomations,
		},
		"commands": map[string]any{
			"pending":    pendingCommands,
			"processing": processingCommands,
			"failed":     failedCommands,
		},
		"schedule": map[string]any{
			"total":      totalSchedule,
			"upcoming":   upcomingSchedule,
			"importedAt": importedAt,
		},
		"email": map[string]any{
			"total":        totalEmails,
			"unread":       unreadEmails,
			"lastFullSync": lastFullSync,
		},
		"personalEvents": map[string]any{
			"upcoming": upcomingPersonalEvents,
		},
		"data": map[string]any{
			"notes":        totalNotes,
			"activeHabits": activeHabits,
		},
		"integrations": map[string]any{
			"backend":                  true,
			"legacyHttpSecret":         configuredSecret(h.cfg.HomeappGASSecret),
			"localBridge":              h.cfg.QueueLightCommands() && bridgeOnline,
			"telegramBot":              h.cfg.TelegramBotEnabled && h.telegram != nil,
			"telegramOwner":            configuredValue(h.cfg.TelegramChatID),
			"telegramWebhookSecret":    configuredSecret(h.cfg.TelegramBridgeSecret),
			"telegramMode":             "long_polling",
			"telegramWebApp":           configuredValue(h.cfg.TelegramWebAppURL),
			"grok":                     configuredValue(h.cfg.GrokAPIKey),
			"grokModel":                h.cfg.GrokModel,
			"grokReasoningEffort":      h.cfg.GrokReasoningEffort,
			"groq":                     configuredValue(h.cfg.GroqAPIKey),
			"googleOAuth":              googleOAuthConfigured(h.cfg),
			"googleCalendar":           googleOAuthConfigured(h.cfg),
			"googleCalendarAutoSync":   h.cfg.GoogleCalendarEnabled && googleOAuthConfigured(h.cfg),
			"gmail":                    googleOAuthConfigured(h.cfg),
			"gmailAutoSync":            h.cfg.GmailEnabled && googleOAuthConfigured(h.cfg),
			"todoist":                  h.cfg.TodoistEnabled && configuredValue(h.cfg.TodoistAPIToken),
			"queueLightCommands":       h.cfg.QueueLightCommands(),
			"startBackgroundEngine":    h.cfg.StartBackgroundEngine,
			"engineCrons":              h.cfg.EngineCronsEnabled,
			"engineAutomations":        h.cfg.EngineAutomationsEnabled,
			"engineCommandPoller":      h.cfg.EngineCommandPollerEnabled,
			"engineStatusPoll":         h.cfg.EngineStatusPollEnabled,
			"bridgeStatusPoll":         h.cfg.BridgeStatusPollEnabled,
			"googlePersonalCalendars":  configuredValue(h.cfg.PersonalCalendarIDs),
			"sdbCalendar":              configuredValue(h.cfg.SDBCalendarID),
			"todoistProjectConfigured": configuredValue(h.cfg.TodoistProjectID),
		},
		"sync": map[string]any{},
		"bridge": map[string]any{
			"online":             bridgeOnline,
			"status":             bridgeStatus,
			"lastSeenAt":         bridgeLastSeen,
			"commandsPending":    pendingCommands,
			"commandsProcessing": processingCommands,
			"commandsFailed":     failedCommands,
			"lastError":          nil,
		},
	}

	JSON(w, http.StatusOK, resp)
}

// Backup returns a JSON dump of the user's data.
// @Summary Export user data
// @Description Generates a JSON backup of all user data
// @Tags Settings
// @Produce json
// @Param userId query string true "User ID"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {string} string "userId required"
// @Router /settings/backup [get]
func (h *SettingsHandler) Backup(w http.ResponseWriter, r *http.Request) {
	// For now, we return a simple JSON showing backup was initiated.
	// In a real scenario, this would query all tables and dump them.
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}

	// Just a structural dump for now
	dump := map[string]any{
		"version":    "1.0",
		"userId":     userID,
		"exportedAt": time.Now().Format(time.RFC3339),
		"message":    "Backup functionaliteit wordt geïmplementeerd in fase 2.",
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"jeffries-homeapp-backup.json\"")
	json.NewEncoder(w).Encode(dump)
}

// TelegramStatus returns the status of the Telegram bot.
// @Summary Get Telegram bot status
// @Description Returns connection status and configuration info for the Telegram bot
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Failure 500 {string} string "Telegram client not configured"
// @Router /settings/telegram/status [get]
func (h *SettingsHandler) TelegramStatus(w http.ResponseWriter, r *http.Request) {
	resp := map[string]any{
		"ok":                       false,
		"enabled":                  h.cfg.TelegramBotEnabled,
		"mode":                     "long_polling",
		"tokenConfigured":          configuredValue(h.cfg.TelegramBotToken),
		"ownerConfigured":          configuredValue(h.cfg.TelegramChatID),
		"ownerChatSuffix":          maskedSuffix(h.cfg.TelegramChatID, 4),
		"webhookSecretConfigured":  configuredSecret(h.cfg.TelegramBridgeSecret),
		"webAppUrlConfigured":      configuredValue(h.cfg.TelegramWebAppURL),
		"webAppUrl":                h.cfg.TelegramWebAppURL,
		"backgroundEngineEnabled":  h.cfg.StartBackgroundEngine,
		"telegramPollerConfigured": h.cfg.StartBackgroundEngine && h.cfg.TelegramBotEnabled && configuredValue(h.cfg.TelegramBotToken),
		"grokConfigured":           configuredValue(h.cfg.GrokAPIKey),
		"grokModel":                h.cfg.GrokModel,
		"grokReasoningEffort":      h.cfg.GrokReasoningEffort,
		"voiceConfigured":          configuredValue(h.cfg.GroqAPIKey),
	}

	if !h.cfg.TelegramBotEnabled {
		resp["reason"] = "TELEGRAM_BOT_ENABLED=false"
		JSON(w, http.StatusOK, resp)
		return
	}

	if h.telegram == nil {
		resp["reason"] = "TELEGRAM_BOT_TOKEN ontbreekt"
		JSON(w, http.StatusOK, resp)
		return
	}

	info, err := h.telegram.GetMe()
	if err != nil {
		resp["reason"] = "Telegram API niet bereikbaar of token ongeldig"
		resp["error"] = err.Error()
		JSON(w, http.StatusOK, resp)
		return
	}

	resp["ok"] = true
	resp["bot"] = map[string]any{
		"username":   info.Username,
		"first_name": info.FirstName,
		"id":         info.ID,
	}

	webhook, err := h.telegram.GetWebhookInfo()
	if err != nil {
		resp["webhook"] = map[string]any{
			"configured": false,
			"error":      err.Error(),
		}
		JSON(w, http.StatusOK, resp)
		return
	}

	resp["webhook"] = map[string]any{
		"configured":              webhook.URL != "",
		"urlHost":                 hostFromURL(webhook.URL),
		"pendingUpdateCount":      webhook.PendingUpdates,
		"lastErrorDate":           telegramUnixTime(webhook.LastErrorDate),
		"lastErrorMessage":        emptyToNil(webhook.LastErrorMessage),
		"maxConnections":          webhook.MaxConnections,
		"allowedUpdates":          webhook.AllowedUpdates,
		"hasCustomCertificate":    webhook.HasCustomCert,
		"lastSyncErrorDate":       telegramUnixTime(webhook.LastSyncErrorDate),
		"longPollingWillBeActive": webhook.URL == "",
	}

	JSON(w, http.StatusOK, resp)
}

type aiDiagnosticCheck struct {
	OK        bool    `json:"ok"`
	Status    string  `json:"status"`
	Label     string  `json:"label"`
	Detail    string  `json:"detail,omitempty"`
	LatencyMS int64   `json:"latencyMs,omitempty"`
	Error     *string `json:"error,omitempty"`
}

// AIDiagnostics returns AI runtime checks and agent/tool capabilities.
// @Summary Get AI diagnostics
// @Description Returns Grok/Groq connectivity status and HomeBot tool capabilities without exposing secrets.
// @Tags Settings
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /settings/ai/diagnostics [get]
func (h *SettingsHandler) AIDiagnostics(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 55*time.Second)
	defer cancel()

	checks := map[string]aiDiagnosticCheck{
		"grokChat":      h.checkGrokChat(ctx),
		"grokWebSearch": h.checkGrokWebSearch(ctx),
		"groqVoice":     h.checkGroqVoice(ctx),
	}

	ok := true
	for _, check := range checks {
		if check.Status == "error" {
			ok = false
			break
		}
	}

	resp := map[string]any{
		"ok":          ok,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"config": map[string]any{
			"grokConfigured":      configuredValue(h.cfg.GrokAPIKey),
			"grokModel":           h.cfg.GrokModel,
			"grokReasoningEffort": h.cfg.GrokReasoningEffort,
			"groqConfigured":      configuredValue(h.cfg.GroqAPIKey),
			"telegramConfigured":  h.cfg.TelegramBotEnabled && h.telegram != nil,
		},
		"checks":          checks,
		"capabilities":    aiCapabilitySummary(),
		"governance":      aiGovernanceSummary(),
		"agents":          aiAgentCapabilities(),
		"recommendations": aiDiagnosticRecommendations(),
	}

	JSON(w, http.StatusOK, resp)
}

func (h *SettingsHandler) checkGrokChat(ctx context.Context) aiDiagnosticCheck {
	if !configuredValue(h.cfg.GrokAPIKey) {
		return skippedCheck("Grok chat", "GROK_API_KEY ontbreekt")
	}

	start := time.Now()
	client := ai.NewGrokClientWithOptions(h.cfg.GrokAPIKey, h.cfg.GrokModel, h.cfg.GrokReasoningEffort)
	result := client.Chat(ctx, "Je bent een statuscheck. Antwoord exact met: OK", "statuscheck", nil, nil, nil)
	latency := time.Since(start).Milliseconds()
	if !result.OK {
		return errorCheck("Grok chat", result.Error, latency)
	}
	if !strings.Contains(strings.ToLower(result.Antwoord), "ok") {
		return warningCheck("Grok chat", "Antwoord ontvangen, maar niet de verwachte statuscheck tekst", latency)
	}
	return successCheck("Grok chat", "Chat completions werkt", latency)
}

func (h *SettingsHandler) checkGrokWebSearch(ctx context.Context) aiDiagnosticCheck {
	if !configuredValue(h.cfg.GrokAPIKey) {
		return skippedCheck("Grok web-search", "GROK_API_KEY ontbreekt")
	}

	start := time.Now()
	client := ai.NewGrokClientWithOptions(h.cfg.GrokAPIKey, h.cfg.GrokModel, h.cfg.GrokReasoningEffort)
	result := client.SearchWeb(ctx, "Statuscheck: controleer via web_search of actueel zoeken beschikbaar is. Antwoord kort met OK.")
	latency := time.Since(start).Milliseconds()
	if !result.OK {
		return errorCheck("Grok web-search", result.Error, latency)
	}
	return successCheck("Grok web-search", "Responses API web_search werkt", latency)
}

func (h *SettingsHandler) checkGroqVoice(ctx context.Context) aiDiagnosticCheck {
	if !configuredValue(h.cfg.GroqAPIKey) {
		return skippedCheck("Groq voice", "GROQ_API_KEY ontbreekt")
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
	if err != nil {
		return errorCheck("Groq voice", err.Error(), time.Since(start).Milliseconds())
	}
	req.Header.Set("Authorization", "Bearer "+h.cfg.GroqAPIKey)

	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return errorCheck("Groq voice", err.Error(), latency)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return errorCheck("Groq voice", "Groq models endpoint "+http.StatusText(resp.StatusCode)+": "+truncateText(string(body), 180), latency)
	}
	return successCheck("Groq voice", "API key geldig, Whisper kan gebruikt worden", latency)
}

func aiCapabilitySummary() map[string]int {
	mutating := 0
	confirmation := 0
	exposed := exposedToolSet()
	for _, tool := range ai.AllTools {
		if ai.IsMutatingTool(tool.Function.Name) {
			mutating++
		}
		if ai.RequiresConfirmation(tool.Function.Name) {
			confirmation++
		}
	}

	pendingPolicy := 0
	pendingMutating := 0
	pendingConfirmation := 0
	for name, policy := range ai.Policies {
		if exposed[name] {
			continue
		}
		pendingPolicy++
		if policy.Mutates {
			pendingMutating++
		}
		if policy.RequiresConfirmation {
			pendingConfirmation++
		}
	}

	return map[string]int{
		"agents":                   len(ai.Registry),
		"tools":                    len(ai.AllTools),
		"mutatingTools":            mutating,
		"confirmationTools":        confirmation,
		"policyTools":              len(ai.Policies),
		"pendingPolicyTools":       pendingPolicy,
		"pendingMutatingTools":     pendingMutating,
		"pendingConfirmationTools": pendingConfirmation,
		"readOnlyTools":            len(ai.AllTools) - mutating,
	}
}

func aiAgentCapabilities() []map[string]any {
	agents := make([]map[string]any, 0, len(ai.Registry))
	exposed := exposedToolSet()
	for _, agent := range ai.Registry {
		tools := ai.GetToolsForAgent(agent.ID, ai.AllTools)
		toolNames := make([]string, 0, len(tools))
		mutating := 0
		confirmation := 0
		for _, tool := range tools {
			name := tool.Function.Name
			toolNames = append(toolNames, name)
			if ai.IsMutatingTool(name) {
				mutating++
			}
			if ai.RequiresConfirmation(name) {
				confirmation++
			}
		}
		sort.Strings(toolNames)

		pendingNames := make([]string, 0)
		pendingMutating := 0
		pendingConfirmation := 0
		for name, policy := range ai.Policies {
			if exposed[name] || !policyAppliesToAgent(policy, agent.ID) {
				continue
			}
			pendingNames = append(pendingNames, name)
			if policy.Mutates {
				pendingMutating++
			}
			if policy.RequiresConfirmation {
				pendingConfirmation++
			}
		}
		sort.Strings(pendingNames)

		agents = append(agents, map[string]any{
			"id":                       agent.ID,
			"naam":                     agent.Naam,
			"emoji":                    agent.Emoji,
			"description":              agent.Beschrijving,
			"tools":                    len(tools),
			"mutatingTools":            mutating,
			"confirmationTools":        confirmation,
			"toolNames":                toolNames,
			"liveToolNames":            toolNames,
			"pendingTools":             len(pendingNames),
			"pendingMutatingTools":     pendingMutating,
			"pendingConfirmationTools": pendingConfirmation,
			"pendingToolNames":         pendingNames,
		})
	}
	return agents
}

func aiGovernanceSummary() map[string]any {
	exposed := exposedToolSet()
	liveNames := sortedExposedToolNames()
	policyOnlyNames := make([]string, 0)
	mutatingNames := make([]string, 0)
	confirmationNames := make([]string, 0)

	for name, policy := range ai.Policies {
		if policy.Mutates {
			mutatingNames = append(mutatingNames, name)
		}
		if policy.RequiresConfirmation {
			confirmationNames = append(confirmationNames, name)
		}
		if !exposed[name] {
			policyOnlyNames = append(policyOnlyNames, name)
		}
	}

	sort.Strings(policyOnlyNames)
	sort.Strings(mutatingNames)
	sort.Strings(confirmationNames)

	coveragePercent := 0
	if len(ai.Policies) > 0 {
		coveragePercent = int(float64(len(ai.AllTools)) / float64(len(ai.Policies)) * 100)
	}

	return map[string]any{
		"liveToolNames":         liveNames,
		"policyOnlyToolNames":   policyOnlyNames,
		"mutatingToolNames":     mutatingNames,
		"confirmationToolNames": confirmationNames,
		"coveragePercent":       coveragePercent,
		"liveTools":             len(ai.AllTools),
		"policyTools":           len(ai.Policies),
		"policyOnlyTools":       len(policyOnlyNames),
	}
}

func aiDiagnosticRecommendations() []map[string]string {
	exposed := exposedToolSet()
	hasPendingConfirmation := false
	hasPendingAgendaWrite := false
	hasPendingEmailWrite := false
	hasPendingCRMWrite := false

	for name, policy := range ai.Policies {
		if exposed[name] {
			continue
		}
		if policy.RequiresConfirmation {
			hasPendingConfirmation = true
		}
		if strings.HasPrefix(name, "afspraak") {
			hasPendingAgendaWrite = true
		}
		if strings.Contains(strings.ToLower(name), "email") || strings.Contains(strings.ToLower(name), "gelezen") || strings.Contains(strings.ToLower(name), "ster") {
			hasPendingEmailWrite = true
		}
		if strings.HasPrefix(name, "laventecare") && policy.Mutates {
			hasPendingCRMWrite = true
		}
	}

	recommendations := make([]map[string]string, 0, 4)
	if hasPendingConfirmation {
		recommendations = append(recommendations, map[string]string{
			"priority": "hoog",
			"title":    "Bevestigingslaag voor mutaties",
			"detail":   "Agenda, email, finance en CRM writes staan in policy, maar blijven bewust buiten live toolcalls totdat approve/reject in Telegram en UI is aangesloten.",
		})
	}
	if hasPendingAgendaWrite {
		recommendations = append(recommendations, map[string]string{
			"priority": "hoog",
			"title":    "Agenda write-flow",
			"detail":   "Afspraken maken, bewerken en verwijderen kunnen daarna veilig als pending acties met samenvatting en bevestigingscode.",
		})
	}
	if hasPendingEmailWrite {
		recommendations = append(recommendations, map[string]string{
			"priority": "middel",
			"title":    "Email actielaag",
			"detail":   "Markeren, beantwoorden en opruimen kunnen via dezelfde confirmation queue zodra Gmail mutations zijn afgeschermd.",
		})
	}
	if hasPendingCRMWrite {
		recommendations = append(recommendations, map[string]string{
			"priority": "middel",
			"title":    "LaventeCare mutaties",
			"detail":   "Leads, projecten en acties zijn nu leesbaar; maken en bijwerken kan daarna gecontroleerd worden toegevoegd.",
		})
	}

	return recommendations
}

func exposedToolSet() map[string]bool {
	exposed := make(map[string]bool, len(ai.AllTools))
	for _, tool := range ai.AllTools {
		exposed[tool.Function.Name] = true
	}
	return exposed
}

func sortedExposedToolNames() []string {
	names := make([]string, 0, len(ai.AllTools))
	for _, tool := range ai.AllTools {
		names = append(names, tool.Function.Name)
	}
	sort.Strings(names)
	return names
}

func policyAppliesToAgent(policy ai.ToolPolicy, agentID string) bool {
	for _, allowedAgent := range policy.Agents {
		if allowedAgent == agentID {
			return true
		}
	}
	return false
}

func successCheck(label, detail string, latencyMS int64) aiDiagnosticCheck {
	return aiDiagnosticCheck{OK: true, Status: "success", Label: label, Detail: detail, LatencyMS: latencyMS}
}

func warningCheck(label, detail string, latencyMS int64) aiDiagnosticCheck {
	return aiDiagnosticCheck{OK: true, Status: "warning", Label: label, Detail: detail, LatencyMS: latencyMS}
}

func skippedCheck(label, detail string) aiDiagnosticCheck {
	return aiDiagnosticCheck{OK: false, Status: "skipped", Label: label, Detail: detail}
}

func errorCheck(label, message string, latencyMS int64) aiDiagnosticCheck {
	if strings.TrimSpace(message) == "" {
		message = "Onbekende fout"
	}
	err := truncateText(message, 260)
	return aiDiagnosticCheck{OK: false, Status: "error", Label: label, LatencyMS: latencyMS, Error: &err}
}

func configuredValue(value string) bool {
	return strings.TrimSpace(value) != ""
}

func configuredSecret(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "change-me") || strings.Contains(lower, "change_me") {
		return false
	}
	switch lower {
	case "homeapp-gas-sync-2026-secure", "homeapp-local-dev-2026-change-in-prod":
		return false
	default:
		return true
	}
}

func googleOAuthConfigured(cfg *config.Config) bool {
	return configuredValue(cfg.GoogleClientID) &&
		configuredValue(cfg.GoogleClientSecret) &&
		configuredValue(cfg.GoogleRefreshToken)
}

func maskedSuffix(value string, keep int) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if keep <= 0 || len(value) <= keep {
		return "***"
	}
	return "***" + value[len(value)-keep:]
}

func emptyToNil(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func telegramUnixTime(value int64) any {
	if value <= 0 {
		return nil
	}
	return time.Unix(value, 0).UTC().Format(time.RFC3339)
}

func hostFromURL(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	if strings.HasPrefix(value, "https://") {
		value = strings.TrimPrefix(value, "https://")
	} else if strings.HasPrefix(value, "http://") {
		value = strings.TrimPrefix(value, "http://")
	}
	return strings.Split(value, "/")[0]
}

func truncateText(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
