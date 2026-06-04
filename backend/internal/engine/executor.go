package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HomeBotExecutor executes AI tool calls against the PostgreSQL database.
type HomeBotExecutor struct {
	pool             *pgxpool.Pool
	userID           string
	emailStore       *store.EmailStore
	scheduleStore    *store.ScheduleStore
	transactionStore *store.TransactionStore
	salaryStore      *store.SalaryStore
	noteStore        *store.NoteStore
	personalEvStore  *store.PersonalEventStore
	habitStore       *store.HabitStore
	automationStore  *store.AutomationStore
	laventeCareStore *store.LaventeCareStore
}

func NewHomeBotExecutor(pool *pgxpool.Pool, userID string) *HomeBotExecutor {
	db := &store.DB{Pool: pool}
	return &HomeBotExecutor{
		pool:             pool,
		userID:           userID,
		emailStore:       store.NewEmailStore(db),
		scheduleStore:    store.NewScheduleStore(db),
		transactionStore: store.NewTransactionStore(db),
		salaryStore:      store.NewSalaryStore(db),
		noteStore:        store.NewNoteStore(db),
		personalEvStore:  store.NewPersonalEventStore(db),
		habitStore:       store.NewHabitStore(db),
		automationStore:  store.NewAutomationStore(db),
		laventeCareStore: store.NewLaventeCareStore(db),
	}
}

// Helpers
func (e *HomeBotExecutor) parseArgs(argsJSON string, v any) error {
	if err := json.Unmarshal([]byte(argsJSON), v); err != nil {
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func (e *HomeBotExecutor) jsonResponse(data any, err error) string {
	if err != nil {
		return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
	}
	if data == nil {
		return `{"error": "Niet gevonden"}`
	}
	b, _ := json.Marshal(data)
	return string(b)
}

func parseToolDateRange(argsJSON string, fallbackToday bool) (startIso, eindIso string, hasRange bool, err error) {
	var args struct {
		StartIso string `json:"startIso"`
		EindIso  string `json:"eindIso"`
	}
	if strings.TrimSpace(argsJSON) == "" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", "", false, fmt.Errorf("invalid arguments: %v", err)
	}

	if args.StartIso == "" && args.EindIso == "" {
		if !fallbackToday {
			return "", "", false, nil
		}
		today := todayAmsterdamISO()
		return today, today, true, nil
	}
	if args.StartIso == "" {
		args.StartIso = args.EindIso
	}
	if args.EindIso == "" {
		args.EindIso = args.StartIso
	}

	start, err := time.Parse("2006-01-02", args.StartIso)
	if err != nil {
		return "", "", false, fmt.Errorf("ongeldige startIso: %s", args.StartIso)
	}
	end, err := time.Parse("2006-01-02", args.EindIso)
	if err != nil {
		return "", "", false, fmt.Errorf("ongeldige eindIso: %s", args.EindIso)
	}
	if end.Before(start) {
		args.StartIso, args.EindIso = args.EindIso, args.StartIso
	}
	return args.StartIso, args.EindIso, true, nil
}

func todayAmsterdamISO() string {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

func parseOptionalNoteDeadline(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02 15:04",
		"2006-01-02",
		"02-01-2006 15:04",
		"02-01-2006",
	} {
		parsed, err := time.ParseInLocation(layout, value, loc)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("ongeldige deadline: %s", value)
}

func visibleSchedules(events []model.Schedule) []model.Schedule {
	visible := make([]model.Schedule, 0, len(events))
	for _, event := range events {
		if event.Status == "VERWIJDERD" {
			continue
		}
		visible = append(visible, event)
	}
	return visible
}

func visiblePersonalEvents(events []model.PersonalEvent) []model.PersonalEvent {
	visible := make([]model.PersonalEvent, 0, len(events))
	for _, event := range events {
		switch event.Status {
		case store.PersonalEventStatusDeleted, store.PersonalEventStatusPendingDelete:
			continue
		}
		visible = append(visible, event)
	}
	return visible
}

func clampToolLimit(value, fallback, max int) int {
	if value <= 0 {
		return fallback
	}
	if value > max {
		return max
	}
	return value
}

func scheduleMetaValue(meta *model.ScheduleMeta, key string) any {
	if meta == nil {
		return nil
	}
	switch key {
	case "importedAt":
		return meta.ImportedAt
	case "fileName":
		return meta.FileName
	case "totalRows":
		return meta.TotalRows
	default:
		return nil
	}
}

func emailMetaValue(meta *model.EmailSyncMeta, key string) any {
	if meta == nil {
		return nil
	}
	switch key {
	case "updatedAt":
		return meta.UpdatedAt
	case "lastFullSync":
		return meta.LastFullSync
	case "totalSynced":
		return meta.TotalSynced
	case "historyID":
		return meta.HistoryID
	default:
		return nil
	}
}

func (e *HomeBotExecutor) executeContractAnalyse(ctx context.Context) string {
	events, err := e.scheduleStore.List(ctx, e.userID)
	if err != nil {
		return e.jsonResponse(nil, err)
	}

	type WeekStats struct {
		Weeknr      string  `json:"weeknr"`
		ActualHours float64 `json:"actualHours"`
		Delta       float64 `json:"delta"`
	}

	type MonthData struct {
		Hours  float64
		Shifts int
	}

	weekMap := make(map[string]float64)
	monthMap := make(map[string]*MonthData)

	for _, ev := range events {
		if ev.Status != "VERWIJDERD" {
			if ev.Weeknr != "" {
				weekMap[ev.Weeknr] += ev.Duur
			}
			if len(ev.StartDatum) >= 7 {
				month := ev.StartDatum[:7]
				if _, ok := monthMap[month]; !ok {
					monthMap[month] = &MonthData{}
				}
				monthMap[month].Hours += ev.Duur
				monthMap[month].Shifts++
			}
		}
	}

	var totalDelta float64
	var weekly []WeekStats
	for w, d := range weekMap {
		delta := d - 16.0 // Hardcoded 16 hours contract
		totalDelta += delta
		weekly = append(weekly, WeekStats{
			Weeknr:      w,
			ActualHours: d,
			Delta:       delta,
		})
	}

	var monthly []map[string]interface{}
	for m, data := range monthMap {
		monthly = append(monthly, map[string]interface{}{
			"month":  m,
			"hours":  data.Hours,
			"shifts": data.Shifts,
		})
	}

	res := map[string]interface{}{
		"contractUren": 16,
		"totalDelta":   totalDelta,
		"weekly":       weekly,
		"monthly":      monthly,
		"message":      "Analyse bevat wekelijkse plus/min (contract=16u) EN ruwe maandtotalen (omdat maanden geen vaste 16u-grens per week hebben). Gebruik de maand-statistieken als de gebruiker naar een maand vraagt.",
	}
	return e.jsonResponse(res, nil)
}

func (e *HomeBotExecutor) Execute(ctx context.Context, toolName string, argsJSON string) string {
	fmt.Printf("[EXECUTOR] Tool: %s, Args: %s\n", toolName, argsJSON)
	switch toolName {

	// ── EMAIL ────────────────────────────────────────────────────────
	case "leesEmail":
		var args struct {
			EmailID string `json:"emailId"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		email, err := e.emailStore.GetByGmailID(ctx, e.userID, args.EmailID)
		return e.jsonResponse(email, err)

	case "zoekEmails":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if args.Limit <= 0 {
			args.Limit = 5
		} else if args.Limit > 10 {
			args.Limit = 10
		}
		emails, err := e.emailStore.Search(ctx, e.userID, args.Query, args.Limit)
		return e.jsonResponse(emails, err)

	// ── SYSTEM & AUTOMATIONS ────────────────────────────────────────
	case "syncStatusOpvragen":
		scheduleMeta, scheduleErr := e.scheduleStore.GetMeta(ctx, e.userID)
		if scheduleErr != nil {
			return e.jsonResponse(nil, scheduleErr)
		}
		emailMeta, emailErr := e.emailStore.GetSyncMeta(ctx, e.userID)
		if emailErr != nil {
			return e.jsonResponse(nil, emailErr)
		}

		var personalTotal, pendingPersonal int
		if err := e.pool.QueryRow(ctx,
			`SELECT COUNT(*),
			        COUNT(*) FILTER (WHERE status IN ($2, $3, $4))
			   FROM personal_events
			  WHERE user_id = $1`,
			e.userID,
			store.PersonalEventStatusPendingCreate,
			store.PersonalEventStatusPendingUpdate,
			store.PersonalEventStatusPendingDelete,
		).Scan(&personalTotal, &pendingPersonal); err != nil {
			return e.jsonResponse(nil, err)
		}

		var pendingCommands, processingCommands, failedCommands int
		if err := e.pool.QueryRow(ctx,
			`SELECT COUNT(*) FILTER (WHERE status = 'pending'),
			        COUNT(*) FILTER (WHERE status = 'processing'),
			        COUNT(*) FILTER (WHERE status = 'failed')
			   FROM device_commands
			  WHERE user_id = $1`,
			e.userID,
		).Scan(&pendingCommands, &processingCommands, &failedCommands); err != nil {
			return e.jsonResponse(nil, err)
		}

		return e.jsonResponse(map[string]any{
			"schedule": map[string]any{
				"importedAt": scheduleMetaValue(scheduleMeta, "importedAt"),
				"totalRows":  scheduleMetaValue(scheduleMeta, "totalRows"),
			},
			"personalCalendar": map[string]any{
				"total":   personalTotal,
				"pending": pendingPersonal,
			},
			"gmail": map[string]any{
				"updatedAt":     emailMetaValue(emailMeta, "updatedAt"),
				"lastFullSync":  emailMetaValue(emailMeta, "lastFullSync"),
				"totalSynced":   emailMetaValue(emailMeta, "totalSynced"),
				"historyIDSet":  emailMeta != nil && strings.TrimSpace(emailMeta.HistoryID) != "",
				"metaAvailable": emailMeta != nil,
			},
			"commands": map[string]int{
				"pending":    pendingCommands,
				"processing": processingCommands,
				"failed":     failedCommands,
			},
		}, nil)

	case "automationsOverzicht":
		automations, err := e.automationStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		items := make([]map[string]any, 0, len(automations))
		active := 0
		for _, automation := range automations {
			if automation.Enabled {
				active++
			}
			items = append(items, map[string]any{
				"id":          automation.ID,
				"name":        automation.Name,
				"enabled":     automation.Enabled,
				"group":       automation.GroupName,
				"lastFiredAt": automation.LastFiredAt,
				"createdAt":   automation.CreatedAt,
			})
		}

		var pendingCommands, processingCommands, failedCommands int
		if err := e.pool.QueryRow(ctx,
			`SELECT COUNT(*) FILTER (WHERE status = 'pending'),
			        COUNT(*) FILTER (WHERE status = 'processing'),
			        COUNT(*) FILTER (WHERE status = 'failed')
			   FROM device_commands
			  WHERE user_id = $1`,
			e.userID,
		).Scan(&pendingCommands, &processingCommands, &failedCommands); err != nil {
			return e.jsonResponse(nil, err)
		}

		return e.jsonResponse(map[string]any{
			"total":    len(automations),
			"active":   active,
			"inactive": len(automations) - active,
			"items":    items,
			"commands": map[string]int{
				"pending":    pendingCommands,
				"processing": processingCommands,
				"failed":     failedCommands,
			},
		}, nil)

	// ── ROOSTER ──────────────────────────────────────────────────────
	case "dienstenOpvragen":
		var events []model.Schedule
		var err error

		startIso, eindIso, hasRange, errParse := parseToolDateRange(argsJSON, false)
		if errParse != nil {
			return e.jsonResponse(nil, errParse)
		}
		if hasRange {
			events, err = e.scheduleStore.ListRange(ctx, e.userID, startIso, eindIso)
		} else {
			// Fallback if no date range is provided
			events, err = e.scheduleStore.ListUpcoming(ctx, e.userID, 15)
		}

		if err != nil {
			return e.jsonResponse(nil, err)
		}

		events = visibleSchedules(events)
		var total float64
		for _, ev := range events {
			total += ev.Duur
		}

		return e.jsonResponse(map[string]any{
			"diensten":  events,
			"totaalUur": total,
		}, nil)

	case "contractAnalyseOpvragen":
		return e.executeContractAnalyse(ctx)

	// ── FINANCE ──────────────────────────────────────────────────────
	case "saldoOpvragen":
		stats, err := e.transactionStore.GetStats(ctx, e.userID)
		return e.jsonResponse(stats, err)

	case "salarisOpvragen":
		salaries, err := e.salaryStore.List(ctx, e.userID)
		return e.jsonResponse(salaries, err)

	case "transactiesZoeken":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if args.Limit <= 0 {
			args.Limit = 10
		} else if args.Limit > 20 {
			args.Limit = 20
		}
		filter := store.TransactionFilter{Zoekterm: args.Query, Limit: args.Limit}
		txs, _, err := e.transactionStore.ListFiltered(ctx, e.userID, filter)
		return e.jsonResponse(txs, err)

	// ── NOTITIES ─────────────────────────────────────────────────────
	case "notitiesZoeken":
		var args struct {
			Query string `json:"query"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		notes, err := e.noteStore.Search(ctx, e.userID, args.Query, 5) // Hard cap op 5
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(activeNotes(notes), nil)

	case "notitiesOverzicht":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		limit := clampToolLimit(args.Limit, 10, 20)
		notes, err := e.noteStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		active := make([]model.Note, 0, limit)
		totalActive := 0
		totalPinned := 0
		totalCompleted := 0
		totalArchived := 0
		for _, note := range notes {
			if note.IsArchived {
				totalArchived++
				continue
			}
			totalActive++
			if note.IsPinned {
				totalPinned++
			}
			if note.IsCompleted {
				totalCompleted++
			}
			if len(active) < limit {
				active = append(active, note)
			}
		}

		return e.jsonResponse(map[string]any{
			"totalActive":    totalActive,
			"totalPinned":    totalPinned,
			"totalCompleted": totalCompleted,
			"totalArchived":  totalArchived,
			"limit":          limit,
			"items":          active,
		}, nil)

	case "notitieAanmaken":
		var args struct {
			Titel      string   `json:"titel"`
			Inhoud     string   `json:"inhoud"`
			Tags       []string `json:"tags"`
			Prioriteit string   `json:"prioriteit"`
			Symbol     string   `json:"symbol"`
			Deadline   string   `json:"deadline"`
			TriageFlag *bool    `json:"triage_flag"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		deadline, err := parseOptionalNoteDeadline(args.Deadline)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		title := strings.TrimSpace(args.Titel)
		if title == "" {
			title = cleanNoteTitle(args.Inhoud)
		}
		if title == "" {
			title = "Nieuwe notitie"
		}
		n, err := e.noteStore.Create(ctx, e.userID, model.Note{
			Titel:      &title,
			Inhoud:     args.Inhoud,
			Tags:       args.Tags,
			Prioriteit: strPtr(args.Prioriteit),
			Symbol:     strPtr(args.Symbol),
			Deadline:   deadline,
			TriageFlag: args.TriageFlag,
		})
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		return fmt.Sprintf(`{"success": true, "note_id": "%s"}`, n.ID)

	case "notitiesVandaag":
		nStore := store.NewNoteStore(&store.DB{Pool: e.pool})
		notes, err := nStore.List(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		loc, _ := time.LoadLocation("Europe/Amsterdam")
		todayStr := time.Now().In(loc).Format("2006-01-02")
		var todayNotes []model.Note
		for _, n := range notes {
			if !n.IsArchived && (n.Aangemaakt.In(loc).Format("2006-01-02") == todayStr || n.Gewijzigd.In(loc).Format("2006-01-02") == todayStr) {
				todayNotes = append(todayNotes, n)
			}
		}
		b, _ := json.Marshal(todayNotes)
		return string(b)

	// ── AGENDA ───────────────────────────────────────────────────────
	case "planningOpvragen":
		startIso, eindIso, _, errParse := parseToolDateRange(argsJSON, true)
		if errParse != nil {
			return e.jsonResponse(nil, errParse)
		}

		diensten, dienstErr := e.scheduleStore.ListRange(ctx, e.userID, startIso, eindIso)
		if dienstErr != nil {
			return e.jsonResponse(nil, dienstErr)
		}
		afspraken, afspraakErr := e.personalEvStore.ListRange(ctx, e.userID, startIso, eindIso)
		if afspraakErr != nil {
			return e.jsonResponse(nil, afspraakErr)
		}

		diensten = visibleSchedules(diensten)
		afspraken = visiblePersonalEvents(afspraken)

		var totaalUur float64
		for _, dienst := range diensten {
			totaalUur += dienst.Duur
		}

		return e.jsonResponse(map[string]any{
			"periode": map[string]string{
				"startIso": startIso,
				"eindIso":  eindIso,
			},
			"diensten":        diensten,
			"afspraken":       afspraken,
			"aantalDiensten":  len(diensten),
			"aantalAfspraken": len(afspraken),
			"totaalUur":       totaalUur,
		}, nil)

	case "afsprakenOpvragen":
		startIso, eindIso, hasRange, errParse := parseToolDateRange(argsJSON, false)
		if errParse != nil {
			return e.jsonResponse(nil, errParse)
		}
		var events []model.PersonalEvent
		var err error
		if hasRange {
			events, err = e.personalEvStore.ListRange(ctx, e.userID, startIso, eindIso)
		} else {
			events, err = e.personalEvStore.ListUpcoming(ctx, e.userID, 10)
		}
		events = visiblePersonalEvents(events)
		return e.jsonResponse(events, err)

	// ── HABITS ───────────────────────────────────────────────────────
	case "habitsOverzicht":
		habits, err := e.habitStore.List(ctx, e.userID)
		return e.jsonResponse(habits, err)

	case "habitStreaks":
		stats, err := e.habitStore.Stats(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		habits, err := e.habitStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		items := make([]map[string]any, 0, len(habits))
		for _, habit := range habits {
			items = append(items, map[string]any{
				"id":             habit.ID,
				"naam":           habit.Naam,
				"emoji":          habit.Emoji,
				"type":           habit.Type,
				"frequentie":     habit.Frequentie,
				"huidigeStreak":  habit.HuidigeStreak,
				"langsteStreak":  habit.LangsteStreak,
				"totaalVoltooid": habit.TotaalVoltooid,
				"totaalXP":       habit.TotaalXP,
				"isPauze":        habit.IsPauze,
			})
		}

		return e.jsonResponse(map[string]any{
			"stats": stats,
			"items": items,
		}, nil)

	case "habitBadges":
		badges, err := e.habitStore.ListBadges(ctx, e.userID)
		return e.jsonResponse(badges, err)

	case "habitRapport":
		var args struct {
			Dagen int `json:"dagen"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		days := clampToolLimit(args.Dagen, 30, 60)

		stats, err := e.habitStore.Stats(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		habits, err := e.habitStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		badges, err := e.habitStore.ListBadges(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		heatmap, err := e.habitStore.HeatmapData(ctx, e.userID, days)
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		return e.jsonResponse(map[string]any{
			"dagen":   days,
			"stats":   stats,
			"habits":  habits,
			"badges":  badges,
			"heatmap": heatmap,
		}, nil)

	// ── LAVENTECARE ──────────────────────────────────────────────────
	case "laventecareCockpit":
		cockpit, err := e.laventeCareStore.GetCockpit(ctx, e.userID)
		return e.jsonResponse(cockpit, err)

	case "laventecareKennisZoeken":
		var args struct {
			Query string `json:"query"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		docs, err := e.laventeCareStore.SearchDocuments(ctx, e.userID, args.Query, 5)
		return e.jsonResponse(docs, err)

	case "laventecareLeadsOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		leads, err := e.laventeCareStore.ListLeads(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(leads, err)

	case "laventecareProjectenOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		projects, err := e.laventeCareStore.ListProjects(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(projects, err)

	case "laventecareActiesOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		actions, err := e.laventeCareStore.ListActions(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(actions, err)

	// ── SMART HOME ───────────────────────────────────────────────────
	case "lampBedien":
		return `{"status": "Geef de actie direct door via de chat, bijv: 'lampen uit' of 'scene ocean'. De bot pikt dit automatisch op voor het AI verzoek."}`

	default:
		return fmt.Sprintf(`{"error": "Tool '%s' niet geïmplementeerd in Go."}`, toolName)
	}
}
