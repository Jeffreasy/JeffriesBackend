package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type HabitStore struct{ db *DB }

// habitUpdatableColumns is the allowlist of columns a client may PATCH. Computed
// fields (streak/xp/totals) and identity/timestamps are excluded, and any other
// key is ignored — without this, raw JSON keys were interpolated into the SQL SET
// clause (mass-assignment + SQL injection).
var habitUpdatableColumns = map[string]bool{
	"naam": true, "emoji": true, "type": true, "beschrijving": true,
	"frequentie": true, "aangepaste_dagen": true, "doel_aantal": true,
	"rooster_filter": true, "is_kwantitatief": true, "doel_waarde": true,
	"eenheid": true, "doel_tijd": true, "xp_per_voltooiing": true,
	"moeilijkheid": true, "financie_categorie": true, "kleur": true,
	"volgorde": true, "is_actief": true, "is_pauze": true, "gepauzeer_om": true,
}

func NewHabitStore(db *DB) *HabitStore { return &HabitStore{db: db} }

type HabitStats struct {
	TotaalXP       int `json:"totaalXP"`
	TotaalVoltooid int `json:"totaalVoltooid"`
	ActiveHabits   int `json:"activeHabits"`
	TodayDue       int `json:"todayDue"`
	TodayCompleted int `json:"todayCompleted"`
	PerfectDays    int `json:"perfectDays"`
	CurrentStreak  int `json:"currentStreak"`
	LongestStreak  int `json:"longestStreak"`
	Incidents30d   int `json:"incidents30d"`
}

type habitScheduleContext struct {
	HasWork  bool
	HasVroeg bool
	HasLaat  bool
}

const habitCols = `id, user_id, naam, emoji, type, beschrijving, frequentie,
	aangepaste_dagen, doel_aantal, rooster_filter, is_kwantitatief, doel_waarde,
	eenheid, doel_tijd, xp_per_voltooiing, moeilijkheid, financie_categorie,
	huidige_streak, langste_streak, totaal_voltooid, totaal_xp, kleur, volgorde,
	is_actief, is_pauze, gepauzeer_om, aangemaakt, gewijzigd`

func scanHabit(row pgx.Row) (model.Habit, error) {
	var h model.Habit
	err := row.Scan(
		&h.ID, &h.UserID, &h.Naam, &h.Emoji, &h.Type, &h.Beschrijving,
		&h.Frequentie, &h.AangepasteDagen, &h.DoelAantal, &h.RoosterFilter,
		&h.IsKwantitatief, &h.DoelWaarde, &h.Eenheid, &h.DoelTijd,
		&h.XPPerVoltooiing, &h.Moeilijkheid, &h.FinancieCategorie,
		&h.HuidigeStreak, &h.LangsteStreak, &h.TotaalVoltooid, &h.TotaalXP,
		&h.Kleur, &h.Volgorde, &h.IsActief, &h.IsPauze, &h.GepauzeerOm,
		&h.Aangemaakt, &h.Gewijzigd,
	)
	return h, err
}

func collectHabit(row pgx.CollectableRow) (model.Habit, error) {
	var h model.Habit
	err := row.Scan(
		&h.ID, &h.UserID, &h.Naam, &h.Emoji, &h.Type, &h.Beschrijving,
		&h.Frequentie, &h.AangepasteDagen, &h.DoelAantal, &h.RoosterFilter,
		&h.IsKwantitatief, &h.DoelWaarde, &h.Eenheid, &h.DoelTijd,
		&h.XPPerVoltooiing, &h.Moeilijkheid, &h.FinancieCategorie,
		&h.HuidigeStreak, &h.LangsteStreak, &h.TotaalVoltooid, &h.TotaalXP,
		&h.Kleur, &h.Volgorde, &h.IsActief, &h.IsPauze, &h.GepauzeerOm,
		&h.Aangemaakt, &h.Gewijzigd,
	)
	return h, err
}

// List returns all active habits for a user, ordered by volgorde.
func (s *HabitStore) List(ctx context.Context, userID string) ([]model.Habit, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT `+habitCols+` FROM habits
		WHERE user_id = $1 AND is_actief = true
		ORDER BY volgorde, aangemaakt
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, collectHabit)
}

// ListDueForDate returns active, unpaused habits that should appear on a date.
func (s *HabitStore) ListDueForDate(ctx context.Context, userID, datum string) ([]model.Habit, error) {
	habits, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	schedule, err := s.scheduleContextForDate(ctx, userID, datum)
	if err != nil {
		return nil, err
	}
	due := make([]model.Habit, 0, len(habits))
	for _, habit := range habits {
		if habitDueOnDate(habit, datum, schedule) {
			due = append(due, habit)
		}
	}
	return due, nil
}

// Get returns a single habit.
func (s *HabitStore) Get(ctx context.Context, id uuid.UUID) (model.Habit, error) {
	return scanHabit(s.db.Pool.QueryRow(ctx, `SELECT `+habitCols+` FROM habits WHERE id = $1`, id))
}

// Create inserts a new habit.
func (s *HabitStore) Create(ctx context.Context, userID string, h model.Habit) (model.Habit, error) {
	h.ID = uuid.New()
	h.UserID = userID
	now := time.Now()
	h.Aangemaakt = now
	h.Gewijzigd = now
	h.IsActief = true

	return scanHabit(s.db.Pool.QueryRow(ctx, `
		INSERT INTO habits (id, user_id, naam, emoji, type, beschrijving, frequentie,
			aangepaste_dagen, doel_aantal, rooster_filter, is_kwantitatief, doel_waarde,
			eenheid, doel_tijd, xp_per_voltooiing, moeilijkheid, financie_categorie,
			kleur, volgorde, is_actief, is_pauze, aangemaakt, gewijzigd)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)
		RETURNING `+habitCols,
		h.ID, h.UserID, h.Naam, h.Emoji, h.Type, h.Beschrijving, h.Frequentie,
		h.AangepasteDagen, h.DoelAantal, h.RoosterFilter, h.IsKwantitatief, h.DoelWaarde,
		h.Eenheid, h.DoelTijd, h.XPPerVoltooiing, h.Moeilijkheid, h.FinancieCategorie,
		h.Kleur, h.Volgorde, h.IsActief, h.IsPauze, h.Aangemaakt, h.Gewijzigd,
	))
}

// Update patches a habit with the given fields.
func (s *HabitStore) Update(ctx context.Context, id uuid.UUID, fields map[string]any) (model.Habit, error) {
	sets := []string{}
	args := []any{}
	i := 1
	for col, val := range fields {
		if !habitUpdatableColumns[col] {
			continue // ignore unknown/computed/injection keys
		}
		sets = append(sets, col+" = $"+strconv.Itoa(i))
		args = append(args, val)
		i++
	}
	if len(sets) == 0 {
		return model.Habit{}, fmt.Errorf("geen geldige velden om bij te werken")
	}
	sets = append(sets, "gewijzigd = $"+strconv.Itoa(i))
	args = append(args, time.Now())
	i++
	args = append(args, id)
	q := `UPDATE habits SET ` + strings.Join(sets, ", ") + ` WHERE id = $` + strconv.Itoa(i) + ` RETURNING ` + habitCols
	return scanHabit(s.db.Pool.QueryRow(ctx, q, args...))
}

// Archive soft-deletes a habit.
func (s *HabitStore) Archive(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE habits SET is_actief = false, gewijzigd = $1 WHERE id = $2`, time.Now(), id)
	return err
}

// Delete permanently removes a habit.
func (s *HabitStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM habits WHERE id = $1`, id)
	return err
}

// Reorder updates the volgorde for multiple habits.
func (s *HabitStore) Reorder(ctx context.Context, items []struct {
	ID       uuid.UUID `json:"id"`
	Volgorde int       `json:"volgorde"`
}) error {
	batch := &pgx.Batch{}
	for _, it := range items {
		batch.Queue(`UPDATE habits SET volgorde = $1, gewijzigd = $2 WHERE id = $3`, it.Volgorde, time.Now(), it.ID)
	}
	return s.db.Pool.SendBatch(ctx, batch).Close()
}

// TogglePause toggles the pause state of a habit.
func (s *HabitStore) TogglePause(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE habits SET
			is_pauze = NOT is_pauze,
			gepauzeer_om = CASE WHEN is_pauze THEN NULL ELSE now() END,
			gewijzigd = now()
		WHERE id = $1
	`, id)
	return err
}

func (s *HabitStore) scheduleContextForDate(ctx context.Context, userID, datum string) (habitScheduleContext, error) {
	contexts, err := s.scheduleContextsRange(ctx, userID, datum, datum)
	if err != nil {
		return habitScheduleContext{}, err
	}
	return contexts[datum], nil
}

func (s *HabitStore) scheduleContextsRange(ctx context.Context, userID, startDate, endDate string) (map[string]habitScheduleContext, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT start_datum::text, LOWER(COALESCE(shift_type, '')), LOWER(COALESCE(titel, ''))
		FROM schedule
		WHERE user_id = $1 AND start_datum >= $2 AND start_datum <= $3 AND UPPER(COALESCE(status, '')) <> 'VERWIJDERD'
	`, userID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	contexts := make(map[string]habitScheduleContext)
	for rows.Next() {
		var datum, shiftType, titel string
		if err := rows.Scan(&datum, &shiftType, &titel); err != nil {
			return nil, err
		}
		ctx := contexts[datum]
		ctx.HasWork = true
		label := shiftType + " " + titel
		if strings.Contains(label, "vroeg") {
			ctx.HasVroeg = true
		}
		if strings.Contains(label, "laat") {
			ctx.HasLaat = true
		}
		contexts[datum] = ctx
	}
	return contexts, rows.Err()
}

func habitDueOnDate(habit model.Habit, datum string, schedule habitScheduleContext) bool {
	if !habit.IsActief || habit.IsPauze {
		return false
	}
	parsed, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return false
	}
	if !habitFrequencyDueOnDate(habit, parsed) {
		return false
	}
	return habitMatchesRoosterFilter(habit.RoosterFilter, schedule)
}

func habitFrequencyDueOnDate(habit model.Habit, date time.Time) bool {
	weekday := int(date.Weekday())
	switch habit.Frequentie {
	case "", "dagelijks", "x_per_week", "x_per_maand":
		return true
	case "weekdagen":
		return weekday >= int(time.Monday) && weekday <= int(time.Friday)
	case "weekenddagen":
		return weekday == int(time.Saturday) || weekday == int(time.Sunday)
	case "aangepast":
		for _, day := range habit.AangepasteDagen {
			if int(day) == weekday {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func habitMatchesRoosterFilter(filter *string, schedule habitScheduleContext) bool {
	if filter == nil {
		return true
	}
	value := strings.TrimSpace(*filter)
	if value == "" || strings.EqualFold(value, "alle") {
		return true
	}
	switch strings.ToLower(value) {
	case "werkdagen":
		return schedule.HasWork
	case "vrijedagen":
		return !schedule.HasWork
	case "vroegedienst":
		return schedule.HasVroeg
	case "latedienst":
		return schedule.HasLaat
	default:
		return true
	}
}

// ─── Habit Logs ──────────────────────────────────────────────────────────────

// GetLog returns the log entry for a habit on a specific date.
func (s *HabitStore) GetLog(ctx context.Context, habitID uuid.UUID, datum string) (model.HabitLog, error) {
	var l model.HabitLog
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
		FROM habit_logs WHERE habit_id = $1 AND datum = $2
	`, habitID, datum).Scan(
		&l.ID, &l.UserID, &l.HabitID, &l.Datum, &l.Voltooid, &l.Waarde,
		&l.IsIncident, &l.TriggerCat, &l.Notitie, &l.Bron, &l.XPVerdiend, &l.Aangemaakt,
	)
	return l, err
}

// UpsertLog creates or updates a habit log (toggle pattern).
func (s *HabitStore) UpsertLog(ctx context.Context, l model.HabitLog) (model.HabitLog, error) {
	habit, err := s.Get(ctx, l.HabitID)
	if err != nil {
		return model.HabitLog{}, err
	}
	if strings.TrimSpace(l.UserID) == "" {
		l.UserID = habit.UserID
	}
	if strings.TrimSpace(l.Datum) == "" {
		l.Datum = todayAmsterdam()
	}
	if strings.TrimSpace(l.Bron) == "" {
		l.Bron = "web"
	}
	l = normalizeHabitLogForHabit(habit, l)
	l.ID = uuid.New()
	l.Aangemaakt = time.Now()
	var out model.HabitLog
	err = s.db.Pool.QueryRow(ctx, `
		INSERT INTO habit_logs (id, user_id, habit_id, datum, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (habit_id, datum) DO UPDATE SET
			voltooid = EXCLUDED.voltooid, waarde = EXCLUDED.waarde,
			is_incident = EXCLUDED.is_incident, trigger_cat = EXCLUDED.trigger_cat,
			notitie = EXCLUDED.notitie, bron = EXCLUDED.bron, xp_verdiend = EXCLUDED.xp_verdiend
		RETURNING id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
	`, l.ID, l.UserID, l.HabitID, l.Datum, l.Voltooid, l.Waarde,
		l.IsIncident, l.TriggerCat, l.Notitie, l.Bron, l.XPVerdiend, l.Aangemaakt,
	).Scan(
		&out.ID, &out.UserID, &out.HabitID, &out.Datum, &out.Voltooid, &out.Waarde,
		&out.IsIncident, &out.TriggerCat, &out.Notitie, &out.Bron, &out.XPVerdiend, &out.Aangemaakt,
	)
	if err != nil {
		return out, err
	}
	return out, s.RefreshHabitProgress(ctx, habit.ID)
}

func normalizeHabitLogForHabit(habit model.Habit, log model.HabitLog) model.HabitLog {
	if log.IsIncident {
		log.Voltooid = false
		log.XPVerdiend = 0
		return log
	}
	if habit.Type == "negatief" {
		log.Voltooid = false
		log.XPVerdiend = 0
		return log
	}
	if habit.IsKwantitatief && habit.DoelWaarde != nil {
		log.Voltooid = log.Waarde != nil && *log.Waarde >= *habit.DoelWaarde
	}
	if log.Voltooid && log.XPVerdiend <= 0 {
		log.XPVerdiend = habit.XPPerVoltooiing
	}
	if !log.Voltooid {
		log.XPVerdiend = 0
	}
	return log
}

// ListLogsForDate returns all logs for a user on a given date.
func (s *HabitStore) ListLogsForDate(ctx context.Context, userID, datum string) ([]model.HabitLog, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
		FROM habit_logs WHERE user_id = $1 AND datum = $2
	`, userID, datum)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.HabitLog, error) {
		var l model.HabitLog
		err := row.Scan(&l.ID, &l.UserID, &l.HabitID, &l.Datum, &l.Voltooid, &l.Waarde,
			&l.IsIncident, &l.TriggerCat, &l.Notitie, &l.Bron, &l.XPVerdiend, &l.Aangemaakt)
		return l, err
	})
}

// ListLogsRange returns all logs for a user in a date range.
func (s *HabitStore) ListLogsRange(ctx context.Context, userID, startDate, endDate string) ([]model.HabitLog, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
		FROM habit_logs WHERE user_id = $1 AND datum >= $2 AND datum <= $3
	`, userID, startDate, endDate)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.HabitLog, error) {
		var l model.HabitLog
		err := row.Scan(&l.ID, &l.UserID, &l.HabitID, &l.Datum, &l.Voltooid, &l.Waarde,
			&l.IsIncident, &l.TriggerCat, &l.Notitie, &l.Bron, &l.XPVerdiend, &l.Aangemaakt)
		return l, err
	})
}

type habitProgressLog struct {
	Datum      string
	Voltooid   bool
	IsIncident bool
	XPVerdiend int
}

// RefreshHabitProgress recalculates streak, totals and badges after a log change.
func (s *HabitStore) RefreshHabitProgress(ctx context.Context, habitID uuid.UUID) error {
	habit, err := s.Get(ctx, habitID)
	if err != nil {
		return err
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT datum::text, voltooid, is_incident, xp_verdiend
		FROM habit_logs
		WHERE habit_id = $1
		ORDER BY datum
	`, habitID)
	if err != nil {
		return err
	}
	defer rows.Close()

	logs := []habitProgressLog{}
	for rows.Next() {
		var log habitProgressLog
		if err := rows.Scan(&log.Datum, &log.Voltooid, &log.IsIncident, &log.XPVerdiend); err != nil {
			return err
		}
		logs = append(logs, log)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	today := todayAmsterdam()
	// Build the due-day predicate so the streak skips off days (weekend/aangepast/
	// rooster-filter habits) instead of breaking the run on them. Fetch the
	// schedule context across the active range for rooster-filtered habits.
	earliest := today
	for _, log := range logs {
		if log.Voltooid && log.Datum < earliest {
			earliest = log.Datum
		}
	}
	schedCtx, scErr := s.scheduleContextsRange(ctx, habit.UserID, earliest, today)
	if scErr != nil {
		schedCtx = map[string]habitScheduleContext{}
	}
	isDue := func(date string) bool {
		parsed, err := time.Parse("2006-01-02", date)
		if err != nil {
			return false
		}
		if !habitFrequencyDueOnDate(habit, parsed) {
			return false
		}
		return habitMatchesRoosterFilter(habit.RoosterFilter, schedCtx[date])
	}
	current, longest, total, xp := calculateHabitProgress(habit, logs, today, isDue)
	if longest < habit.LangsteStreak {
		longest = habit.LangsteStreak
	}
	if current > longest {
		longest = current
	}
	if _, err := s.db.Pool.Exec(ctx, `
		UPDATE habits
		SET huidige_streak = $1, langste_streak = $2, totaal_voltooid = $3,
		    totaal_xp = $4, gewijzigd = now()
		WHERE id = $5
	`, current, longest, total, xp, habitID); err != nil {
		return err
	}
	return s.awardHabitBadges(ctx, habit.UserID, habitID, current, longest, total)
}

func calculateHabitProgress(habit model.Habit, logs []habitProgressLog, today string, isDue func(string) bool) (current, longest, total, xp int) {
	if habit.Type == "negatief" {
		return calculateNegativeHabitProgress(habit, logs, today)
	}
	completed := make(map[string]bool)
	earliest := today
	for _, log := range logs {
		if log.Voltooid {
			completed[log.Datum] = true
			total++
			xp += log.XPVerdiend
			if log.Datum < earliest {
				earliest = log.Datum
			}
		}
	}
	if len(completed) == 0 {
		return 0, habit.LangsteStreak, total, xp
	}
	startD, err1 := time.Parse("2006-01-02", earliest)
	endD, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return 0, habit.LangsteStreak, total, xp
	}
	// A streak is consecutive DUE dates that are all completed. Non-due days (off
	// days for weekend/aangepast/rooster-filter habits) are skipped, not counted
	// as misses — so they no longer break the run.
	var dueSeq []string
	for d := startD; !d.After(endD); d = d.AddDate(0, 0, 1) {
		k := d.Format("2006-01-02")
		if isDue(k) {
			dueSeq = append(dueSeq, k)
		}
	}
	run := 0
	for _, k := range dueSeq {
		if completed[k] {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	// Current run: trailing consecutive completed due-dates. If the last due date
	// is today and not yet completed, today is still in progress — don't break on it.
	i := len(dueSeq) - 1
	if i >= 0 && dueSeq[i] == today && !completed[today] {
		i--
	}
	for ; i >= 0; i-- {
		if completed[dueSeq[i]] {
			current++
		} else {
			break
		}
	}
	return current, longest, total, xp
}

func calculateNegativeHabitProgress(habit model.Habit, logs []habitProgressLog, today string) (current, longest, total, xp int) {
	createdDate := habit.Aangemaakt.Format("2006-01-02")
	if createdDate == "0001-01-01" {
		createdDate = today
	}
	incidentDays := make(map[string]bool)
	for _, log := range logs {
		if log.IsIncident {
			incidentDays[log.Datum] = true
		}
	}
	start, err := time.Parse("2006-01-02", createdDate)
	if err != nil {
		start, _ = time.Parse("2006-01-02", today)
	}
	end, err := time.Parse("2006-01-02", today)
	if err != nil {
		return 0, habit.LangsteStreak, 0, 0
	}
	run := 0
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if incidentDays[key] {
			run = 0
			continue
		}
		run++
		total++
		if run > longest {
			longest = run
		}
	}
	current = run
	return current, longest, total, 0
}

func habitLogByID(logs []model.HabitLog) map[uuid.UUID]model.HabitLog {
	out := make(map[uuid.UUID]model.HabitLog, len(logs))
	for _, log := range logs {
		out[log.HabitID] = log
	}
	return out
}

func habitLogByDate(logs []model.HabitLog) map[string]map[uuid.UUID]model.HabitLog {
	out := make(map[string]map[uuid.UUID]model.HabitLog)
	for _, log := range logs {
		if _, ok := out[log.Datum]; !ok {
			out[log.Datum] = make(map[uuid.UUID]model.HabitLog)
		}
		out[log.Datum][log.HabitID] = log
	}
	return out
}

func completionForDate(habits []model.Habit, logs map[uuid.UUID]model.HabitLog, datum string, schedule habitScheduleContext) (completed, due int) {
	for _, habit := range habits {
		if !habitDueOnDate(habit, datum, schedule) {
			continue
		}
		due++
		log, hasLog := logs[habit.ID]
		if habit.Type == "negatief" {
			if !hasLog || !log.IsIncident {
				completed++
			}
			continue
		}
		if hasLog && log.Voltooid {
			completed++
		}
	}
	return completed, due
}

type habitBadgeDefinition struct {
	ID           string
	Naam         string
	Emoji        string
	Beschrijving string
	XPBonus      int
}

func (s *HabitStore) awardHabitBadges(ctx context.Context, userID string, habitID uuid.UUID, current, longest, total int) error {
	definitions := []habitBadgeDefinition{}
	if total >= 1 {
		definitions = append(definitions, habitBadgeDefinition{"first_habit", "De Eerste", "🚀", "Eerste habit voltooid!", 10})
	}
	for _, def := range []struct {
		threshold int
		badge     habitBadgeDefinition
	}{
		{3, habitBadgeDefinition{"streak_3", "Beginner", "🌱", "3 dagen streak bereikt", 25}},
		{7, habitBadgeDefinition{"streak_7", "Week Warrior", "⚡", "7 dagen streak bereikt", 50}},
		{14, habitBadgeDefinition{"streak_14", "Twee Weken", "🔥", "14 dagen streak bereikt", 100}},
		{30, habitBadgeDefinition{"streak_30", "Maand Master", "💎", "30 dagen streak bereikt", 250}},
		{60, habitBadgeDefinition{"streak_60", "Discipline King", "👑", "60 dagen streak bereikt", 500}},
		{100, habitBadgeDefinition{"streak_100", "Centurion", "🏆", "100 dagen streak bereikt", 1000}},
		{365, habitBadgeDefinition{"streak_365", "Jaarlegenda", "🌟", "365 dagen streak bereikt", 5000}},
	} {
		if longest >= def.threshold || current >= def.threshold {
			definitions = append(definitions, def.badge)
		}
	}
	for _, def := range []struct {
		threshold int
		badge     habitBadgeDefinition
	}{
		{10, habitBadgeDefinition{"total_10", "Eerste Stappen", "👣", "10 keer voltooid", 20}},
		{50, habitBadgeDefinition{"total_50", "Halfweg", "🎯", "50 keer voltooid", 75}},
		{100, habitBadgeDefinition{"total_100", "Honderdtal", "💯", "100 keer voltooid", 200}},
		{500, habitBadgeDefinition{"total_500", "Veteraan", "🎖️", "500 keer voltooid", 500}},
		{1000, habitBadgeDefinition{"total_1000", "Legende", "🏅", "1000 keer voltooid", 2000}},
	} {
		if total >= def.threshold {
			definitions = append(definitions, def.badge)
		}
	}
	for _, def := range definitions {
		_, err := s.db.Pool.Exec(ctx, `
			INSERT INTO habit_badges (id, user_id, badge_id, habit_id, naam, emoji, beschrijving, xp_bonus, behaald_op)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,now())
			ON CONFLICT (user_id, badge_id) DO NOTHING
		`, uuid.New(), userID, def.ID, habitID, def.Naam, def.Emoji, def.Beschrijving, def.XPBonus)
		if err != nil {
			return err
		}
	}
	return nil
}

func todayAmsterdam() string {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

// DayRow represents a single day's completion data for the heatmap.
type DayRow struct {
	Datum string  `json:"datum"`
	Count int     `json:"count"`
	Due   int     `json:"due"`
	Rate  float64 `json:"rate"`
}

// HeatmapData returns daily completion rates for the last N days.
func (s *HabitStore) HeatmapData(ctx context.Context, userID string, days int) ([]DayRow, error) {
	if days <= 0 {
		days = 365
	}
	if days > 365 {
		days = 365
	}
	end := todayAmsterdam()
	endDate, err := time.Parse("2006-01-02", end)
	if err != nil {
		return nil, err
	}
	start := endDate.AddDate(0, 0, -days+1).Format("2006-01-02")
	habits, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	logs, err := s.ListLogsRange(ctx, userID, start, end)
	if err != nil {
		return nil, err
	}
	schedules, err := s.scheduleContextsRange(ctx, userID, start, end)
	if err != nil {
		return nil, err
	}
	logMap := habitLogByDate(logs)
	out := make([]DayRow, 0, days)
	for i := 0; i < days; i++ {
		date := endDate.AddDate(0, 0, -days+1+i).Format("2006-01-02")
		count, due := completionForDate(habits, logMap[date], date, schedules[date])
		rate := 0.0
		if due > 0 {
			rate = float64(count) / float64(due)
		}
		out = append(out, DayRow{Datum: date, Count: count, Due: due, Rate: rate})
	}
	return out, nil
}

// Stats returns aggregate stats for a user.
func (s *HabitStore) Stats(ctx context.Context, userID string) (HabitStats, error) {
	var stats HabitStats
	err := s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(totaal_xp), 0)
		         + COALESCE((SELECT SUM(xp_bonus) FROM habit_badges WHERE user_id = $1), 0),
		       COALESCE(SUM(totaal_voltooid), 0),
		       COUNT(*),
		       COALESCE(MAX(huidige_streak), 0),
		       COALESCE(MAX(langste_streak), 0)
		FROM habits WHERE user_id = $1 AND is_actief = true
	`, userID).Scan(&stats.TotaalXP, &stats.TotaalVoltooid, &stats.ActiveHabits, &stats.CurrentStreak, &stats.LongestStreak)
	if err != nil {
		return stats, err
	}

	today := todayAmsterdam()
	habits, err := s.List(ctx, userID)
	if err != nil {
		return stats, err
	}
	logs, err := s.ListLogsForDate(ctx, userID, today)
	if err != nil {
		return stats, err
	}
	schedule, err := s.scheduleContextForDate(ctx, userID, today)
	if err != nil {
		return stats, err
	}
	stats.TodayCompleted, stats.TodayDue = completionForDate(habits, habitLogByID(logs), today, schedule)

	_ = s.db.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT datum)
		FROM habit_logs
		WHERE user_id = $1 AND is_incident = true AND datum >= CURRENT_DATE - INTERVAL '30 days'
	`, userID).Scan(&stats.Incidents30d)
	_ = s.db.Pool.QueryRow(ctx, `
		WITH active AS (
			SELECT COUNT(*) AS total FROM habits WHERE user_id = $1 AND is_actief = true AND is_pauze = false
		),
		daily AS (
			SELECT datum, COUNT(*) FILTER (WHERE voltooid = true) AS done
			FROM habit_logs
			WHERE user_id = $1
			GROUP BY datum
		)
		SELECT COUNT(*) FROM daily, active WHERE active.total > 0 AND daily.done >= active.total
	`, userID).Scan(&stats.PerfectDays)
	return stats, nil
}

// ─── Badges ──────────────────────────────────────────────────────────────────

// ListBadges returns all badges for a user.
func (s *HabitStore) ListBadges(ctx context.Context, userID string) ([]model.HabitBadge, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, badge_id, habit_id, naam, emoji, beschrijving, xp_bonus, behaald_op
		FROM habit_badges WHERE user_id = $1 ORDER BY behaald_op DESC
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.HabitBadge, error) {
		var b model.HabitBadge
		err := row.Scan(&b.ID, &b.UserID, &b.BadgeID, &b.HabitID, &b.Naam, &b.Emoji, &b.Beschrijving, &b.XPBonus, &b.BehaaldOp)
		return b, err
	})
}
