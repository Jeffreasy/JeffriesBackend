package engine

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/ai"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

var (
	financeDateRe1      = regexp.MustCompile(`\b(20\d{2})[-/ ](0?[1-9]|1[0-2])\b`)
	financeDateRe2      = regexp.MustCompile(`\b(0?[1-9]|1[0-2])[-/ ](20\d{2})\b`)
	financeYearRe       = regexp.MustCompile(`\b(20\d{2})\b`)
	financeExactYearRe  = regexp.MustCompile(`^\s*(20\d{2})\s*$`)
	financeExactMonthRe = regexp.MustCompile(`^\s*(0?[1-9]|1[0-2])\s*$`)

	dutchMonthRe = regexp.MustCompile(`\b(januari|jan|februari|feb|maart|mrt|april|apr|mei|juni|jun|juli|jul|augustus|aug|september|sep|oktober|okt|november|nov|december|dec)\b`)

	dutchMonthLookup = map[string]time.Month{
		"januari": time.January, "jan": time.January,
		"februari": time.February, "feb": time.February,
		"maart": time.March, "mrt": time.March,
		"april": time.April, "apr": time.April,
		"mei": time.May,
		"juni": time.June, "jun": time.June,
		"juli": time.July, "jul": time.July,
		"augustus": time.August, "aug": time.August,
		"september": time.September, "sep": time.September,
		"oktober": time.October, "okt": time.October,
		"november": time.November, "nov": time.November,
		"december": time.December, "dec": time.December,
	}
)

type telegramStartSnapshot struct {
	Now             time.Time
	TodaySchedules  int
	TodayEvents     int
	TodayNotes      int
	ActiveNotes     int
	ActiveHabits    int
	TotalDevices    int
	OnlineDevices   int
	PendingCommands int
	PendingAI       int
	NextSchedule    *model.Schedule
}

func (e *Engine) handleStart(ctx context.Context, client *tg.Client, chatID int64) {
	_ = client.SendTyping(chatID)
	snapshot := e.buildStartSnapshot(ctx)
	text := buildWelcomeText(snapshot)
	_ = client.SendMessageWithKeyboard(chatID, text, buildMainMenu())

	chatStore := store.NewChatStore(e.db.Pool)
	agentID := "brain"
	_ = chatStore.SaveMessage(ctx, chatID, "assistant", text, &agentID)
}

func (e *Engine) buildStartSnapshot(ctx context.Context) telegramStartSnapshot {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}

	sCtx, cancel := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer cancel()

	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	end := now.AddDate(0, 0, 30).Format("2006-01-02")
	userID := e.cfg.HomeappUserID

	snapshot := telegramStartSnapshot{Now: now}

	scheduleStore := store.NewScheduleStore(e.db)
	if schedules, err := scheduleStore.ListRange(sCtx, userID, today, today); err == nil {
		snapshot.TodaySchedules = len(visibleSchedules(schedules))
	}
	if upcoming, err := scheduleStore.ListRange(sCtx, userID, today, end); err == nil {
		for _, schedule := range visibleSchedules(upcoming) {
			if scheduleStartsAfter(schedule, now, loc) {
				next := schedule
				snapshot.NextSchedule = &next
				break
			}
		}
	}

	eventStore := store.NewPersonalEventStore(e.db)
	if events, err := eventStore.ListRange(sCtx, userID, today, today); err == nil {
		snapshot.TodayEvents = len(visiblePersonalEvents(events))
	}

	noteStore := store.NewNoteStore(e.db)
	if notes, err := noteStore.List(sCtx, userID); err == nil {
		for _, note := range notes {
			if note.IsArchived {
				continue
			}
			snapshot.ActiveNotes++
			if note.Aangemaakt.In(loc).Format("2006-01-02") == today || note.Gewijzigd.In(loc).Format("2006-01-02") == today {
				snapshot.TodayNotes++
			}
		}
	}

	habitStore := store.NewHabitStore(e.db)
	if stats, err := habitStore.Stats(sCtx, userID); err == nil {
		snapshot.ActiveHabits = stats.ActiveHabits
	}

	_ = e.db.Pool.QueryRow(sCtx,
		`SELECT COUNT(*),
		        COUNT(*) FILTER (WHERE status = 'online')
		   FROM devices`,
	).Scan(&snapshot.TotalDevices, &snapshot.OnlineDevices)

	_ = e.db.Pool.QueryRow(sCtx,
		`SELECT COUNT(*) FROM device_commands WHERE user_id = $1 AND status IN ('pending', 'processing')`,
		userID,
	).Scan(&snapshot.PendingCommands)

	_ = e.db.Pool.QueryRow(sCtx,
		`SELECT COUNT(*) FROM ai_pending_actions WHERE user_id = $1 AND status = 'pending' AND expires_at > now()`,
		userID,
	).Scan(&snapshot.PendingAI)

	return snapshot
}

func (e *Engine) handleAIStatus(_ context.Context, client *tg.Client, chatID int64) {
	mutating, confirmation := countExposedAITools()
	policyOnly := len(ai.Policies) - len(ai.AllTools)
	if policyOnly < 0 {
		policyOnly = 0
	}

	text := fmt.Sprintf(`🤖 AI status

Model: %s
Reasoning: %s
Grok chat: %s
Web-search: %s
Groq voice: %s

Agents: %d
Live tools: %d
Live mutaties: %d
Bevestiging live: %d
Beschermde policy-tools: %d

Gebruik /briefing voor een volledige dagstart of stel direct een natuurlijke vraag.`,
		emptyFallback(e.cfg.GrokModel, "onbekend"),
		emptyFallback(e.cfg.GrokReasoningEffort, "default"),
		configStatus(e.cfg.GrokAPIKey != ""),
		configStatus(e.cfg.GrokAPIKey != ""),
		configStatus(e.cfg.GroqAPIKey != ""),
		len(ai.Registry),
		len(ai.AllTools),
		mutating,
		confirmation,
		policyOnly,
	)

	_ = client.SendMessageWithKeyboard(chatID, text, tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "🧠 Dagbriefing", CallbackData: "/briefing"},
				{Text: "📍 Planning", CallbackData: "/planning"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	})
}

func (e *Engine) handleLampStatus(ctx context.Context, client *tg.Client, chatID int64) {
	dStore := store.NewDeviceStore(e.db)
	devices, err := dStore.GetAll(ctx, 0, 100)
	if err != nil {
		_ = client.SendMessage(chatID, "⚠️ Lampstatus kon niet worden opgehaald.")
		return
	}

	online := 0
	on := 0
	names := make([]string, 0, len(devices))
	for _, device := range devices {
		if device.Status == "online" {
			online++
		}
		if deviceIsOn(device) {
			on++
		}
		if len(names) < 6 {
			names = append(names, device.Name)
		}
	}

	text := fmt.Sprintf("💡 Lampen\n\nOnline: %d/%d\nAan: %d\nQueue: %s\n", online, len(devices), on, queueModeLabel(e.cfg.QueueLightCommands()))
	if len(names) > 0 {
		text += "\nBekend: " + strings.Join(names, ", ")
	}
	text += "\n\nDirect bedienen of typ natuurlijk: lampen uit, lampen 50%, scene focus."

	_ = client.SendMessageWithKeyboard(chatID, text, buildLampMenu())
}

func (e *Engine) handleHabitStatus(ctx context.Context, client *tg.Client, chatID int64) {
	_ = client.SendTyping(chatID)

	habitStore := store.NewHabitStore(e.db)
	userID := e.cfg.HomeappUserID
	today := time.Now().In(telegramLocation()).Format("2006-01-02")

	stats, statsErr := habitStore.Stats(ctx, userID)
	habits, habitsErr := habitStore.List(ctx, userID)
	due, dueErr := habitStore.ListDueForDate(ctx, userID, today)
	logs, logsErr := habitStore.ListLogsForDate(ctx, userID, today)
	badges, badgesErr := habitStore.ListBadges(ctx, userID)
	if statsErr != nil || habitsErr != nil {
		_ = client.SendMessage(chatID, "❌ Habit status ophalen mislukt.")
		return
	}

	logByHabit := make(map[string]model.HabitLog)
	if logsErr == nil {
		for _, log := range logs {
			logByHabit[log.HabitID.String()] = log
		}
	}

	active := make([]model.Habit, 0, len(habits))
	paused := 0
	for _, habit := range habits {
		if habit.IsPauze {
			paused++
			continue
		}
		active = append(active, habit)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "🎯 Habit cockpit — %s\n\n", today)
	fmt.Fprintf(&b, "Vandaag\n")
	fmt.Fprintf(&b, "• Due: %d · voltooid: %d\n", len(due), stats.TodayCompleted)
	fmt.Fprintf(&b, "• Streak: %dd · record: %dd\n", stats.CurrentStreak, stats.LongestStreak)
	fmt.Fprintf(&b, "• Incidenten 30d: %d\n\n", stats.Incidents30d)

	fmt.Fprintf(&b, "Systeem\n")
	fmt.Fprintf(&b, "• Actief: %d", len(active))
	if paused > 0 {
		fmt.Fprintf(&b, " · pauze: %d", paused)
	}
	fmt.Fprintf(&b, "\n• XP: %d · voltooiingen: %d", stats.TotaalXP, stats.TotaalVoltooid)
	if badgesErr == nil {
		fmt.Fprintf(&b, " · badges: %d", len(badges))
	}
	fmt.Fprintf(&b, "\n\n")

	if len(due) > 0 {
		fmt.Fprintf(&b, "Vandaag gepland\n")
		for i, habit := range due[:minInt(len(due), 6)] {
			log, hasLog := logByHabit[habit.ID.String()]
			status := "open"
			if habit.Type == "negatief" && !log.IsIncident {
				status = "clean"
			}
			if hasLog && log.Voltooid {
				status = "voltooid"
			}
			if hasLog && log.IsIncident {
				status = "incident"
			}
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, formatHabitTelegramName(habit), status)
		}
		fmt.Fprintf(&b, "\n")
	} else if len(active) > 0 {
		fmt.Fprintf(&b, "Niet gepland vandaag\n")
		for i, habit := range active[:minInt(len(active), 5)] {
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, formatHabitTelegramName(habit), formatHabitScheduleSummary(habit))
		}
		fmt.Fprintf(&b, "\n")
	}

	if dueErr != nil {
		fmt.Fprintf(&b, "Let op: due-check had een fallback nodig: %s\n\n", truncateRunes(dueErr.Error(), 120))
	}
	if logsErr != nil {
		fmt.Fprintf(&b, "Let op: logs van vandaag konden niet worden geladen.\n\n")
	}

	if len(due) > 0 {
		fmt.Fprintf(&b, "Tip: stuur /check of zeg welke habit je wil afvinken.")
	} else if len(active) > 0 {
		fmt.Fprintf(&b, "Tip: als dit wel dagelijks moet, zet frequentie/roosterfilter in de Habits UI goed.")
	} else {
		fmt.Fprintf(&b, "Tip: maak je eerste habit aan via de Habits UI of Telegram.")
	}

	_ = client.SendMessageWithKeyboard(chatID, b.String(), buildMainMenu())
}

type telegramFinancePeriod struct {
	Label    string
	DatumVan string
	DatumTot string
	AllTime  bool
	Default  bool
}

func (e *Engine) handleFinanceStatus(ctx context.Context, client *tg.Client, chatID int64, text string) {
	_ = client.SendTyping(chatID)

	transactionStore := store.NewTransactionStore(e.db)
	userID := e.cfg.HomeappUserID
	period := parseTelegramFinancePeriod(text, time.Now().In(amsterdamLocation()))

	stats, err := transactionStore.GetStats(ctx, userID)
	if err != nil {
		_ = client.SendMessage(chatID, "❌ Finance status ophalen mislukt.")
		return
	}

	var firstDate, lastDate string
	_ = e.db.Pool.QueryRow(ctx, `
		SELECT COALESCE(MIN(datum)::text, ''), COALESCE(MAX(datum)::text, '')
		FROM transactions WHERE user_id = $1
	`, userID).Scan(&firstDate, &lastDate)

	periodFilter := store.TransactionFilter{
		ExcludeIntern: true,
		Limit:         20000,
	}
	if !period.AllTime {
		periodFilter.DatumVan = period.DatumVan
		periodFilter.DatumTot = period.DatumTot
	}
	periodTxs, totalPeriod, periodErr := transactionStore.ListFiltered(ctx, userID, periodFilter)
	if periodErr != nil {
		_ = client.SendMessage(chatID, "❌ Finance periode ophalen mislukt: "+truncateRunes(periodErr.Error(), 180))
		return
	}

	outgoingFilter := periodFilter
	outgoingFilter.Richting = "uit"
	txs, totalOutgoing, txErr := transactionStore.ListFiltered(ctx, userID, outgoingFilter)
	if txErr != nil {
		_ = client.SendMessage(chatID, "❌ Finance breakdown ophalen mislukt: "+truncateRunes(txErr.Error(), 180))
		return
	}

	summary := summarizeFinanceTransactions(periodTxs)
	periodIncome := floatFromSummary(summary, "inkomsten")
	periodExpenses := floatFromSummary(summary, "uitgaven")
	periodNet := floatFromSummary(summary, "netto")
	if period.AllTime {
		periodIncome = floatFromSummary(stats, "inkomsten")
		periodExpenses = math.Abs(floatFromSummary(stats, "uitgaven"))
		periodNet = floatFromSummary(stats, "saldo")
	}
	topCategories := topFinanceBreakdowns(txs, "categorie", 5)
	topMerchants := topFinanceBreakdowns(txs, "merchant", 5)
	uncategorized := uncategorizedFinanceTransactions(txs, 5)

	var b strings.Builder
	fmt.Fprintf(&b, "💰 Finance cockpit\n\n")
	fmt.Fprintf(&b, "Status\n")
	fmt.Fprintf(&b, "• Huidig saldo: %s\n", formatEuroTelegram(floatFromSummary(stats, "saldo")))
	fmt.Fprintf(&b, "• Dataset: %d transacties", intFromSummary(stats, "totaal"))
	if firstDate != "" && lastDate != "" {
		fmt.Fprintf(&b, " (%s t/m %s)", firstDate, lastDate)
	}
	fmt.Fprintf(&b, "\n\n")

	fmt.Fprintf(&b, "Periode: %s\n", period.Label)
	if !period.AllTime {
		fmt.Fprintf(&b, "• Range: %s t/m %s\n", period.DatumVan, period.DatumTot)
	}
	fmt.Fprintf(&b, "• Inkomsten: %s\n", formatEuroTelegram(periodIncome))
	fmt.Fprintf(&b, "• Uitgaven: %s\n", formatEuroTelegram(periodExpenses))
	fmt.Fprintf(&b, "• Netto: %s\n", formatEuroTelegram(periodNet))
	fmt.Fprintf(&b, "• Transacties: %d", totalPeriod)
	if totalPeriod > len(periodTxs) {
		fmt.Fprintf(&b, " · sample %d", len(periodTxs))
	}
	fmt.Fprintf(&b, "\n\n")

	fmt.Fprintf(&b, "Uitgaven in periode\n")
	fmt.Fprintf(&b, "• Uitgaand totaal: %s\n", formatEuroTelegram(periodExpenses))
	fmt.Fprintf(&b, "• Uitgaande transacties: %d", totalOutgoing)
	if totalOutgoing > len(txs) {
		fmt.Fprintf(&b, " · sample %d", len(txs))
	}
	fmt.Fprintf(&b, "\n\n")

	appendFinanceBreakdown(&b, "Top categorieën", topCategories)
	appendFinanceBreakdown(&b, "Top tegenpartijen", topMerchants)

	if len(uncategorized) > 0 {
		fmt.Fprintf(&b, "\nOngelabeld\n")
		for i, tx := range uncategorized {
			fmt.Fprintf(&b, "%d. %s — %s\n", i+1, transactionCounterparty(tx), formatEuroTelegram(math.Abs(tx.Bedrag)))
		}
	} else {
		fmt.Fprintf(&b, "\nOngelabeld\nGeen ongelabelde transacties in de geanalyseerde set.\n")
	}
	fmt.Fprintf(&b, "\n\nScopes: /finance · /finance vorige maand · /finance 2026 · /finance alles")

	_ = client.SendMessageWithKeyboard(chatID, b.String(), buildMainMenu())
}

func parseTelegramFinancePeriod(text string, now time.Time) telegramFinancePeriod {
	if now.IsZero() {
		now = time.Now().In(amsterdamLocation())
	}
	raw := strings.TrimSpace(text)
	lower := strings.ToLower(raw)
	if lower == "/finance" {
		return telegramFinanceMonthPeriod(now, true)
	}
	if strings.HasPrefix(lower, "/finance") {
		raw = strings.TrimSpace(raw[len("/finance"):])
		lower = strings.TrimSpace(lower[len("/finance"):])
	}
	if lower == "" || lower == "maand" || lower == "deze maand" || lower == "huidige maand" {
		return telegramFinanceMonthPeriod(now, true)
	}
	if containsAny(lower, "alles", "alltime", "lifetime", "totaal", "alle jaren") {
		return telegramFinancePeriod{Label: "alles (2018-heden)", AllTime: true}
	}
	if strings.Contains(lower, "vorige maand") || strings.Contains(lower, "last month") {
		return telegramFinanceMonthPeriod(now.AddDate(0, -1, 0), false)
	}
	if lower == "jaar" || lower == "dit jaar" || lower == "huidig jaar" {
		return telegramFinanceYearPeriod(now.Year())
	}

	if match := financeDateRe1.FindStringSubmatch(lower); len(match) == 3 {
		return telegramFinanceMonthPeriodByParts(match[1], match[2], false)
	}
	if match := financeDateRe2.FindStringSubmatch(lower); len(match) == 3 {
		return telegramFinanceMonthPeriodByParts(match[2], match[1], false)
	}

	year := now.Year()
	if match := financeYearRe.FindStringSubmatch(lower); len(match) == 2 {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			year = parsed
		}
	}
	if month, ok := parseDutchFinanceMonth(lower); ok {
		return telegramFinanceMonthPeriod(time.Date(year, month, 1, 0, 0, 0, 0, amsterdamLocation()), false)
	}

	if match := financeExactYearRe.FindStringSubmatch(lower); len(match) == 2 {
		if parsed, err := strconv.Atoi(match[1]); err == nil {
			return telegramFinanceYearPeriod(parsed)
		}
	}
	if match := financeExactMonthRe.FindStringSubmatch(lower); len(match) == 2 {
		return telegramFinanceMonthPeriodByParts(strconv.Itoa(now.Year()), match[1], false)
	}
	return telegramFinanceMonthPeriod(now, true)
}

func telegramFinanceMonthPeriodByParts(yearValue, monthValue string, isDefault bool) telegramFinancePeriod {
	year, yearErr := strconv.Atoi(yearValue)
	month, monthErr := strconv.Atoi(monthValue)
	if yearErr != nil || monthErr != nil || month < 1 || month > 12 {
		return telegramFinanceMonthPeriod(time.Now().In(amsterdamLocation()), true)
	}
	return telegramFinanceMonthPeriod(time.Date(year, time.Month(month), 1, 0, 0, 0, 0, amsterdamLocation()), isDefault)
}

func telegramFinanceMonthPeriod(date time.Time, isDefault bool) telegramFinancePeriod {
	loc := amsterdamLocation()
	localDate := date.In(loc)
	start := time.Date(localDate.Year(), localDate.Month(), 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, -1)
	label := dutchFinanceMonthName(start.Month()) + " " + strconv.Itoa(start.Year())
	if isDefault {
		end = localDate
		label += " tot nu (standaard)"
	}
	return telegramFinancePeriod{
		Label:    label,
		DatumVan: start.Format("2006-01-02"),
		DatumTot: end.Format("2006-01-02"),
		Default:  isDefault,
	}
}

func dutchFinanceMonthName(month time.Month) string {
	names := map[time.Month]string{
		time.January:   "januari",
		time.February:  "februari",
		time.March:     "maart",
		time.April:     "april",
		time.May:       "mei",
		time.June:      "juni",
		time.July:      "juli",
		time.August:    "augustus",
		time.September: "september",
		time.October:   "oktober",
		time.November:  "november",
		time.December:  "december",
	}
	if name, ok := names[month]; ok {
		return name
	}
	return strings.ToLower(month.String())
}

func telegramFinanceYearPeriod(year int) telegramFinancePeriod {
	return telegramFinancePeriod{
		Label:    strconv.Itoa(year),
		DatumVan: fmt.Sprintf("%04d-01-01", year),
		DatumTot: fmt.Sprintf("%04d-12-31", year),
	}
}

func parseDutchFinanceMonth(lower string) (time.Month, bool) {
	m := dutchMonthRe.FindString(lower)
	if m == "" {
		return time.January, false
	}
	month, ok := dutchMonthLookup[m]
	return month, ok
}

func formatHabitTelegramName(habit model.Habit) string {
	name := strings.TrimSpace(habit.Naam)
	emoji := strings.TrimSpace(habit.Emoji)
	if emoji == "" && name == "" {
		return "Habit"
	}
	if emoji == "" {
		return name
	}
	if name == "" {
		return emoji
	}
	return emoji + " " + name
}

func formatHabitScheduleSummary(habit model.Habit) string {
	parts := []string{}
	frequency := strings.TrimSpace(habit.Frequentie)
	if frequency == "" {
		frequency = "dagelijks"
	}

	switch frequency {
	case "dagelijks":
		parts = append(parts, "dagelijks")
	case "weekdagen":
		parts = append(parts, "weekdagen")
	case "weekenddagen":
		parts = append(parts, "weekend")
	case "aangepast":
		if len(habit.AangepasteDagen) > 0 {
			parts = append(parts, "dagen "+formatHabitDayLabels(habit.AangepasteDagen))
		} else {
			parts = append(parts, "aangepaste dagen leeg")
		}
	case "x_per_week":
		target := 1
		if habit.DoelAantal != nil && *habit.DoelAantal > 0 {
			target = *habit.DoelAantal
		}
		parts = append(parts, fmt.Sprintf("%dx per week", target))
	case "x_per_maand":
		target := 1
		if habit.DoelAantal != nil && *habit.DoelAantal > 0 {
			target = *habit.DoelAantal
		}
		parts = append(parts, fmt.Sprintf("%dx per maand", target))
	default:
		parts = append(parts, frequency)
	}

	if habit.RoosterFilter != nil {
		filter := strings.TrimSpace(*habit.RoosterFilter)
		if filter != "" && !strings.EqualFold(filter, "alle") {
			parts = append(parts, "rooster "+filter)
		}
	}
	if habit.DoelTijd != nil && strings.TrimSpace(*habit.DoelTijd) != "" {
		parts = append(parts, "tijd "+strings.TrimSpace(*habit.DoelTijd))
	}
	if habit.Type == "negatief" {
		parts = append(parts, "avoid")
	}
	if len(parts) == 0 {
		return "geen planning zichtbaar"
	}
	return strings.Join(parts, " · ")
}

func formatHabitDayLabels(days []int32) string {
	labels := map[int32]string{
		0: "zo",
		1: "ma",
		2: "di",
		3: "wo",
		4: "do",
		5: "vr",
		6: "za",
	}
	out := make([]string, 0, len(days))
	for _, day := range days {
		if label, ok := labels[day]; ok {
			out = append(out, label)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ", ")
}

func appendFinanceBreakdown(b *strings.Builder, title string, rows []map[string]any) {
	fmt.Fprintf(b, "%s\n", title)
	if len(rows) == 0 {
		fmt.Fprintf(b, "Geen data.\n\n")
		return
	}
	for i, row := range rows {
		name, _ := row["naam"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			name = "Onbekend"
		}
		fmt.Fprintf(
			b,
			"%d. %s — %s (%dx)\n",
			i+1,
			truncateRunes(name, 54),
			formatEuroTelegram(floatFromSummary(row, "bedrag")),
			intFromSummary(row, "count"),
		)
	}
	fmt.Fprintf(b, "\n")
}

func buildWelcomeText(snapshot telegramStartSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "👋 Jeffries HomeBot\n")
	fmt.Fprintf(&b, "AI cockpit actief — %s %02d %s %s\n\n",
		dutchDayName(snapshot.Now.Weekday()),
		snapshot.Now.Day(),
		dutchMonthName(snapshot.Now.Month()),
		snapshot.Now.Format("15:04"),
	)
	fmt.Fprintf(&b, "📍 Vandaag\n")
	fmt.Fprintf(&b, "• Werk: %s\n", pluralNL(snapshot.TodaySchedules, "dienst", "diensten"))
	fmt.Fprintf(&b, "• Agenda: %s\n", pluralNL(snapshot.TodayEvents, "afspraak", "afspraken"))
	fmt.Fprintf(&b, "• Notities: %d vandaag / %d actief\n", snapshot.TodayNotes, snapshot.ActiveNotes)
	fmt.Fprintf(&b, "• Habits: %d actief\n\n", snapshot.ActiveHabits)
	fmt.Fprintf(&b, "⏭️ Volgende dienst\n")
	fmt.Fprintf(&b, "%s\n\n", formatNextSchedule(snapshot.NextSchedule, snapshot.Now))
	fmt.Fprintf(&b, "🏠 Systeem\n")
	fmt.Fprintf(&b, "• Lampen: %d/%d online\n", snapshot.OnlineDevices, snapshot.TotalDevices)
	fmt.Fprintf(&b, "• Bridge queue: %d actief\n", snapshot.PendingCommands)
	fmt.Fprintf(&b, "• AI: %d agents / %d tools live\n", len(ai.Registry), len(ai.AllTools))
	fmt.Fprintf(&b, "• Bevestigingen: %d open\n\n", snapshot.PendingAI)
	fmt.Fprintf(&b, "Typ of spreek natuurlijk, bijvoorbeeld:\n")
	fmt.Fprintf(&b, "• wat staat er vandaag op mijn planning?\n")
	fmt.Fprintf(&b, "• geef mijn dagbriefing\n")
	fmt.Fprintf(&b, "• noteer: bel morgen terug")
	return b.String()
}

// buildHelpText is the /help output. It's the one documentation surface
// that has to compensate for the native "/" menu deliberately not showing
// every alias (see telegramMenuCommands) — grouped into short sections so
// it scans on a phone instead of reading like one unbroken wall of text,
// and lists every real working command/synonym so nothing here is only
// discoverable by reading Go source.
func buildHelpText() string {
	sections := []struct {
		title string
		lines []string
	}{
		{"Dagstart", []string{
			"/start — AI cockpit en startmenu",
			"/briefing — complete dagbriefing (ook: /brain, /dashboard)",
			"/news — actueel nieuws via web-search (ook: /nieuws)",
		}},
		{"Werk & agenda", []string{
			"/planning — planning vandaag (diensten + afspraken)",
			"/rooster — weekplanning en uren",
			"/agenda — persoonlijke afspraken (ook: /calendar)",
			"/afspraak — nieuwe agenda-afspraak aanmaken",
			"/sync — Gmail- en agenda-sync nu uitvoeren",
		}},
		{"Notities", []string{
			"/notities — notitie-cockpit",
			"/noteer [tekst] — slimme snelle notitie",
			"/zoeknote [term] — notities doorzoeken",
			"/vandaag — notities van vandaag",
			"/week — notities van deze week",
			"/noteai — AI-triage van notities (ook: /notitieai)",
			"/notetriage — urgentie-gesorteerde actielijst (ook: /triagenotes)",
			"/notesamenvatting — thematische samenvatting (ook: /samenvatnotes)",
			"/notehelp — meer over notitie-commando's",
		}},
		{"Finance & LaventeCare", []string{
			"/finance — saldo en transacties",
			"/laventecare — CRM cockpit (ook: /lc)",
		}},
		{"Email", []string{
			"/email — inbox en e-mailsignalen (ook: /inbox)",
			"/compose — nieuwe email opstellen",
		}},
		{"Habits", []string{
			"/habits — habit-cockpit (ook: /streak, /habitrapport)",
			"/check — habit snel afvinken",
		}},
		{"Lampen", []string{
			"/lampen — lampstatus en snelle acties",
			"Vrije tekst werkt ook: 'lampen uit', 'lampen 50%', 'lampen focus'",
		}},
		{"Bevestigingen (AI-acties)", []string{
			"/pending — openstaande bevestigingen (ook: /bevestigingen)",
			"/approve CODE — actie uitvoeren (ook: /confirm, /akkoord)",
			"/reject CODE — actie annuleren (ook: /cancel, /annuleer)",
		}},
		{"Systeem", []string{
			"/ai — AI-diagnose en tool-status",
			"/status — backend health (ook: /health)",
			"/automations — automation- en sync-status",
		}},
	}

	var b strings.Builder
	b.WriteString("🏠 Jeffries HomeBot\n🧠 Vrije tekst gaat standaard naar Jeffries Brain. Notitie-achtige tekst gaat naar de Notes-agent.\n")
	for _, section := range sections {
		fmt.Fprintf(&b, "\n%s\n", section.title)
		for _, line := range section.lines {
			fmt.Fprintf(&b, "%s\n", line)
		}
	}
	b.WriteString("\n🎙️ Spraakberichten worden automatisch herkend — zie /voicehelp.")
	return b.String()
}

func buildMainMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "🧠 Dagbriefing", CallbackData: "/briefing"},
				{Text: "📍 Planning", CallbackData: "/planning"},
			},
			{
				{Text: "📅 Agenda", CallbackData: "/agenda"},
				{Text: "📋 Werkrooster", CallbackData: "/rooster"},
			},
			{
				{Text: "💡 Lampen", CallbackData: "/lampen"},
				{Text: "🌙 Nacht", CallbackData: "lampen nacht"},
			},
			{
				{Text: "📝 Notities", CallbackData: "/notities"},
				{Text: "✍️ Noteer", CallbackData: "/notehelp"},
			},
			{
				{Text: "💰 Finance", CallbackData: "/finance"},
				{Text: "🏢 LaventeCare", CallbackData: "/laventecare"},
			},
			{
				{Text: "🎯 Habits", CallbackData: "/habits"},
				{Text: "📧 Inbox", CallbackData: "/email"},
			},
			{
				{Text: "🔎 Nieuws", CallbackData: "/news"},
				{Text: "⏳ Bevestigingen", CallbackData: "/pending"},
			},
			{
				{Text: "🤖 AI status", CallbackData: "/ai"},
				{Text: "🎙️ Spraak", CallbackData: "/voicehelp"},
			},
			{
				{Text: "❔ Help", CallbackData: "/help"},
			},
		},
	}
}

func buildPendingMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "⏳ Bevestigingen", CallbackData: "/pending"},
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	}
}

func buildLampMenu() tg.InlineKeyboardMarkup {
	return tg.InlineKeyboardMarkup{
		InlineKeyboard: [][]tg.InlineKeyboardButton{
			{
				{Text: "💡 Aan", CallbackData: "lampen aan"},
				{Text: "🌑 Uit", CallbackData: "lampen uit"},
			},
			{
				{Text: "🌅 Ochtend", CallbackData: "lampen ochtend"},
				{Text: "🌙 Nacht", CallbackData: "lampen nacht"},
			},
			{
				{Text: "🎯 Focus", CallbackData: "lampen focus"},
				{Text: "📺 TV", CallbackData: "lampen tv"},
			},
			{
				{Text: "🏠 Startmenu", CallbackData: "/start"},
			},
		},
	}
}

func countExposedAITools() (mutating int, confirmation int) {
	for _, tool := range ai.AllTools {
		name := tool.Function.Name
		if ai.IsMutatingTool(name) {
			mutating++
		}
		if ai.RequiresConfirmation(name) {
			confirmation++
		}
	}
	return mutating, confirmation
}

func configStatus(ok bool) string {
	if ok {
		return "actief"
	}
	return "niet ingesteld"
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func queueModeLabel(queued bool) string {
	if queued {
		return "Render queue"
	}
	return "direct"
}

func deviceIsOn(device model.Device) bool {
	if device.CurrentState == nil {
		return false
	}
	for _, key := range []string{"on", "state"} {
		value, ok := device.CurrentState[key]
		if !ok {
			continue
		}
		switch typed := value.(type) {
		case bool:
			return typed
		case string:
			lower := strings.ToLower(strings.TrimSpace(typed))
			return lower == "true" || lower == "on" || lower == "aan"
		}
	}
	return false
}
