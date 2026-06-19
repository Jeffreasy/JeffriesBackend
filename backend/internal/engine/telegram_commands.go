package engine

import (
	"context"
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
		"/afspraak": {agentID: "agenda"},
		"/compose":  {agentID: "email"},
		"/triage":   {agentID: "email"},
		"/search":   {agentID: "email"},
		"/notities": {agentID: "notes"},
		"/notehelp": {agentID: "notes"},
		"/noteer":   {agentID: "notes"},
		"/zoeknote": {agentID: "notes"},
		"/sync":     {agentID: "automations"},
	}
)

// telegramMenuCommands is the curated "/" menu registered via setMyCommands.
// The long alias set in commandRegistry stays available as hidden synonyms.
func telegramMenuCommands() []tg.BotCommand {
	return []tg.BotCommand{
		{Command: "start", Description: "Startmenu en dagoverzicht"},
		{Command: "briefing", Description: "AI dagbriefing"},
		{Command: "planning", Description: "Planning (rooster + afspraken)"},
		{Command: "agenda", Description: "Agenda / afspraken"},
		{Command: "rooster", Description: "Werkrooster en uren"},
		{Command: "finance", Description: "Saldo en transacties"},
		{Command: "laventecare", Description: "LaventeCare cockpit"},
		{Command: "email", Description: "Inbox en e-mailsignalen"},
		{Command: "notities", Description: "Notities-overzicht"},
		{Command: "noteai", Description: "AI notitie-assistent"},
		{Command: "habits", Description: "Habits en streaks"},
		{Command: "lampen", Description: "Lampstatus en bediening"},
		{Command: "news", Description: "Actueel nieuws (web search)"},
		{Command: "pending", Description: "Openstaande bevestigingen"},
		{Command: "sync", Description: "Gmail/agenda sync-status"},
		{Command: "ai", Description: "AI-diagnose en status"},
		{Command: "help", Description: "Alle commando's"},
	}
}

func (e *Engine) processText(ctx context.Context, client *tg.Client, chatID int64, text string) {
	// NOTE: the user message is intentionally NOT persisted here. Slash commands,
	// callback tokens (note_*, pending_*) and lamp commands return before reaching
	// the model, so persisting up-front would pollute AI history with non-
	// conversational tokens. We only save genuine free text just before it is sent
	// to the model (see the ProcessAIPrompt call below).
	chatStore := store.NewChatStore(e.db.Pool)

	if e.handlePendingConfirmationCommand(ctx, client, chatID, text) {
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
	case command == "/finance" || strings.HasPrefix(command, "/finance "):
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
		e.handleNoteDone(ctx, client, chatID, strings.TrimPrefix(text, "note_done_"))
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

	agentID := routeFreeText(text)
	if agentHint != "" {
		agentID = agentHint
	}

	// Persist only the conversational text that actually reaches the model.
	_ = chatStore.SaveMessage(ctx, chatID, "user", text, nil)
	_, _ = e.ProcessAIPrompt(ctx, chatID, text, agentID, true)
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

		// 1. Process pending calendar operations in DB first
		peStore := store.NewPersonalEventStore(e.db)
		pending, err := peStore.ListPendingCalendar(ctx, userID, 50)
		if err == nil {
			for _, event := range pending {
				calendarID := strings.TrimSpace(event.Kalender)
				if calendarID == "" || strings.EqualFold(calendarID, "Main") {
					calendarID = "primary"
				}
				googleEventID := event.EventID
				if calendarID != "primary" {
					googleEventID = strings.TrimPrefix(googleEventID, calendarID+":")
				}

				// Map pending states
				switch event.Status {
				case store.PersonalEventStatusPendingCreate:
					createdID, err := google.CreatePersonalEvent(ctx, oauthClient, calendarID, event)
					if err == nil {
						targetID := createdID
						if calendarID != "primary" {
							targetID = calendarID + ":" + createdID
						}
						_ = peStore.ReplaceEventIDAndStatus(ctx, event.UserID, event.EventID, targetID, store.PersonalEventStatusUpcoming)
					}
				case store.PersonalEventStatusPendingUpdate:
					if err := google.UpdatePersonalEvent(ctx, oauthClient, calendarID, googleEventID, event); err == nil {
						_ = peStore.UpdateStatus(ctx, event.UserID, event.EventID, store.PersonalEventStatusUpcoming)
					}
				case store.PersonalEventStatusPendingDelete:
					if err := google.DeletePersonalEvent(ctx, oauthClient, calendarID, googleEventID); err == nil {
						_ = peStore.UpdateStatus(ctx, event.UserID, event.EventID, store.PersonalEventStatusDeleted)
					}
				}
			}
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
			for _, pe := range personalEvents {
				startTijd := pe.StartTijd
				var pStartTijd *string
				if startTijd != "" {
					pStartTijd = &startTijd
				}
				eindTijd := pe.EindTijd
				var pEindTijd *string
				if eindTijd != "" {
					pEindTijd = &eindTijd
				}
				locatie := pe.Locatie
				var pLocatie *string
				if locatie != "" {
					pLocatie = &locatie
				}
				beschrijving := pe.Beschrijving
				var pBeschrijving *string
				if beschrijving != "" {
					pBeschrijving = &beschrijving
				}
				symbol := pe.Symbol
				var pSymbol *string
				if symbol != "" {
					pSymbol = &symbol
				}
				businessContextType := pe.BusinessContextType
				var pBusinessContextType *string
				if businessContextType != "" {
					pBusinessContextType = &businessContextType
				}
				businessContextID := pe.BusinessContextID
				var pBusinessContextID *string
				if businessContextID != "" {
					pBusinessContextID = &businessContextID
				}
				businessContextTitle := pe.BusinessContextTitle
				var pBusinessContextTitle *string
				if businessContextTitle != "" {
					pBusinessContextTitle = &businessContextTitle
				}

				_ = peStore.UpsertSynced(ctx, model.PersonalEvent{
					UserID:               userID,
					EventID:              pe.EventID,
					Titel:                pe.Titel,
					StartDatum:           pe.StartDatum,
					StartTijd:            pStartTijd,
					EindDatum:            pe.EindDatum,
					EindTijd:             pEindTijd,
					Heledag:              pe.Heledag,
					Locatie:              pLocatie,
					Beschrijving:         pBeschrijving,
					Symbol:               pSymbol,
					BusinessContextType:  pBusinessContextType,
					BusinessContextID:    pBusinessContextID,
					BusinessContextTitle: pBusinessContextTitle,
					Status:               pe.Status,
					Kalender:             pe.Kalender,
				})
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

func (e *Engine) handlePendingConfirmationCommand(ctx context.Context, client *tg.Client, chatID int64, text string) bool {
	normalized := strings.TrimSpace(text)
	lower := strings.ToLower(normalized)

	switch {
	case lower == "/pending" || lower == "/bevestigingen":
		e.handlePendingList(ctx, client, chatID)
		return true
	case strings.HasPrefix(lower, "pending_confirm_"):
		id := strings.TrimPrefix(normalized, "pending_confirm_")
		e.handlePendingConfirmID(ctx, client, chatID, id)
		return true
	case strings.HasPrefix(lower, "pending_reject_"):
		id := strings.TrimPrefix(normalized, "pending_reject_")
		e.handlePendingCancelID(ctx, client, chatID, id)
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

func (e *Engine) handlePendingList(ctx context.Context, client *tg.Client, chatID int64) {
	actions, err := store.NewPendingStore(e.db.Pool).ListPending(ctx, e.cfg.HomeappUserID)
	if err != nil {
		_ = client.SendMessage(chatID, "❌ Bevestigingen ophalen mislukt: "+err.Error())
		return
	}
	if len(actions) == 0 {
		_ = client.SendMessage(chatID, "✅ Geen openstaande bevestigingen.")
		return
	}

	var b strings.Builder
	fmt.Fprintf(&b, "⏳ Openstaande bevestigingen (%d)\n\n", len(actions))
	rows := make([][]tg.InlineKeyboardButton, 0, len(actions)*2+1)
	for i, action := range actions {
		if i >= 8 {
			fmt.Fprintf(&b, "… en %d extra via Settings.\n", len(actions)-i)
			break
		}
		fmt.Fprintf(&b, "%d. %s\nCode: %s\nTool: %s\n\n", i+1, action.Summary, action.Code, action.ToolName)
		short := truncateRunes(action.Summary, 24)
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "✅ " + short, CallbackData: "pending_confirm_" + action.ID},
		})
		rows = append(rows, []tg.InlineKeyboardButton{
			{Text: "✕ Annuleer " + action.Code, CallbackData: "pending_reject_" + action.ID},
		})
	}
	rows = append(rows, []tg.InlineKeyboardButton{{Text: "🏠 Startmenu", CallbackData: "/start"}})
	_ = client.SendMessageWithKeyboard(chatID, b.String(), tg.InlineKeyboardMarkup{InlineKeyboard: rows})
}

func (e *Engine) handlePendingConfirmID(ctx context.Context, client *tg.Client, chatID int64, id string) {
	result, err := ConfirmPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, id, e.googleOAuthClient())
	e.sendPendingOutcome(client, chatID, result, err, "uitgevoerd")
}

func (e *Engine) handlePendingConfirmCode(ctx context.Context, client *tg.Client, chatID int64, code string) {
	result, err := ConfirmPendingActionByCode(ctx, e.db.Pool, e.cfg.HomeappUserID, code, e.googleOAuthClient())
	e.sendPendingOutcome(client, chatID, result, err, "uitgevoerd")
}

func (e *Engine) handlePendingCancelID(ctx context.Context, client *tg.Client, chatID int64, id string) {
	result, err := CancelPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, id)
	e.sendPendingOutcome(client, chatID, result, err, "geannuleerd")
}

func (e *Engine) handlePendingCancelCode(ctx context.Context, client *tg.Client, chatID int64, code string) {
	pending := store.NewPendingStore(e.db.Pool)
	action, err := pending.FindByCode(ctx, e.cfg.HomeappUserID, code)
	if err != nil {
		_ = client.SendMessage(chatID, "❌ Bevestiging niet gevonden of verlopen.")
		return
	}
	result, err := CancelPendingAction(ctx, e.db.Pool, e.cfg.HomeappUserID, action.ID)
	e.sendPendingOutcome(client, chatID, result, err, "geannuleerd")
}

func (e *Engine) sendPendingOutcome(client *tg.Client, chatID int64, result map[string]any, err error, verb string) {
	if err != nil {
		_ = client.SendMessageWithKeyboard(chatID, "❌ Actie mislukt: "+err.Error(), buildPendingMenu())
		return
	}
	summary := ""
	if raw, ok := result["summary"]; ok {
		summary = strings.TrimSpace(fmt.Sprint(raw))
	}
	if summary == "" {
		summary = "AI-actie"
	}
	_ = client.SendMessageWithKeyboard(chatID, fmt.Sprintf("✅ %s %s.", summary, verb), buildPendingMenu())
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
