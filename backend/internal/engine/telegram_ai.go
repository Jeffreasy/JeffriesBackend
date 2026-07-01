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
		_ = client.SendMessage(chatID, "❌ "+classifyUserFacingError(err.Error()))
		return
	}
	audio, err := client.DownloadFile(filePath)
	if err != nil {
		_ = client.SendMessage(chatID, "❌ "+classifyUserFacingError(err.Error()))
		return
	}

	groqKey := e.cfg.GroqAPIKey
	if groqKey == "" {
		_ = client.SendMessage(chatID, "❌ GROQ_API_KEY niet geconfigureerd")
		return
	}

	transcript, err := tg.TranscribeVoice(groqKey, audio, "voice.ogg")
	if err != nil {
		_ = client.SendMessage(chatID, "❌ "+classifyUserFacingError(err.Error()))
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
	return google.SharedOAuthClient(e.cfg.GoogleClientID, e.cfg.GoogleClientSecret, e.cfg.GoogleRefreshToken)
}

// liveSnapshotErrorPlaceholder logs the real error server-side and returns a
// short Dutch-safe placeholder for the live prompt context. Everything under
// "live" gets JSON-marshaled straight into ai.BuildSystemPrompt — a raw
// err.Error() there (driver errors, English text) could otherwise be echoed
// back to the user by the model.
func liveSnapshotErrorPlaceholder(name string, err error) string {
	slog.Warn("live context snapshot failed", "snapshot", name, "error", err)
	return "Live " + name + " snapshot tijdelijk niet beschikbaar."
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
			live["briefingError"] = liveSnapshotErrorPlaceholder("briefing", err)
		}
	case "laventecare":
		briefing, err := NewHomeBotExecutorWithGoogle(e.db.Pool, e.cfg.HomeappUserID, e.googleOAuthClient()).
			buildContextBriefing(ctx, contextBriefingOptions{Scope: "laventecare", Dagen: 7, Limit: 5})
		if err == nil {
			live["businessBriefing"] = briefing
		} else {
			live["businessBriefingError"] = liveSnapshotErrorPlaceholder("businessBriefing", err)
		}
	}

	switch agentID {
	case "notes", "brain", "dashboard":
		if snapshot, err := e.buildNotesAISnapshot(ctx, 8); err == nil {
			live["notes"] = snapshot
		} else {
			live["notesError"] = liveSnapshotErrorPlaceholder("notes", err)
		}
	}

	// The specialist agents below are directly selectable in Telegram (not
	// just reachable via brain), but previously got nothing but {"status":
	// "Go backend"} here — forcing a mandatory extra tool round-trip for
	// even the most common question in their own domain. Seed a small
	// compact snapshot for each, mirroring the notes pattern above.
	switch agentID {
	case "rooster":
		if snapshot, err := e.buildRoosterAISnapshot(ctx); err == nil {
			live["rooster"] = snapshot
		} else {
			live["roosterError"] = liveSnapshotErrorPlaceholder("rooster", err)
		}
	case "agenda":
		if snapshot, err := e.buildAgendaAISnapshot(ctx); err == nil {
			live["agenda"] = snapshot
		} else {
			live["agendaError"] = liveSnapshotErrorPlaceholder("agenda", err)
		}
	case "finance":
		if snapshot, err := e.buildFinanceAISnapshot(ctx); err == nil {
			live["finance"] = snapshot
		} else {
			live["financeError"] = liveSnapshotErrorPlaceholder("finance", err)
		}
	case "habits":
		if snapshot, err := e.buildHabitsAISnapshot(ctx); err == nil {
			live["habits"] = snapshot
		} else {
			live["habitsError"] = liveSnapshotErrorPlaceholder("habits", err)
		}
	}

	return live
}

// buildRoosterAISnapshot gives the rooster agent the next couple of shifts
// and their combined hours without a mandatory dienstenOpvragen round-trip.
func (e *Engine) buildRoosterAISnapshot(ctx context.Context) (map[string]any, error) {
	const want = 2
	// Fetch a buffer BEFORE filtering: ListUpcoming can return hidden/
	// deleted shifts that visibleSchedules then drops, so limiting to `want`
	// up front could leave fewer than `want` (or zero) visible items even
	// when more real upcoming shifts exist.
	events, err := store.NewScheduleStore(e.db).ListUpcoming(ctx, e.cfg.HomeappUserID, want*5)
	if err != nil {
		return nil, err
	}
	events = visibleSchedules(events)
	if len(events) > want {
		events = events[:want]
	}
	var totaalUur float64
	for _, ev := range events {
		totaalUur += ev.Duur
	}
	return map[string]any{
		"source":         "server-side live rooster snapshot",
		"aantalDiensten": len(events),
		"totaalUur":      totaalUur,
		"diensten":       events,
		"instruction":    "Compacte snapshot van de eerstvolgende 2 diensten. Gebruik dienstenOpvragen voor een specifieke periode of langere lijst.",
	}, nil
}

// buildAgendaAISnapshot gives the agenda agent the next few appointments
// without a mandatory afsprakenOpvragen round-trip.
func (e *Engine) buildAgendaAISnapshot(ctx context.Context) (map[string]any, error) {
	const want = 3
	// Same fetch-before-filter reasoning as buildRoosterAISnapshot: pending/
	// hidden personal events would otherwise consume slots before
	// visiblePersonalEvents filters them out.
	events, err := store.NewPersonalEventStore(e.db).ListUpcoming(ctx, e.cfg.HomeappUserID, want*5)
	if err != nil {
		return nil, err
	}
	events = visiblePersonalEvents(events)
	if len(events) > want {
		events = events[:want]
	}
	return map[string]any{
		"source":          "server-side live agenda snapshot",
		"aantalAfspraken": len(events),
		"afspraken":       events,
		"instruction":     "Compacte snapshot van de eerstvolgende 3 afspraken. Gebruik afsprakenOpvragen voor een specifieke periode of langere lijst.",
	}, nil
}

// buildFinanceAISnapshot gives the finance agent current saldo + this-month
// summary without a mandatory saldoOpvragen round-trip.
func (e *Engine) buildFinanceAISnapshot(ctx context.Context) (map[string]any, error) {
	txStore := store.NewTransactionStore(e.db)
	stats, err := txStore.GetStats(ctx, e.cfg.HomeappUserID)
	if err != nil {
		return nil, err
	}
	_, _, from, to := currentFinanceMonthToDate(time.Now())
	filter := store.TransactionFilter{ExcludeIntern: true, DatumVan: from, DatumTot: to, Limit: 20000}
	currentMonthTxs, _, err := txStore.ListFiltered(ctx, e.cfg.HomeappUserID, filter)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"source":         "server-side live finance snapshot",
		"stats":          stats,
		"defaultSummary": summarizeFinanceTransactions(currentMonthTxs),
		"instruction":    "stats = huidig totaalsaldo/dataset. defaultSummary = huidige maand tot nu. Gebruik saldoOpvragen/transactiesZoeken voor een andere periode of zoekterm.",
	}, nil
}

// buildHabitsAISnapshot gives the habits agent today's due habits without a
// mandatory habitsOverzicht/habitRapport round-trip.
func (e *Engine) buildHabitsAISnapshot(ctx context.Context) (map[string]any, error) {
	due, err := store.NewHabitStore(e.db).ListDueForDate(ctx, e.cfg.HomeappUserID, todayAmsterdamISO())
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"source":      "server-side live habits snapshot",
		"vandaagDue":  len(due),
		"habits":      due,
		"instruction": "habits bevat de vandaag-verschuldigde habits (frequentie/pauze/roosterfilter toegepast). Gebruik habitRapport voor streaks/badges/historie.",
	}, nil
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
	// Serialize all processing for this chat. Without this, two updates for
	// the same chat (a rapid follow-up message, or a message arriving while
	// the cron briefing is mid-flight) could each load history, save their
	// own turn, and interleave — previously masked by a "drop the last row"
	// heuristic below that assumed single-writer and could silently discard
	// the WRONG concurrent turn.
	unlock := e.lockChat(chatID)
	defer unlock()

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

	// Preflight before touching history: if we can't actually call Grok,
	// bail out before saving the user's turn, so we never leave an orphaned
	// user message in history with no assistant reply beside it.
	grokKey := e.cfg.GrokAPIKey
	if grokKey == "" {
		err := fmt.Errorf("GROK_API_KEY niet geconfigureerd")
		reply := "❌ " + classifyUserFacingError(err.Error())
		_ = client.SendMessage(chatID, reply)
		_ = chatStore.SaveMessage(ctx, chatID, "user", text, nil)
		_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
		return "", err
	}

	// Load PRIOR history, then persist the current turn — in that order, so
	// the current turn is never in the result set and there is nothing to
	// drop positionally. (Callers no longer save the user message themselves.)
	history, _ := chatStore.GetHistory(ctx, chatID, 10)
	var aiHistory []ai.Message
	for _, m := range history {
		if m.Role == "user" || m.Role == "assistant" {
			content := m.Content
			aiHistory = append(aiHistory, ai.Message{Role: m.Role, Content: &content})
		}
	}
	_ = chatStore.SaveMessage(ctx, chatID, "user", text, nil)

	grokClient := e.grok()

	// Bound the whole interaction (the multi-round tool loop, not just one HTTP
	// round) with a single overall budget so a slow loop cannot run for minutes.
	aiCtx, cancelAI := context.WithTimeout(ctx, 90*time.Second)
	defer cancelAI()

	if hasExternalNewsIntent(strings.ToLower(text)) {
		result := grokClient.SearchWeb(aiCtx, text)
		e.logAICall(ctx, agentID, "web_search", result)
		var reply string
		if result.OK && result.Antwoord != "" {
			reply = normalizeAssistantText(result.Antwoord)
		} else {
			reply = "❌ " + classifyUserFacingError(result.Error)
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
	e.logAICall(ctx, agentID, "chat", result)

	var reply string
	if result.OK && result.Antwoord != "" {
		reply = normalizeAssistantText(result.Antwoord)
	} else {
		reply = "❌ " + classifyUserFacingError(result.Error)
	}

	_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
	_ = client.SendMessage(chatID, reply)

	return reply, nil
}
