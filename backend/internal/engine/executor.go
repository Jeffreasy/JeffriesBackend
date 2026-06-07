package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/google"
	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/Jeffreasy/JeffriesBackend/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
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
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" || argsJSON == "null" {
		argsJSON = "{}"
	}
	if err := json.Unmarshal([]byte(argsJSON), v); err != nil {
		if lenientErr := unmarshalLenientToolArgs(argsJSON, v); lenientErr == nil {
			return nil
		}
		return fmt.Errorf("invalid arguments: %v", err)
	}
	return nil
}

func unmarshalLenientToolArgs(argsJSON string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(argsJSON))
	decoder.UseNumber()
	var raw map[string]any
	if err := decoder.Decode(&raw); err != nil {
		return err
	}

	target := reflect.ValueOf(v)
	if target.Kind() != reflect.Ptr || target.IsNil() {
		return fmt.Errorf("target must be pointer")
	}
	target = target.Elem()
	if target.Kind() != reflect.Struct {
		return fmt.Errorf("target must point to struct")
	}

	normalized := make(map[string]any, len(raw))
	for i := 0; i < target.NumField(); i++ {
		field := target.Type().Field(i)
		name := strings.Split(field.Tag.Get("json"), ",")[0]
		if name == "" || name == "-" {
			name = field.Name
		}
		value, ok := raw[name]
		if !ok {
			continue
		}
		normalized[name] = coerceToolArgValue(value, field.Type)
	}

	data, err := json.Marshal(normalized)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func coerceToolArgValue(value any, target reflect.Type) any {
	if value == nil {
		return nil
	}
	if target.Kind() == reflect.Ptr {
		return coerceToolArgValue(value, target.Elem())
	}

	switch target.Kind() {
	case reflect.String:
		return stringifyToolArg(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return intifyToolArg(value)
	case reflect.Float32, reflect.Float64:
		return floatifyToolArg(value)
	case reflect.Bool:
		return boolifyToolArg(value)
	case reflect.Slice:
		items, ok := value.([]any)
		if !ok {
			return value
		}
		out := make([]any, 0, len(items))
		for _, item := range items {
			out = append(out, coerceToolArgValue(item, target.Elem()))
		}
		return out
	default:
		return value
	}
}

func stringifyToolArg(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(v)
	default:
		return fmt.Sprint(v)
	}
}

func intifyToolArg(value any) any {
	switch v := value.(type) {
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return int(parsed)
		}
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
		parsedFloat, err := v.Float64()
		if err == nil {
			return int(parsedFloat)
		}
	case float64:
		return int(v)
	}
	return value
}

func floatifyToolArg(value any) any {
	switch v := value.(type) {
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err == nil {
			return parsed
		}
	case json.Number:
		parsed, err := v.Float64()
		if err == nil {
			return parsed
		}
	}
	return value
}

func boolifyToolArg(value any) any {
	switch v := value.(type) {
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(v))
		if err == nil {
			return parsed
		}
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed != 0
		}
	case float64:
		return v != 0
	}
	return value
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

func (e *HomeBotExecutor) resolveHabit(ctx context.Context, idValue, nameValue string) (model.Habit, error) {
	idValue = strings.TrimSpace(idValue)
	if idValue != "" {
		id, err := uuid.Parse(idValue)
		if err != nil {
			return model.Habit{}, err
		}
		habit, err := e.habitStore.Get(ctx, id)
		if err != nil {
			return model.Habit{}, err
		}
		if habit.UserID != e.userID {
			return model.Habit{}, fmt.Errorf("habit niet gevonden")
		}
		return habit, nil
	}

	needle := strings.ToLower(strings.TrimSpace(nameValue))
	if needle == "" {
		return model.Habit{}, fmt.Errorf("habit id of naam verplicht")
	}
	habits, err := e.habitStore.List(ctx, e.userID)
	if err != nil {
		return model.Habit{}, err
	}
	for _, habit := range habits {
		if strings.EqualFold(strings.TrimSpace(habit.Naam), needle) {
			return habit, nil
		}
	}
	for _, habit := range habits {
		if strings.Contains(strings.ToLower(habit.Naam), needle) {
			return habit, nil
		}
	}
	return model.Habit{}, fmt.Errorf("habit niet gevonden: %s", needle)
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

func parseOptionalUUID(value string) (*uuid.UUID, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	id, err := uuid.Parse(value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseToolDateRange(argsJSON string, fallbackToday bool) (startIso, eindIso string, hasRange bool, err error) {
	var args struct {
		StartIso string `json:"startIso"`
		EindIso  string `json:"eindIso"`
	}
	argsJSON = strings.TrimSpace(argsJSON)
	if argsJSON == "" || argsJSON == "null" {
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

func toolPeriodLabel(startIso, eindIso string, hasRange bool) string {
	if !hasRange {
		return "eerstvolgend"
	}
	if startIso == eindIso {
		return startIso
	}
	return startIso + " t/m " + eindIso
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

func applyFinancePeriodFilter(filter *store.TransactionFilter, jaar, maand string) error {
	if maand != "" {
		normalizedMonth, err := normalizeFinanceMonth(jaar, maand)
		if err != nil {
			return err
		}
		from, to, err := financeMonthRange(normalizedMonth)
		if err != nil {
			return err
		}
		filter.DatumVan = from
		filter.DatumTot = to
		return nil
	}
	if jaar == "" {
		return nil
	}
	if len(jaar) != 4 {
		return fmt.Errorf("ongeldig jaar: %s", jaar)
	}
	if _, err := time.Parse("2006", jaar); err != nil {
		return fmt.Errorf("ongeldig jaar: %s", jaar)
	}
	filter.DatumVan = jaar + "-01-01"
	filter.DatumTot = jaar + "-12-31"
	return nil
}

func normalizeFinanceMonth(jaar, maand string) (string, error) {
	jaar = strings.TrimSpace(jaar)
	maand = strings.TrimSpace(maand)
	if maand == "" {
		return "", fmt.Errorf("maand verplicht")
	}
	if len(maand) == 7 && strings.Contains(maand, "-") {
		return maand, nil
	}
	monthNumber, err := strconv.Atoi(maand)
	if err != nil || monthNumber < 1 || monthNumber > 12 {
		return "", fmt.Errorf("ongeldige maand: %s", maand)
	}
	if jaar == "" {
		jaar = time.Now().In(amsterdamLocation()).Format("2006")
	}
	if len(jaar) != 4 {
		return "", fmt.Errorf("ongeldig jaar: %s", jaar)
	}
	return fmt.Sprintf("%s-%02d", jaar, monthNumber), nil
}

func financeMonthRange(month string) (string, string, error) {
	if month == "" {
		return "", "", fmt.Errorf("maand verplicht in YYYY-MM formaat")
	}
	start, err := time.Parse("2006-01", month)
	if err != nil {
		return "", "", fmt.Errorf("ongeldige maand: %s", month)
	}
	end := start.AddDate(0, 1, -1)
	return start.Format("2006-01-02"), end.Format("2006-01-02"), nil
}

func amsterdamLocation() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return time.UTC
	}
	return loc
}

func financePeriodLabel(jaar, maand string) string {
	jaar = strings.TrimSpace(jaar)
	maand = strings.TrimSpace(maand)
	if maand != "" {
		if normalized, err := normalizeFinanceMonth(jaar, maand); err == nil {
			return normalized
		}
		return maand
	}
	if jaar != "" {
		return jaar
	}
	return "alles"
}

func currentFinanceMonthToDate(now time.Time) (jaar, maand, from, to string) {
	loc := amsterdamLocation()
	local := now.In(loc)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, loc)
	return strconv.Itoa(local.Year()), strconv.Itoa(int(local.Month())), start.Format("2006-01-02"), local.Format("2006-01-02")
}

func summarizeFinanceTransactions(txs []model.Transaction) map[string]any {
	var inkomsten, uitgaven float64
	for _, tx := range txs {
		if tx.Bedrag >= 0 {
			inkomsten += tx.Bedrag
		} else {
			uitgaven += math.Abs(tx.Bedrag)
		}
	}
	return map[string]any{
		"aantal":    len(txs),
		"inkomsten": financeRound2(inkomsten),
		"uitgaven":  financeRound2(uitgaven),
		"netto":     financeRound2(inkomsten - uitgaven),
	}
}

func compareFinanceSummaries(a, b map[string]any) map[string]any {
	return map[string]any{
		"aantal":    intFromSummary(b, "aantal") - intFromSummary(a, "aantal"),
		"inkomsten": financeRound2(floatFromSummary(b, "inkomsten") - floatFromSummary(a, "inkomsten")),
		"uitgaven":  financeRound2(floatFromSummary(b, "uitgaven") - floatFromSummary(a, "uitgaven")),
		"netto":     financeRound2(floatFromSummary(b, "netto") - floatFromSummary(a, "netto")),
	}
}

func floatFromSummary(summary map[string]any, key string) float64 {
	switch value := summary[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	default:
		return 0
	}
}

func intFromSummary(summary map[string]any, key string) int {
	switch value := summary[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func topFinanceBreakdowns(txs []model.Transaction, mode string, limit int) []map[string]any {
	type bucket struct {
		key    string
		count  int
		amount float64
	}
	buckets := make(map[string]*bucket)
	for _, tx := range txs {
		key := transactionCategory(tx)
		if mode == "merchant" {
			key = transactionCounterparty(tx)
		}
		item, ok := buckets[key]
		if !ok {
			item = &bucket{key: key}
			buckets[key] = item
		}
		item.count++
		item.amount += math.Abs(tx.Bedrag)
	}
	items := make([]*bucket, 0, len(buckets))
	for _, item := range buckets {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].amount == items[j].amount {
			return items[i].count > items[j].count
		}
		return items[i].amount > items[j].amount
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"naam":   item.key,
			"count":  item.count,
			"bedrag": financeRound2(item.amount),
		})
	}
	return out
}

func recurringFinanceExpenses(txs []model.Transaction, limit int) []map[string]any {
	type recurring struct {
		key        string
		months     map[string]bool
		count      int
		total      float64
		lastDate   string
		categories map[string]int
	}
	buckets := make(map[string]*recurring)
	for _, tx := range txs {
		if len(tx.Datum) < 7 {
			continue
		}
		key := transactionCounterparty(tx)
		item, ok := buckets[key]
		if !ok {
			item = &recurring{key: key, months: make(map[string]bool), categories: make(map[string]int)}
			buckets[key] = item
		}
		item.months[tx.Datum[:7]] = true
		item.count++
		item.total += math.Abs(tx.Bedrag)
		if tx.Datum > item.lastDate {
			item.lastDate = tx.Datum
		}
		item.categories[transactionCategory(tx)]++
	}
	items := make([]*recurring, 0, len(buckets))
	for _, item := range buckets {
		if len(item.months) >= 2 {
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if len(items[i].months) == len(items[j].months) {
			return items[i].total > items[j].total
		}
		return len(items[i].months) > len(items[j].months)
	})
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		months := len(item.months)
		out = append(out, map[string]any{
			"naam":      item.key,
			"maanden":   months,
			"count":     item.count,
			"totaal":    financeRound2(item.total),
			"gemiddeld": financeRound2(item.total / float64(maxInt(months, 1))),
			"laatste":   item.lastDate,
			"categorie": mostUsedCategory(item.categories),
		})
	}
	return out
}

func uncategorizedFinanceTransactions(txs []model.Transaction, limit int) []model.Transaction {
	out := make([]model.Transaction, 0, limit)
	for _, tx := range txs {
		if tx.Categorie != nil && strings.TrimSpace(*tx.Categorie) != "" {
			continue
		}
		out = append(out, tx)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func transactionCategory(tx model.Transaction) string {
	if tx.Categorie != nil && strings.TrimSpace(*tx.Categorie) != "" {
		return strings.TrimSpace(*tx.Categorie)
	}
	return "Ongelabeld"
}

func transactionCounterparty(tx model.Transaction) string {
	if tx.TegenpartijNaam != nil && strings.TrimSpace(*tx.TegenpartijNaam) != "" {
		return strings.TrimSpace(*tx.TegenpartijNaam)
	}
	if strings.TrimSpace(tx.Omschrijving) != "" {
		return truncateRunes(strings.TrimSpace(tx.Omschrijving), 80)
	}
	if tx.TegenrekeningIban != nil && strings.TrimSpace(*tx.TegenrekeningIban) != "" {
		return strings.TrimSpace(*tx.TegenrekeningIban)
	}
	return "Onbekend"
}

func mostUsedCategory(values map[string]int) string {
	best := "Ongelabeld"
	bestCount := -1
	for name, count := range values {
		if count > bestCount {
			best = name
			bestCount = count
		}
	}
	return best
}

func financeRound2(value float64) float64 {
	return math.Round(value*100) / 100
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func normalizedHabitType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "negatief", "vermijden", "avoid":
		return "negatief"
	default:
		return "positief"
	}
}

func normalizedHabitFrequency(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "weekdagen", "werkdagen":
		return "weekdagen"
	case "weekend", "weekenddagen":
		return "weekenddagen"
	case "aangepast", "custom":
		return "aangepast"
	case "x_per_week", "per_week":
		return "x_per_week"
	case "x_per_maand", "per_maand":
		return "x_per_maand"
	default:
		return "dagelijks"
	}
}

func normalizedHabitDifficulty(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "makkelijk", "easy":
		return "makkelijk"
	case "moeilijk", "hard":
		return "moeilijk"
	default:
		return "normaal"
	}
}

func habitXPForDifficulty(value string) int {
	switch normalizedHabitDifficulty(value) {
	case "makkelijk":
		return 5
	case "moeilijk":
		return 20
	default:
		return 10
	}
}

func habitSummary(habit model.Habit) map[string]any {
	return map[string]any{
		"id":             habit.ID.String(),
		"naam":           habit.Naam,
		"emoji":          habit.Emoji,
		"type":           habit.Type,
		"frequentie":     habit.Frequentie,
		"huidigeStreak":  habit.HuidigeStreak,
		"langsteStreak":  habit.LangsteStreak,
		"totaalVoltooid": habit.TotaalVoltooid,
	}
}

func habitNames(habits []model.Habit) []string {
	names := make([]string, 0, len(habits))
	for _, habit := range habits {
		names = append(names, strings.TrimSpace(habit.Emoji+" "+habit.Naam))
	}
	return names
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
	slog.Info("AI tool execute", "tool", toolName)
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

	case "contextBriefingOpvragen":
		var args contextBriefingOptions
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		briefing, err := e.buildContextBriefing(ctx, args)
		return e.jsonResponse(briefing, err)

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
			"scope":          "werkrooster",
			"periode":        toolPeriodLabel(startIso, eindIso, hasRange),
			"aantalDiensten": len(events),
			"diensten":       events,
			"totaalUur":      total,
			"instruction":    "Vermeld totaalUur altijd wanneer je diensten samenvat. Zonder opgegeven periode zijn dit de eerstvolgende diensten.",
		}, nil)

	case "contractAnalyseOpvragen":
		return e.executeContractAnalyse(ctx)

	// ── FINANCE ──────────────────────────────────────────────────────
	case "saldoOpvragen":
		stats, err := e.transactionStore.GetStats(ctx, e.userID)
		jaar, maand, from, to := currentFinanceMonthToDate(time.Now())
		filter := store.TransactionFilter{ExcludeIntern: true, DatumVan: from, DatumTot: to, Limit: 20000}
		var currentMonthTxs []model.Transaction
		var currentMonthTotal int
		periodErr := err
		if periodErr == nil {
			currentMonthTxs, currentMonthTotal, periodErr = e.transactionStore.ListFiltered(ctx, e.userID, filter)
		}
		return e.jsonResponse(map[string]any{
			"scope":               "finance dashboard",
			"stats":               stats,
			"defaultPeriode":      financePeriodLabel(jaar, maand) + " tot nu",
			"defaultPeriodeVan":   from,
			"defaultPeriodeTot":   to,
			"defaultPeriodeTotal": currentMonthTotal,
			"defaultSummary":      summarizeFinanceTransactions(currentMonthTxs),
			"instruction":         "Gebruik stats alleen voor huidig totaalsaldo/dataset. Voor analyse zonder expliciete periode gebruik je defaultSummary van de huidige maand tot nu. Lifetime/all-time alleen noemen als de gebruiker daarom vraagt.",
		}, periodErr)

	case "salarisOpvragen":
		salaries, err := e.salaryStore.List(ctx, e.userID)
		return e.jsonResponse(map[string]any{
			"scope":       "salaris",
			"count":       len(salaries),
			"items":       salaries,
			"instruction": "Gebruik alleen bedragen en periodes uit deze loonstroken. Combineer met rooster-tools als de vraag over uren of prognose gaat.",
		}, err)

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
		txs, total, err := e.transactionStore.ListFiltered(ctx, e.userID, filter)
		return e.jsonResponse(map[string]any{
			"scope":       "financiele transacties",
			"query":       strings.TrimSpace(args.Query),
			"limit":       args.Limit,
			"count":       len(txs),
			"total":       total,
			"items":       txs,
			"instruction": "Dit is een beperkte selectie. Zeg expliciet hoeveel resultaten zijn teruggegeven en hoeveel totaal matchen.",
		}, err)

	case "uitgavenOverzicht":
		var args struct {
			Jaar  string `json:"jaar"`
			Maand string `json:"maand"`
			Iban  string `json:"iban"`
			Limit int    `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		filter := store.TransactionFilter{
			ExcludeIntern: true,
			Richting:      "uit",
			Iban:          strings.TrimSpace(args.Iban),
			Limit:         20000,
		}
		jaar := strings.TrimSpace(args.Jaar)
		maand := strings.TrimSpace(args.Maand)
		defaulted := false
		if jaar == "" && maand == "" {
			jaar, maand, filter.DatumVan, filter.DatumTot = currentFinanceMonthToDate(time.Now())
			defaulted = true
		} else if err := applyFinancePeriodFilter(&filter, jaar, maand); err != nil {
			return e.jsonResponse(nil, err)
		}
		txs, total, err := e.transactionStore.ListFiltered(ctx, e.userID, filter)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		periodLabel := financePeriodLabel(jaar, maand)
		if defaulted {
			periodLabel += " tot nu"
		}
		return e.jsonResponse(map[string]any{
			"scope":            "uitgavenoverzicht",
			"periode":          periodLabel,
			"periodeVan":       filter.DatumVan,
			"periodeTot":       filter.DatumTot,
			"defaulted":        defaulted,
			"rekening":         strings.TrimSpace(args.Iban),
			"totalMatches":     total,
			"sampled":          len(txs),
			"summary":          summarizeFinanceTransactions(txs),
			"topCategorieen":   topFinanceBreakdowns(txs, "categorie", clampToolLimit(args.Limit, 5, 10)),
			"topTegenpartijen": topFinanceBreakdowns(txs, "merchant", clampToolLimit(args.Limit, 5, 10)),
			"instruction":      "Dit overzicht gebruikt uitgaande externe transacties binnen de periode. Zonder expliciete periode is dit de huidige maand tot nu, niet alle jaren en niet de volledige toekomstige maand. Noem totalMatches als sampled lager is dan totalMatches.",
		}, nil)

	case "maandVergelijken":
		var args struct {
			MaandA string `json:"maandA"`
			MaandB string `json:"maandB"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		aFrom, aTo, err := financeMonthRange(strings.TrimSpace(args.MaandA))
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		bFrom, bTo, err := financeMonthRange(strings.TrimSpace(args.MaandB))
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		aTxs, aTotal, err := e.transactionStore.ListFiltered(ctx, e.userID, store.TransactionFilter{ExcludeIntern: true, DatumVan: aFrom, DatumTot: aTo, Limit: 5000})
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		bTxs, bTotal, err := e.transactionStore.ListFiltered(ctx, e.userID, store.TransactionFilter{ExcludeIntern: true, DatumVan: bFrom, DatumTot: bTo, Limit: 5000})
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		aSummary := summarizeFinanceTransactions(aTxs)
		bSummary := summarizeFinanceTransactions(bTxs)
		return e.jsonResponse(map[string]any{
			"scope":       "maandvergelijking",
			"maandA":      map[string]any{"maand": args.MaandA, "totalMatches": aTotal, "sampled": len(aTxs), "summary": aSummary},
			"maandB":      map[string]any{"maand": args.MaandB, "totalMatches": bTotal, "sampled": len(bTxs), "summary": bSummary},
			"verschil":    compareFinanceSummaries(aSummary, bSummary),
			"instruction": "Vergelijk maandB met maandA. Gebruik de verschilvelden en verzin geen verklaringen zonder transactiedata.",
		}, nil)

	case "vasteLastenAnalyse":
		var args struct {
			Jaar  string `json:"jaar"`
			Limit int    `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		filter := store.TransactionFilter{ExcludeIntern: true, Richting: "uit", Limit: 5000}
		if err := applyFinancePeriodFilter(&filter, strings.TrimSpace(args.Jaar), ""); err != nil {
			return e.jsonResponse(nil, err)
		}
		txs, total, err := e.transactionStore.ListFiltered(ctx, e.userID, filter)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{
			"scope":        "vaste lasten analyse",
			"periode":      financePeriodLabel(args.Jaar, ""),
			"totalMatches": total,
			"sampled":      len(txs),
			"items":        recurringFinanceExpenses(txs, clampToolLimit(args.Limit, 10, 15)),
			"instruction":  "Dit zijn terugkerende uitgaven op basis van tegenpartij/omschrijving die in meerdere maanden voorkomen.",
		}, nil)

	case "ongelabeldAnalyse":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		limit := clampToolLimit(args.Limit, 20, 30)
		txs, total, err := e.transactionStore.ListFiltered(ctx, e.userID, store.TransactionFilter{ExcludeIntern: true, Limit: 1000})
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		ungrouped := uncategorizedFinanceTransactions(txs, limit)
		return e.jsonResponse(map[string]any{
			"scope":       "ongelabelde transacties",
			"scanned":     len(txs),
			"total":       total,
			"limit":       limit,
			"items":       ungrouped,
			"groups":      topFinanceBreakdowns(ungrouped, "merchant", 10),
			"instruction": "Dit zijn recente externe transacties zonder categorie. Gebruik categorieWijzigen of bulkCategoriseren alleen via bevestiging.",
		}, nil)

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

		items := make([]model.Note, 0, limit)
		totalOpen := 0
		totalPinned := 0
		totalCompleted := 0
		totalArchived := 0
		for _, note := range notes {
			if note.IsArchived {
				totalArchived++
				continue
			}
			if note.IsCompleted {
				totalCompleted++
				continue
			}
			totalOpen++
			if note.IsPinned {
				totalPinned++
			}
			if len(items) < limit {
				items = append(items, note)
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
			"totalActive":       totalOpen,
			"totalOpen":         totalOpen,
			"totalInCollection": totalOpen + totalCompleted,
			"totalPinned":       totalPinned,
			"totalCompleted":    totalCompleted,
			"totalArchived":     totalArchived,
			"limit":             limit,
			"hasActive":         totalOpen > 0,
			"stats": map[string]any{
				"active":    stats.Active,
				"today":     stats.Today,
				"pinned":    stats.Pinned,
				"completed": stats.Completed,
				"attention": stats.Attention,
				"topTags":   stats.TopTags,
			},
			"focus":       focus,
			"items":       items,
			"instruction": "Als totalActive groter is dan 0, zeg nooit dat er geen actieve notities zijn. Gebruik focus voor prioriteit, deadline, checklist en triage.",
		}, nil)

	case "notitieAanmaken":
		var args struct {
			Titel                string   `json:"titel"`
			Inhoud               string   `json:"inhoud"`
			Tags                 []string `json:"tags"`
			Prioriteit           string   `json:"prioriteit"`
			Symbol               string   `json:"symbol"`
			Deadline             string   `json:"deadline"`
			TriageFlag           *bool    `json:"triage_flag"`
			BusinessContextType  string   `json:"businessContextType"`
			BusinessContextID    string   `json:"businessContextId"`
			BusinessContextTitle string   `json:"businessContextTitle"`
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
			Titel:                &title,
			Inhoud:               args.Inhoud,
			Tags:                 args.Tags,
			Prioriteit:           strPtr(args.Prioriteit),
			Symbol:               strPtr(args.Symbol),
			Deadline:             deadline,
			TriageFlag:           args.TriageFlag,
			BusinessContextType:  optionalStringPtr(args.BusinessContextType),
			BusinessContextID:    optionalStringPtr(args.BusinessContextID),
			BusinessContextTitle: optionalStringPtr(args.BusinessContextTitle),
		})
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		return fmt.Sprintf(`{"success": true, "note_id": "%s"}`, n.ID)

	case "notitiePinnen":
		var args struct {
			ID     string `json:"id"`
			Pinned *bool  `json:"pinned"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(strings.TrimSpace(args.ID))
		if err != nil {
			return e.jsonResponse(nil, fmt.Errorf("ongeldig notitie-id"))
		}
		current, err := e.noteStore.GetForUser(ctx, e.userID, id)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		nextPinned := !current.IsPinned
		if args.Pinned != nil {
			nextPinned = *args.Pinned
		}
		updated, err := e.noteStore.UpdateForUser(ctx, e.userID, id, map[string]any{"is_pinned": nextPinned})
		loc, _ := time.LoadLocation("Europe/Amsterdam")
		if loc == nil {
			loc = time.UTC
		}
		return e.jsonResponse(map[string]any{"ok": true, "note": noteAIItem(updated, time.Now().In(loc), loc)}, err)

	case "notitieBewerken":
		var args struct {
			ID                   string   `json:"id"`
			Titel                *string  `json:"titel"`
			Inhoud               *string  `json:"inhoud"`
			Tags                 []string `json:"tags"`
			Prioriteit           *string  `json:"prioriteit"`
			Symbol               *string  `json:"symbol"`
			Deadline             *string  `json:"deadline"`
			TriageFlag           *bool    `json:"triage_flag"`
			IsCompleted          *bool    `json:"is_completed"`
			BusinessContextType  *string  `json:"businessContextType"`
			BusinessContextID    *string  `json:"businessContextId"`
			BusinessContextTitle *string  `json:"businessContextTitle"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(strings.TrimSpace(args.ID))
		if err != nil {
			return e.jsonResponse(nil, fmt.Errorf("ongeldig notitie-id"))
		}
		fields := map[string]any{}
		if args.Titel != nil {
			fields["titel"] = strings.TrimSpace(*args.Titel)
		}
		if args.Inhoud != nil {
			fields["inhoud"] = strings.TrimRight(*args.Inhoud, "\r\n\t ")
		}
		if args.Tags != nil {
			fields["tags"] = args.Tags
		}
		if args.Prioriteit != nil {
			priority := strings.TrimSpace(strings.ToLower(*args.Prioriteit))
			if priority != "" && priority != "laag" && priority != "normaal" && priority != "hoog" {
				return e.jsonResponse(nil, fmt.Errorf("prioriteit moet laag, normaal of hoog zijn"))
			}
			fields["prioriteit"] = priority
		}
		if args.Symbol != nil {
			symbol := strings.TrimSpace(*args.Symbol)
			if symbol == "" {
				fields["symbol"] = nil
			} else {
				fields["symbol"] = symbol
			}
		}
		if args.Deadline != nil {
			deadline, err := parseOptionalNoteDeadline(*args.Deadline)
			if err != nil {
				return e.jsonResponse(nil, err)
			}
			fields["deadline"] = deadline
		}
		if args.TriageFlag != nil {
			fields["triage_flag"] = *args.TriageFlag
		}
		if args.BusinessContextType != nil {
			fields["business_context_type"] = optionalStringPtr(*args.BusinessContextType)
		}
		if args.BusinessContextID != nil {
			fields["business_context_id"] = optionalStringPtr(*args.BusinessContextID)
		}
		if args.BusinessContextTitle != nil {
			fields["business_context_title"] = optionalStringPtr(*args.BusinessContextTitle)
		}
		if args.IsCompleted != nil {
			fields["is_completed"] = *args.IsCompleted
			if *args.IsCompleted {
				fields["completed_at"] = time.Now()
				fields["triage_flag"] = false
			} else {
				fields["completed_at"] = nil
			}
		}
		if len(fields) == 0 {
			return e.jsonResponse(nil, fmt.Errorf("geen wijzigingen opgegeven"))
		}
		updated, err := e.noteStore.UpdateForUser(ctx, e.userID, id, fields)
		loc, _ := time.LoadLocation("Europe/Amsterdam")
		if loc == nil {
			loc = time.UTC
		}
		return e.jsonResponse(map[string]any{"ok": true, "note": noteAIItem(updated, time.Now().In(loc), loc)}, err)

	case "notitieArchiveren":
		var args struct {
			ID       string `json:"id"`
			Archived *bool  `json:"archived"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		id, err := uuid.Parse(strings.TrimSpace(args.ID))
		if err != nil {
			return e.jsonResponse(nil, fmt.Errorf("ongeldig notitie-id"))
		}
		nextArchived := true
		if args.Archived != nil {
			nextArchived = *args.Archived
		}
		updated, err := e.noteStore.UpdateForUser(ctx, e.userID, id, map[string]any{"is_archived": nextArchived})
		loc, _ := time.LoadLocation("Europe/Amsterdam")
		if loc == nil {
			loc = time.UTC
		}
		return e.jsonResponse(map[string]any{"ok": true, "archived": nextArchived, "note": noteAIItem(updated, time.Now().In(loc), loc)}, err)

	case "bulkArchiveerNotities":
		var args struct {
			IDs []string `json:"ids"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if len(args.IDs) == 0 {
			return e.jsonResponse(nil, fmt.Errorf("ids verplicht"))
		}
		if len(args.IDs) > 20 {
			args.IDs = args.IDs[:20]
		}
		archived := 0
		for _, rawID := range args.IDs {
			id, err := uuid.Parse(strings.TrimSpace(rawID))
			if err != nil {
				return e.jsonResponse(nil, fmt.Errorf("ongeldig notitie-id: %s", rawID))
			}
			if _, err := e.noteStore.UpdateForUser(ctx, e.userID, id, map[string]any{"is_archived": true}); err != nil {
				return e.jsonResponse(nil, err)
			}
			archived++
		}
		return e.jsonResponse(map[string]any{"ok": true, "archived": archived}, nil)

	case "notitiesVandaag":
		notes, err := e.noteStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		loc, err := time.LoadLocation("Europe/Amsterdam")
		if err != nil {
			loc = time.UTC
		}
		now := time.Now().In(loc)
		todayStr := now.Format("2006-01-02")
		var todayNotes []model.Note
		for _, n := range notes {
			if !n.IsArchived && (n.Aangemaakt.In(loc).Format("2006-01-02") == todayStr || n.Gewijzigd.In(loc).Format("2006-01-02") == todayStr) {
				todayNotes = append(todayNotes, n)
			}
		}

		active := activeNotes(notes)
		open := openNotes(notes)
		stats := buildNoteStats(active, now, loc)
		return e.jsonResponse(map[string]any{
			"scope":       "notities aangemaakt of gewijzigd vandaag",
			"date":        todayStr,
			"count":       len(todayNotes),
			"items":       todayNotes,
			"totalActive": len(open),
			"totalOpen":   len(open),
			"hasActive":   len(open) > 0,
			"stats": map[string]any{
				"active":    stats.Active,
				"today":     stats.Today,
				"pinned":    stats.Pinned,
				"completed": stats.Completed,
				"attention": stats.Attention,
				"topTags":   stats.TopTags,
			},
			"instruction": "Een lege items-lijst betekent alleen dat er vandaag niets is aangemaakt of gewijzigd. Gebruik Live Data.notes of notitiesOverzicht voor alle actieve notities.",
		}, nil)

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
		return e.jsonResponse(map[string]any{
			"scope":           "persoonlijke agenda-afspraken",
			"periode":         toolPeriodLabel(startIso, eindIso, hasRange),
			"aantalAfspraken": len(events),
			"afspraken":       events,
			"instruction":     "Dit zijn persoonlijke afspraken, niet de diensten. Gebruik planningOpvragen wanneer diensten en afspraken samen nodig zijn.",
		}, err)

	case "afspraakMaken":
		var args struct {
			Titel                string `json:"titel"`
			Title                string `json:"title"`
			StartDatum           string `json:"startDatum"`
			StartDatumDB         string `json:"start_datum"`
			StartIso             string `json:"startIso"`
			StartTijd            string `json:"startTijd"`
			StartTijdDB          string `json:"start_tijd"`
			EindDatum            string `json:"eindDatum"`
			EindDatumDB          string `json:"eind_datum"`
			EindIso              string `json:"eindIso"`
			EindTijd             string `json:"eindTijd"`
			EindTijdDB           string `json:"eind_tijd"`
			Heledag              *bool  `json:"heledag"`
			AllDay               *bool  `json:"allDay"`
			Locatie              string `json:"locatie"`
			Beschrijving         string `json:"beschrijving"`
			Symbol               string `json:"symbol"`
			BusinessContextType  string `json:"businessContextType"`
			BusinessContextID    string `json:"businessContextId"`
			BusinessContextTitle string `json:"businessContextTitle"`
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
			UserID:               e.userID,
			EventID:              eventID,
			Titel:                title,
			StartDatum:           startDate,
			StartTijd:            optionalStringPtr(firstNonEmpty(args.StartTijd, args.StartTijdDB)),
			EindDatum:            endDate,
			EindTijd:             optionalStringPtr(firstNonEmpty(args.EindTijd, args.EindTijdDB)),
			Heledag:              allDay,
			Locatie:              optionalStringPtr(args.Locatie),
			Beschrijving:         optionalStringPtr(args.Beschrijving),
			Symbol:               optionalStringPtr(args.Symbol),
			BusinessContextType:  optionalStringPtr(args.BusinessContextType),
			BusinessContextID:    optionalStringPtr(args.BusinessContextID),
			BusinessContextTitle: optionalStringPtr(args.BusinessContextTitle),
			Status:               store.PersonalEventStatusPendingCreate,
			Kalender:             "AI",
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
			EventID              string `json:"eventId"`
			EventIDDB            string `json:"event_id"`
			Titel                string `json:"titel"`
			Title                string `json:"title"`
			StartDatum           string `json:"startDatum"`
			StartDatumDB         string `json:"start_datum"`
			StartIso             string `json:"startIso"`
			StartTijd            string `json:"startTijd"`
			StartTijdDB          string `json:"start_tijd"`
			EindDatum            string `json:"eindDatum"`
			EindDatumDB          string `json:"eind_datum"`
			EindIso              string `json:"eindIso"`
			EindTijd             string `json:"eindTijd"`
			EindTijdDB           string `json:"eind_tijd"`
			Heledag              *bool  `json:"heledag"`
			AllDay               *bool  `json:"allDay"`
			Locatie              string `json:"locatie"`
			Beschrijving         string `json:"beschrijving"`
			Symbol               string `json:"symbol"`
			BusinessContextType  string `json:"businessContextType"`
			BusinessContextID    string `json:"businessContextId"`
			BusinessContextTitle string `json:"businessContextTitle"`
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
		if strings.TrimSpace(args.BusinessContextType) != "" {
			event.BusinessContextType = optionalStringPtr(args.BusinessContextType)
		}
		if strings.TrimSpace(args.BusinessContextID) != "" {
			event.BusinessContextID = optionalStringPtr(args.BusinessContextID)
		}
		if strings.TrimSpace(args.BusinessContextTitle) != "" {
			event.BusinessContextTitle = optionalStringPtr(args.BusinessContextTitle)
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
	case "habitAanmaken":
		var args struct {
			Naam              string   `json:"naam"`
			Emoji             string   `json:"emoji"`
			Type              string   `json:"type"`
			Beschrijving      string   `json:"beschrijving"`
			Frequentie        string   `json:"frequentie"`
			AangepasteDagen   []int32  `json:"aangepaste_dagen"`
			DoelAantal        *int     `json:"doel_aantal"`
			RoosterFilter     string   `json:"rooster_filter"`
			IsKwantitatief    bool     `json:"is_kwantitatief"`
			DoelWaarde        *float64 `json:"doel_waarde"`
			Eenheid           string   `json:"eenheid"`
			DoelTijd          string   `json:"doel_tijd"`
			Moeilijkheid      string   `json:"moeilijkheid"`
			FinancieCategorie string   `json:"financie_categorie"`
			Kleur             string   `json:"kleur"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Naam) == "" {
			return e.jsonResponse(nil, fmt.Errorf("naam verplicht"))
		}
		habit := model.Habit{
			Naam:              strings.TrimSpace(args.Naam),
			Emoji:             firstNonEmpty(args.Emoji, "🎯"),
			Type:              normalizedHabitType(args.Type),
			Beschrijving:      optionalStringPtr(args.Beschrijving),
			Frequentie:        normalizedHabitFrequency(args.Frequentie),
			AangepasteDagen:   args.AangepasteDagen,
			DoelAantal:        args.DoelAantal,
			RoosterFilter:     optionalStringPtr(args.RoosterFilter),
			IsKwantitatief:    args.IsKwantitatief,
			DoelWaarde:        args.DoelWaarde,
			Eenheid:           optionalStringPtr(args.Eenheid),
			DoelTijd:          optionalStringPtr(args.DoelTijd),
			XPPerVoltooiing:   habitXPForDifficulty(args.Moeilijkheid),
			Moeilijkheid:      normalizedHabitDifficulty(args.Moeilijkheid),
			FinancieCategorie: optionalStringPtr(args.FinancieCategorie),
			Kleur:             optionalStringPtr(firstNonEmpty(args.Kleur, "#f97316")),
		}
		created, err := e.habitStore.Create(ctx, e.userID, habit)
		return e.jsonResponse(map[string]any{"ok": true, "scope": "habit aangemaakt", "habit": created}, err)

	case "habitVoltooien":
		var args struct {
			ID      string   `json:"id"`
			HabitID string   `json:"habitId"`
			Naam    string   `json:"naam"`
			Datum   string   `json:"datum"`
			Waarde  *float64 `json:"waarde"`
			Notitie string   `json:"notitie"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		habit, err := e.resolveHabit(ctx, firstNonEmpty(args.ID, args.HabitID), args.Naam)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		datum := firstNonEmpty(args.Datum, todayAmsterdamISO())
		log, err := e.habitStore.UpsertLog(ctx, model.HabitLog{
			UserID:   e.userID,
			HabitID:  habit.ID,
			Datum:    datum,
			Voltooid: true,
			Waarde:   args.Waarde,
			Notitie:  optionalStringPtr(args.Notitie),
			Bron:     "telegram",
		})
		return e.jsonResponse(map[string]any{
			"ok":          true,
			"scope":       "habit voltooid",
			"habit":       habitSummary(habit),
			"log":         log,
			"instruction": "Bij kwantitatieve habits is voltooid alleen true als de waarde het doel haalt.",
		}, err)

	case "habitIncident":
		var args struct {
			ID      string `json:"id"`
			HabitID string `json:"habitId"`
			Naam    string `json:"naam"`
			Trigger string `json:"trigger"`
			Notitie string `json:"notitie"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		habit, err := e.resolveHabit(ctx, firstNonEmpty(args.ID, args.HabitID), args.Naam)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		log, err := e.habitStore.UpsertLog(ctx, model.HabitLog{
			UserID:     e.userID,
			HabitID:    habit.ID,
			Datum:      todayAmsterdamISO(),
			IsIncident: true,
			TriggerCat: optionalStringPtr(args.Trigger),
			Notitie:    optionalStringPtr(args.Notitie),
			Bron:       "telegram",
		})
		return e.jsonResponse(map[string]any{"ok": true, "scope": "habit incident", "habit": habitSummary(habit), "log": log}, err)

	case "habitNotitie":
		var args struct {
			ID      string `json:"id"`
			HabitID string `json:"habitId"`
			Naam    string `json:"naam"`
			Datum   string `json:"datum"`
			Notitie string `json:"notitie"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Notitie) == "" {
			return e.jsonResponse(nil, fmt.Errorf("notitie verplicht"))
		}
		habit, err := e.resolveHabit(ctx, firstNonEmpty(args.ID, args.HabitID), args.Naam)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		datum := firstNonEmpty(args.Datum, todayAmsterdamISO())
		existing, err := e.habitStore.GetLog(ctx, habit.ID, datum)
		if err != nil && err != pgx.ErrNoRows {
			return e.jsonResponse(nil, err)
		}
		logInput := existing
		if err == pgx.ErrNoRows {
			logInput = model.HabitLog{UserID: e.userID, HabitID: habit.ID, Datum: datum, Bron: "telegram"}
		}
		logInput.Notitie = optionalStringPtr(args.Notitie)
		logInput.Bron = "telegram"
		log, err := e.habitStore.UpsertLog(ctx, logInput)
		return e.jsonResponse(map[string]any{"ok": true, "scope": "habit lognotitie", "habit": habitSummary(habit), "log": log}, err)

	case "habitsOverzicht":
		habits, err := e.habitStore.List(ctx, e.userID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		todayHabits, err := e.habitStore.ListDueForDate(ctx, e.userID, todayAmsterdamISO())
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		return e.jsonResponse(map[string]any{
			"scope":       "habits overzicht",
			"count":       len(habits),
			"vandaagDue":  len(todayHabits),
			"items":       habits,
			"instruction": "items bevat alle actieve habits; vandaagDue gebruikt frequentie, pauze en roosterfilter.",
		}, nil)

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
		return e.jsonResponse(map[string]any{"scope": "habit badges", "count": len(badges), "items": badges}, err)

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
		todayHabits, err := e.habitStore.ListDueForDate(ctx, e.userID, todayAmsterdamISO())
		if err != nil {
			return e.jsonResponse(nil, err)
		}

		return e.jsonResponse(map[string]any{
			"scope":             "habit rapport",
			"dagen":             days,
			"stats":             stats,
			"habits":            habits,
			"badges":            badges,
			"heatmap":           heatmap,
			"vandaagDue":        len(todayHabits),
			"vandaagHabitNames": habitNames(todayHabits),
			"instruction":       "Gebruik stats.todayDue/todayCompleted voor vandaag. Heatmap-rate gebruikt due habits per datum.",
		}, nil)

	// ── LAVENTECARE ──────────────────────────────────────────────────
	case "laventecareCockpit":
		cockpit, err := e.laventeCareStore.GetCockpit(ctx, e.userID)
		return e.jsonResponse(map[string]any{
			"scope":       "laventecare cockpit",
			"cockpit":     cockpit,
			"instruction": "Gebruik summary als hoofdbron. Als summary.documentsSeeded false is of summary.documents 0 is, benoem dat de documentbasis nog leeg is.",
		}, err)

	case "laventecareKennisZoeken":
		var args struct {
			Query string `json:"query"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		docs, err := e.laventeCareStore.SearchDocuments(ctx, e.userID, args.Query, 5)
		return e.jsonResponse(map[string]any{
			"scope":       "laventecare kennisbank",
			"query":       strings.TrimSpace(args.Query),
			"count":       len(docs),
			"items":       docs,
			"instruction": "Gebruik alleen deze documenten als kennisbron. Bij count 0: zeg dat er niets gevonden is en adviseer de documentbasis te initialiseren of een concretere zoekterm te gebruiken.",
		}, err)

	case "laventecareLeadsOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		leads, err := e.laventeCareStore.ListLeads(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(map[string]any{"scope": "laventecare leads", "count": len(leads), "items": leads}, err)

	case "laventecareProjectenOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		projects, err := e.laventeCareStore.ListProjects(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(map[string]any{"scope": "laventecare projecten", "count": len(projects), "items": projects}, err)

	case "laventecareActiesOpvragen":
		var args struct {
			Limit int `json:"limit"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		actions, err := e.laventeCareStore.ListActions(ctx, e.userID, clampToolLimit(args.Limit, 10, 30))
		return e.jsonResponse(map[string]any{"scope": "laventecare acties", "count": len(actions), "items": actions}, err)

	case "laventecareDossierDocumentenOpvragen":
		var args struct {
			Limit     int    `json:"limit"`
			LeadID    string `json:"lead_id"`
			ProjectID string `json:"project_id"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.LeadID) != "" && strings.TrimSpace(args.ProjectID) != "" {
			return e.jsonResponse(nil, fmt.Errorf("gebruik lead_id of project_id, niet allebei"))
		}
		leadID, err := parseOptionalUUID(args.LeadID)
		if err != nil {
			return e.jsonResponse(nil, fmt.Errorf("ongeldige lead_id: %w", err))
		}
		projectID, err := parseOptionalUUID(args.ProjectID)
		if err != nil {
			return e.jsonResponse(nil, fmt.Errorf("ongeldige project_id: %w", err))
		}
		docs, err := e.laventeCareStore.ListDossierDocuments(ctx, e.userID, clampToolLimit(args.Limit, 8, 30), leadID, projectID)
		return e.jsonResponse(map[string]any{
			"scope":       "laventecare dossierdocumenten",
			"count":       len(docs),
			"items":       docs,
			"instruction": "Gebruik deze lijst als PDF dossierhistorie. Zeg bij count 0 dat er nog geen PDF in het dossier is vastgelegd.",
		}, err)

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
			args.Status = "actief"
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
			args.ActionType = "opvolgen"
		}
		if strings.TrimSpace(args.Priority) == "" {
			args.Priority = "normaal"
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

	case "laventecareBesluitMaken":
		var args struct {
			ProjectID string  `json:"project_id"`
			Titel     string  `json:"titel"`
			Besluit   string  `json:"besluit"`
			Reden     string  `json:"reden"`
			Impact    *string `json:"impact"`
			Status    string  `json:"status"`
			Datum     string  `json:"datum"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Titel) == "" || strings.TrimSpace(args.Besluit) == "" {
			return e.jsonResponse(nil, fmt.Errorf("titel en besluit verplicht"))
		}
		if strings.TrimSpace(args.Reden) == "" {
			args.Reden = "Niet gespecificeerd"
		}
		projectID, err := parseOptionalUUID(args.ProjectID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		decision, err := e.laventeCareStore.CreateDecision(ctx, e.userID, model.LCDecision{
			ProjectID: projectID,
			Titel:     args.Titel,
			Besluit:   args.Besluit,
			Reden:     args.Reden,
			Impact:    args.Impact,
			Status:    args.Status,
			Datum:     args.Datum,
		})
		return e.jsonResponse(map[string]any{"ok": true, "decision": decision}, err)

	case "laventecareChangeRequestMaken":
		var args struct {
			ProjectID      string  `json:"project_id"`
			Titel          string  `json:"titel"`
			Impact         string  `json:"impact"`
			PlanningImpact *string `json:"planning_impact"`
			BudgetImpact   *string `json:"budget_impact"`
			Status         string  `json:"status"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Titel) == "" || strings.TrimSpace(args.Impact) == "" {
			return e.jsonResponse(nil, fmt.Errorf("titel en impact verplicht"))
		}
		projectID, err := parseOptionalUUID(args.ProjectID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		change, err := e.laventeCareStore.CreateChangeRequest(ctx, e.userID, model.LCChangeRequest{
			ProjectID:      projectID,
			Titel:          args.Titel,
			Impact:         args.Impact,
			PlanningImpact: args.PlanningImpact,
			BudgetImpact:   args.BudgetImpact,
			Status:         args.Status,
		})
		return e.jsonResponse(map[string]any{"ok": true, "changeRequest": change}, err)

	case "laventecareSlaIncidentMaken":
		var args struct {
			ProjectID       string  `json:"project_id"`
			Titel           string  `json:"titel"`
			Prioriteit      string  `json:"prioriteit"`
			Status          string  `json:"status"`
			Kanaal          string  `json:"kanaal"`
			ReactieDeadline string  `json:"reactie_deadline"`
			Samenvatting    *string `json:"samenvatting"`
		}
		if err := e.parseArgs(argsJSON, &args); err != nil {
			return e.jsonResponse(nil, err)
		}
		if strings.TrimSpace(args.Titel) == "" {
			return e.jsonResponse(nil, fmt.Errorf("titel verplicht"))
		}
		projectID, err := parseOptionalUUID(args.ProjectID)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		deadline, err := parseOptionalNoteDeadline(args.ReactieDeadline)
		if err != nil {
			return e.jsonResponse(nil, err)
		}
		incident, err := e.laventeCareStore.CreateSlaIncident(ctx, e.userID, model.LCSlaIncident{
			ProjectID:       projectID,
			Titel:           args.Titel,
			Prioriteit:      args.Prioriteit,
			Status:          args.Status,
			Kanaal:          args.Kanaal,
			ReactieDeadline: deadline,
			Samenvatting:    args.Samenvatting,
		})
		return e.jsonResponse(map[string]any{"ok": true, "slaIncident": incident}, err)

	// ── SMART HOME ───────────────────────────────────────────────────
	case "lampBedien":
		return `{"status": "Geef de actie direct door via de chat, bijv: 'lampen uit' of 'scene ocean'. De bot pikt dit automatisch op voor het AI verzoek."}`

	default:
		return fmt.Sprintf(`{"error": "Tool '%s' niet geïmplementeerd in Go."}`, toolName)
	}
}
