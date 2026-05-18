package engine

import (
	"context"
	"encoding/json"
	"fmt"

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
		notes, err := nStore.Search(ctx, e.userID, args.Query, 10)
		if err != nil {
			return fmt.Sprintf(`{"error": "Database fout: %v"}`, err)
		}
		b, _ := json.Marshal(notes)
		return string(b)

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
		docs, err := lcStore.ListDocuments(ctx, e.userID)
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
