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
		return e.jsonResponse(notes, err)

	case "notitieAanmaken":
		var args struct {
			Titel  string   `json:"titel"`
			Inhoud string   `json:"inhoud"`
			Tags   []string `json:"tags"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		n, err := e.noteStore.Create(ctx, e.userID, model.Note{
			Titel:  &args.Titel,
			Inhoud: args.Inhoud,
			Tags:   args.Tags,
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
			if !n.IsArchived && n.Aangemaakt.In(loc).Format("2006-01-02") == todayStr {
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

	// ── SMART HOME ───────────────────────────────────────────────────
	case "lampBedien":
		return `{"status": "Geef de actie direct door via de chat, bijv: 'lampen uit' of 'scene ocean'. De bot pikt dit automatisch op voor het AI verzoek."}`

	default:
		return fmt.Sprintf(`{"error": "Tool '%s' niet geïmplementeerd in Go."}`, toolName)
	}
}
