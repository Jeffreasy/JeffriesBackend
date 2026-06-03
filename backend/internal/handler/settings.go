package handler

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

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
			"localBridge":              h.cfg.QueueLightCommands() && configuredSecret(h.cfg.BridgeAPIKey),
			"telegramBot":              h.cfg.TelegramBotEnabled && h.telegram != nil,
			"telegramOwner":            configuredValue(h.cfg.TelegramChatID),
			"telegramWebhookSecret":    configuredSecret(h.cfg.TelegramBridgeSecret),
			"telegramMode":             "long_polling",
			"telegramWebApp":           configuredValue(h.cfg.TelegramWebAppURL),
			"grok":                     configuredValue(h.cfg.GrokAPIKey),
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
			"online":             true,
			"status":             "Active",
			"lastSeenAt":         time.Now().Format(time.RFC3339),
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
