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
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
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

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		updates, err := client.GetUpdates(offset, 25)
		if err != nil {
			slog.Error("telegram getUpdates failed", "error", err)
			sleepCtx(ctx, 3*time.Second)
			continue
		}

		if len(updates) > 0 {
			slog.Info("📩 telegram updates received", "count", len(updates))
		}

		for _, update := range updates {
			func() {
				defer func() {
					if r := recover(); r != nil {
						slog.Error("telegram processUpdate panic", "recover", r)
					}
				}()
				e.processUpdate(ctx, client, update)
			}()
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
		}

		sleepCtx(ctx, 100*time.Millisecond)
	}
}

func (e *Engine) processUpdate(ctx context.Context, client *tg.Client, update tg.Update) {
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
		_ = client.SendMessage(chatID, buildWelcomeText())
		return
	case text == "/help":
		_ = client.SendMessage(chatID, buildHelpText())
		return
	case text == "/status" || text == "/health":
		_ = client.SendMessage(chatID, "⚙️ Go backend actief")
		return
	}

	// Lamp command detection → execute via WiZ UDP
	if cmd := detectLampCommand(text); cmd != nil {
		slog.Info("💡 lamp command detected", "beschrijving", cmd.beschrijving, "chat", chatID)
		_ = client.SendTyping(chatID)

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

	// Route to Grok AI
	_ = client.SendTyping(chatID)

	agentID := routeFreeText(text)
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

	grokClient := ai.NewGrokClient(grokKey)
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

func routeFreeText(text string) string {
	cmd := strings.Split(text, " ")[0]
	cmd = strings.ToLower(strings.Split(cmd, "@")[0])

	if agentID, ok := commandMap[cmd]; ok {
		return agentID
	}

	lower := strings.ToLower(text)
	if hasAgendaIntent(lower) {
		return "agenda"
	}
	return "brain"
}

func hasAgendaIntent(lower string) bool {
	for _, kw := range []string{"agenda", "afspraak", "afspraken", "calendar", "kalender", "gepland", "planning"} {
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
	"ocean":    {map[string]any{"state": true, "r": 0, "g": 80, "b": 200, "dimming": 60}, "Ocean"},
	"romance":  {map[string]any{"state": true, "r": 200, "g": 50, "b": 80, "dimming": 40}, "Romance"},
	"sunset":   {map[string]any{"state": true, "temp": 2200, "dimming": 60}, "Sunset"},
	"party":    {map[string]any{"state": true, "r": 255, "g": 0, "b": 150, "dimming": 100}, "Party"},
	"feest":    {map[string]any{"state": true, "r": 255, "g": 0, "b": 150, "dimming": 100}, "Party"},
	"cozy":     {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"cosy":     {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"cossy":    {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"gezellig": {map[string]any{"state": true, "temp": 2700, "dimming": 40}, "Cozy"},
	"warm":     {map[string]any{"state": true, "temp": 2700, "dimming": 50}, "Warm"},
	"focus":    {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"studeren": {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"werk":     {map[string]any{"state": true, "temp": 6000, "dimming": 90}, "Focus"},
	"relax":    {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"ontspan":  {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"chill":    {map[string]any{"state": true, "temp": 2500, "dimming": 30}, "Relax"},
	"tv":       {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"film":     {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"netflix":  {map[string]any{"state": true, "r": 100, "g": 0, "b": 180, "dimming": 30}, "TV Time"},
	"kerst":    {map[string]any{"state": true, "r": 200, "g": 30, "b": 0, "dimming": 70}, "Christmas"},
	"christmas": {map[string]any{"state": true, "r": 200, "g": 30, "b": 0, "dimming": 70}, "Christmas"},
	"helder":   {map[string]any{"state": true, "temp": 5000, "dimming": 100}, "Helder"},
	"bright":   {map[string]any{"state": true, "temp": 5000, "dimming": 100}, "Helder"},
	"ochtend":  {map[string]any{"state": true, "temp": 2500, "dimming": 40}, "Ochtend"},
	"nacht":    {map[string]any{"state": true, "temp": 2200, "dimming": 15}, "Nacht"},
	"avond":    {map[string]any{"state": true, "temp": 2700, "dimming": 60}, "Avond"},
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
		if val > 100 { val = 100 }
		if val < 10 { val = 10 }
		return &lampCommand{map[string]any{"state": true, "dimming": val}, fmt.Sprintf("Helderheid naar %d%%", val)}
	}
	if strings.Contains(lower, "dim") {
		return &lampCommand{map[string]any{"state": true, "dimming": 30, "temp": 2700}, "Lampen dimmen (30%)"}
	}

	return nil
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func normalizeAssistantText(text string) string {
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		if t, ok := parsed["telegramText"].(string); ok { return t }
		if t, ok := parsed["antwoord"].(string); ok { return t }
	}
	return text
}

func buildWelcomeText() string {
	return "👋 Welkom bij Jeffries HomeBot!\n\nJeffries Brain is je centrale cockpit.\nTyp of spreek — ik combineer planning, agenda, mail, notities, habits, lampen en systeemstatus.\n\n💡 'Lampen uit'  📅 'Wanneer werk ik?'\n💰 'Salaris'  📧 'Ongelezen emails'\n📝 'Noteer: ...'  ⚙️ '/status'\n\nType /help voor alle commando's."
}

func buildHelpText() string {
	return "🏠 Jeffries HomeBot\n🧠 Vrije tekst gaat standaard naar Jeffries Brain.\n\n/status — systeem health\n/brain — centrale assistent\n/lampen — lamp status\n/rooster — weekplanning\n/agenda — afspraken\n/finance — salaris & transacties\n/email — inbox\n/notities — notities\n/habits — habits\n\n💡 Lamp bediening: 'lampen uit', 'lampen 50%', 'dim'\n🎙️ Spraakberichten worden automatisch herkend."
}

// noopExecutor is a placeholder until tool execution is wired.
type noopExecutor struct{}

func (n *noopExecutor) Execute(_ context.Context, toolName, _ string) string {
	return fmt.Sprintf(`{"error":"Tool %s nog niet beschikbaar in Go"}`, toolName)
}
