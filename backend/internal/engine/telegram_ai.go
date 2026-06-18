package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

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

func (e *Engine) googleOAuthClient() *google.OAuthClient {
	if e.cfg.GoogleClientID == "" || e.cfg.GoogleClientSecret == "" || e.cfg.GoogleRefreshToken == "" {
		return nil
	}
	return google.NewOAuthClient(e.cfg.GoogleClientID, e.cfg.GoogleClientSecret, e.cfg.GoogleRefreshToken)
}

func (e *Engine) buildAILiveContext(ctx context.Context, agentID string) map[string]any {
	live := map[string]any{"status": "Go backend"}

	switch agentID {
	case "brain", "dashboard":
		briefing, err := NewHomeBotExecutorWithGoogle(e.db.Pool, e.cfg.HomeappUserID, e.googleOAuthClient()).
			buildContextBriefing(ctx, contextBriefingOptions{Scope: "vandaag", Dagen: 2, Limit: 5})
		if err == nil {
			live["briefing"] = briefing
		} else {
			live["briefingError"] = err.Error()
		}
	case "laventecare":
		briefing, err := NewHomeBotExecutorWithGoogle(e.db.Pool, e.cfg.HomeappUserID, e.googleOAuthClient()).
			buildContextBriefing(ctx, contextBriefingOptions{Scope: "laventecare", Dagen: 7, Limit: 5})
		if err == nil {
			live["businessBriefing"] = briefing
		} else {
			live["businessBriefingError"] = err.Error()
		}
	}

	switch agentID {
	case "notes", "brain", "dashboard":
		if snapshot, err := e.buildNotesAISnapshot(ctx, 8); err == nil {
			live["notes"] = snapshot
		} else {
			live["notesError"] = err.Error()
		}
	}

	return live
}

func (e *Engine) buildNotesAISnapshot(ctx context.Context, limit int) (map[string]any, error) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	notes, err := store.NewNoteStore(e.db).List(ctx, e.cfg.HomeappUserID)
	if err != nil {
		return nil, err
	}
	active := activeNotes(notes)
	stats := buildNoteStats(active, now, loc)
	focus := selectFocusNotes(active, now, loc, limit)

	items := make([]map[string]any, 0, len(focus))
	for _, note := range focus {
		items = append(items, noteAIItem(note, now, loc))
	}

	return map[string]any{
		"source":      "server-side live note snapshot",
		"generatedAt": now.Format(time.RFC3339),
		"stats": map[string]any{
			"active":    stats.Active,
			"today":     stats.Today,
			"pinned":    stats.Pinned,
			"completed": stats.Completed,
			"attention": stats.Attention,
			"topTags":   stats.TopTags,
		},
		"focus":       items,
		"instruction": "Gebruik deze snapshot als basis voor notitievragen. Zeg niet dat er geen actieve notities zijn wanneer stats.active groter is dan 0.",
	}, nil
}

func noteAIItem(note model.Note, now time.Time, loc *time.Location) map[string]any {
	checked, total := checklistProgress(note.Inhoud)
	item := map[string]any{
		"id":       note.ID.String(),
		"title":    noteTitle(note),
		"priority": optionalNoteString(note.Prioriteit),
		"symbol":   optionalNoteString(note.Symbol),
		"businessContext": map[string]any{
			"type":  optionalNoteString(note.BusinessContextType),
			"id":    optionalNoteString(note.BusinessContextID),
			"title": optionalNoteString(note.BusinessContextTitle),
		},
		"tags":           note.Tags,
		"isPinned":       note.IsPinned,
		"isCompleted":    note.IsCompleted,
		"triageFlag":     note.TriageFlag != nil && *note.TriageFlag,
		"checklistDone":  checked,
		"checklistTotal": total,
		"attention":      noteNeedsAttention(note, now, loc),
		"updatedAt":      note.Gewijzigd.In(loc).Format(time.RFC3339),
		"summaryLine":    formatNoteListLine(note, now, loc),
	}
	if note.Deadline != nil {
		item["deadline"] = note.Deadline.In(loc).Format(time.RFC3339)
		item["deadlineLabel"] = formatNoteDeadline(*note.Deadline, now, loc)
	}
	snippet := strings.TrimSpace(note.Inhoud)
	if snippet != "" {
		item["snippet"] = truncateRunes(strings.ReplaceAll(snippet, "\n", " "), 220)
	}
	return item
}

// SendProactiveNotification sends a message to the owner unless Quiet Hours are active.
func (e *Engine) SendProactiveNotification(ctx context.Context, text string) error {
	token := e.cfg.TelegramBotToken
	chatIDStr := e.cfg.TelegramChatID
	if token == "" || !e.cfg.TelegramBotEnabled || chatIDStr == "" {
		return nil // Telegram disabled
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram chat id: %w", err)
	}

	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)

	var quietStart, quietEnd string
	err = e.db.Pool.QueryRow(ctx,
		`SELECT COALESCE(quiet_hours_start, ''), COALESCE(quiet_hours_end, '')
		   FROM brain_preferences
		  WHERE user_id = $1`,
		e.cfg.HomeappUserID,
	).Scan(&quietStart, &quietEnd)

	if err == nil && quietStart != "" && quietEnd != "" {
		if inQuietHours(now, quietStart, quietEnd) {
			slog.Info("🔇 proactive telegram notification skipped: quiet hours active", "start", quietStart, "end", quietEnd)
			return nil
		}
	}

	client := tg.NewClient(token)
	return client.SendMessage(chatID, text)
}

func inQuietHours(now time.Time, startStr, endStr string) bool {
	startStr = strings.TrimSpace(startStr)
	endStr = strings.TrimSpace(endStr)
	if startStr == "" || endStr == "" {
		return false
	}

	parseHM := func(s string) (int, int, bool) {
		parts := strings.Split(s, ":")
		if len(parts) != 2 {
			return 0, 0, false
		}
		h, err1 := strconv.Atoi(parts[0])
		m, err2 := strconv.Atoi(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0, false
		}
		return h, m, true
	}

	sh, sm, ok1 := parseHM(startStr)
	eh, em, ok2 := parseHM(endStr)
	if !ok1 || !ok2 {
		return false
	}

	currentMinutes := now.Hour()*60 + now.Minute()
	startMinutes := sh*60 + sm
	endMinutes := eh*60 + em

	if startMinutes <= endMinutes {
		return currentMinutes >= startMinutes && currentMinutes <= endMinutes
	}
	// Overlap midnight (e.g. 22:00 to 07:00)
	return currentMinutes >= startMinutes || currentMinutes <= endMinutes
}

func buildVoiceHelpText() string {
	return "🎙️ Spraak in Telegram\n\nStuur een voice message en ik transcribeer hem met Groq Whisper. Daarna routeert Brain automatisch naar planning, notities, lampen, mail of een andere agent.\n\nVoorbeelden:\n• wat staat er morgen op mijn planning?\n• noteer dat ik HenkeWonen moet terugbellen\n• zet de lampen op nachtstand"
}

// ProcessAIPrompt routes a prompt to Grok AI, executes tools, saves the history, and sends the reply.
func (e *Engine) ProcessAIPrompt(ctx context.Context, chatID int64, text string, agentID string, showTyping bool) (string, error) {
	client := tg.NewClient(e.cfg.TelegramBotToken)
	chatStore := store.NewChatStore(e.db.Pool)

	if showTyping {
		stopTyping := sendTypingLoop(ctx, client, chatID)
		defer stopTyping()
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

	grokKey := e.cfg.GrokAPIKey
	if grokKey == "" {
		err := fmt.Errorf("GROK_API_KEY niet geconfigureerd")
		_ = client.SendMessage(chatID, err.Error())
		return "", err
	}

	grokClient := e.grok()

	// Bound the whole interaction (the multi-round tool loop, not just one HTTP
	// round) with a single overall budget so a slow loop cannot run for minutes.
	aiCtx, cancelAI := context.WithTimeout(ctx, 90*time.Second)
	defer cancelAI()

	if hasExternalNewsIntent(strings.ToLower(text)) {
		result := grokClient.SearchWeb(aiCtx, text)
		var reply string
		if result.OK && result.Antwoord != "" {
			reply = normalizeAssistantText(result.Antwoord)
		} else {
			reply = "❌ " + result.Error
		}
		_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
		_ = client.SendMessage(chatID, reply)
		return reply, nil
	}

	tools := ai.GetToolsForAgent(agentID, ai.AllTools)
	prompt := ai.BuildSystemPrompt(agent, e.buildAILiveContext(ctx, agentID), tools)

	executor := NewConfirmingExecutor(
		e.db.Pool,
		e.cfg.HomeappUserID,
		agentID,
		NewHomeBotExecutorWithGoogle(e.db.Pool, e.cfg.HomeappUserID, e.googleOAuthClient()),
	)
	result := grokClient.Chat(aiCtx, prompt, text, aiHistory, tools, executor)

	var reply string
	if result.OK && result.Antwoord != "" {
		reply = normalizeAssistantText(result.Antwoord)
	} else {
		reply = "❌ " + result.Error
	}

	_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
	_ = client.SendMessage(chatID, reply)

	return reply, nil
}
