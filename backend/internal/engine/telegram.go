package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
	"github.com/google/uuid"
)

// loopTelegram polls for Telegram updates and processes them natively in Go.
func (e *Engine) loopTelegram(ctx context.Context) {
	token := e.cfg.TelegramBotToken
	if token == "" {
		slog.Warn("TELEGRAM_BOT_TOKEN not set, telegram poller disabled")
		return
	}

	slog.Info("🤖 telegram poller started (native Go)")

	client := tg.NewClient(token)
	_ = client.DeleteWebhook(false)

	var offset int64
	backoff := 3 * time.Second

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := client.GetUpdates(offset, 25)
		if err != nil {
			slog.Error("telegram getUpdates failed", "error", err, "backoff", backoff)
			sleepCtx(ctx, backoff)
			backoff *= 2
			if backoff > 60*time.Second {
				backoff = 60 * time.Second
			}
			continue
		}

		// Reset backoff on success
		backoff = 3 * time.Second

		if len(updates) > 0 {
			slog.Info("📩 telegram updates received", "count", len(updates))
		}

		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			go func(u tg.Update) {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("telegram processUpdate panic", "recover", r)
					}
				}()
				e.processUpdate(ctx, client, u)
			}(update)
		}

		sleepCtx(ctx, 100*time.Millisecond)
	}
}

func (e *Engine) processUpdate(ctx context.Context, client *tg.Client, update tg.Update) {
	// Handle Callback Queries (Button Clicks)
	if cb := update.CallbackQuery; cb != nil {
		if cb.Message == nil || cb.Message.Chat == nil {
			return
		}
		chatID := cb.Message.Chat.ID

		ownerID := e.cfg.TelegramChatID
		if ownerID == "" || strconv.FormatInt(chatID, 10) != ownerID {
			_ = client.AnswerCallbackQuery(cb.ID, "Ongeautoriseerd.")
			return
		}

		// Acknowledge the click immediately so the loading spinner goes away
		_ = client.AnswerCallbackQuery(cb.ID, "")

		// Process the callback data exactly as if the user typed it
		e.processText(ctx, client, chatID, strings.TrimSpace(cb.Data))
		return
	}

	msg := update.Message
	if msg == nil || msg.Chat == nil {
		return
	}
	chatID := msg.Chat.ID

	// Security: owner-only
	ownerID := e.cfg.TelegramChatID
	if ownerID == "" || strconv.FormatInt(chatID, 10) != ownerID {
		_ = client.SendMessage(chatID, "Je bent niet geautoriseerd om deze bot te gebruiken.")
		return
	}

	// Voice message → Groq Whisper
	voice := msg.Voice
	if voice == nil {
		voice = msg.Audio
	}
	if voice != nil && msg.Text == "" {
		e.handleVoice(ctx, client, chatID, voice.FileID)
		return
	}

	if msg.Text == "" {
		return
	}

	e.processText(ctx, client, chatID, strings.TrimSpace(msg.Text))
}

// sendTypingLoop keeps the "typing..." indicator alive during long AI tasks.
func sendTypingLoop(ctx context.Context, client *tg.Client, chatID int64) context.CancelFunc {
	tCtx, cancel := context.WithCancel(ctx)
	go func() {
		_ = client.SendTyping(chatID)
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-tCtx.Done():
				return
			case <-ticker.C:
				_ = client.SendTyping(chatID)
			}
		}
	}()
	return cancel
}

func (e *Engine) handleVoice(ctx context.Context, client *tg.Client, chatID int64, fileID string) {
	_ = client.SendTyping(chatID)

	filePath, err := client.GetFile(fileID)
	if err != nil {
		_ = client.SendMessage(chatID, fmt.Sprintf("Fout: %s", err.Error()))
		return
	}
	audio, err := client.DownloadFile(filePath)
	if err != nil {
		_ = client.SendMessage(chatID, fmt.Sprintf("Fout: %s", err.Error()))
		return
	}

	groqKey := e.cfg.GroqAPIKey
	if groqKey == "" {
		_ = client.SendMessage(chatID, "GROQ_API_KEY niet geconfigureerd")
		return
	}

	transcript, err := tg.TranscribeVoice(groqKey, audio, "voice.ogg")
	if err != nil {
		_ = client.SendMessage(chatID, fmt.Sprintf("Fout: %s", err.Error()))
		return
	}

	if strings.TrimSpace(transcript) == "" {
		_ = client.SendMessage(chatID, "Kon geen spraak herkennen.")
		return
	}

	_ = client.SendMessage(chatID, fmt.Sprintf("\"%s\"", transcript))
	e.processText(ctx, client, chatID, transcript)
}

func (e *Engine) processText(ctx context.Context, client *tg.Client, chatID int64, text string) {
	// Save user message
	chatStore := store.NewChatStore(e.db.Pool)
	_ = chatStore.SaveMessage(ctx, chatID, "user", text, nil)

	// Built-in commands
	switch {
	case text == "/start":
		e.handleStart(ctx, client, chatID)
		return
	case text == "/help":
		_ = client.SendMessageWithKeyboard(chatID, buildHelpText(), buildMainMenu())
		return
	case text == "/status" || text == "/health":
		_ = client.SendMessage(chatID, "⚙️ Go backend actief")
		return
	case text == "/ai":
		e.handleAIStatus(ctx, client, chatID)
		return
	case text == "/notehelp":
		_ = client.SendMessageWithKeyboard(chatID, buildNoteHelpText(), buildNotesMenu())
		return
	case text == "/voicehelp":
		_ = client.SendMessageWithKeyboard(chatID, buildVoiceHelpText(), buildMainMenu())
		return
	case text == "/lampen":
		e.handleLampStatus(ctx, client, chatID)
		return
	case text == "/notities":
		e.handleNotitiesDashboard(ctx, client, chatID)
		return
	case text == "/vandaag":
		e.handleVandaagNotities(ctx, client, chatID)
		return
	case text == "/week":
		e.handleWeekNotities(ctx, client, chatID)
		return
	case strings.HasPrefix(text, "/noteer "):
		e.handleQuickNote(ctx, client, chatID, strings.TrimPrefix(text, "/noteer "))
		return
	case strings.HasPrefix(text, "note_read_"):
		e.handleNoteRead(ctx, client, chatID, strings.TrimPrefix(text, "note_read_"))
		return
	case strings.HasPrefix(text, "note_pin_"):
		e.handleNotePin(ctx, client, chatID, strings.TrimPrefix(text, "note_pin_"))
		return
	case strings.HasPrefix(text, "note_archive_"):
		e.handleNoteArchive(ctx, client, chatID, strings.TrimPrefix(text, "note_archive_"))
		return
	}

	agentHint := ""
	if expanded, hint, ok := expandTelegramCommand(text); ok {
		text = expanded
		agentHint = hint
	}

	// Lamp command detection → execute via WiZ UDP
	if cmd := detectLampCommand(text); cmd != nil {
		slog.Info("💡 lamp command detected", "beschrijving", cmd.beschrijving, "chat", chatID)
		_ = client.SendTyping(chatID)

		if e.cfg.QueueLightCommands() {
			if err := e.enqueueDeviceCommand(ctx, nil, cmd.wizParams); err != nil {
				slog.Warn("queue telegram lamp command failed", "error", err)
				_ = client.SendMessage(chatID, "⚠️ Lampopdracht kon niet in de wachtrij.")
				return
			}
			reply := fmt.Sprintf("💡 %s — opdracht staat in de wachtrij", cmd.beschrijving)
			_ = client.SendMessage(chatID, reply)
			agentID := "lampen"
			_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
			return
		}

		// Get all device IPs
		deviceMap, err := e.getDeviceMap(ctx)
		if err != nil || len(deviceMap) == 0 {
			_ = client.SendMessage(chatID, "⚠️ Geen lampen gevonden.")
			return
		}

		// Send raw setPilot params to each device
		var success, fail int
		for _, di := range deviceMap {
			_, wizErr := e.wiz.SendCommand(di.IP, "setPilot", cmd.wizParams)
			if wizErr != nil {
				slog.Warn("WiZ command failed", "ip", di.IP, "error", wizErr)
				fail++
			} else {
				slog.Info("WiZ command OK", "ip", di.IP, "action", cmd.beschrijving)
				success++
			}
		}

		reply := fmt.Sprintf("💡 %s — %d/%d lampen", cmd.beschrijving, success, success+fail)
		_ = client.SendMessage(chatID, reply)
		agentID := "lampen"
		_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
		return
	}

	// Route to Grok AI with continuous typing
	stopTyping := sendTypingLoop(ctx, client, chatID)
	defer stopTyping()

	agentID := routeFreeText(text)
	if agentHint != "" {
		agentID = agentHint
	}
	agent := ai.GetAgent(agentID)
	if agent == nil {
		agent = ai.GetAgent("brain")
		agentID = "brain"
	}

	// Load chat history
	history, _ := chatStore.GetHistory(ctx, chatID, 10)
	var aiHistory []ai.Message
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			content := m.Content
			aiHistory = append(aiHistory, ai.Message{Role: m.Role, Content: &content})
		}
	}
	// Remove the last one (current user msg already in history)
	if len(aiHistory) > 0 {
		aiHistory = aiHistory[:len(aiHistory)-1]
	}

	// Build context and call Grok
	grokKey := e.cfg.GrokAPIKey
	if grokKey == "" {
		_ = client.SendMessage(chatID, "GROK_API_KEY niet geconfigureerd")
		return
	}

	grokClient := ai.NewGrokClientWithOptions(grokKey, e.cfg.GrokModel, e.cfg.GrokReasoningEffort)
	if hasExternalNewsIntent(strings.ToLower(text)) {
		agentID := "brain"
		result := grokClient.SearchWeb(ctx, text)
		reply := ""
		if result.OK && result.Antwoord != "" {
			reply = normalizeAssistantText(result.Antwoord)
		} else {
			reply = "❌ " + result.Error
		}
		_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
		_ = client.SendMessage(chatID, reply)
		return
	}

	tools := ai.GetToolsForAgent(agentID, ai.AllTools)
	prompt := ai.BuildSystemPrompt(agent, map[string]any{"status": "Go backend"}, tools)

	executor := NewHomeBotExecutor(e.db.Pool, e.cfg.HomeappUserID)
	result := grokClient.Chat(ctx, prompt, text, aiHistory, tools, executor)

	var reply string
	if result.OK && result.Antwoord != "" {
		reply = normalizeAssistantText(result.Antwoord)
	} else {
		reply = "❌ " + result.Error
	}

	_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
	_ = client.SendMessage(chatID, reply)
}

// ─── Command Routing ────────────────────────────────────────────────────────

var commandMap = map[string]string{
	"/brain": "brain", "/briefing": "brain", "/dashboard": "dashboard",
	"/lampen": "lampen", "/rooster": "rooster", "/afspraak": "agenda",
	"/agenda": "agenda", "/calendar": "agenda", "/finance": "finance",
	"/email": "email", "/inbox": "email", "/compose": "email",
	"/triage": "email", "/search": "email", "/notities": "notes",
	"/noteer": "notes", "/automations": "automations", "/habits": "habits",
	"/streak": "habits", "/check": "habits",
}

func expandTelegramCommand(text string) (expanded string, agentHint string, ok bool) {
	cmd := strings.ToLower(strings.TrimSpace(strings.Split(text, " ")[0]))
	cmd = strings.Split(cmd, "@")[0]

	switch cmd {
	case "/briefing", "/brain", "/dashboard":
		return "Geef mij een compacte dagbriefing voor vandaag. Combineer planning, werkrooster, afspraken, notities, habits, email, lampen en systeemstatus. Sluit af met maximaal drie concrete aandachtspunten.", "brain", true
	case "/planning":
		return "Wat staat er vandaag op mijn planning? Combineer werkdiensten en persoonlijke afspraken, en noem conflicten of aandachtspunten.", "agenda", true
	case "/agenda", "/calendar":
		return "Geef mijn aankomende agenda-afspraken en combineer ze waar relevant met mijn werkrooster.", "agenda", true
	case "/rooster":
		return "Geef mijn aankomende diensten en vermeld het totaal aantal uren in de periode die je ophaalt.", "rooster", true
	case "/finance":
		return "Geef een compacte finance status met saldo, salaris en opvallende transacties als die beschikbaar zijn.", "finance", true
	case "/email", "/inbox":
		return "Geef een compacte inbox status en noem welke emails aandacht nodig lijken.", "email", true
	case "/habits", "/streak":
		return "Geef mijn habit status met actieve habits, streaks, badges en een kort advies voor vandaag.", "habits", true
	case "/automations":
		return "Geef de automation en sync status van mijn systeem.", "automations", true
	case "/news", "/nieuws":
		return "Wat was het belangrijkste nieuws van de afgelopen 24 uur? Geef een compacte top 5 met bron per punt.", "brain", true
	}

	return "", "", false
}

func routeFreeText(text string) string {
	cmd := strings.Split(text, " ")[0]
	cmd = strings.ToLower(strings.Split(cmd, "@")[0])

	if agentID, ok := commandMap[cmd]; ok {
		return agentID
	}

	lower := strings.ToLower(text)
	if hasPlanningQuestion(lower) {
		return "agenda"
	}
	if hasAgendaIntent(lower) {
		return "agenda"
	}
	return "brain"
}

func hasPlanningQuestion(lower string) bool {
	if strings.Contains(lower, "op de planning") || strings.Contains(lower, "dagplanning") {
		return true
	}
	for _, dayWord := range []string{"vandaag", "morgen", "overmorgen", "deze week"} {
		if strings.Contains(lower, dayWord) && (strings.Contains(lower, "staat er") || strings.Contains(lower, "heb ik")) {
			return true
		}
	}
	return false
}

func hasAgendaIntent(lower string) bool {
	for _, kw := range []string{"agenda", "afspraak", "afspraken", "calendar", "kalender", "gepland", "planning"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasExternalNewsIntent(lower string) bool {
	for _, blocker := range []string{"nieuwsbrief", "newsletter", "emails", "mail"} {
		if strings.Contains(lower, blocker) {
			return false
		}
	}
	for _, kw := range []string{"nieuws", "actualiteit", "actualiteiten", "headlines", "breaking news", "laatste ontwikkelingen"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// ─── Lamp Detection ─────────────────────────────────────────────────────────

type lampCommand struct {
	wizParams    map[string]any // raw WiZ setPilot params
	beschrijving string
}

// scenePresets maps keywords to direct WiZ setPilot params (dimming + temp/rgb).
// More reliable than WiZ scene IDs which behave inconsistently across firmware.
var scenePresets = map[string]struct {
	params map[string]any
	naam   string
}{
	"ocean":     {map[string]any{"state": true, "r": 0, "g": 80, "b": 200, "dimming": 60}, "Ocean"},
	"romance":   {map[string]any{"state": true, "r": 200, "g": 50, "b": 80, "dimming": 40}, "Romance"},
	"sunset":    {map[string]any{"state": true, "temp": 2200, "dimming": 60}, "Sunset"},
	"party":     {map[string]any{"state": true, "r": 255, "g": 0, "b": 150, "dimming": 100}, "Party"},
	"feest":     {map[string]any{"state": true, "r": 255, "g": 0, "b": 150, "dimming": 100}, "Party"},
	"cozy":      {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"cosy":      {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"cossy":     {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"gezellig":  {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"warm":      {map[string]any{"state": true, "temp": 2700, "dimming": 50}, "Warm"},
	"focus":     {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"studeren":  {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"werk":      {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"relax":     {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"ontspan":   {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"chill":     {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"tv":        {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"film":      {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"netflix":   {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"kerst":     {map[string]any{"state": true, "r": 200, "g": 30, "b": 0, "dimming": 70}, "Christmas"},
	"christmas": {map[string]any{"state": true, "r": 200, "g": 30, "b": 0, "dimming": 70}, "Christmas"},
	"helder":    {map[string]any{"state": true, "temp": 5000, "dimming": 100}, "Helder"},
	"bright":    {map[string]any{"state": true, "temp": 5000, "dimming": 100}, "Helder"},
	"ochtend":   {map[string]any{"state": true, "temp": 2500, "dimming": 40}, "Ochtend"},
	"nacht":     {map[string]any{"state": true, "temp": 2200, "dimming": 15}, "Nacht"},
	"avond":     {map[string]any{"state": true, "temp": 2700, "dimming": 60}, "Avond"},
}

var brightnessRe = regexp.MustCompile(`(\d+)\s*%`)

func detectLampCommand(text string) *lampCommand {
	lower := strings.ToLower(text)
	isLamp := false
	for _, w := range []string{"lamp", "lampen", "licht", "lichten", "scene", "sfeer"} {
		if strings.Contains(lower, w) {
			isLamp = true
			break
		}
	}
	if !isLamp {
		return nil
	}

	// Questions → let Grok handle
	for _, w := range []string{"welke", "hoeveel", "staan", "status", "wat", "zijn er"} {
		if strings.Contains(lower, w) {
			return nil
		}
	}

	// Off
	for _, p := range []string{"uit", "off", "uitzetten"} {
		if strings.Contains(lower, p) {
			return &lampCommand{map[string]any{"state": false}, "Lampen uitzetten"}
		}
	}
	// On
	for _, p := range []string{"aan", "on", "aanzetten"} {
		if strings.Contains(lower, p) {
			return &lampCommand{map[string]any{"state": true}, "Lampen aanzetten"}
		}
	}
	// Scene presets (direct dimming+temp/rgb)
	for kw, preset := range scenePresets {
		if strings.Contains(lower, kw) {
			return &lampCommand{preset.params, "Scene: " + preset.naam}
		}
	}
	// Brightness
	if m := brightnessRe.FindStringSubmatch(lower); m != nil {
		val, _ := strconv.Atoi(m[1])
		if val > 100 {
			val = 100
		}
		if val < 10 {
			val = 10
		}
		return &lampCommand{map[string]any{"state": true, "dimming": val}, fmt.Sprintf("Helderheid naar %d%%", val)}
	}
	if strings.Contains(lower, "dim") {
		return &lampCommand{map[string]any{"state": true, "dimming": 30, "temp": 2700}, "Lampen dimmen (30%)"}
	}

	return nil
}

// ─── Telegram Start & Status Cards ─────────────────────────────────────────

type telegramStartSnapshot struct {
	Now             time.Time
	TodaySchedules  int
	TodayEvents     int
	TodayNotes      int
	ActiveNotes     int
	ActiveHabits    int
	TotalDevices    int
	OnlineDevices   int
	PendingCommands int
	NextSchedule    *model.Schedule
}

func (e *Engine) handleStart(ctx context.Context, client *tg.Client, chatID int64) {
	_ = client.SendTyping(chatID)
	snapshot := e.buildStartSnapshot(ctx)
	text := buildWelcomeText(snapshot)
	_ = client.SendMessageWithKeyboard(chatID, text, buildMainMenu())

	chatStore := store.NewChatStore(e.db.Pool)
	agentID := "brain"
	_ = chatStore.SaveMessage(ctx, chatID, "assistant", text, &agentID)
}

func (e *Engine) buildStartSnapshot(ctx context.Context) telegramStartSnapshot {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}

	sCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	end := now.AddDate(0, 0, 30).Format("2006-01-02")
	userID := e.cfg.HomeappUserID

	snapshot := telegramStartSnapshot{Now: now}

	scheduleStore := store.NewScheduleStore(e.db)
	if schedules, err := scheduleStore.ListRange(sCtx, userID, today, today); err == nil {
		snapshot.TodaySchedules = len(visibleSchedules(schedules))
	}
	if upcoming, err := scheduleStore.ListRange(sCtx, userID, today, end); err == nil {
		for _, schedule := range visibleSchedules(upcoming) {
			if scheduleStartsAfter(schedule, now, loc) {
				next := schedule
				snapshot.NextSchedule = &next
				break
			}
		}
	}

	eventStore := store.NewPersonalEventStore(e.db)
	if events, err := eventStore.ListRange(sCtx, userID, today, today); err == nil {
		snapshot.TodayEvents = len(visiblePersonalEvents(events))
	}

	noteStore := store.NewNoteStore(e.db)
	if notes, err := noteStore.List(sCtx, userID); err == nil {
		for _, note := range notes {
			if note.IsArchived {
				continue
			}
			snapshot.ActiveNotes++
			if note.Aangemaakt.In(loc).Format("2006-01-02") == today || note.Gewijzigd.In(loc).Format("2006-01-02") == today {
				snapshot.TodayNotes++
			}
		}
	}

	habitStore := store.NewHabitStore(e.db)
	if stats, err := habitStore.Stats(sCtx, userID); err == nil {
		snapshot.ActiveHabits = stats.ActiveHabits
	}

	_ = e.db.Pool.QueryRow(sCtx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'online')
		   FROM devices`,
	).Scan(&snapshot.TotalDevices, &snapshot.OnlineDevices)

	_ = e.db.Pool.QueryRow(sCtx,
		`SELECT COUNT(*) FROM device_commands WHERE user_id = $1 AND status IN ('pending', 'processing')`,
		userID,
	).Scan(&snapshot.PendingCommands)

	return snapshot
}

func (e *Engine) handleAIStatus(ctx context.Context, client *tg.Client, chatID int64) {
	mutating, confirmation := countExposedAITools()
	policyOnly := len(ai.Policies) - len(ai.AllTools)
	if policyOnly < 0 {
		policyOnly = 0
	}

	text := fmt.Sprintf(`🤖 AI status

Model: %s
Reasoning: %s
Grok chat: %s
Web-search: %s
Groq voice: %s

Agents: %d
Live tools: %d
Live mutaties: %d
Bevestiging live: %d
Beschermde policy-tools: %d

Gebruik /briefing voor een volledige dagstart of stel direct een natuurlijke vraag.`,
		emptyFallback(e.cfg.GrokModel, "onbekend"),
		emptyFallback(e.cfg.GrokReasoningEffort, "default"),
		configStatus(e.cfg.GrokAPIKey != ""),
		configStatus(e.cfg.GrokAPIKey != ""),
		configStatus(e.cfg.GroqAPIKey != ""),
		len(ai.Registry),
		len(ai.AllTools),
		mutating,
		confirmation,
		policyOnly,
	)

	_ = client.SendMessageWithKeyboard(chatID, text, tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "🧠 Dagbriefing", CallbackData: "/briefing"},
				{Text: "📍 Planning", CallbackData: "/planning"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	})
}

func (e *Engine) handleLampStatus(ctx context.Context, client *tg.Client, chatID int64) {
	dStore := store.NewDeviceStore(e.db)
	devices, err := dStore.GetAll(ctx, 0, 100)
	if err != nil {
		_ = client.SendMessage(chatID, "⚠️ Lampstatus kon niet worden opgehaald.")
		return
	}

	online := 0
	on := 0
	names := make([]string, 0, len(devices))
	for _, device := range devices {
		if device.Status == "online" {
			online++
		}
		if deviceIsOn(device) {
			on++
		}
		if len(names) < 6 {
			names = append(names, device.Name)
		}
	}

	text := fmt.Sprintf("💡 Lampen\n\nOnline: %d/%d\nAan: %d\nQueue: %s\n", online, len(devices), on, queueModeLabel(e.cfg.QueueLightCommands()))
	if len(names) > 0 {
		text += "\nBekend: " + strings.Join(names, ", ")
	}
	text += "\n\nDirect bedienen of typ natuurlijk: lampen 50%, scene focus, lampen uit."

	_ = client.SendMessageWithKeyboard(chatID, text, buildLampMenu())
}

// ─── Native Telegram Dashboards ─────────────────────────────────────────────

func (e *Engine) handleNotitiesDashboard(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	if len(notes) == 0 {
		_ = client.SendMessage(chatID, "📝 Je hebt nog geen notities. Stuur een spraakbericht of typ een idee, en ik sla het voor je op!")
		return
	}

	// Neem de top 5 notities
	limit := 5
	if len(notes) < limit {
		limit = len(notes)
	}
	topNotes := notes[:limit]

	var keyboard [][]tg.InlineKeyboardButton
	text := "📝 **Jouw Laatste Notities**\n\n"

	for i, n := range topNotes {
		titel := "Naamloze notitie"
		if n.Titel != nil && *n.Titel != "" {
			titel = *n.Titel
		}
		pinStr := ""
		if n.IsPinned {
			pinStr = "📌 "
		}

		text += fmt.Sprintf("%d. %s%s\n", i+1, pinStr, titel)
		if len(n.Tags) > 0 {
			text += fmt.Sprintf("   🏷️ %s\n", strings.Join(n.Tags, ", "))
		}

		row := []tg.InlineKeyboardButton{
			{Text: fmt.Sprintf("👁️ %d", i+1), CallbackData: "note_read_" + n.ID.String()},
			{Text: "📌", CallbackData: "note_pin_" + n.ID.String()},
			{Text: "📥", CallbackData: "note_archive_" + n.ID.String()},
		}
		keyboard = append(keyboard, row)
	}

	text += "\n_Tip: Reageer in de chat met een idee om direct een nieuwe notitie aan te maken._"

	markup := tg.InlineKeyboardMarkup{InlineKeyboard: keyboard}
	_ = client.SendMessageWithKeyboard(chatID, text, markup)
}

func (e *Engine) handleNoteRead(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)
	note, err := nStore.Get(ctx, id)
	if err != nil {
		_ = client.SendMessage(chatID, "Notitie niet gevonden.")
		return
	}

	titel := "Naamloze notitie"
	if note.Titel != nil && *note.Titel != "" {
		titel = *note.Titel
	}

	text := fmt.Sprintf("📝 **%s**\n", titel)
	if len(note.Tags) > 0 {
		text += fmt.Sprintf("Tags: %s\n", strings.Join(note.Tags, ", "))
	}
	text += fmt.Sprintf("\n%s\n\n_Laatst gewijzigd: %s_", note.Inhoud, note.Gewijzigd.Format("02-01-2006 15:04"))

	_ = client.SendMessage(chatID, text)
}

func (e *Engine) handleNoteArchive(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)

	_, err = nStore.Update(ctx, id, map[string]any{"is_archived": true})
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij archiveren.")
		return
	}

	_ = client.SendMessage(chatID, "✅ Notitie gearchiveerd.")
	// Refresh dashboard
	e.handleNotitiesDashboard(ctx, client, chatID)
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func normalizeAssistantText(text string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		if t, ok := parsed["telegramText"].(string); ok {
			return stripTelegramPlainText(t)
		}
		if t, ok := parsed["antwoord"].(string); ok {
			return stripTelegramPlainText(t)
		}
	}
	return stripTelegramPlainText(text)
}

var markdownLinkRe = regexp.MustCompile(`\[(.*?)\]\((https?://[^)]+)\)`)

func stripTelegramPlainText(text string) string {
	text = markdownLinkRe.ReplaceAllString(text, "$1 ($2)")
	for _, token := range []string{"**", "__", "`"} {
		text = strings.ReplaceAll(text, token, "")
	}
	for _, prefix := range []string{"### ", "## ", "# "} {
		text = strings.ReplaceAll(text, prefix, "")
	}
	return strings.TrimSpace(text)
}

func buildWelcomeText(snapshot telegramStartSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "👋 Jeffries HomeBot\n")
	fmt.Fprintf(&b, "AI cockpit actief — %s %02d %s %s\n\n",
		dutchDayName(snapshot.Now.Weekday()),
		snapshot.Now.Day(),
		dutchMonthName(snapshot.Now.Month()),
		snapshot.Now.Format("15:04"),
	)
	fmt.Fprintf(&b, "📍 Vandaag\n")
	fmt.Fprintf(&b, "• Werk: %s\n", pluralNL(snapshot.TodaySchedules, "dienst", "diensten"))
	fmt.Fprintf(&b, "• Agenda: %s\n", pluralNL(snapshot.TodayEvents, "afspraak", "afspraken"))
	fmt.Fprintf(&b, "• Notities: %d vandaag / %d actief\n", snapshot.TodayNotes, snapshot.ActiveNotes)
	fmt.Fprintf(&b, "• Habits: %d actief\n\n", snapshot.ActiveHabits)
	fmt.Fprintf(&b, "⏭️ Volgende dienst\n")
	fmt.Fprintf(&b, "%s\n\n", formatNextSchedule(snapshot.NextSchedule, snapshot.Now))
	fmt.Fprintf(&b, "🏠 Systeem\n")
	fmt.Fprintf(&b, "• Lampen: %d/%d online\n", snapshot.OnlineDevices, snapshot.TotalDevices)
	fmt.Fprintf(&b, "• Bridge queue: %d actief\n", snapshot.PendingCommands)
	fmt.Fprintf(&b, "• AI: %d agents / %d tools live\n\n", len(ai.Registry), len(ai.AllTools))
	fmt.Fprintf(&b, "Typ of spreek natuurlijk, bijvoorbeeld:\n")
	fmt.Fprintf(&b, "• wat staat er vandaag op mijn planning?\n")
	fmt.Fprintf(&b, "• geef mijn dagbriefing\n")
	fmt.Fprintf(&b, "• noteer: bel morgen terug")
	return b.String()
}

func buildHelpText() string {
	return "🏠 Jeffries HomeBot\n🧠 Vrije tekst gaat standaard naar Jeffries Brain.\n\n/start — AI cockpit\n/briefing — complete dagbriefing\n/planning — planning vandaag\n/ai — AI status en tools\n/status — backend health\n/lampen — lamp status en snelle acties\n/rooster — weekplanning\n/agenda — afspraken\n/finance — salaris & transacties\n/email — inbox\n/notities — notities\n/vandaag — notities van vandaag\n/week — weekoverzicht notities\n/noteer [tekst] — snelle notitie\n/habits — habits\n/news — nieuws via web-search\n\n💡 Lamp bediening: 'lampen uit', 'lampen 50%', 'scene focus'\n🎙️ Spraakberichten worden automatisch herkend."
}

func buildMainMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "🧠 Dagbriefing", CallbackData: "/briefing"},
				{Text: "📍 Planning", CallbackData: "/planning"},
			},
			{
				{Text: "📅 Agenda", CallbackData: "/agenda"},
				{Text: "📋 Werkrooster", CallbackData: "/rooster"},
			},
			{
				{Text: "💡 Lampen", CallbackData: "/lampen"},
				{Text: "🌙 Nacht", CallbackData: "lampen nacht"},
			},
			{
				{Text: "📝 Notities", CallbackData: "/notities"},
				{Text: "✍️ Noteer", CallbackData: "/notehelp"},
			},
			{
				{Text: "💰 Finance", CallbackData: "/finance"},
				{Text: "📧 Inbox", CallbackData: "/email"},
			},
			{
				{Text: "🔎 Nieuws", CallbackData: "/news"},
				{Text: "🤖 AI status", CallbackData: "/ai"},
			},
			{
				{Text: "🎙️ Spraak", CallbackData: "/voicehelp"},
				{Text: "❔ Help", CallbackData: "/help"},
			},
		},
	}
}

func buildLampMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "💡 Aan", CallbackData: "lampen aan"},
				{Text: "🌑 Uit", CallbackData: "lampen uit"},
			},
			{
				{Text: "🌅 Ochtend", CallbackData: "lampen ochtend"},
				{Text: "🌙 Nacht", CallbackData: "lampen nacht"},
			},
			{
				{Text: "🎯 Focus", CallbackData: "lampen focus"},
				{Text: "📺 TV", CallbackData: "lampen tv"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	}
}

func buildNotesMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "📝 Notities", CallbackData: "/notities"},
				{Text: "📍 Vandaag", CallbackData: "/vandaag"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	}
}

func buildNoteHelpText() string {
	return "✍️ Snel noteren\n\nGebruik:\n/noteer jouw tekst\n\nVoorbeelden:\n/noteer DKL evaluatie voorbereiden\n/noteer idee: dashboard start sneller maken\n\nLosse ideeën in gewone chat of via spraak kan Brain ook automatisch als notitie opslaan."
}

func buildVoiceHelpText() string {
	return "🎙️ Spraak in Telegram\n\nStuur een voice message en ik transcribeer hem met Groq Whisper. Daarna routeert Brain automatisch naar planning, notities, lampen, mail of een andere agent.\n\nVoorbeelden:\n• wat staat er morgen op mijn planning?\n• noteer dat ik HenkeWonen moet terugbellen\n• zet de lampen op nachtstand"
}

func formatNextSchedule(schedule *model.Schedule, now time.Time) string {
	if schedule == nil {
		return "Geen aankomende dienst gevonden."
	}

	label := relativeDateLabel(schedule.StartDatum, now)
	title := strings.TrimSpace(schedule.ShiftType)
	if title == "" {
		title = strings.TrimSpace(schedule.Titel)
	}
	if title == "" {
		title = "Dienst"
	}

	timeLabel := schedule.StartTijd
	if schedule.EindTijd != "" {
		timeLabel += "–" + schedule.EindTijd
	}
	if timeLabel == "" {
		timeLabel = "hele dag"
	}

	location := strings.TrimSpace(schedule.Locatie)
	if location != "" {
		return fmt.Sprintf("%s — %s (%s) · %s", title, label, timeLabel, location)
	}
	return fmt.Sprintf("%s — %s (%s)", title, label, timeLabel)
}

func scheduleStartsAfter(schedule model.Schedule, now time.Time, loc *time.Location) bool {
	start, err := parseScheduleDateTime(schedule.StartDatum, schedule.StartTijd, loc)
	if err != nil {
		return false
	}
	end, err := parseScheduleDateTime(emptyFallback(schedule.EindDatum, schedule.StartDatum), schedule.EindTijd, loc)
	if err != nil {
		end = start
	}
	if end.Before(start) {
		end = end.AddDate(0, 0, 1)
	}
	return !end.Before(now.Add(-15 * time.Minute))
}

func parseScheduleDateTime(datePart, timePart string, loc *time.Location) (time.Time, error) {
	datePart = strings.TrimSpace(datePart)
	timePart = strings.TrimSpace(timePart)
	if timePart == "" {
		return time.ParseInLocation("2006-01-02", datePart, loc)
	}
	return time.ParseInLocation("2006-01-02 15:04", datePart+" "+timePart, loc)
}

func relativeDateLabel(iso string, now time.Time) string {
	loc := now.Location()
	date, err := time.ParseInLocation("2006-01-02", iso, loc)
	if err != nil {
		return iso
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	target := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	diff := int(target.Sub(today).Hours() / 24)
	dateLabel := target.Format("02-01-2006")
	switch diff {
	case 0:
		return "vandaag (" + dateLabel + ")"
	case 1:
		return "morgen (" + dateLabel + ")"
	case 2:
		return "overmorgen (" + dateLabel + ")"
	default:
		return dutchDayName(target.Weekday()) + " (" + dateLabel + ")"
	}
}

func pluralNL(count int, singular, plural string) string {
	if count == 1 {
		return fmt.Sprintf("1 %s", singular)
	}
	return fmt.Sprintf("%d %s", count, plural)
}

func countExposedAITools() (mutating int, confirmation int) {
	for _, tool := range ai.AllTools {
		name := tool.Function.Name
		if ai.IsMutatingTool(name) {
			mutating++
		}
		if ai.RequiresConfirmation(name) {
			confirmation++
		}
	}
	return mutating, confirmation
}

func configStatus(ok bool) string {
	if ok {
		return "actief"
	}
	return "niet ingesteld"
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func queueModeLabel(queued bool) string {
	if queued {
		return "Render queue"
	}
	return "direct"
}

func deviceIsOn(device model.Device) bool {
	if device.CurrentState == nil {
		return false
	}
	for _, key := range []string{"on", "state"} {
		value, ok := device.CurrentState[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			lower := strings.ToLower(strings.TrimSpace(typed))
			return lower == "true" || lower == "on" || lower == "aan"
		}
	}
	return false
}

func (e *Engine) handleVandaagNotities(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)
	todayStr := now.Format("2006-01-02")
	dagNaam := dutchDayName(now.Weekday())
	datumStr := fmt.Sprintf("%d %s", now.Day(), dutchMonthName(now.Month()))

	var todayNotes []model.Note
	for _, n := range notes {
		if n.Aangemaakt.In(loc).Format("2006-01-02") == todayStr && !n.IsArchived {
			todayNotes = append(todayNotes, n)
		}
	}

	text := fmt.Sprintf("📝 Vandaag — %s %s\n\n", dagNaam, datumStr)

	if len(todayNotes) == 0 {
		text += "Nog geen notities vandaag.\nGebruik /noteer [tekst] of stuur een spraakbericht.\n"
	} else {
		for i, n := range todayNotes {
			titel := "Naamloze notitie"
			if n.Titel != nil && *n.Titel != "" {
				titel = *n.Titel
			}
			pinStr := ""
			if n.IsPinned {
				pinStr = "📌 "
			}
			tijdStr := n.Aangemaakt.In(loc).Format("15:04")
			text += fmt.Sprintf("%d. %s%s  (%s)\n", i+1, pinStr, titel, tijdStr)

			// Preview first line
			preview := strings.SplitN(n.Inhoud, "\n", 2)[0]
			if len(preview) > 60 {
				preview = preview[:57] + "..."
			}
			if preview != "" && preview != titel {
				text += fmt.Sprintf("   %s\n", preview)
			}

			if len(n.Tags) > 0 {
				text += fmt.Sprintf("   🏷️ %s\n", strings.Join(n.Tags, ", "))
			}
			text += "\n"
		}
	}

	text += "━━━━━━━━━━━━━━━━━━━━━"

	keyboard := tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "📋 Alle notities", CallbackData: "/notities"},
				{Text: "📅 Week overzicht", CallbackData: "/week"},
			},
		},
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

func (e *Engine) handleWeekNotities(ctx context.Context, client *tg.Client, chatID int64) {
	nStore := store.NewNoteStore(e.db)
	notes, err := nStore.List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij ophalen notities.")
		return
	}

	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Now().In(loc)

	// Find Monday of current week
	weekday := now.Weekday()
	if weekday == time.Sunday {
		weekday = 7
	}
	monday := now.AddDate(0, 0, -int(weekday-time.Monday))
	monday = time.Date(monday.Year(), monday.Month(), monday.Day(), 0, 0, 0, 0, loc)

	_, weekNr := monday.ISOWeek()
	sundayDate := monday.AddDate(0, 0, 6)

	text := fmt.Sprintf("📅 Week %d — %d %s – %d %s\n\n",
		weekNr,
		monday.Day(), dutchMonthName(monday.Month()),
		sundayDate.Day(), dutchMonthName(sundayDate.Month()))

	totalWeek := 0
	for i := 0; i < 7; i++ {
		day := monday.AddDate(0, 0, i)
		dayStr := day.Format("2006-01-02")
		dagNaam := dutchDayShort(day.Weekday())
		datumStr := fmt.Sprintf("%d %s", day.Day(), dutchMonthName(day.Month()))

		count := 0
		for _, n := range notes {
			if !n.IsArchived && n.Aangemaakt.In(loc).Format("2006-01-02") == dayStr {
				count++
			}
		}
		totalWeek += count

		indicator := "📋"
		if dayStr == now.Format("2006-01-02") {
			indicator = "📍"
		}
		countStr := fmt.Sprintf("%d notitie", count)
		if count != 1 {
			countStr += "s"
		}
		text += fmt.Sprintf("%s %s %s — %s\n", indicator, dagNaam, datumStr, countStr)
	}

	text += fmt.Sprintf("\nTotaal: %d notities deze week\n", totalWeek)
	text += "━━━━━━━━━━━━━━━━━━━━━"

	keyboard := tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "📝 Vandaag", CallbackData: "/vandaag"},
				{Text: "📋 Alle notities", CallbackData: "/notities"},
			},
		},
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

func (e *Engine) handleQuickNote(ctx context.Context, client *tg.Client, chatID int64, text string) {
	if strings.TrimSpace(text) == "" {
		_ = client.SendMessage(chatID, "Gebruik: /noteer [jouw notitie tekst]")
		return
	}

	nStore := store.NewNoteStore(e.db)
	titel := text
	if len(titel) > 80 {
		titel = titel[:77] + "..."
	}
	n, err := nStore.Create(ctx, e.cfg.HomeappUserID, model.Note{
		Titel:  &titel,
		Inhoud: text,
	})
	if err != nil {
		_ = client.SendMessage(chatID, fmt.Sprintf("Fout: %s", err.Error()))
		return
	}

	reply := fmt.Sprintf("✅ Notitie opgeslagen!\n📝 \"%s\"", titel)

	chatStore := store.NewChatStore(e.db.Pool)
	agentID := "notes"
	_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)

	keyboard := tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "👁️ Lees", CallbackData: "note_read_" + n.ID.String()},
				{Text: "📌 Pin", CallbackData: "note_pin_" + n.ID.String()},
				{Text: "📥 Archiveer", CallbackData: "note_archive_" + n.ID.String()},
			},
		},
	}
	_ = client.SendMessageWithKeyboard(chatID, reply, keyboard)
}

func (e *Engine) handleNotePin(ctx context.Context, client *tg.Client, chatID int64, noteIDStr string) {
	id, err := uuid.Parse(noteIDStr)
	if err != nil {
		_ = client.SendMessage(chatID, "Ongeldig notitie ID.")
		return
	}
	nStore := store.NewNoteStore(e.db)

	note, err := nStore.Get(ctx, id)
	if err != nil {
		_ = client.SendMessage(chatID, "Notitie niet gevonden.")
		return
	}

	newPinned := !note.IsPinned
	_, err = nStore.Update(ctx, id, map[string]any{"is_pinned": newPinned})
	if err != nil {
		_ = client.SendMessage(chatID, "Fout bij pinnen.")
		return
	}

	if newPinned {
		_ = client.SendMessage(chatID, "📌 Notitie vastgezet.")
	} else {
		_ = client.SendMessage(chatID, "📌 Pin verwijderd.")
	}
	// Refresh dashboard
	e.handleNotitiesDashboard(ctx, client, chatID)
}

func dutchDayName(d time.Weekday) string {
	names := [...]string{"zondag", "maandag", "dinsdag", "woensdag", "donderdag", "vrijdag", "zaterdag"}
	return names[d]
}

func dutchDayShort(d time.Weekday) string {
	names := [...]string{"Zo", "Ma", "Di", "Wo", "Do", "Vr", "Za"}
	return names[d]
}

func dutchMonthName(m time.Month) string {
	names := [...]string{"", "jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}
	return names[m]
}

// noopExecutor is a placeholder until tool execution is wired.
type noopExecutor struct{}

func (n *noopExecutor) Execute(_ context.Context, toolName, _ string) string {
	return fmt.Sprintf(`{"error":"Tool %s nog niet beschikbaar in Go"}`, toolName)
}
