package engine

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

type commandRoute struct {
	agentID   string
	expansion string
}

const (
	briefingPrompt = "Geef mij een compacte dagbriefing voor vandaag. Combineer planning, werkrooster, afspraken, notities, habits, email, lampen en systeemstatus. Sluit af met maximaal drie concrete aandachtspunten."
	planningPrompt = "Wat staat er vandaag op mijn planning? Combineer werkdiensten en persoonlijke afspraken, en noem conflicten of aandachtspunten."
	agendaPrompt   = "Geef mijn aankomende persoonlijke agenda-afspraken. Gebruik afsprakenOpvragen en combineer met planningOpvragen wanneer diensten relevant zijn. Maak duidelijk onderscheid tussen afspraken, diensten en wachtrij-items."
	roosterPrompt  = "Geef mijn aankomende diensten. Gebruik dienstenOpvragen en vermeld aantal diensten, totaalUur, eerstvolgende dienst en eventuele relevante afspraken op dezelfde dag."
	financePrompt  = "Geef een compacte finance status voor de huidige maand. Gebruik saldoOpvragen als basis: stats is alleen huidig totaalsaldo/dataset, defaultSummary is de maandanalyse. Gebruik uitgavenOverzicht zonder periode voor categorieen/merchants van de huidige maand. Noem all-time alleen als de gebruiker daarom vraagt."
	lcPrompt       = "Geef de LaventeCare cockpit. Gebruik laventecareCockpit als basis en benoem klantdossiers/klanten, contacten, leads, opdrachten/werkstreams, projecten, actiepunten, signalen, aankomende agenda/follow-ups, relevante notities, PDF dossierdocumenten en of de documentbasis is geinitialiseerd. Gebruik laventecareBillingOpvragen voor offertes, uren, facturen, open bedragen, betaalstatus en bunq-readiness. Gebruik laventecareKlantenOpvragen en laventecareContactenOpvragen bij klantvragen. Gebruik laventecareOpdrachtenOpvragen wanneer kleine of flexibele klussen relevant zijn. Gebruik laventecareKennisAdviesOpvragen voor passende templates/documenten en laventecareDossierCheckOpvragen voor dossier-volledigheid, ontbrekende bouwblokken en volgende stappen. Gebruik planningOpvragen of afsprakenOpvragen voor agenda-context, notitiesZoeken of notitiesOverzicht voor notitie-context, en laventecareDossierDocumentenOpvragen als de gebruiker naar PDFs, offertes, rapportages of dossierhistorie vraagt. Houd klantdossiers, CRM, opdrachten, commercie, agenda, notities en dossierstukken duidelijk gescheiden. Gebruik geen verzonnen CRM-data."
	emailPrompt    = "Geef een compacte inbox status en noem welke emails aandacht nodig lijken."
	habitsPrompt   = "Geef mijn habit status voor vandaag. Gebruik habitRapport als basis en benoem vandaagDue, vandaagCompleted, streaks, badges, incidenten en maximaal drie concrete adviezen."
	checkPrompt    = "Help mij een habit af te vinken. Gebruik habitRapport om de habits van vandaag te tonen en vraag kort welke habit ik wil voltooien als de naam ontbreekt."
	noteAIPrompt   = "Analyseer mijn actieve notities als slimme notitieassistent. Gebruik eerst Live Data.notes as actueel overzicht en verifieer daarna met notitiesOverzicht. Gebruik notitiesVandaag alleen voor nieuw/gewijzigd vandaag; leeg vandaag betekent niet dat er geen actieve notities zijn. Geef: 1) belangrijkste thema's, 2) open acties, 3) wat vandaag aandacht nodig heeft, 4) maximaal drie concrete vervolgstappen."
	triagePrompt   = "Doe een triage van mijn actieve notities. Gebruik eerst Live Data.notes en verifieer daarna met notitiesOverzicht. Gebruik notitiesVandaag alleen voor nieuw/gewijzigd vandaag. Sorteer op urgentie, deadline, prioriteit, incomplete checklists en triage-vlaggen. Geef een compacte actielijst voor vandaag."
	summaryPrompt  = "Vat mijn actieve notities compact samen. Gebruik eerst Live Data.notes en verifieer met notitiesOverzicht. Groepeer per thema/tag en benoem losse actiepunten apart."
	autoPrompt     = "Geef the automation en sync status van mijn systeem."
	newsPrompt     = "Wat was het belangrijkste nieuws van de afgelopen 24 uur? Geef een compacte top 5 met bron per punt."

	afspraakPrompt    = "Ik wil een nieuwe agenda-afspraak aanmaken. Vraag kort naar titel, datum en tijd als die nog ontbreken, en gebruik dan afspraakMaken."
	composePrompt     = "Ik wil een nieuwe email opstellen en versturen. Vraag kort naar ontvanger, onderwerp en inhoud als die nog ontbreken, en gebruik dan emailVersturen."
	inboxTriagePrompt = "Doe een triage van mijn inbox. Gebruik zoekEmails en/of leesEmail waar nodig. Geef een compacte actielijst: welke emails vandaag aandacht nodig hebben, welke via inboxOpruimen of bulkVerwijder opgeruimd kunnen worden, en wat de volgende stap is."
	emailSearchPrompt = "Ik wil in mijn email zoeken. Vraag kort naar het zoekwoord als dat ontbreekt, en gebruik dan zoekEmails."
)

var (
	brightnessRe = regexp.MustCompile(`(\d+)\s*%`)

	commandRegistry = map[string]commandRoute{
		"/brain":            {agentID: "brain", expansion: briefingPrompt},
		"/briefing":         {agentID: "brain", expansion: briefingPrompt},
		"/dashboard":        {agentID: "brain", expansion: briefingPrompt},
		"/planning":         {agentID: "agenda", expansion: planningPrompt},
		"/agenda":           {agentID: "agenda", expansion: agendaPrompt},
		"/calendar":         {agentID: "agenda", expansion: agendaPrompt},
		"/rooster":          {agentID: "rooster", expansion: roosterPrompt},
		"/finance":          {agentID: "finance", expansion: financePrompt},
		"/laventecare":      {agentID: "laventecare", expansion: lcPrompt},
		"/lc":               {agentID: "laventecare", expansion: lcPrompt},
		"/email":            {agentID: "email", expansion: emailPrompt},
		"/inbox":            {agentID: "email", expansion: emailPrompt},
		"/habits":           {agentID: "habits", expansion: habitsPrompt},
		"/streak":           {agentID: "habits", expansion: habitsPrompt},
		"/habitrapport":     {agentID: "habits", expansion: habitsPrompt},
		"/check":            {agentID: "habits", expansion: checkPrompt},
		"/noteai":           {agentID: "notes", expansion: noteAIPrompt},
		"/notitieai":        {agentID: "notes", expansion: noteAIPrompt},
		"/notetriage":       {agentID: "notes", expansion: triagePrompt},
		"/triagenotes":      {agentID: "notes", expansion: triagePrompt},
		"/notesamenvatting": {agentID: "notes", expansion: summaryPrompt},
		"/samenvatnotes":    {agentID: "notes", expansion: summaryPrompt},
		"/automations":      {agentID: "automations", expansion: autoPrompt},
		"/news":             {agentID: "brain", expansion: newsPrompt},
		"/nieuws":           {agentID: "brain", expansion: newsPrompt},

		"/lampen":   {agentID: "lampen"},
		"/afspraak": {agentID: "agenda", expansion: afspraakPrompt},
		"/compose":  {agentID: "email", expansion: composePrompt},
		"/triage":   {agentID: "email", expansion: inboxTriagePrompt},
		"/search":   {agentID: "email", expansion: emailSearchPrompt},
		"/notities": {agentID: "notes"},
		"/notehelp": {agentID: "notes"},
		"/noteer":   {agentID: "notes"},
		"/zoeknote": {agentID: "notes"},
		"/sync":     {agentID: "automations"},
	}
)

// telegramMenuCommands is the "/" menu registered via setMyCommands. This
// covers every genuinely distinct capability in commandRegistry — pure
// Dutch/English synonyms of an already-listed command (e.g. /calendar for
// /agenda, /inbox for /email, /lc for /laventecare, /news for /nieuws) are
// deliberately left out to avoid a cluttered popup, but stay reachable as
// typed aliases (see commandRegistry) and are all documented in /help.
func telegramMenuCommands() []tg.BotCommand {
	return []tg.BotCommand{
		{Command: "start", Description: "Startmenu en dagoverzicht"},
		{Command: "briefing", Description: "AI dagbriefing"},
		{Command: "planning", Description: "Planning (rooster + afspraken)"},
		{Command: "agenda", Description: "Agenda / afspraken"},
		{Command: "afspraak", Description: "Nieuwe agenda-afspraak aanmaken"},
		{Command: "rooster", Description: "Werkrooster en uren"},
		{Command: "finance", Description: "Saldo en transacties"},
		{Command: "laventecare", Description: "LaventeCare cockpit"},
		{Command: "email", Description: "Inbox en e-mailsignalen"},
		{Command: "compose", Description: "Nieuwe email opstellen"},
		{Command: "notities", Description: "Notities-overzicht"},
		{Command: "noteer", Description: "Snel een notitie vastleggen"},
		{Command: "zoeknote", Description: "Notities doorzoeken"},
		{Command: "vandaag", Description: "Notities van vandaag"},
		{Command: "week", Description: "Notities van deze week"},
		{Command: "noteai", Description: "AI notitie-assistent"},
		{Command: "notehelp", Description: "Notitie-commando's uitleg"},
		{Command: "habits", Description: "Habits en streaks"},
		{Command: "check", Description: "Habit snel afvinken"},
		{Command: "lampen", Description: "Lampstatus en bediening"},
		{Command: "automations", Description: "Automation- en sync-status"},
		{Command: "news", Description: "Actueel nieuws (web search)"},
		{Command: "pending", Description: "Openstaande bevestigingen"},
		{Command: "sync", Description: "Gmail/agenda sync uitvoeren"},
		{Command: "voicehelp", Description: "Spraakbericht uitleg"},
		{Command: "ai", Description: "AI-diagnose en status"},
		{Command: "help", Description: "Alle commando's"},
	}
}

// processText handles one incoming message or callback-tap. originMessageID
// is non-nil only when this call originated from a tapped inline-keyboard
// button (see processUpdate): button-originated note/pending actions use it
// to edit that message in place instead of always sending a new one, which
// would otherwise leave the original message's now-stale buttons (referring
// to an already-archived note, an already-confirmed pending action, etc.)
// live and re-tappable.
func (e *Engine) processText(ctx context.Context, client *tg.Client, chatID int64, text string, originMessageID *int64) {
	// NOTE: the user message is intentionally NOT persisted here. Slash commands,
	// callback tokens (note_*, pending_*) and lamp commands return before reaching
	// the model, so persisting up-front would pollute AI history with non-
	// conversational tokens. We only save genuine free text just before it is sent
	// to the model (see the ProcessAIPrompt call below).
	chatStore := store.NewChatStore(e.db.Pool)

	if e.handlePendingConfirmationCommand(ctx, client, chatID, text, originMessageID) {
		return
	}

	command := strings.ToLower(strings.TrimSpace(text))

	// Built-in commands
	switch {
	case command == "/start":
		e.handleStart(ctx, client, chatID)
		return
	case command == "/help":
		_ = client.SendMessageWithKeyboard(chatID, buildHelpText(), buildMainMenu())
		return
	case command == "/status" || command == "/health":
		_ = client.SendMessage(chatID, "⚙️ Go backend actief")
		return
	case command == "/ai":
		e.handleAIStatus(ctx, client, chatID)
		return
	case command == "/notehelp":
		_ = client.SendMessageWithKeyboard(chatID, buildNoteHelpText(), buildNotesMenu())
		return
	case command == "/zoeknote":
		_ = client.SendMessageWithKeyboard(chatID, buildNoteSearchHelpText(), buildNotesMenu())
		return
	case command == "/voicehelp":
		_ = client.SendMessageWithKeyboard(chatID, buildVoiceHelpText(), buildMainMenu())
		return
	case command == "/lampen":
		e.handleLampStatus(ctx, client, chatID)
		return
	case command == "/habits" || command == "/habitrapport" || command == "/streak":
		e.handleHabitStatus(ctx, client, chatID)
		return
	case command == "/finance":
		// Bare "/finance" only — matches the dual-mode pattern used by
		// /habits etc.: with trailing args, this falls through to
		// expandTelegramCommand's financePrompt AI-expansion path below
		// instead of being swallowed here (previously the HasPrefix match
		// caught "/finance <anything>" too, making financePrompt dead code).
		e.handleFinanceStatus(ctx, client, chatID, text)
		return
	case command == "/notities":
		e.handleNotitiesDashboard(ctx, client, chatID)
		return
	case command == "/vandaag":
		e.handleVandaagNotities(ctx, client, chatID)
		return
	case command == "/week":
		e.handleWeekNotities(ctx, client, chatID)
		return
	case command == "/sync":
		e.handleSync(ctx, client, chatID)
		return
	case strings.HasPrefix(command, "/noteer "):
		e.handleQuickNote(ctx, client, chatID, strings.TrimSpace(text[strings.Index(text, " ")+1:]))
		return
	case strings.HasPrefix(command, "/zoeknote "):
		e.handleNoteSearch(ctx, client, chatID, strings.TrimSpace(text[strings.Index(text, " ")+1:]))
		return
	case strings.HasPrefix(text, "note_read_"):
		e.handleNoteRead(ctx, client, chatID, strings.TrimPrefix(text, "note_read_"))
		return
	case strings.HasPrefix(text, "note_done_"):
		e.handleNoteDone(ctx, client, chatID, strings.TrimPrefix(text, "note_done_"), originMessageID)
		return
	case strings.HasPrefix(text, "note_pin_"):
		e.handleNotePin(ctx, client, chatID, strings.TrimPrefix(text, "note_pin_"), originMessageID)
		return
	case strings.HasPrefix(text, "note_archive_"):
		e.handleNoteArchive(ctx, client, chatID, strings.TrimPrefix(text, "note_archive_"), originMessageID)
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

		// Send raw setPilot params to each device concurrently
		var wg sync.WaitGroup
		var successCount, failCount int
		var mu sync.Mutex
		for _, di := range deviceMap {
			wg.Add(1)
			go func(info deviceInfo) {
				defer wg.Done()
				_, wizErr := e.wiz.SendCommand(info.IP, "setPilot", cmd.wizParams)
				mu.Lock()
				defer mu.Unlock()
				if wizErr != nil {
					slog.Warn("WiZ command failed", "ip", info.IP, "error", wizErr)
					failCount++
				} else {
					slog.Info("WiZ command OK", "ip", info.IP, "action", cmd.beschrijving)
					successCount++
				}
			}(di)
		}
		wg.Wait()

		reply := fmt.Sprintf("💡 %s — %d/%d lampen", cmd.beschrijving, successCount, successCount+failCount)
		_ = client.SendMessage(chatID, reply)
		agentID := "lampen"
		_ = chatStore.SaveMessage(ctx, chatID, "assistant", reply, &agentID)
		return
	}

	// At this point, nothing recognized the message: not a built-in switch
	// case, not a commandRegistry alias, not free-text lamp control. If it
	// still looks like a slash-command attempt (a typo like "/aproove", an
	// old/removed command, or one half-remembered), stop here with an
	// explicit Dutch hint instead of silently handing the raw "/whatever"
	// string to Grok as if it were a genuine conversational turn — the
	// bot would otherwise produce a confused AI answer with zero signal
	// that the command itself never fired, which is especially dangerous
	// for a mistyped /approve or /reject on a pending money/email action.
	if trimmed := strings.TrimSpace(text); strings.HasPrefix(trimmed, "/") {
		typed := strings.Fields(trimmed)[0]
		reply := fmt.Sprintf("❓ Onbekend commando: %s\nTyp /help voor het overzicht.", typed)
		if suggestion := closestKnownCommand(typed); suggestion != "" {
			reply += fmt.Sprintf("\nBedoelde je %s?", suggestion)
		}
		_ = client.SendMessage(chatID, reply)
		return
	}

	agentID := routeFreeText(text)
	if agentHint != "" {
		agentID = agentHint
	}

	// ProcessAIPrompt persists the user turn itself (after loading prior
	// history), so it's never saved here — see telegram_ai.go.
	_, _ = e.ProcessAIPrompt(ctx, chatID, text, agentID, true)
}

// knownCommandNames lists every command the bot actually recognizes, for
// the unknown-command hint above — commandRegistry plus the built-in
// switch/pending-flow commands that aren't in that map.
func knownCommandNames() []string {
	names := make([]string, 0, len(commandRegistry)+16)
	for cmd := range commandRegistry {
		names = append(names, cmd)
	}
	names = append(names,
		"/start", "/help", "/status", "/health", "/ai", "/voicehelp",
		"/vandaag", "/week",
		"/pending", "/bevestigingen", "/approve", "/confirm", "/akkoord",
		"/reject", "/cancel", "/annuleer",
	)
	return names
}

// closestKnownCommand returns the closest known command to a mistyped one
// (by Levenshtein distance on the command word, case-insensitive, ignoring
// the leading slash), or "" if nothing is close enough to be a useful guess.
func closestKnownCommand(typed string) string {
	needle := strings.ToLower(strings.TrimPrefix(typed, "/"))
	if needle == "" {
		return ""
	}
	best := ""
	bestDist := -1
	for _, known := range knownCommandNames() {
		candidate := strings.ToLower(strings.TrimPrefix(known, "/"))
		dist := levenshteinDistance(needle, candidate)
		// Only suggest for a genuinely close typo, scaled to word length —
		// otherwise short commands (e.g. "/ai") would match almost anything.
		maxDist := len(candidate) / 3
		if maxDist < 1 {
			maxDist = 1
		}
		if dist > maxDist {
			continue
		}
		if bestDist == -1 || dist < bestDist {
			bestDist = dist
			best = known
		}
	}
	return best
}

// levenshteinDistance computes the classic edit distance between two
// strings (insertions/deletions/substitutions), used only for the small
// (~40 command) "did you mean" lookup above.
func levenshteinDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	la, lb := len(ra), len(rb)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			min := del
			if ins < min {
				min = ins
			}
			if sub < min {
				min = sub
			}
			curr[j] = min
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func (e *Engine) handleSync(ctx context.Context, client *tg.Client, chatID int64) {
	_ = client.SendTyping(chatID)
	userID := e.cfg.HomeappUserID
	oauthClient := e.googleOAuthClient()
	if oauthClient == nil {
		_ = client.SendMessage(chatID, "❌ Google OAuth is niet geconfigureerd.")
		return
	}

	_ = client.SendMessage(chatID, "🔄 Synchronisatie gestart (Kalender & Gmail)...")

	var (
		wg sync.WaitGroup

		scheduleErr error
		personalErr error

		schedules      []google.ScheduleDienst
		personalEvents []google.PersonalEventSync
		schedulePruned int
		personalPruned int
		gmailMsg       string
	)

	// Goroutine 1: Calendar sync pipeline (Pending DB calendar changes, google shifts & personal events)
	wg.Add(1)
	go func() {
		defer wg.Done()

		// 1. Process pending calendar operations first — shared retry-aware path
		//    (records failures + dead-letters instead of silently dropping them).
		if _, deadLettered, perr := e.processPendingCalendarOps(ctx, oauthClient, userID); perr != nil {
			slog.Warn("telegram sync: pending calendar processing aborted", "error", perr)
		} else if deadLettered > 0 {
			e.alertPendingCalendarFailures(ctx, deadLettered)
		}

		// 2. Sync Google Calendar shifts
		scheduleSync, err := google.SyncScheduleDetailed(ctx, oauthClient, userID, e.cfg.SDBCalendarID)
		scheduleErr = err
		if scheduleErr == nil {
			schedules = scheduleSync.Diensten
			scheduleStore := store.NewScheduleStore(e.db)
			var scheduleImports []model.ScheduleImport
			for _, d := range schedules {
				scheduleImports = append(scheduleImports, model.ScheduleImport{
					EventID:      d.EventID,
					Titel:        d.Titel,
					StartDatum:   d.StartDatum,
					StartTijd:    d.StartTijd,
					EindDatum:    d.EindDatum,
					EindTijd:     d.EindTijd,
					Werktijd:     d.Werktijd,
					Locatie:      d.Locatie,
					Team:         d.Team,
					ShiftType:    d.ShiftType,
					Prioriteit:   d.Prioriteit,
					Duur:         d.Duur,
					Weeknr:       d.Weeknr,
					Dag:          d.Dag,
					Status:       d.Status,
					Beschrijving: d.Beschrijving,
					Heledag:      d.Heledag,
				})
			}
			if _, err := scheduleStore.BulkUpsert(ctx, userID, scheduleImports); err != nil {
				scheduleErr = err
			}
			if scheduleErr == nil {
				schedulePruned, scheduleErr = scheduleStore.PruneMissingInDateRange(
					ctx,
					userID,
					scheduleSync.PruneStartDatum,
					scheduleSync.PruneEindDatum,
					scheduleSync.FetchedEventIDs,
				)
			}
			if scheduleErr == nil {
				_ = scheduleStore.UpsertMeta(ctx, userID, "Telegram Sync", len(scheduleImports))
			}
		}

		// 3. Sync Personal calendar events
		calendarIDs := []string{"primary"}
		if e.cfg.PersonalCalendarIDs != "" {
			parts := strings.Split(e.cfg.PersonalCalendarIDs, ",")
			calendarIDs = make([]string, 0, len(parts))
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					calendarIDs = append(calendarIDs, part)
				}
			}
		}
		personalSync, err := google.SyncPersonalEventsDetailed(ctx, oauthClient, userID, calendarIDs, e.cfg.SDBCalendarID)
		personalErr = err
		if personalErr == nil {
			personalEvents = personalSync.Events
			peStore := store.NewPersonalEventStore(e.db)
			ptr := func(s string) *string {
				if s == "" {
					return nil
				}
				return &s
			}
			peEvents := make([]model.PersonalEvent, 0, len(personalEvents))
			for _, pe := range personalEvents {
				peEvents = append(peEvents, model.PersonalEvent{
					UserID:               userID,
					EventID:              pe.EventID,
					Titel:                pe.Titel,
					StartDatum:           pe.StartDatum,
					StartTijd:            ptr(pe.StartTijd),
					EindDatum:            pe.EindDatum,
					EindTijd:             ptr(pe.EindTijd),
					Heledag:              pe.Heledag,
					Locatie:              ptr(pe.Locatie),
					Beschrijving:         ptr(pe.Beschrijving),
					Symbol:               ptr(pe.Symbol),
					BusinessContextType:  ptr(pe.BusinessContextType),
					BusinessContextID:    ptr(pe.BusinessContextID),
					BusinessContextTitle: ptr(pe.BusinessContextTitle),
					Status:               pe.Status,
					Kalender:             pe.Kalender,
				})
			}
			if _, bulkErr := peStore.BulkUpsertSynced(ctx, peEvents); bulkErr != nil {
				slog.Warn("telegram personal events batch upsert failed, falling back to per-row", "error", bulkErr)
				for _, pe := range peEvents {
					_ = peStore.UpsertSynced(ctx, pe)
				}
			}
			personalPruned, personalErr = peStore.MarkMissingSyncedInDateRange(
				ctx,
				userID,
				personalSync.PruneStartDatum,
				personalSync.PruneEindDatum,
				personalSync.FetchedEventIDs,
				personalSync.SyncedKalenders,
			)
			if personalErr != nil {
				slog.Warn("telegram personal events prune failed", "error", personalErr)
			}
		}
	}()

	// Goroutine 2: Gmail sync pipeline
	wg.Add(1)
	go func() {
		defer wg.Done()

		emailStore := store.NewEmailStore(e.db)
		meta, metaErr := emailStore.GetSyncMeta(ctx, userID)
		if metaErr == nil {
			storedBefore, _ := emailStore.Count(ctx, userID)
			historyID := ""
			if meta != nil && storedBefore > 0 {
				historyID = meta.HistoryID
			}
			result, parsedEmails, newHistoryID, gmailErrVal := google.SyncGmail(ctx, oauthClient, userID, historyID)
			if gmailErrVal == nil {
				upserted := 0
				if len(parsedEmails) > 0 {
					modelEmails := make([]model.Email, len(parsedEmails))
					for i, pe := range parsedEmails {
						var cc, bcc, categorie *string
						if pe.CC != "" {
							cc = &pe.CC
						}
						if pe.BCC != "" {
							bcc = &pe.BCC
						}
						if pe.Categorie != "" {
							categorie = &pe.Categorie
						}
						syncedAt, _ := time.Parse(time.RFC3339, pe.SyncedAt)
						if syncedAt.IsZero() {
							syncedAt = time.Now().UTC()
						}
						modelEmails[i] = model.Email{
							UserID:        pe.UserID,
							GmailID:       pe.GmailID,
							ThreadID:      pe.ThreadID,
							FromAddr:      pe.From,
							ToAddr:        pe.To,
							CC:            cc,
							BCC:           bcc,
							Subject:       pe.Subject,
							Snippet:       pe.Snippet,
							Datum:         pe.Datum,
							Ontvangen:     pe.Ontvangen,
							IsGelezen:     pe.IsGelezen,
							IsSter:        pe.IsSter,
							IsVerwijderd:  pe.IsVerwijderd,
							IsDraft:       pe.IsDraft,
							LabelIDs:      pe.LabelIDs,
							Categorie:     categorie,
							HeeftBijlagen: pe.HeeftBijlagen,
							BijlagenCount: pe.BijlagenCount,
							SearchText:    pe.SearchText,
							SyncedAt:      syncedAt,
						}
					}
					upserted, _ = emailStore.BulkUpsert(ctx, modelEmails)
				}

				if newHistoryID == "" && meta != nil {
					newHistoryID = meta.HistoryID
				}
				totalSynced, _ := emailStore.Count(ctx, userID)
				lastFullSync := meta.LastFullSync
				if result.Mode == "full" {
					now := time.Now().UTC()
					lastFullSync = &now
				}
				_ = emailStore.UpsertSyncMeta(ctx, userID, newHistoryID, lastFullSync, totalSynced)
				gmailMsg = fmt.Sprintf("Gmail: %d nieuwe mails gesynchroniseerd (totaal %d).", upserted, totalSynced)
			} else {
				_ = emailStore.MarkSyncFailed(ctx, userID, gmailErrVal.Error())
				gmailMsg = fmt.Sprintf("Gmail sync mislukt: %v", gmailErrVal)
			}
		} else {
			gmailMsg = fmt.Sprintf("Gmail sync mislukt: %v", metaErr)
		}
	}()

	// Wait for both sync routines to complete
	wg.Wait()

	var b strings.Builder
	b.WriteString("✅ Synchronisatie voltooid!\n\n")
	if scheduleErr != nil {
		fmt.Fprintf(&b, "❌ Kalender sync mislukt: %v\n", scheduleErr)
	} else {
		fmt.Fprintf(&b, "📅 Kalender: %d diensten, %d persoonlijke afspraken gesynchroniseerd", len(schedules), len(personalEvents))
		if schedulePruned > 0 {
			fmt.Fprintf(&b, " (%d stale dienst(en) verwijderd)", schedulePruned)
		}
		if personalPruned > 0 {
			fmt.Fprintf(&b, " (%d stale afspraak/afspraken gemarkeerd)", personalPruned)
		}
		b.WriteString(".\n")
	}
	if gmailMsg != "" {
		fmt.Fprintf(&b, "📧 %s\n", gmailMsg)
	}

	_ = client.SendMessage(chatID, b.String())
}

func (e *Engine) handlePendingConfirmationCommand(ctx context.Context, client *tg.Client, chatID int64, text string, originMessageID *int64) bool {
	normalized := strings.TrimSpace(text)
	lower := strings.ToLower(normalized)

	switch {
	case lower == "/pending" || lower == "/bevestigingen":
		e.handlePendingList(ctx, client, chatID)
		return true
	case strings.HasPrefix(lower, "pending_confirm_"):
		id := strings.TrimPrefix(lower, "pending_confirm_")
		e.handlePendingConfirmID(ctx, client, chatID, id, originMessageID)
		return true
	case strings.HasPrefix(lower, "pending_reject_"):
		id := strings.TrimPrefix(lower, "pending_reject_")
		e.handlePendingCancelID(ctx, client, chatID, id, originMessageID)
		return true
	}

	fields := strings.Fields(normalized)
	if len(fields) < 2 {
		return false
	}

	cmd := strings.ToLower(strings.Split(fields[0], "@")[0])
	code := strings.TrimSpace(fields[1])
	switch cmd {
	case "/approve", "/confirm", "/akkoord":
		e.handlePendingConfirmCode(ctx, client, chatID, code)
		return true
	case "/reject", "/cancel", "/annuleer":
		e.handlePendingCancelCode(ctx, client, chatID, code)
		return true
	default:
		return false
	}
}

// pendingListButtonCap bounds how many pending actions get confirm/reject
// buttons — a keyboard beyond this many rows pushes the Startmenu escape
// button off-screen on a phone. Items beyond the cap are still listed as
// plain text (with their code) so /approve <code>/reject <code> keeps
// working for them from chat — the one place this human-in-the-loop safety
// net is meant to operate — instead of being invisible until the user
// switches to a separate Settings UI.
const pendingListButtonCap = 5

func (e *Engine) handlePendingList(ctx context.Context, client *tg.Client, chatID int64) {
	actions, err := store.NewPendingStore(e.db.Pool).ListPending(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "❌ "+classifyUserFacingError(err.Error()))
		return
	}
	if len(actions) == 0 {
		_ = client.SendMessage(chatID, "✅ Geen openstaande bevestigingen.")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "⏳ Openstaande bevestigingen (%d)\n\n", len(actions))
	rows := make([][]tg.InlineKeyboardButton, 0, pendingListButtonCap+1)
	for i, action := range actions {
		fmt.Fprintf(&b, "%d. %s\nCode: %s\nTool: %s\n\n", i+1, action.Summary, action.Code, action.ToolName)
		if i >= pendingListButtonCap {
			continue
		}
		short := truncateRunes(action.Summary, 18)
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "✅ " + short, CallbackData: "pending_confirm_" + action.ID},
			{Text: "✕ " + action.Code, CallbackData: "pending_reject_" + action.ID},
		})
	}
	if len(actions) > pendingListButtonCap {
		fmt.Fprintf(&b, "Nog %d via /approve CODE of /reject CODE hierboven.\n", len(actions)-pendingListButtonCap)
	}
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "🏠 Startmenu", CallbackData: "/start"}})
	_ = client.SendMessageWithKeyboard(chatID, b.String(), tg.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (e *Engine) handlePendingConfirmID(ctx context.Context, client *tg.Client, chatID int64, id string, originMessageID *int64) {
	result, err := ConfirmPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, id, e.googleOAuthClient())
	e.sendPendingOutcome(client, chatID, result, err, "uitgevoerd", originMessageID)
}

func (e *Engine) handlePendingConfirmCode(ctx context.Context, client *tg.Client, chatID int64, code string) {
	result, err := ConfirmPendingActionByCode(ctx, e.db.Pool, e.cfg.HomeappUserID, code, e.googleOAuthClient())
	e.sendPendingOutcome(client, chatID, result, err, "uitgevoerd", nil)
}

func (e *Engine) handlePendingCancelID(ctx context.Context, client *tg.Client, chatID int64, id string, originMessageID *int64) {
	result, err := CancelPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, id)
	e.sendPendingOutcome(client, chatID, result, err, "geannuleerd", originMessageID)
}

func (e *Engine) handlePendingCancelCode(ctx context.Context, client *tg.Client, chatID int64, code string) {
	pending := store.NewPendingStore(e.db.Pool)
	action, err := pending.FindByCode(ctx, e.cfg.HomeappUserID, code)
	if err != nil {
		_ = client.SendMessage(chatID, classifyPendingCodeError(err))
		return
	}
	if action == nil {
		_ = client.SendMessage(chatID, "❌ Code onbekend, al gebruikt, of verlopen. Typ /pending voor de actuele lijst.")
		return
	}
	result, err := CancelPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, action.ID)
	e.sendPendingOutcome(client, chatID, result, err, "geannuleerd", nil)
}

// sendPendingOutcome reports the result of a confirm/reject action. When
// originMessageID is set (the action was triggered by tapping a button on
// the /pending list), it EDITS that message instead of sending a new one —
// otherwise the list's other confirm/reject buttons for still-pending items
// stay live, but the row for the item just actioned would keep showing a
// now-stale button referencing an already-claimed action.
func (e *Engine) sendPendingOutcome(client *tg.Client, chatID int64, result map[string]any, err error, verb string, originMessageID *int64) {
	var text string
	if err != nil {
		text = classifyPendingCodeError(err)
	} else {
		summary := ""
		if raw, ok := result["summary"]; ok {
			summary = strings.TrimSpace(fmt.Sprint(raw))
		}
		if summary == "" {
			summary = "AI-actie"
		}
		text = fmt.Sprintf("✅ %s %s.\n\nTyp /pending voor de actuele lijst.", summary, verb)
	}
	keyboard := buildPendingMenu()
	if originMessageID != nil {
		_ = client.EditMessageText(chatID, *originMessageID, text, &keyboard)
		return
	}
	_ = client.SendMessageWithKeyboard(chatID, text, keyboard)
}

// classifyPendingCodeError maps a pending-action lookup/claim failure to a
// short, actionable Dutch message instead of forwarding whatever the store
// layer returned (which, for a plain "not found" case, used to be the raw
// English pgx text "no rows in result set") — this is precisely the command
// gating money/email/deletion actions, so a confusing failure message here
// is the worst place for one.
func classifyPendingCodeError(err error) string {
	if err == nil || errors.Is(err, ErrPendingActionNotFound) {
		return "❌ Code onbekend, al gebruikt, of verlopen. Typ /pending voor de actuele lijst."
	}
	return "❌ " + classifyUserFacingError(err.Error())
}

func expandTelegramCommand(text string) (expanded string, agentHint string, ok bool) {
	cmd := strings.ToLower(strings.TrimSpace(strings.Split(text, " ")[0]))
	cmd = strings.Split(cmd, "@")[0]

	if route, exists := commandRegistry[cmd]; exists && route.expansion != "" {
		args := strings.TrimSpace(text[len(cmd):])
		expandedPrompt := route.expansion
		if args != "" {
			expandedPrompt = expandedPrompt + "\n\nExtra context/instructie van gebruiker: " + args
		}
		return expandedPrompt, route.agentID, true
	}
	return "", "", false
}

func routeFreeText(text string) string {
	cmd := strings.Split(text, " ")[0]
	cmd = strings.ToLower(strings.Split(cmd, "@")[0])

	if route, exists := commandRegistry[cmd]; exists {
		return route.agentID
	}

	lower := strings.ToLower(text)
	laventeCareIntent := hasLaventeCareIntent(lower)
	if laventeCareIntent && !hasNoteCaptureIntent(lower) && (hasPlanningQuestion(lower) || hasAgendaIntent(lower) || hasNoteIntent(lower) || hasLaventeCareDossierIntent(lower)) {
		return "laventecare"
	}
	if hasPlanningQuestion(lower) {
		return "agenda"
	}
	if hasNoteIntent(lower) {
		return "notes"
	}
	if hasHabitIntent(lower) {
		return "habits"
	}
	if laventeCareIntent {
		return "laventecare"
	}
	if hasFinanceIntent(lower) {
		return "finance"
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

func hasNoteIntent(lower string) bool {
	for _, kw := range []string{"noteer", "notitie", "notities", "onthoud", "vergeet niet", "idee:", "todo", "actiepunt", "checklist"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasNoteCaptureIntent(lower string) bool {
	for _, kw := range []string{"noteer", "onthoud", "vergeet niet", "maak notitie", "nieuwe notitie", "idee:"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasLaventeCareIntent(lower string) bool {
	for _, kw := range []string{"laventecare", "lead", "leads", "crm", "opdracht", "opdrachten", "werkstream", "werkstreams", "project funnel", "klantproject", "business cockpit", "offerte", "offertes", "rapportage", "rapportages", "klantdossier", "dossierstuk"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasLaventeCareDossierIntent(lower string) bool {
	for _, kw := range []string{"pdf", "pdfs", "dossier", "dossierdocument", "dossierdocumenten", "offerte", "offertes", "rapportage", "rapportages"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasFinanceIntent(lower string) bool {
	for _, kw := range []string{"finance", "financien", "financiën", "geld", "saldo", "salaris", "loonstrook", "transactie", "transacties", "uitgaven", "inkomsten", "vaste lasten", "stornering"} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

func hasHabitIntent(lower string) bool {
	for _, kw := range []string{"habit", "habits", "gewoonte", "gewoontes", "streak", "streaks", "afvinken", "afgevinkt", "gedaan", "voltooid", "xp", "terugval"} {
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

type lampCommand struct {
	wizParams    map[string]any // raw WiZ setPilot params
	beschrijving string
}

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
