package config

import (
	"fmt"
	"log/slog"
	"net/url"
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

	// TrustedProxyCount is the number of reverse-proxy hops in front of the app
	// whose X-Forwarded-For entries may be trusted (e.g. 1 behind Render's edge).
	// 0 means trust nothing and use the real TCP peer (un-spoofable).
	TrustedProxyCount    int
	APIRateLimitRPS      float64
	APIRateLimitBurst    int
	BridgeRateLimitRPS   float64
	BridgeRateLimitBurst int

	// Database
	DatabaseURL string

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
	LaventeCareIntakeSecret    string
	LaventeCareSecretKey       string

	// AI APIs
	GrokAPIKey          string
	GrokModel           string
	GrokReasoningEffort string
	GroqAPIKey          string

	// Optional price per 1M tokens (input/output) used only to estimate cost in
	// AI diagnostics. Default 0 → cost shows as 0 until configured.
	GrokPriceInputPerMTok  float64
	GrokPriceOutputPerMTok float64

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
	automationEngineEnabled := envBoolOr("AUTOMATION_ENGINE_ENABLED", false)

	cfg := &Config{
		AppEnv:       envOr("APP_ENV", "development"),
		AppSecretKey: envOr("APP_SECRET_KEY", "change-me"),
		AppHost:      envOr("APP_HOST", "0.0.0.0"),
		AppPort:      envIntOr("APP_PORT", envIntOr("PORT", 8000)),
		AppDebug:     envBoolOr("APP_DEBUG", true),

		TrustedProxyCount:    envIntOr("TRUSTED_PROXY_COUNT", 0),
		APIRateLimitRPS:      envFloatOr("API_RATE_LIMIT_RPS", 30),
		APIRateLimitBurst:    envIntOr("API_RATE_LIMIT_BURST", 60),
		BridgeRateLimitRPS:   envFloatOr("BRIDGE_RATE_LIMIT_RPS", 20),
		BridgeRateLimitBurst: envIntOr("BRIDGE_RATE_LIMIT_BURST", 80),

		// No built-in database credential: local development must opt in too.
		DatabaseURL: envOr("DATABASE_URL", ""),

		HomeappUserID:           envOr("HOMEAPP_USER_ID", "user_3Ax561ZvuSkGtWpKFooeY65HNtY"),
		TelegramBridgeSecret:    envOr("TELEGRAM_BRIDGE_SECRET", ""),
		TelegramBotToken:        envOr("TELEGRAM_BOT_TOKEN", ""),
		TelegramChatID:          envOr("TELEGRAM_CHAT_ID", ""),
		TelegramWebAppURL:       envOr("TELEGRAM_WEBAPP_URL", "https://jeffries-homeapp.vercel.app"),
		AutomationEngineEnabled: automationEngineEnabled,
		// Backwards-compatible alias; START_BACKGROUND_ENGINE wins when set.
		StartBackgroundEngine:      envBoolOr("START_BACKGROUND_ENGINE", automationEngineEnabled),
		TelegramBotEnabled:         envBoolOr("TELEGRAM_BOT_ENABLED", true),
		LightCommandMode:           lightCommandMode,
		EngineCronsEnabled:         envBoolOr("ENGINE_CRONS_ENABLED", true),
		EngineAutomationsEnabled:   envBoolOr("ENGINE_AUTOMATIONS_ENABLED", true),
		EngineCommandPollerEnabled: envBoolOr("ENGINE_COMMAND_POLLER_ENABLED", !queueLightCommands),
		EngineStatusPollEnabled:    envBoolOr("ENGINE_STATUS_POLL_ENABLED", !queueLightCommands),
		BridgeAPIURL:               strings.TrimRight(envOr("BRIDGE_API_URL", ""), "/"),
		BridgeAPIKey:               envOr("BRIDGE_API_KEY", ""),
		BridgeStatusPollEnabled:    envBoolOr("BRIDGE_STATUS_POLL_ENABLED", true),
		LaventeCareIntakeSecret:    envOr("LAVENTECARE_INTAKE_SECRET", ""),
		LaventeCareSecretKey:       envOr("LAVENTECARE_SECRET_KEY", ""),

		GrokAPIKey:          envOr("GROK_API_KEY", ""),
		GrokModel:           envOr("GROK_MODEL", "grok-4.3"),
		GrokReasoningEffort: envOr("GROK_REASONING_EFFORT", "low"),
		GroqAPIKey:          envOr("GROQ_API_KEY", ""),
		WizDeviceIPs:        envOr("WIZ_DEVICE_IPS", ""),

		GrokPriceInputPerMTok:  envFloatOr("GROK_PRICE_INPUT_PER_MTOK", 0),
		GrokPriceOutputPerMTok: envFloatOr("GROK_PRICE_OUTPUT_PER_MTOK", 0),

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

// weakSecrets are placeholder values that must never gate a real deployment.
var weakSecrets = map[string]bool{
	"":                                  true,
	"change-me":                         true,
	"change-me-to-a-long-random-secret": true,
}

// Validate reports fatal misconfiguration and logs non-fatal warnings. Outside
// development it refuses an empty or well-known-default APP_SECRET_KEY so the API
// can never boot fully open.
func (c *Config) Validate() error {
	appSecret := strings.TrimSpace(c.AppSecretKey)
	if isWeakSecret(appSecret) || len(appSecret) < 32 {
		if !c.IsDevelopment() {
			return fmt.Errorf("APP_SECRET_KEY must contain at least 32 characters and not be a known default in env %q", c.AppEnv)
		}
		slog.Warn("APP_SECRET_KEY is shorter than 32 characters or a default value — acceptable for development only, never deploy like this")
	}

	if len(strings.TrimSpace(c.HomeappUserID)) < 12 {
		return fmt.Errorf("HOMEAPP_USER_ID must contain the configured owner id (at least 12 characters)")
	}
	if strings.TrimSpace(c.DatabaseURL) == "" {
		return fmt.Errorf("DATABASE_URL is required; no default database credential is used")
	}
	if weak, reason := weakDatabaseURL(c.DatabaseURL); weak {
		if !c.IsDevelopment() {
			return fmt.Errorf("DATABASE_URL is unsafe in env %q: %s", c.AppEnv, reason)
		}
		slog.Warn("DATABASE_URL uses a weak development credential", "reason", reason)
	}

	bridgeRequired := c.BridgeAPIURL != "" || c.QueueLightCommands()
	bridgeSecret := strings.TrimSpace(c.BridgeAPIKey)
	if bridgeRequired && (isWeakSecret(bridgeSecret) || len(bridgeSecret) < 32) {
		return fmt.Errorf("BRIDGE_API_KEY must contain at least 32 characters when bridge/queue mode is enabled")
	}
	if bridgeSecret != "" && len(bridgeSecret) < 32 {
		if !c.IsDevelopment() {
			return fmt.Errorf("BRIDGE_API_KEY must contain at least 32 characters when configured")
		}
		slog.Warn("BRIDGE_API_KEY is shorter than 32 characters — do not deploy this value")
	}
	if bridgeSecret != "" && bridgeSecret == appSecret {
		if !c.IsDevelopment() || bridgeRequired {
			return fmt.Errorf("BRIDGE_API_KEY must differ from APP_SECRET_KEY")
		}
		slog.Warn("BRIDGE_API_KEY equals APP_SECRET_KEY — use a separate bridge-only secret")
	}
	vaultSecret := strings.TrimSpace(c.LaventeCareSecretKey)
	if isWeakSecret(vaultSecret) || len(vaultSecret) < 32 {
		if !c.IsDevelopment() {
			return fmt.Errorf("LAVENTECARE_SECRET_KEY must contain at least 32 characters and is required outside development")
		}
		slog.Warn("LAVENTECARE_SECRET_KEY is missing, shorter than 32 characters or a default — access-secret writes stay disabled")
	}
	if vaultSecret != "" {
		for name, other := range map[string]string{
			"APP_SECRET_KEY":            appSecret,
			"BRIDGE_API_KEY":            bridgeSecret,
			"LAVENTECARE_INTAKE_SECRET": strings.TrimSpace(c.LaventeCareIntakeSecret),
		} {
			if other != "" && vaultSecret == other {
				return fmt.Errorf("LAVENTECARE_SECRET_KEY must differ from %s", name)
			}
		}
	}

	intakeSecret := strings.TrimSpace(c.LaventeCareIntakeSecret)
	if intakeSecret != "" {
		if isWeakSecret(intakeSecret) || len(intakeSecret) < 32 {
			return fmt.Errorf("LAVENTECARE_INTAKE_SECRET must contain at least 32 characters")
		}
		if intakeSecret == strings.TrimSpace(c.AppSecretKey) || intakeSecret == strings.TrimSpace(c.BridgeAPIKey) {
			return fmt.Errorf("LAVENTECARE_INTAKE_SECRET must differ from APP_SECRET_KEY and BRIDGE_API_KEY")
		}
	}
	return nil
}

func isWeakSecret(raw string) bool {
	return weakSecrets[strings.ToLower(strings.TrimSpace(raw))]
}

func weakDatabaseURL(raw string) (bool, string) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" {
		return true, "invalid connection URL"
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	if scheme != "postgres" && scheme != "postgresql" {
		return true, "connection URL must use postgres or postgresql scheme"
	}
	if strings.TrimSpace(parsed.Hostname()) == "" {
		return true, "database hostname is missing"
	}
	if parsed.User == nil {
		return true, "database user/password are missing"
	}
	password, ok := parsed.User.Password()
	password = strings.ToLower(strings.TrimSpace(password))
	if !ok || password == "" {
		return true, "database password is missing"
	}
	if weakSecrets[password] {
		return true, "database password is empty or a known default"
	}
	return false, ""
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

func envFloatOr(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
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
