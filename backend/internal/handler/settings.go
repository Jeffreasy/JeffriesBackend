package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/bunq"
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
	// Count only non-expired pendings so the metric stays truthful between TTL
	// sweeps (expired commands no longer replay; see device_command TTL).
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'pending' AND created_at > now() - interval '10 minutes'`).Scan(&pendingCommands)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'processing'`).Scan(&processingCommands)
	_ = h.db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_commands WHERE status = 'failed' AND COALESCE(completed_at, updated_at) > now() - interval '24 hours'`).Scan(&failedCommands)

	var bridgeLastSeen *time.Time
	_ = h.db.Pool.QueryRow(ctx, `SELECT MAX(last_seen) FROM bridge_heartbeat`).Scan(&bridgeLastSeen)
	bridgeOnline := bridgeLastSeen != nil && time.Since(bridgeLastSeen.UTC()) <= bridgeOfflineThreshold
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
			"bunq":                     bunqConfigured(h.cfg),
			"bunqEnvironment":          h.cfg.BunqEnvironment,
			"bunqApiKeyConfigured":     configuredSecret(h.cfg.BunqAPIKey),
			"bunqUserConfigured":       configuredValue(h.cfg.BunqUserID),
			"bunqMonetaryAccount":      configuredValue(h.cfg.BunqMonetaryAccountID),
			"bunqCallbackConfigured":   configuredSecret(h.cfg.BunqCallbackSecret),
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
// exportTables are the user-scoped tables included in a data export (GDPR
// portability). Smart-home tables (rooms/devices/scenes) have no user_id and are
// excluded, as are encrypted access credentials and the mail outbox.
var exportTables = []string{
	"notes", "note_links", "habits", "habit_logs", "habit_badges",
	"transactions", "personal_events", "schedule", "salary", "loonstroken",
	"privacy_settings", "brain_preferences", "emails",
	"lc_companies", "lc_contacts", "lc_leads", "lc_projects", "lc_workstreams",
	"lc_action_items", "lc_invoices", "lc_invoice_lines", "lc_quotes", "lc_quote_lines",
	"lc_time_entries", "lc_activity_events", "lc_documents", "lc_dossier_documents",
}

func (h *SettingsHandler) Backup(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("userId")
	if userID == "" {
		Error(w, http.StatusBadRequest, "userId required")
		return
	}

	data := make(map[string][]json.RawMessage, len(exportTables))
	for _, table := range exportTables {
		rows, err := h.dumpUserTable(r.Context(), table, userID)
		if err != nil {
			InternalError(w, r, fmt.Errorf("export %s: %w", table, err))
			return
		}
		data[table] = rows
	}

	dump := map[string]any{
		"version":    "2.0",
		"userId":     userID,
		"exportedAt": time.Now().UTC().Format(time.RFC3339),
		"data":       data,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\"jeffries-homeapp-export.json\"")
	json.NewEncoder(w).Encode(dump)
}

// dumpUserTable returns every row of a user-scoped table as raw JSON. The table
// name is from the fixed exportTables list (never user input), so the formatted
// query is safe.
func (h *SettingsHandler) dumpUserTable(ctx context.Context, table, userID string) ([]json.RawMessage, error) {
	rows, err := h.db.Pool.Query(ctx, fmt.Sprintf(`SELECT row_to_json(t) FROM %s t WHERE user_id = $1`, table), userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []json.RawMessage{}
	for rows.Next() {
		var raw json.RawMessage
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		out = append(out, raw)
	}
	return out, rows.Err()
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
		"googleOAuth":   h.checkGoogleOAuth(ctx),
		"gmailSync":     h.checkGmailSyncFreshness(ctx),
		"calendarSync":  h.checkCalendarSyncFreshness(ctx),
	}

	ok := true
	for _, check := range checks {
		if check.Status == "error" {
			ok = false
			break
		}
	}

	// AI usage rollup (tokens / latency / estimated cost) over cumulative windows.
	logStore := store.NewAICallLogStore(h.db)
	now := time.Now()
	windows := []struct {
		label string
		since time.Time
	}{
		{"today", now.Add(-24 * time.Hour)},
		{"last7d", now.Add(-7 * 24 * time.Hour)},
		{"last30d", now.Add(-30 * 24 * time.Hour)},
	}
	usage := map[string]any{
		"priced": h.cfg.GrokPriceInputPerMTok > 0 || h.cfg.GrokPriceOutputPerMTok > 0,
	}
	for _, win := range windows {
		w, err := logStore.UsageSince(ctx, win.since)
		if err != nil {
			continue
		}
		estCost := (float64(w.PromptTokens)/1e6)*h.cfg.GrokPriceInputPerMTok +
			(float64(w.CompletionTokens)/1e6)*h.cfg.GrokPriceOutputPerMTok
		usage[win.label] = map[string]any{
			"calls":            w.Calls,
			"errors":           w.Errors,
			"promptTokens":     w.PromptTokens,
			"completionTokens": w.CompletionTokens,
			"totalTokens":      w.TotalTokens,
			"avgDurationMs":    w.AvgDurationMs,
			"maxDurationMs":    w.MaxDurationMs,
			"estCost":          estCost,
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
		"usage":           usage,
		"capabilities":    aiCapabilitySummary(),
		"governance":      aiGovernanceSummary(),
		"agents":          aiAgentCapabilities(),
		"recommendations": aiDiagnosticRecommendations(),
	}

	JSON(w, http.StatusOK, resp)
}

// BunqIntrospect creates a temporary bunq API context from Render env and
// returns only the IDs needed for configuration. It never exposes API keys.
func (h *SettingsHandler) BunqIntrospect(w http.ResponseWriter, r *http.Request) {
	if !configuredSecret(h.cfg.AppSecretKey) {
		Error(w, http.StatusForbidden, "APP_SECRET_KEY moet eerst een echte secret zijn voordat bunq introspectie beschikbaar is.")
		return
	}
	if !configuredSecret(h.cfg.BunqAPIKey) {
		Error(w, http.StatusBadRequest, "BUNQ_API_KEY ontbreekt of is een placeholder.")
		return
	}

	result, err := bunq.Discover(r.Context(), bunq.Config{
		Environment:       h.cfg.BunqEnvironment,
		APIKey:            h.cfg.BunqAPIKey,
		DeviceDescription: h.cfg.BunqDeviceDescription,
	})
	if err != nil {
		Error(w, http.StatusBadGateway, "Bunq introspectie mislukt: "+err.Error())
		return
	}

	recommendedEnv := map[string]string{}
	if result.UserID > 0 {
		recommendedEnv["BUNQ_USER_ID"] = strconv.Itoa(result.UserID)
	}
	if result.PrimaryAccountID != nil {
		recommendedEnv["BUNQ_MONETARY_ACCOUNT_ID"] = strconv.Itoa(*result.PrimaryAccountID)
	}

	JSON(w, http.StatusOK, map[string]any{
		"ok":             true,
		"configured":     bunqConfigured(h.cfg),
		"envStatus":      bunqEnvStatus(h.cfg),
		"result":         result,
		"recommendedEnv": recommendedEnv,
		"next":           "Zet BUNQ_USER_ID en BUNQ_MONETARY_ACCOUNT_ID in Render env. Roteer daarna de gelekte API key.",
	})
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

func (h *SettingsHandler) checkGoogleOAuth(ctx context.Context) aiDiagnosticCheck {
	if !googleOAuthConfigured(h.cfg) {
		return skippedCheck("Google OAuth", "Google client, secret of refresh token ontbreekt")
	}

	start := time.Now()
	body := url.Values{
		"client_id":     {h.cfg.GoogleClientID},
		"client_secret": {h.cfg.GoogleClientSecret},
		"refresh_token": {h.cfg.GoogleRefreshToken},
		"grant_type":    {"refresh_token"},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(body.Encode()))
	if err != nil {
		return errorCheck("Google OAuth", err.Error(), time.Since(start).Milliseconds())
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return errorCheck("Google OAuth", err.Error(), latency)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(resp.Body)
		return errorCheck("Google OAuth", "Refresh token test faalt: "+truncateText(string(data), 180), latency)
	}
	return successCheck("Google OAuth", "Refresh token geldig voor Gmail en Calendar", latency)
}

func (h *SettingsHandler) checkGmailSyncFreshness(ctx context.Context) aiDiagnosticCheck {
	if !googleOAuthConfigured(h.cfg) {
		return skippedCheck("Gmail sync", "Google OAuth ontbreekt")
	}
	meta, err := store.NewEmailStore(h.db).GetSyncMeta(ctx, h.cfg.HomeappUserID)
	if err != nil {
		return errorCheck("Gmail sync", err.Error(), 0)
	}
	if meta == nil {
		return warningCheck("Gmail sync", "Nog geen Gmail sync metadata", 0)
	}
	age := time.Since(meta.UpdatedAt)
	detail := "Laatste sync " + meta.UpdatedAt.UTC().Format(time.RFC3339) + " · " + strconv.Itoa(meta.TotalSynced) + " emails"
	if age > 24*time.Hour {
		return warningCheck("Gmail sync", detail+" · ouder dan 24 uur", 0)
	}
	return successCheck("Gmail sync", detail, 0)
}

func (h *SettingsHandler) checkCalendarSyncFreshness(ctx context.Context) aiDiagnosticCheck {
	if !googleOAuthConfigured(h.cfg) {
		return skippedCheck("Calendar sync", "Google OAuth ontbreekt")
	}
	meta, err := store.NewScheduleStore(h.db).GetMeta(ctx, h.cfg.HomeappUserID)
	if err != nil {
		return errorCheck("Calendar sync", err.Error(), 0)
	}
	if meta == nil {
		return warningCheck("Calendar sync", "Nog geen rooster-sync metadata", 0)
	}

	var pendingPersonal int
	_ = h.db.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM personal_events
		  WHERE user_id = $1 AND status IN ($2, $3, $4)`,
		h.cfg.HomeappUserID,
		store.PersonalEventStatusPendingCreate,
		store.PersonalEventStatusPendingUpdate,
		store.PersonalEventStatusPendingDelete,
	).Scan(&pendingPersonal)

	detail := "Rooster sync " + meta.ImportedAt.UTC().Format(time.RFC3339) + " · " + strconv.Itoa(meta.TotalRows) + " diensten"
	if pendingPersonal > 0 {
		return warningCheck("Calendar sync", detail+" · "+strconv.Itoa(pendingPersonal)+" afspraak/afspraken in wachtrij", 0)
	}
	return successCheck("Calendar sync", detail, 0)
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
	hasPendingFinanceWrite := false
	hasPendingCRMWrite := false

	for name, policy := range ai.Policies {
		if exposed[name] {
			continue
		}
		targetWrite := policy.Mutates && (strings.HasPrefix(name, "afspraak") ||
			name == "categorieWijzigen" ||
			name == "bulkCategoriseren" ||
			strings.Contains(strings.ToLower(name), "email") ||
			strings.Contains(strings.ToLower(name), "gelezen") ||
			strings.Contains(strings.ToLower(name), "ster") ||
			name == "laventecareKlantMaken" ||
			name == "laventecareKlantBijwerken" ||
			name == "laventecareContactMaken" ||
			name == "laventecareLeadMaken" ||
			name == "laventecareLeadBijwerken" ||
			name == "laventecareLeadNaarProject" ||
			name == "laventecareOpdrachtMaken" ||
			name == "laventecareOpdrachtBijwerken" ||
			name == "laventecareOpdrachtNaarProject" ||
			name == "laventecareProjectMaken" ||
			name == "laventecareProjectBijwerken" ||
			name == "laventecareActieMaken" ||
			name == "laventecareActieAfronden")
		if policy.RequiresConfirmation && targetWrite {
			hasPendingConfirmation = true
		}
		if strings.HasPrefix(name, "afspraak") {
			hasPendingAgendaWrite = true
		}
		if strings.Contains(strings.ToLower(name), "email") || strings.Contains(strings.ToLower(name), "gelezen") || strings.Contains(strings.ToLower(name), "ster") {
			hasPendingEmailWrite = true
		}
		if name == "categorieWijzigen" || name == "bulkCategoriseren" {
			hasPendingFinanceWrite = true
		}
		if name == "laventecareKlantMaken" ||
			name == "laventecareKlantBijwerken" ||
			name == "laventecareContactMaken" ||
			name == "laventecareLeadMaken" ||
			name == "laventecareLeadBijwerken" ||
			name == "laventecareLeadNaarProject" ||
			name == "laventecareOpdrachtMaken" ||
			name == "laventecareOpdrachtBijwerken" ||
			name == "laventecareOpdrachtNaarProject" ||
			name == "laventecareProjectMaken" ||
			name == "laventecareProjectBijwerken" ||
			name == "laventecareActieMaken" ||
			name == "laventecareActieAfronden" {
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
	if hasPendingFinanceWrite {
		recommendations = append(recommendations, map[string]string{
			"priority": "middel",
			"title":    "Finance mutaties",
			"detail":   "Categoriseren en bulk-categoriseren kunnen daarna veilig als pending acties met bevestiging worden uitgevoerd.",
		})
	}
	if hasPendingCRMWrite {
		recommendations = append(recommendations, map[string]string{
			"priority": "middel",
			"title":    "LaventeCare mutaties",
			"detail":   "Klanten, contacten, leads, opdrachten, projecten en acties lopen via dezelfde veilige confirmation queue.",
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

func bunqConfigured(cfg *config.Config) bool {
	return configuredSecret(cfg.BunqAPIKey) &&
		configuredValue(cfg.BunqUserID) &&
		configuredValue(cfg.BunqMonetaryAccountID)
}

func bunqEnvStatus(cfg *config.Config) map[string]bool {
	return map[string]bool{
		"apiKey":          configuredSecret(cfg.BunqAPIKey),
		"userId":          configuredValue(cfg.BunqUserID),
		"monetaryAccount": configuredValue(cfg.BunqMonetaryAccountID),
		"callbackSecret":  configuredSecret(cfg.BunqCallbackSecret),
	}
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
