package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/jackc/pgx/v5/pgxpool"
)

// HomeBotExecutor executes AI tool calls against the PostgreSQL database.
type HomeBotExecutor struct {
	pool   *pgxpool.Pool
	userID string
}

func NewHomeBotExecutor(pool *pgxpool.Pool, userID string) *HomeBotExecutor {
	return &HomeBotExecutor{
		pool:   pool,
		userID: userID,
	}
}

func (e *HomeBotExecutor) Execute(ctx context.Context, toolName string, argsJSON string) string {
	switch toolName {

	// ── EMAIL ────────────────────────────────────────────────────────
	case "leesEmail":
		var args struct {
			EmailID string `json:"emailId"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		
		emailStore := store.NewEmailStore(&store.DB{Pool: e.pool})
		email, err := emailStore.GetByGmailID(ctx, e.userID, args.EmailID)
		if err != nil || email == nil {
			return `{"error": "Email niet gevonden"}`
		}
		emailBytes, _ := json.Marshal(email)
		return string(emailBytes)

	case "zoekEmails":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		if args.Limit <= 0 {
			args.Limit = 5
		}
		if args.Limit > 10 {
			args.Limit = 10
		}
		emailStore := store.NewEmailStore(&store.DB{Pool: e.pool})
		emails, err := emailStore.Search(ctx, e.userID, args.Query, args.Limit)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(emails)
		return string(b)

	// ── ROOSTER ──────────────────────────────────────────────────────
	case "dienstenOpvragen":
		s := store.NewScheduleStore(&store.DB{Pool: e.pool})
		events, err := s.ListUpcoming(ctx, e.userID, 15)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(events)
		return string(b)

	case "contractAnalyseOpvragen":
		s := store.NewScheduleStore(&store.DB{Pool: e.pool})
		events, err := s.List(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
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
		b, _ := json.Marshal(res)
		return string(b)

	// ── FINANCE ──────────────────────────────────────────────────────
	case "saldoOpvragen":
		tStore := store.NewTransactionStore(&store.DB{Pool: e.pool})
		stats, err := tStore.GetStats(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(stats)
		return string(b)

	case "salarisOpvragen":
		s := store.NewSalaryStore(&store.DB{Pool: e.pool})
		salaries, err := s.List(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(salaries)
		return string(b)

	case "transactiesZoeken":
		var args struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		if args.Limit <= 0 {
			args.Limit = 10
		}
		if args.Limit > 20 {
			args.Limit = 20
		}
		tStore := store.NewTransactionStore(&store.DB{Pool: e.pool})
		filter := store.TransactionFilter{Zoekterm: args.Query, Limit: args.Limit}
		txs, _, err := tStore.ListFiltered(ctx, e.userID, filter)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(txs)
		return string(b)

	// ── NOTITIES ─────────────────────────────────────────────────────
	case "notitiesZoeken":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		nStore := store.NewNoteStore(&store.DB{Pool: e.pool})
		notes, err := nStore.Search(ctx, e.userID, args.Query, 5) // Hard cap op 5
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(notes)
		return string(b)

	case "notitieAanmaken":
		var args struct {
			Titel  string   `json:"titel"`
			Inhoud string   `json:"inhoud"`
			Tags   []string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		nStore := store.NewNoteStore(&store.DB{Pool: e.pool})
		n, err := nStore.Create(ctx, e.userID, model.Note{
			Titel:  &args.Titel,
			Inhoud: args.Inhoud,
			Tags:   args.Tags,
		})
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		return fmt.Sprintf(`{"success": true, "note_id": "%s"}`, n.ID)

	// ── AGENDA ───────────────────────────────────────────────────────
	case "afsprakenOpvragen":
		pStore := store.NewPersonalEventStore(&store.DB{Pool: e.pool})
		events, err := pStore.ListUpcoming(ctx, e.userID, 10) // MVP limit
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(events)
		return string(b)

	// ── HABITS ───────────────────────────────────────────────────────
	case "habitsOverzicht":
		hStore := store.NewHabitStore(&store.DB{Pool: e.pool})
		habits, err := hStore.List(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(habits)
		return string(b)

	// ── LAVENTECARE ──────────────────────────────────────────────────
	case "laventecareCockpit":
		lcStore := store.NewLaventeCareStore(&store.DB{Pool: e.pool})
		cockpit, err := lcStore.GetCockpit(ctx, e.userID)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(cockpit)
		return string(b)

	case "laventecareKennisZoeken":
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return fmt.Sprintf(`{"error": "Invalid arguments: %v"}`, err)
		}
		lcStore := store.NewLaventeCareStore(&store.DB{Pool: e.pool})
		docs, err := lcStore.SearchDocuments(ctx, e.userID, args.Query, 5)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(docs)
		return string(b)

	// ── SMART HOME ───────────────────────────────────────────────────
	case "lampBedien":
		return `{"status": "Geef de actie direct door via de chat, bijv: 'lampen uit' of 'scene ocean'. De bot pikt dit automatisch op voor het AI verzoek."}`

	default:
		return fmt.Sprintf(`{"error": "Tool '%s' niet geïmplementeerd in Go."}`, toolName)
	}
}
