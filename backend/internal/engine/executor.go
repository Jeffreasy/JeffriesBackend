package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/google/uuid"
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
	googleClient     *google.OAuthClient
}

func NewHomeBotExecutor(pool *pgxpool.Pool, userID string) *HomeBotExecutor {
	return NewHomeBotExecutorWithGoogle(pool, userID, nil)
}

func NewHomeBotExecutorWithGoogle(pool *pgxpool.Pool, userID string, googleClient *google.OAuthClient) *HomeBotExecutor {
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
		googleClient:     googleClient,
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

func optionalStringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func parseUUIDs(values []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		id, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("ongeldige uuid: %s", value)
		}
		ids = append(ids, id)
	}
	return ids, nil
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

	case "markeerGelezen":
		var args struct {
			EmailID      string `json:"emailId"`
			EmailIDSnake string `json:"email_id"`
			GmailID      string `json:"gmailId"`
			GmailIDSnake string `json:"gmail_id"`
			ID           string `json:"id"`
			Read         *bool  `json:"read"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		gmailID := firstNonEmpty(args.GmailID, args.GmailIDSnake, args.EmailID, args.EmailIDSnake, args.ID)
		if gmailID == "" {
			return e.jsonResponse(nil, fmt.Errorf("emailId of gmailId verplicht"))
		}
		read := true
		if args.Read != nil {
			read = *args.Read
		}
		if e.googleClient != nil {
			add, remove := []string{}, []string{"UNREAD"}
			if !read {
				add, remove = []string{"UNREAD"}, []string{}
			}
			if err := google.ModifyGmailLabels(ctx, e.googleClient, gmailID, add, remove); err != nil {
				return e.jsonResponse(nil, err)
			}
		}
		if err := e.emailStore.MarkRead(ctx, e.userID, gmailID, read); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "gmailId": gmailID, "read": read, "remote": e.googleClient != nil}, nil)

	case "markeerSter":
		var args struct {
			EmailID      string `json:"emailId"`
			EmailIDSnake string `json:"email_id"`
			GmailID      string `json:"gmailId"`
			GmailIDSnake string `json:"gmail_id"`
			ID           string `json:"id"`
			Starred      *bool  `json:"starred"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		gmailID := firstNonEmpty(args.GmailID, args.GmailIDSnake, args.EmailID, args.EmailIDSnake, args.ID)
		if gmailID == "" {
			return e.jsonResponse(nil, fmt.Errorf("emailId of gmailId verplicht"))
		}
		starred := true
		if args.Starred != nil {
			starred = *args.Starred
		}
		if e.googleClient != nil {
			add, remove := []string{}, []string{"STARRED"}
			if starred {
				add, remove = []string{"STARRED"}, []string{}
			}
			if err := google.ModifyGmailLabels(ctx, e.googleClient, gmailID, add, remove); err != nil {
				return e.jsonResponse(nil, err)
			}
		}
		if err := e.emailStore.MarkStar(ctx, e.userID, gmailID, starred); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "gmailId": gmailID, "starred": starred, "remote": e.googleClient != nil}, nil)

	case "verwijderEmail":
		var args struct {
			EmailID      string `json:"emailId"`
			EmailIDSnake string `json:"email_id"`
			GmailID      string `json:"gmailId"`
			GmailIDSnake string `json:"gmail_id"`
			ID           string `json:"id"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		gmailID := firstNonEmpty(args.GmailID, args.GmailIDSnake, args.EmailID, args.EmailIDSnake, args.ID)
		if gmailID == "" {
			return e.jsonResponse(nil, fmt.Errorf("emailId of gmailId verplicht"))
		}
		if e.googleClient != nil {
			if err := google.TrashGmailMessage(ctx, e.googleClient, gmailID); err != nil {
				return e.jsonResponse(nil, err)
			}
		}
		if err := e.emailStore.MarkDeleted(ctx, e.userID, gmailID); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "gmailId": gmailID, "deleted": true, "remote": e.googleClient != nil}, nil)

	case "bulkMarkeerGelezen":
		var args struct {
			EmailIDs      []string `json:"emailIds"`
			EmailIDsSnake []string `json:"email_ids"`
			GmailIDs      []string `json:"gmailIds"`
			GmailIDsSnake []string `json:"gmail_ids"`
			Read          *bool    `json:"read"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		ids := args.GmailIDs
		if len(ids) == 0 {
			ids = args.GmailIDsSnake
		}
		if len(ids) == 0 {
			ids = args.EmailIDs
		}
		if len(ids) == 0 {
			ids = args.EmailIDsSnake
		}
		if len(ids) > 20 {
			ids = ids[:20]
		}
		read := true
		if args.Read != nil {
			read = *args.Read
		}
		updated := 0
		for _, gmailID := range ids {
			gmailID = strings.TrimSpace(gmailID)
			if gmailID == "" {
				continue
			}
			if e.googleClient != nil {
				add, remove := []string{}, []string{"UNREAD"}
				if !read {
					add, remove = []string{"UNREAD"}, []string{}
				}
				if err := google.ModifyGmailLabels(ctx, e.googleClient, gmailID, add, remove); err != nil {
					return e.jsonResponse(nil, err)
				}
			}
			if err := e.emailStore.MarkRead(ctx, e.userID, gmailID, read); err != nil {
				return e.jsonResponse(nil, err)
			}
			updated++
		}
		return e.jsonResponse(map[string]any{"ok": true, "updated": updated, "read": read, "remote": e.googleClient != nil}, nil)

	case "bulkVerwijder":
		var args struct {
			EmailIDs      []string `json:"emailIds"`
			EmailIDsSnake []string `json:"email_ids"`
			GmailIDs      []string `json:"gmailIds"`
			GmailIDsSnake []string `json:"gmail_ids"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		ids := args.GmailIDs
		if len(ids) == 0 {
			ids = args.GmailIDsSnake
		}
		if len(ids) == 0 {
			ids = args.EmailIDs
		}
		if len(ids) == 0 {
			ids = args.EmailIDsSnake
		}
		if len(ids) > 20 {
			ids = ids[:20]
		}
		deleted := 0
		for _, gmailID := range ids {
			gmailID = strings.TrimSpace(gmailID)
			if gmailID == "" {
				continue
			}
			if e.googleClient != nil {
				if err := google.TrashGmailMessage(ctx, e.googleClient, gmailID); err != nil {
					return e.jsonResponse(nil, err)
				}
			}
			if err := e.emailStore.MarkDeleted(ctx, e.userID, gmailID); err != nil {
				return e.jsonResponse(nil, err)
			}
			deleted++
		}
		return e.jsonResponse(map[string]any{"ok": true, "deleted": deleted, "remote": e.googleClient != nil}, nil)

	case "inboxOpruimen":
		var args struct {
			Query  string `json:"query"`
			Action string `json:"action"`
			Limit  int    `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		limit := clampToolLimit(args.Limit, 10, 20)
		emails, err := e.emailStore.Search(ctx, e.userID, args.Query, limit)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		action := strings.ToLower(strings.TrimSpace(args.Action))
		if action == "" {
			action = "mark_read"
		}
		changed := 0
		for _, email := range emails {
			switch action {
			case "delete", "trash", "verwijder":
				if e.googleClient != nil {
					if err := google.TrashGmailMessage(ctx, e.googleClient, email.GmailID); err != nil {
						return e.jsonResponse(nil, err)
					}
				}
				if err := e.emailStore.MarkDeleted(ctx, e.userID, email.GmailID); err != nil {
					return e.jsonResponse(nil, err)
				}
			default:
				if e.googleClient != nil {
					if err := google.ModifyGmailLabels(ctx, e.googleClient, email.GmailID, []string{}, []string{"UNREAD"}); err != nil {
						return e.jsonResponse(nil, err)
					}
				}
				if err := e.emailStore.MarkRead(ctx, e.userID, email.GmailID, true); err != nil {
					return e.jsonResponse(nil, err)
				}
			}
			changed++
		}
		return e.jsonResponse(map[string]any{"ok": true, "action": action, "matched": len(emails), "changed": changed, "remote": e.googleClient != nil}, nil)

	case "emailVersturen":
		var args struct {
			To      string `json:"to"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.To) == "" || strings.TrimSpace(args.Subject) == "" || strings.TrimSpace(args.Body) == "" {
			return e.jsonResponse(nil, fmt.Errorf("to, subject en body zijn verplicht"))
		}
		result, err := google.SendGmailMessage(ctx, e.googleClient, args.To, args.Subject, args.Body)
		return e.jsonResponse(map[string]any{"ok": true, "sent": result}, err)

	case "emailBeantwoorden":
		var args struct {
			EmailID      string `json:"emailId"`
			EmailIDSnake string `json:"email_id"`
			GmailID      string `json:"gmailId"`
			GmailIDSnake string `json:"gmail_id"`
			ID           string `json:"id"`
			Body         string `json:"body"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		gmailID := firstNonEmpty(args.GmailID, args.GmailIDSnake, args.EmailID, args.EmailIDSnake, args.ID)
		if gmailID == "" || strings.TrimSpace(args.Body) == "" {
			return e.jsonResponse(nil, fmt.Errorf("emailId/gmailId en body zijn verplicht"))
		}
		email, err := e.emailStore.GetByGmailID(ctx, e.userID, gmailID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		if email == nil {
			return e.jsonResponse(nil, fmt.Errorf("email niet gevonden: %s", gmailID))
		}
		to := google.ExtractEmailAddress(email.FromAddr)
		result, err := google.ReplyGmailMessage(ctx, e.googleClient, email.ThreadID, to, email.Subject, args.Body)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		_ = e.emailStore.MarkRead(ctx, e.userID, gmailID, true)
		return e.jsonResponse(map[string]any{"ok": true, "reply": result, "threadId": email.ThreadID}, nil)

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

	case "categorieWijzigen":
		var args struct {
			TransactionID string `json:"transactionId"`
			ID            string `json:"id"`
			Categorie     string `json:"categorie"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		idValue := firstNonEmpty(args.TransactionID, args.ID)
		if idValue == "" || strings.TrimSpace(args.Categorie) == "" {
			return e.jsonResponse(nil, fmt.Errorf("transactionId en categorie verplicht"))
		}
		id, err := uuid.Parse(idValue)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		if err := e.transactionStore.UpdateCategorie(ctx, id, args.Categorie); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "transactionId": id.String(), "categorie": args.Categorie}, nil)

	case "bulkCategoriseren":
		var args struct {
			TransactionIDs []string `json:"transactionIds"`
			IDs            []string `json:"ids"`
			Categorie      string   `json:"categorie"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		idsRaw := args.TransactionIDs
		if len(idsRaw) == 0 {
			idsRaw = args.IDs
		}
		if len(idsRaw) > 50 {
			idsRaw = idsRaw[:50]
		}
		ids, err := parseUUIDs(idsRaw)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		if len(ids) == 0 || strings.TrimSpace(args.Categorie) == "" {
			return e.jsonResponse(nil, fmt.Errorf("transactionIds en categorie verplicht"))
		}
		updated, err := e.transactionStore.BulkUpdateCategorie(ctx, ids, args.Categorie)
		return e.jsonResponse(map[string]any{"ok": true, "updated": updated, "categorie": args.Categorie}, err)

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

		loc, err := time.LoadLocation("Europe/Amsterdam")
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		stats := buildNoteStats(activeNotes(notes), now, loc)
		focusNotes := selectFocusNotes(activeNotes(notes), now, loc, limit)
		focus := make([]map[string]any, 0, len(focusNotes))
		for _, note := range focusNotes {
			focus = append(focus, noteAIItem(note, now, loc))
		}

		return e.jsonResponse(map[string]any{
			"totalActive":    totalActive,
			"totalPinned":    totalPinned,
			"totalCompleted": totalCompleted,
			"totalArchived":  totalArchived,
			"limit":          limit,
			"hasActive":      totalActive > 0,
			"stats": map[string]any{
				"active":    stats.Active,
				"today":     stats.Today,
				"pinned":    stats.Pinned,
				"completed": stats.Completed,
				"attention": stats.Attention,
				"topTags":   stats.TopTags,
			},
			"focus":       focus,
			"items":       active,
			"instruction": "Als totalActive groter is dan 0, zeg nooit dat er geen actieve notities zijn. Gebruik focus voor prioriteit, deadline, checklist en triage.",
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

	case "afspraakMaken":
		var args struct {
			Titel        string `json:"titel"`
			Title        string `json:"title"`
			StartDatum   string `json:"startDatum"`
			StartDatumDB string `json:"start_datum"`
			StartIso     string `json:"startIso"`
			StartTijd    string `json:"startTijd"`
			StartTijdDB  string `json:"start_tijd"`
			EindDatum    string `json:"eindDatum"`
			EindDatumDB  string `json:"eind_datum"`
			EindIso      string `json:"eindIso"`
			EindTijd     string `json:"eindTijd"`
			EindTijdDB   string `json:"eind_tijd"`
			Heledag      *bool  `json:"heledag"`
			AllDay       *bool  `json:"allDay"`
			Locatie      string `json:"locatie"`
			Beschrijving string `json:"beschrijving"`
			Symbol       string `json:"symbol"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		title := firstNonEmpty(args.Titel, args.Title)
		startDate := firstNonEmpty(args.StartDatum, args.StartDatumDB, args.StartIso)
		endDate := firstNonEmpty(args.EindDatum, args.EindDatumDB, args.EindIso, startDate)
		if title == "" || startDate == "" {
			return e.jsonResponse(nil, fmt.Errorf("titel en startDatum zijn verplicht"))
		}
		allDay := false
		if args.Heledag != nil {
			allDay = *args.Heledag
		}
		if args.AllDay != nil {
			allDay = *args.AllDay
		}
		eventID := "ai-" + uuid.NewString()
		event := model.PersonalEvent{
			UserID:       e.userID,
			EventID:      eventID,
			Titel:        title,
			StartDatum:   startDate,
			StartTijd:    optionalStringPtr(firstNonEmpty(args.StartTijd, args.StartTijdDB)),
			EindDatum:    endDate,
			EindTijd:     optionalStringPtr(firstNonEmpty(args.EindTijd, args.EindTijdDB)),
			Heledag:      allDay,
			Locatie:      optionalStringPtr(args.Locatie),
			Beschrijving: optionalStringPtr(args.Beschrijving),
			Symbol:       optionalStringPtr(args.Symbol),
			Status:       store.PersonalEventStatusPendingCreate,
			Kalender:     "AI",
		}
		if event.Heledag {
			event.StartTijd = nil
			event.EindTijd = nil
		}
		if err := e.personalEvStore.Upsert(ctx, event); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{
			"ok":      true,
			"eventId": eventID,
			"status":  store.PersonalEventStatusPendingCreate,
			"message": "Afspraak staat klaar voor Google Calendar sync.",
		}, nil)

	case "afspraakBewerken":
		var args struct {
			EventID      string `json:"eventId"`
			EventIDDB    string `json:"event_id"`
			Titel        string `json:"titel"`
			Title        string `json:"title"`
			StartDatum   string `json:"startDatum"`
			StartDatumDB string `json:"start_datum"`
			StartIso     string `json:"startIso"`
			StartTijd    string `json:"startTijd"`
			StartTijdDB  string `json:"start_tijd"`
			EindDatum    string `json:"eindDatum"`
			EindDatumDB  string `json:"eind_datum"`
			EindIso      string `json:"eindIso"`
			EindTijd     string `json:"eindTijd"`
			EindTijdDB   string `json:"eind_tijd"`
			Heledag      *bool  `json:"heledag"`
			AllDay       *bool  `json:"allDay"`
			Locatie      string `json:"locatie"`
			Beschrijving string `json:"beschrijving"`
			Symbol       string `json:"symbol"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		eventID := firstNonEmpty(args.EventID, args.EventIDDB)
		if eventID == "" {
			return e.jsonResponse(nil, fmt.Errorf("eventId verplicht"))
		}
		event, err := e.personalEvStore.GetByUserEventID(ctx, e.userID, eventID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		if title := firstNonEmpty(args.Titel, args.Title); title != "" {
			event.Titel = title
		}
		if startDate := firstNonEmpty(args.StartDatum, args.StartDatumDB, args.StartIso); startDate != "" {
			event.StartDatum = startDate
		}
		if endDate := firstNonEmpty(args.EindDatum, args.EindDatumDB, args.EindIso); endDate != "" {
			event.EindDatum = endDate
		}
		if startTime := firstNonEmpty(args.StartTijd, args.StartTijdDB); startTime != "" {
			event.StartTijd = optionalStringPtr(startTime)
		}
		if endTime := firstNonEmpty(args.EindTijd, args.EindTijdDB); endTime != "" {
			event.EindTijd = optionalStringPtr(endTime)
		}
		if args.Heledag != nil {
			event.Heledag = *args.Heledag
		}
		if args.AllDay != nil {
			event.Heledag = *args.AllDay
		}
		if event.Heledag {
			event.StartTijd = nil
			event.EindTijd = nil
		}
		if strings.TrimSpace(args.Locatie) != "" {
			event.Locatie = optionalStringPtr(args.Locatie)
		}
		if strings.TrimSpace(args.Beschrijving) != "" {
			event.Beschrijving = optionalStringPtr(args.Beschrijving)
		}
		if strings.TrimSpace(args.Symbol) != "" {
			event.Symbol = optionalStringPtr(args.Symbol)
		}
		event.Status = store.PersonalEventStatusPendingUpdate
		if err := e.personalEvStore.Upsert(ctx, event); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{
			"ok":      true,
			"eventId": event.EventID,
			"status":  store.PersonalEventStatusPendingUpdate,
			"message": "Afspraakwijziging staat klaar voor Google Calendar sync.",
		}, nil)

	case "afspraakVerwijderen":
		var args struct {
			EventID   string `json:"eventId"`
			EventIDDB string `json:"event_id"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		eventID := firstNonEmpty(args.EventID, args.EventIDDB)
		if eventID == "" {
			return e.jsonResponse(nil, fmt.Errorf("eventId verplicht"))
		}
		if err := e.personalEvStore.UpdateStatus(ctx, e.userID, eventID, store.PersonalEventStatusPendingDelete); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{
			"ok":      true,
			"eventId": eventID,
			"status":  store.PersonalEventStatusPendingDelete,
			"message": "Afspraakverwijdering staat klaar voor Google Calendar sync.",
		}, nil)

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

	case "laventecareLeadMaken":
		var args model.LCLeadCreate
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Titel) == "" {
			return e.jsonResponse(nil, fmt.Errorf("titel verplicht"))
		}
		if strings.TrimSpace(args.Bron) == "" {
			args.Bron = "ai"
		}
		lead, err := e.laventeCareStore.CreateLead(ctx, e.userID, args)
		return e.jsonResponse(map[string]any{"ok": true, "lead": lead}, err)

	case "laventecareLeadBijwerken":
		var args struct {
			ID                 string  `json:"id"`
			Status             *string `json:"status"`
			FitScore           *int    `json:"fit_score"`
			Pijnpunt           *string `json:"pijnpunt"`
			Prioriteit         *string `json:"prioriteit"`
			VolgendeStap       *string `json:"volgende_stap"`
			VolgendeActieDatum *string `json:"volgende_actie_datum"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		input := model.LCLeadUpdate{
			Status:             args.Status,
			FitScore:           args.FitScore,
			Pijnpunt:           args.Pijnpunt,
			Prioriteit:         args.Prioriteit,
			VolgendeStap:       args.VolgendeStap,
			VolgendeActieDatum: args.VolgendeActieDatum,
		}
		if err := e.laventeCareStore.UpdateLead(ctx, e.userID, id, input); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "leadId": id.String()}, nil)

	case "laventecareLeadNaarProject":
		var args struct {
			LeadID       string  `json:"lead_id"`
			Naam         string  `json:"naam"`
			Fase         *string `json:"fase"`
			Status       *string `json:"status"`
			Samenvatting *string `json:"samenvatting"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		leadID, err := uuid.Parse(args.LeadID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		project, err := e.laventeCareStore.ConvertLeadToProject(ctx, e.userID, model.LCConvertLeadToProject{
			LeadID:       leadID,
			Naam:         args.Naam,
			Fase:         args.Fase,
			Status:       args.Status,
			Samenvatting: args.Samenvatting,
		})
		return e.jsonResponse(map[string]any{"ok": true, "project": project}, err)

	case "laventecareProjectMaken":
		var args model.LCProjectCreate
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Naam) == "" {
			return e.jsonResponse(nil, fmt.Errorf("naam verplicht"))
		}
		if strings.TrimSpace(args.Fase) == "" {
			args.Fase = "intake"
		}
		if strings.TrimSpace(args.Status) == "" {
			args.Status = "active"
		}
		project, err := e.laventeCareStore.CreateProject(ctx, e.userID, model.LCProject{
			Naam:            args.Naam,
			Fase:            args.Fase,
			Status:          args.Status,
			WaardeIndicatie: args.WaardeIndicatie,
			StartDatum:      args.StartDatum,
			Deadline:        args.Deadline,
			Samenvatting:    args.Samenvatting,
		})
		return e.jsonResponse(map[string]any{"ok": true, "project": project}, err)

	case "laventecareProjectBijwerken":
		var args struct {
			ID              string  `json:"id"`
			Fase            *string `json:"fase"`
			Status          *string `json:"status"`
			WaardeIndicatie *int    `json:"waarde_indicatie"`
			StartDatum      *string `json:"start_datum"`
			Deadline        *string `json:"deadline"`
			Samenvatting    *string `json:"samenvatting"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		input := model.LCProjectUpdate{
			Fase:            args.Fase,
			Status:          args.Status,
			WaardeIndicatie: args.WaardeIndicatie,
			StartDatum:      args.StartDatum,
			Deadline:        args.Deadline,
			Samenvatting:    args.Samenvatting,
		}
		if err := e.laventeCareStore.UpdateProject(ctx, e.userID, id, input); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "projectId": id.String()}, nil)

	case "laventecareActieMaken":
		var args struct {
			Source          string  `json:"source"`
			SourceID        *string `json:"source_id"`
			Title           string  `json:"title"`
			Summary         *string `json:"summary"`
			ActionType      string  `json:"action_type"`
			Priority        string  `json:"priority"`
			DueDate         *string `json:"due_date"`
			LinkedLeadID    *string `json:"linked_lead_id"`
			LinkedProjectID *string `json:"linked_project_id"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Title) == "" {
			return e.jsonResponse(nil, fmt.Errorf("title verplicht"))
		}
		if strings.TrimSpace(args.Source) == "" {
			args.Source = "ai"
		}
		if strings.TrimSpace(args.ActionType) == "" {
			args.ActionType = "follow_up"
		}
		if strings.TrimSpace(args.Priority) == "" {
			args.Priority = "normal"
		}
		var linkedLeadID, linkedProjectID *uuid.UUID
		if args.LinkedLeadID != nil && strings.TrimSpace(*args.LinkedLeadID) != "" {
			id, err := uuid.Parse(*args.LinkedLeadID)
			if err != nil {
				return e.jsonResponse(nil, err)
			}
			linkedLeadID = &id
		}
		if args.LinkedProjectID != nil && strings.TrimSpace(*args.LinkedProjectID) != "" {
			id, err := uuid.Parse(*args.LinkedProjectID)
			if err != nil {
				return e.jsonResponse(nil, err)
			}
			linkedProjectID = &id
		}
		action, err := e.laventeCareStore.CreateAction(ctx, e.userID, model.LCActionCreate{
			Source:          args.Source,
			SourceID:        args.SourceID,
			Title:           args.Title,
			Summary:         args.Summary,
			ActionType:      args.ActionType,
			Priority:        args.Priority,
			DueDate:         args.DueDate,
			LinkedLeadID:    linkedLeadID,
			LinkedProjectID: linkedProjectID,
		})
		return e.jsonResponse(map[string]any{"ok": true, "action": action}, err)

	case "laventecareActieAfronden":
		var args struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(args.ID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		status := strings.TrimSpace(args.Status)
		if status == "" {
			status = "done"
		}
		if err := e.laventeCareStore.UpdateActionStatus(ctx, e.userID, id, status); err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{"ok": true, "actionId": id.String(), "status": status}, nil)

	// ── SMART HOME ───────────────────────────────────────────────────
	case "lampBedien":
		return `{"status": "Geef de actie direct door via de chat, bijv: 'lampen uit' of 'scene ocean'. De bot pikt dit automatisch op voor het AI verzoek."}`

	default:
		return fmt.Sprintf(`{"error": "Tool '%s' niet geïmplementeerd in Go."}`, toolName)
	}
}
