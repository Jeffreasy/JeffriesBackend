package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/config"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/Jeffreasy/JeffriesBackend/internal/wiz"
	"github.com/google/uuid"
)

// maxConcurrentAISessions bounds how many Telegram updates run the full
// AI + tool pipeline at once, preventing an update burst from stampeding xAI
// and the DB pool.
const maxConcurrentAISessions = 3

const (
	EngineInterval  = 30 * time.Second
	MinFireInterval = 55.0 // seconds
	StatusPollEvery = 10
)

var amsterdam *time.Location

func init() {
	var err error
	amsterdam, err = time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		amsterdam = time.UTC
		slog.Warn("failed to load Europe/Amsterdam timezone, using UTC")
	}
}

func isClosedPoolError(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "closed pool")
}

func (e *Engine) databasePoolClosed(ctx context.Context) bool {
	if ctx.Err() != nil {
		return true
	}
	if e == nil || e.db == nil || e.db.Pool == nil {
		return false
	}

	pingCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	if err := e.db.Ping(pingCtx); isClosedPoolError(err) {
		slog.Warn("database pool closed; stopping engine work")
		return true
	}
	return false
}

// Engine is the server-side automation engine — fully PostgreSQL-driven.
type Engine struct {
	wiz *wiz.Client
	cfg *config.Config
	db  *store.DB

	autoStore  *store.AutomationStore
	devStore   *store.DeviceStore
	schedStore *store.ScheduleStore
	cmdStore   *store.DeviceCommandStore
	cron       *CronScheduler

	firedAt   map[string]time.Time
	firedMu   sync.Mutex
	tickCount int

	// grokClient is a shared, lazily-built Grok client so its circuit breaker
	// and HTTP transport persist across requests instead of being recreated
	// per prompt (a per-call breaker can never trip).
	grokClient *ai.GrokClient
	grokOnce   sync.Once

	// aiSem bounds concurrent AI sessions started from the Telegram poller.
	aiSem chan struct{}

	// aiLog records token/latency telemetry per AI call.
	aiLog *store.AICallLogStore

	// alertFiredAt de-duplicates proactive alerts so a persistently-failing
	// condition (e.g. Google invalid_grant) does not notify every cron tick.
	alertMu      sync.Mutex
	alertFiredAt map[string]time.Time
}

// shouldFireAlert reports whether an alert keyed by `key` may fire now, given it
// last fired more than `window` ago. It records the fire time when it returns true.
func (e *Engine) shouldFireAlert(key string, window time.Duration) bool {
	e.alertMu.Lock()
	defer e.alertMu.Unlock()
	if e.alertFiredAt == nil {
		e.alertFiredAt = make(map[string]time.Time)
	}
	now := time.Now()
	if last, ok := e.alertFiredAt[key]; ok && now.Sub(last) < window {
		return false
	}
	e.alertFiredAt[key] = now
	return true
}

// alertGoogleReauthOnce sends a single de-duplicated re-auth reminder (max once
// per 24h) when Google sync fails with an expired/revoked refresh token.
func (e *Engine) alertGoogleReauthOnce(ctx context.Context) {
	if !e.shouldFireAlert("google-reauth", 24*time.Hour) {
		return
	}
	msg := "🔑 Google re-auth nodig\n\nDe Google refresh token is verlopen of ingetrokken (invalid_grant), waardoor Gmail- en agenda-sync stilliggen.\n\nHerstel: draai scripts/gen-gmail-token.mjs, vervang GOOGLE_REFRESH_TOKEN op Render en redeploy."
	if err := e.SendProactiveNotification(ctx, msg); err != nil {
		slog.Warn("alertGoogleReauthOnce: failed to send", "error", err)
	}
}

// grok returns the shared Grok client, building it once on first use.
func (e *Engine) grok() *ai.GrokClient {
	e.grokOnce.Do(func() {
		e.grokClient = ai.NewGrokClientWithOptions(e.cfg.GrokAPIKey, e.cfg.GrokModel, e.cfg.GrokReasoningEffort)
	})
	return e.grokClient
}

// New creates a new automation engine.
func New(cfg *config.Config, db *store.DB) *Engine {
	wizClient := wiz.NewClient()
	scheduler := NewCronScheduler()

	return &Engine{
		wiz:        wizClient,
		cfg:        cfg,
		db:         db,
		autoStore:  store.NewAutomationStore(db),
		devStore:   store.NewDeviceStore(db),
		schedStore: store.NewScheduleStore(db),
		cmdStore:   store.NewDeviceCommandStore(db),
		cron:       scheduler,
		firedAt:    make(map[string]time.Time),
		aiSem:      make(chan struct{}, maxConcurrentAISessions),
		aiLog:      store.NewAICallLogStore(db),
	}
}

// logAICall records one AI interaction's telemetry (best-effort).
func (e *Engine) logAICall(ctx context.Context, agentID, kind string, result ai.ChatResult) {
	if e.aiLog == nil {
		return
	}
	entry := store.AICallLog{
		UserID:       e.cfg.HomeappUserID,
		AgentID:      agentID,
		Model:        e.cfg.GrokModel,
		Kind:         kind,
		Rounds:       result.Rounds,
		DurationMs:   int(result.DurationMs),
		ToolsUsed:    result.ToolsUsed,
		FinishReason: result.FinishReason,
		OK:           result.OK,
		Error:        result.Error,
	}
	if result.Tokens != nil {
		entry.PromptTokens = result.Tokens.PromptTokens
		entry.CompletionTokens = result.Tokens.CompletionTokens
		entry.TotalTokens = result.Tokens.TotalTokens
	}
	// Use a short detached timeout so logging never blocks or is cancelled with
	// the (possibly already-expired) request context.
	logCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	if err := e.aiLog.Insert(logCtx, entry); err != nil {
		slog.Warn("ai_call_log insert failed", "error", err)
	}
}

// Run starts all engine goroutines and blocks until context is cancelled.
func (e *Engine) Run(ctx context.Context) {
	slog.Info("🤖 automation engine starting",
		"interval", EngineInterval.String(),
		"backend", "PostgreSQL (native)",
		"user", e.cfg.HomeappUserID[:12]+"...",
	)

	var wg sync.WaitGroup

	if e.cfg.EngineCronsEnabled {
		RegisterHomeappCrons(e.cron, e, CronConfig{
			TelegramBotToken:      e.cfg.TelegramBotToken,
			TelegramChatID:        e.cfg.TelegramChatID,
			GmailEnabled:          e.cfg.GmailEnabled,
			GoogleCalendarEnabled: e.cfg.GoogleCalendarEnabled,
			TodoistEnabled:        e.cfg.TodoistEnabled,
			UserID:                e.cfg.HomeappUserID,
			GoogleClientID:        e.cfg.GoogleClientID,
			GoogleClientSecret:    e.cfg.GoogleClientSecret,
			GoogleRefreshToken:    e.cfg.GoogleRefreshToken,
			SDBCalendarID:         e.cfg.SDBCalendarID,
			PersonalCalendarIDs:   e.cfg.PersonalCalendarIDs,
			TodoistAPIToken:       e.cfg.TodoistAPIToken,
			TodoistProjectID:      e.cfg.TodoistProjectID,
		})

		wg.Add(1)
		go func() {
			defer wg.Done()
			e.cron.Run(ctx)
		}()
	}

	if e.cfg.EngineAutomationsEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.loopAutomations(ctx)
		}()
	}

	if e.cfg.EngineCommandPollerEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.loopDeviceCommands(ctx)
		}()
	}

	if e.cfg.EngineStatusPollEnabled && !e.cfg.EngineAutomationsEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.loopDeviceStatus(ctx)
		}()
	}

	if e.cfg.TelegramBotToken != "" && e.cfg.TelegramBotEnabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.loopTelegramWithLock(ctx)
		}()
	}

	wg.Wait()
	slog.Info("🛑 automation engine stopped")
}

// ─── Automation Loop ─────────────────────────────────────────────────────────

func (e *Engine) loopAutomations(ctx context.Context) {
	slog.Info("automation loop started")
	ticker := time.NewTicker(EngineInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := e.tick(ctx); err != nil {
				if isClosedPoolError(err) {
					slog.Warn("automation loop stopping because database pool is closed")
					return
				}
				slog.Warn("automation tick error", "error", err)
			}
		}
	}
}

func (e *Engine) tick(ctx context.Context) (err error) {
	// Recover so a panic (e.g. a malformed automation config in ShouldFire /
	// executeAction) becomes a logged tick error instead of killing the whole
	// automation loop — symmetric with the command poller's recover.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("automation tick panicked: %v", r)
		}
	}()

	now := time.Now().In(amsterdam)
	today := now.Format("2006-01-02")

	// 1. Fetch automations from PostgreSQL
	automations, err := e.autoStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		return fmt.Errorf("fetch automations: %w", err)
	}
	if len(automations) == 0 {
		return nil
	}

	// 2. Fetch today's schedule from PostgreSQL
	diensten, err := e.schedStore.ListByDate(ctx, e.cfg.HomeappUserID, today)
	if err != nil {
		slog.Warn("schedule fetch failed, continuing without", "error", err)
	}

	todayShiftTypes := make(map[string]bool)
	for _, d := range diensten {
		if d.Status != "VERWIJDERD" && d.Status != "Gedraaid" {
			if d.ShiftType != "" {
				todayShiftTypes[d.ShiftType] = true
			}
		}
	}

	// 3. Get device map from PostgreSQL
	deviceMap, err := e.getDeviceMap(ctx)
	if err != nil || len(deviceMap) == 0 {
		return nil
	}

	// 4. Check each automation
	for _, auto := range automations {
		if !auto.Enabled {
			continue
		}

		autoID := auto.ID.String()

		e.firedMu.Lock()
		last, exists := e.firedAt[autoID]
		e.firedMu.Unlock()
		if exists && now.Sub(last).Seconds() < MinFireInterval {
			continue
		}
		// The in-memory firedAt is lost on restart; fall back to the persisted
		// last_fired_at so a process restart inside the window can't double-fire.
		if !exists && auto.LastFiredAt != nil && now.Sub(*auto.LastFiredAt).Seconds() < MinFireInterval {
			continue
		}

		// Convert typed AutomationRow to map for trigger/action evaluation
		autoMap := automationRowToMap(auto)

		if !ShouldFire(autoMap, now, todayShiftTypes, e.firedAt) {
			continue
		}

		slog.Info("⚡ automation fires!", "name", auto.Name, "id", autoID)
		e.executeAction(ctx, autoMap, deviceMap)

		e.firedMu.Lock()
		e.firedAt[autoID] = now
		e.firedMu.Unlock()

		if err := e.autoStore.MarkFired(ctx, auto.ID); err != nil {
			slog.Warn("markFired failed", "id", autoID, "error", err)
		}
	}

	// Cleanup old fired entries (>1 hour)
	cutoff := now.Add(-time.Hour)
	e.firedMu.Lock()
	for k, v := range e.firedAt {
		if v.Before(cutoff) {
			delete(e.firedAt, k)
		}
	}
	e.firedMu.Unlock()

	// Device status poll every N ticks
	e.tickCount++
	if e.cfg.EngineStatusPollEnabled && e.tickCount%StatusPollEvery == 0 {
		e.pollDeviceStatus(ctx)
	}

	return nil
}

// ─── Action Execution ────────────────────────────────────────────────────────

func (e *Engine) executeAction(ctx context.Context, auto map[string]any, deviceMap map[string]deviceInfo) {
	action, _ := auto["action"].(map[string]any)
	if action == nil {
		return
	}

	actionType, _ := action["type"].(string)
	if actionType == "" {
		actionType = "on"
	}

	var infos []deviceInfo
	if deviceIDs, ok := action["deviceIds"].([]any); ok && len(deviceIDs) > 0 {
		for _, did := range deviceIDs {
			if id, ok := did.(string); ok {
				if info, exists := deviceMap[id]; exists {
					infos = append(infos, info)
				}
			}
		}
	} else {
		for _, info := range deviceMap {
			infos = append(infos, info)
		}
	}

	if len(infos) == 0 {
		name, _ := auto["name"].(string)
		slog.Warn("no devices for automation", "name", name)
		return
	}

	var wg sync.WaitGroup
	for _, info := range infos {
		wg.Add(1)
		go func(di deviceInfo) {
			defer wg.Done()
			e.applyAction(ctx, di, actionType, action)
		}(info)
	}
	wg.Wait()
}

func (e *Engine) applyAction(ctx context.Context, di deviceInfo, actionType string, action map[string]any) {
	if e.cfg.QueueLightCommands() {
		if di.ID == uuid.Nil {
			slog.Warn("cannot queue automation for fallback device without UUID", "type", actionType, "ip", di.IP)
			return
		}
		command, ok := buildDeviceCommandFromAction(actionType, action)
		if !ok {
			slog.Warn("unknown action type", "type", actionType, "ip", di.IP)
			return
		}
		if err := e.enqueueDeviceCommand(ctx, &di.ID, command); err != nil {
			slog.Warn("queue automation command failed", "type", actionType, "device", di.ID, "error", err)
		}
		return
	}

	ip := di.IP
	var err error

	switch actionType {
	case "off":
		err = e.wiz.TurnOff(ip)
	case "on":
		err = e.wiz.TurnOn(ip)
	case "brightness":
		b := getIntField(action, "brightness", 80)
		err = e.wiz.SetBrightness(ip, b)
	case "color_temp":
		mireds := getIntField(action, "colorTempMireds", 250)
		kelvin := wiz.MiredsToKelvin(mireds)
		err = e.wiz.SetColorTemp(ip, kelvin)
	case "scene":
		// Support integer scene_id (from Telegram commands) and string sceneId (from automations)
		if sid := getIntField(action, "scene_id", 0); sid > 0 {
			err = e.wiz.SetScene(ip, sid)
		} else {
			sceneKey := getStringField(action, "sceneId", "helder")
			sceneDef, ok := SceneDefinitions[sceneKey]
			if !ok {
				sceneDef = SceneDefinitions["helder"]
			}
			err = e.wiz.SetState(ip, sceneDef)
		}
	case "color":
		hexColor := getStringField(action, "colorHex", "#ffffff")
		r, g, b := wiz.HexToRGB(hexColor)
		err = e.wiz.SetColor(ip, r, g, b)
	default:
		slog.Warn("unknown action type", "type", actionType, "ip", ip)
		return
	}

	if err != nil {
		slog.Warn("WiZ action failed", "type", actionType, "ip", ip, "error", err)
	}
}

// ─── Device Map (PostgreSQL) ─────────────────────────────────────────────────

type deviceInfo struct {
	ID         uuid.UUID
	IP         string
	DeviceType string
}

func (e *Engine) getDeviceMap(ctx context.Context) (map[string]deviceInfo, error) {
	devices, err := e.devStore.GetAll(ctx, 0, 100)
	if err != nil {
		slog.Warn("device map fetch failed", "error", err)
		return e.fallbackDeviceMap(), nil
	}

	result := make(map[string]deviceInfo, len(devices))
	for _, d := range devices {
		ip := ""
		if d.IPAddress != nil {
			ip = *d.IPAddress
		}
		if ip == "" {
			continue
		}
		dt := d.DeviceType
		if dt == "" {
			dt = "color_light"
		}
		result[d.ID.String()] = deviceInfo{ID: d.ID, IP: ip, DeviceType: dt}
	}

	// Fall back to WIZ_DEVICE_IPS when DB has no devices with IPs
	if len(result) == 0 {
		return e.fallbackDeviceMap(), nil
	}
	return result, nil
}

func (e *Engine) fallbackDeviceMap() map[string]deviceInfo {
	if e.cfg.WizDeviceIPs == "" {
		return nil
	}
	result := make(map[string]deviceInfo)
	for _, ip := range splitComma(e.cfg.WizDeviceIPs) {
		result[ip] = deviceInfo{IP: ip, DeviceType: "color_light"}
	}
	return result
}

// ─── Device Status Poller ────────────────────────────────────────────────────

func (e *Engine) loopDeviceStatus(ctx context.Context) {
	slog.Info("device status poller started")
	ticker := time.NewTicker(time.Duration(StatusPollEvery) * EngineInterval)
	defer ticker.Stop()

	e.pollDeviceStatus(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			e.pollDeviceStatus(ctx)
		}
	}
}

func (e *Engine) pollDeviceStatus(ctx context.Context) {
	slog.Info("🔍 device status poll started")
	deviceMap, err := e.getDeviceMap(ctx)
	if err != nil || len(deviceMap) == 0 {
		return
	}

	for idStr, info := range deviceMap {
		state, err := e.wiz.GetState(info.IP)
		status := "online"
		if err != nil {
			status = "offline"
		}

		id, parseErr := uuid.Parse(idStr)
		if parseErr != nil {
			continue
		}
		if err := e.devStore.SetStatus(ctx, id, status); err != nil {
			slog.Debug("status update failed", "device", idStr, "error", err)
		}
		if state != nil {
			patch := map[string]any{
				"on":         state.On,
				"brightness": state.Brightness,
				"color_temp": state.ColorTemp,
				"r":          state.R,
				"g":          state.G,
				"b":          state.B,
			}
			if err := e.devStore.UpdateState(ctx, id, patch); err != nil {
				slog.Debug("state update failed", "device", idStr, "error", err)
			}
		}
	}
	slog.Info("✅ device status poll done", "count", len(deviceMap))
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func automationRowToMap(a model.AutomationRow) map[string]any {
	m := map[string]any{
		"_id":     a.ID.String(),
		"name":    a.Name,
		"enabled": a.Enabled,
	}
	if a.LastFiredAt != nil {
		m["lastFiredAt"] = a.LastFiredAt.UnixMilli()
	}

	var trigger map[string]any
	if len(a.TriggerConfig) > 0 {
		_ = json.Unmarshal(a.TriggerConfig, &trigger)
	}
	m["trigger"] = trigger

	var action map[string]any
	if len(a.ActionConfig) > 0 {
		_ = json.Unmarshal(a.ActionConfig, &action)
	}
	m["action"] = action

	return m
}

func getIntField(m map[string]any, key string, fallback int) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}

func getStringField(m map[string]any, key, fallback string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

func splitComma(s string) []string {
	var result []string
	for _, p := range splitString(s, ',') {
		p = trimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

func splitString(s string, sep byte) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[start:end]
}
