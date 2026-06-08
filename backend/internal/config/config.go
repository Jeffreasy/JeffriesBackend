package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Application
	AppEnv       string
	AppSecretKey string
	AppHost      string
	AppPort      int
	AppDebug     bool

	// Database
	DatabaseURL string

	HomeappGASSecret           string
	HomeappUserID              string
	TelegramBridgeSecret       string
	TelegramBotToken           string
	TelegramChatID             string
	TelegramWebAppURL          string
	AutomationEngineEnabled    bool
	StartBackgroundEngine      bool
	TelegramBotEnabled         bool
	LightCommandMode           string
	EngineCronsEnabled         bool
	EngineAutomationsEnabled   bool
	EngineCommandPollerEnabled bool
	EngineStatusPollEnabled    bool
	WizDeviceIPs               string
	BridgeAPIURL               string
	BridgeAPIKey               string
	BridgeStatusPollEnabled    bool

	// AI APIs
	GrokAPIKey          string
	GrokModel           string
	GrokReasoningEffort string
	GroqAPIKey          string

	// Cron Feature Flags
	GmailEnabled          bool
	GoogleCalendarEnabled bool
	TodoistEnabled        bool

	// Google OAuth (for Gmail + Calendar sync)
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRefreshToken  string
	SDBCalendarID       string
	PersonalCalendarIDs string

	// Todoist
	TodoistAPIToken  string
	TodoistProjectID string

	// bunq (LaventeCare billing)
	BunqEnvironment       string
	BunqAPIKey            string
	BunqUserID            string
	BunqMonetaryAccountID string
	BunqCallbackSecret    string
	BunqDeviceDescription string

	// LaventeCare mailbox (Microsoft Graph application permissions)
	LaventeCareMailEnabled  bool
	LaventeCareMailProvider string
	LaventeCareMailFromName string
	MicrosoftTenantID       string
	MicrosoftClientID       string
	MicrosoftClientSecret   string
	MicrosoftSenderEmail    string

	// CORS
	CORSOrigins []string

	// Logging
	LogLevel string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() *Config {
	lightCommandMode := envOr("LIGHT_COMMAND_MODE", "direct")
	queueLightCommands := strings.EqualFold(lightCommandMode, "queue")

	cfg := &Config{
		AppEnv:       envOr("APP_ENV", "development"),
		AppSecretKey: envOr("APP_SECRET_KEY", "change-me"),
		AppHost:      envOr("APP_HOST", "0.0.0.0"),
		AppPort:      envIntOr("APP_PORT", envIntOr("PORT", 8000)),
		AppDebug:     envBoolOr("APP_DEBUG", true),

		DatabaseURL: envOr("DATABASE_URL", "postgres://homeapp:change-me@localhost:5432/homeapp?sslmode=disable"),

		HomeappGASSecret:           envOr("HOMEAPP_GAS_SECRET", "homeapp-gas-sync-2026-secure"),
		HomeappUserID:              envOr("HOMEAPP_USER_ID", "user_3Ax561ZvuSkGtWpKFooeY65HNtY"),
		TelegramBridgeSecret:       envOr("TELEGRAM_BRIDGE_SECRET", ""),
		TelegramBotToken:           envOr("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:             envOr("TELEGRAM_CHAT_ID", ""),
		TelegramWebAppURL:          envOr("TELEGRAM_WEBAPP_URL", "https://jeffrieshomeapp.com"),
		AutomationEngineEnabled:    envBoolOr("AUTOMATION_ENGINE_ENABLED", false),
		StartBackgroundEngine:      envBoolOr("START_BACKGROUND_ENGINE", false),
		TelegramBotEnabled:         envBoolOr("TELEGRAM_BOT_ENABLED", true),
		LightCommandMode:           lightCommandMode,
		EngineCronsEnabled:         envBoolOr("ENGINE_CRONS_ENABLED", true),
		EngineAutomationsEnabled:   envBoolOr("ENGINE_AUTOMATIONS_ENABLED", true),
		EngineCommandPollerEnabled: envBoolOr("ENGINE_COMMAND_POLLER_ENABLED", !queueLightCommands),
		EngineStatusPollEnabled:    envBoolOr("ENGINE_STATUS_POLL_ENABLED", !queueLightCommands),
		BridgeAPIURL:               strings.TrimRight(envOr("BRIDGE_API_URL", ""), "/"),
		BridgeAPIKey:               envOr("BRIDGE_API_KEY", envOr("APP_SECRET_KEY", "change-me")),
		BridgeStatusPollEnabled:    envBoolOr("BRIDGE_STATUS_POLL_ENABLED", true),

		GrokAPIKey:          envOr("GROK_API_KEY", ""),
		GrokModel:           envOr("GROK_MODEL", "grok-4.3"),
		GrokReasoningEffort: envOr("GROK_REASONING_EFFORT", "low"),
		GroqAPIKey:          envOr("GROQ_API_KEY", ""),
		WizDeviceIPs:        envOr("WIZ_DEVICE_IPS", ""),

		GmailEnabled:          envBoolOr("GMAIL_SYNC_ENABLED", false),
		GoogleCalendarEnabled: envBoolOr("GOOGLE_CALENDAR_SYNC_ENABLED", false),
		TodoistEnabled:        envBoolOr("TODOIST_SYNC_ENABLED", false),

		GoogleClientID:      envOr("GOOGLE_CLIENT_ID", ""),
		GoogleClientSecret:  envOr("GOOGLE_CLIENT_SECRET", ""),
		GoogleRefreshToken:  envOr("GOOGLE_REFRESH_TOKEN", ""),
		SDBCalendarID:       envOr("SDB_CALENDAR_ID", "7gml08968kada988va91mu3i2qkci0ts@import.calendar.google.com"),
		PersonalCalendarIDs: envOr("GOOGLE_PERSONAL_CALENDAR_IDS", ""),

		TodoistAPIToken:  envOr("TODOIST_API_TOKEN", ""),
		TodoistProjectID: envOr("TODOIST_PROJECT_ID", ""),

		BunqEnvironment:       envOr("BUNQ_ENVIRONMENT", "sandbox"),
		BunqAPIKey:            envOr("BUNQ_API_KEY", ""),
		BunqUserID:            envOr("BUNQ_USER_ID", ""),
		BunqMonetaryAccountID: envOr("BUNQ_MONETARY_ACCOUNT_ID", ""),
		BunqCallbackSecret:    envOr("BUNQ_CALLBACK_SECRET", ""),
		BunqDeviceDescription: envOr("BUNQ_DEVICE_DESCRIPTION", "JeffriesHomeapp Render"),

		LaventeCareMailEnabled:  envBoolOr("LAVENTECARE_MAIL_ENABLED", false),
		LaventeCareMailProvider: envOr("LAVENTECARE_MAIL_PROVIDER", "microsoft_graph"),
		LaventeCareMailFromName: envOr("LAVENTECARE_MAIL_FROM_NAME", "LaventeCare"),
		MicrosoftTenantID:       envOr("MICROSOFT_TENANT_ID", ""),
		MicrosoftClientID:       envOr("MICROSOFT_CLIENT_ID", ""),
		MicrosoftClientSecret:   envOr("MICROSOFT_CLIENT_SECRET", ""),
		MicrosoftSenderEmail:    strings.ToLower(envOr("MICROSOFT_SENDER_EMAIL", "")),

		CORSOrigins: envSliceOr("CORS_ORIGINS", []string{"http://localhost:3000"}),

		LogLevel: envOr("LOG_LEVEL", "INFO"),
	}

	return cfg
}

// IsDevelopment returns true when running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.AppEnv == "development"
}

// SlogLevel converts the string log level to slog.Level.
func (c *Config) SlogLevel() slog.Level {
	switch strings.ToUpper(c.LogLevel) {
	case "DEBUG":
		return slog.LevelDebug
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Addr returns "host:port" for the HTTP server.
func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.AppHost, c.AppPort)
}

// QueueLightCommands returns true when cloud services should enqueue lamp
// commands instead of trying to reach WiZ devices over the local network.
func (c *Config) QueueLightCommands() bool {
	return strings.EqualFold(c.LightCommandMode, "queue")
}

// LaventeCareMailConfigured returns true when outbound LaventeCare mail can use
// Microsoft Graph application permissions.
func (c *Config) LaventeCareMailConfigured() bool {
	return c.LaventeCareMailEnabled &&
		c.MicrosoftTenantID != "" &&
		c.MicrosoftClientID != "" &&
		c.MicrosoftClientSecret != "" &&
		c.MicrosoftSenderEmail != ""
}

// --- helpers ---

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return fallback
}

func envSliceOr(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	// Support JSON-style array or comma-separated
	v = strings.Trim(v, "[]\"")
	parts := strings.Split(v, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.Trim(strings.TrimSpace(p), "\"")
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
