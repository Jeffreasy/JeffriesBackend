package store

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// pgQuerier is satisfied by both *pgxpool.Pool and pgx.Tx, so the habit-log
// mutation + progress-refresh flow can run inside one transaction (M9) while
// read paths keep using the pool directly.
type pgQuerier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

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
	today := todayAmsterdam()
	due := make([]model.Habit, 0, len(habits))
	for _, habit := range habits {
		if habitDueOnDate(habit, datum, today, schedule) {
			due = append(due, habit)
		}
	}
	return due, nil
}

// Get returns a single habit.
func (s *HabitStore) Get(ctx context.Context, id uuid.UUID) (model.Habit, error) {
	return getHabitQ(ctx, s.db.Pool, id, false)
}

// getHabitQ fetches a habit through the given querier. With forUpdate it locks
// the habits row (SELECT ... FOR UPDATE) so concurrent toggle/incident/undo
// requests on the same habit serialize instead of racing the progress refresh.
func getHabitQ(ctx context.Context, q pgQuerier, id uuid.UUID, forUpdate bool) (model.Habit, error) {
	query := `SELECT ` + habitCols + ` FROM habits WHERE id = $1`
	if forUpdate {
		query += ` FOR UPDATE`
	}
	return scanHabit(q.QueryRow(ctx, query, id))
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

// Archive soft-deletes a habit. Returns pgx.ErrNoRows when the habit no longer
// exists so the handler can answer 404 instead of a silent success.
func (s *HabitStore) Archive(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `UPDATE habits SET is_actief = false, gewijzigd = $1 WHERE id = $2`, time.Now(), id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return err
}

// Delete permanently removes a habit. Returns pgx.ErrNoRows when nothing matched.
func (s *HabitStore) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM habits WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
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

// TogglePause toggles the pause state of a habit. Returns pgx.ErrNoRows when
// the habit no longer exists.
func (s *HabitStore) TogglePause(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `
		UPDATE habits SET
			is_pauze = NOT is_pauze,
			gepauzeer_om = CASE WHEN is_pauze THEN NULL ELSE now() END,
			gewijzigd = now()
		WHERE id = $1
	`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
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
	return scheduleContextsRangeQ(ctx, s.db.Pool, userID, startDate, endDate)
}

func scheduleContextsRangeQ(ctx context.Context, q pgQuerier, userID, startDate, endDate string) (map[string]habitScheduleContext, error) {
	rows, err := q.Query(ctx, `
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

// habitDueOnDate reports whether a habit is "due" on datum. The today argument
// is the Amsterdam-local reference day.
//
// Pause/archive semantics apply ONLY to today and the future: a currently paused
// or archived habit must NOT retroactively erase its historical due-days, or the
// heatmap and PerfectDays would drop completions that genuinely happened before
// the pause (R3-item7a). For historical dates due-ness is decided purely by
// creation-date + frequency/schedule; the current pause window is subtracted
// separately in the streak isDue predicate (see refreshHabitProgressQ).
func habitDueOnDate(habit model.Habit, datum, today string, schedule habitScheduleContext) bool {
	// Today/future only: a paused or archived habit is not due going forward.
	if datum >= today && (!habit.IsActief || habit.IsPauze) {
		return false
	}
	// A habit only exists from its creation date onward: without this, creating
	// (or re-activating) a habit rewrote a year of heatmap history retroactively,
	// with every pre-creation day suddenly counting as a missed due day.
	if !habit.Aangemaakt.IsZero() && datum < habit.Aangemaakt.In(amsterdamLocationStore()).Format("2006-01-02") {
		return false
	}
	parsed, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return false
	}
	if !habitFrequencyDueOnDate(habit, parsed) {
		return false
	}
	if !habitMatchesRoosterFilter(habit.RoosterFilter, schedule) {
		return false
	}
	// A period-satisfied x_per_week / x_per_maand habit is NOT due on a day with
	// no log: once the weekly/monthly target is met, remaining days in the period
	// must not count as due/incomplete (N5 parity with the frontend). Callers that
	// know the period is already satisfied pass it via completionForDate; here we
	// only handle the base frequency/schedule gate.
	return true
}

var habitAmsterdamLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return time.UTC
	}
	return loc
}()

func amsterdamLocationStore() *time.Location { return habitAmsterdamLoc }

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

// normalizeHabitLogDefaults fills in the log defaults shared by the toggle and
// incident paths.
func normalizeHabitLogDefaults(habit model.Habit, l model.HabitLog) model.HabitLog {
	if strings.TrimSpace(l.UserID) == "" {
		l.UserID = habit.UserID
	}
	if strings.TrimSpace(l.Datum) == "" {
		l.Datum = todayAmsterdam()
	}
	if strings.TrimSpace(l.Bron) == "" {
		l.Bron = "web"
	}
	l.ID = uuid.New()
	l.Aangemaakt = time.Now()
	return l
}

// UpsertLog creates or updates a habit log (toggle pattern). The habit row is
// locked and the progress refresh runs in the same transaction, so a concurrent
// double-tap can no longer interleave a half-refreshed streak (M9).
func (s *HabitStore) UpsertLog(ctx context.Context, l model.HabitLog) (model.HabitLog, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.HabitLog{}, err
	}
	defer tx.Rollback(ctx)

	habit, err := getHabitQ(ctx, tx, l.HabitID, true)
	if err != nil {
		return model.HabitLog{}, err
	}
	l = normalizeHabitLogDefaults(habit, l)
	l = normalizeHabitLogForHabit(habit, l)
	var out model.HabitLog
	err = tx.QueryRow(ctx, `
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
	if err := refreshHabitProgressQ(ctx, tx, habit); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

// UpsertIncident logs an incident on a day WITHOUT touching an existing
// completion: only the incident fields (is_incident/trigger_cat/notitie/bron)
// are written on conflict, so voltooid/waarde/xp_verdiend of an earlier
// check-off survive (R2 — incident + undo used to destroy the completion).
func (s *HabitStore) UpsertIncident(ctx context.Context, l model.HabitLog) (model.HabitLog, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.HabitLog{}, err
	}
	defer tx.Rollback(ctx)

	habit, err := getHabitQ(ctx, tx, l.HabitID, true)
	if err != nil {
		return model.HabitLog{}, err
	}
	l = normalizeHabitLogDefaults(habit, l)
	l.IsIncident = true
	l.Voltooid = false
	l.Waarde = nil
	l.XPVerdiend = 0

	var out model.HabitLog
	err = tx.QueryRow(ctx, `
		INSERT INTO habit_logs (id, user_id, habit_id, datum, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt)
		VALUES ($1,$2,$3,$4,$5,$6,true,$7,$8,$9,0,$10)
		ON CONFLICT (habit_id, datum) DO UPDATE SET
			is_incident = true,
			trigger_cat = EXCLUDED.trigger_cat,
			notitie     = COALESCE(EXCLUDED.notitie, habit_logs.notitie),
			bron        = EXCLUDED.bron
		RETURNING id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
	`, l.ID, l.UserID, l.HabitID, l.Datum, l.Voltooid, l.Waarde,
		l.TriggerCat, l.Notitie, l.Bron, l.Aangemaakt,
	).Scan(
		&out.ID, &out.UserID, &out.HabitID, &out.Datum, &out.Voltooid, &out.Waarde,
		&out.IsIncident, &out.TriggerCat, &out.Notitie, &out.Bron, &out.XPVerdiend, &out.Aangemaakt,
	)
	if err != nil {
		return out, err
	}
	if err := refreshHabitProgressQ(ctx, tx, habit); err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
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

// DeleteIncidentLog undoes an incident on a date. The row is only DELETEd when
// it holds no completion (voltooid=false and no waarde); otherwise only the
// incident fields are cleared so an earlier check-off on that day survives the
// undo (R2). Returns pgx.ErrNoRows when no incident was logged on that date.
// The notitie column is shared between completions and incidents; since the
// incident path is the one that writes it in this flow, the undo clears it —
// documented trade-off, provenance is not tracked per field.
func (s *HabitStore) DeleteIncidentLog(ctx context.Context, habitID uuid.UUID, datum string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the habit row so a concurrent toggle/incident on the same habit
	// serializes with this undo (M9).
	if _, err := getHabitQ(ctx, tx, habitID, true); err != nil {
		return err
	}

	tag, err := tx.Exec(ctx, `
		DELETE FROM habit_logs
		WHERE habit_id = $1 AND datum = $2 AND is_incident = true
		  AND voltooid = false AND COALESCE(waarde, 0) = 0
	`, habitID, datum)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// The row carries a completion: clear only the incident fields.
		tag, err = tx.Exec(ctx, `
			UPDATE habit_logs
			SET is_incident = false, trigger_cat = NULL, notitie = NULL
			WHERE habit_id = $1 AND datum = $2 AND is_incident = true
		`, habitID, datum)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return pgx.ErrNoRows
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Refresh AFTER the commit: a failing recalculation must never resurrect the
	// incident (the undo already happened). The caller treats this as success;
	// the next log mutation recomputes progress anyway (M9).
	if err := s.RefreshHabitProgress(ctx, habitID); err != nil {
		slog.Warn("habit progress refresh failed after incident undo (undo committed)",
			"habitId", habitID, "datum", datum, "error", err)
	}
	return nil
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
	return refreshHabitProgressQ(ctx, s.db.Pool, habit)
}

// refreshHabitProgressQ is the querier-based core of RefreshHabitProgress so it
// can run inside the same transaction as the log mutation (M9).
func refreshHabitProgressQ(ctx context.Context, q pgQuerier, habit model.Habit) error {
	habitID := habit.ID
	rows, err := q.Query(ctx, `
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
	rows.Close()

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
	schedCtx, scErr := scheduleContextsRangeQ(ctx, q, habit.UserID, earliest, today)
	if scErr != nil {
		schedCtx = map[string]habitScheduleContext{}
	}
	// Current (open) pause window: from gepauzeer_om (Amsterdam date) up to today,
	// while the habit is paused. Days inside it are treated as not-due so the pause
	// neither breaks nor counts against the streak (R3-item7b). Only the currently
	// open window is persisted (gepauzeer_om is cleared on resume); closed/historic
	// pause windows are not recoverable.
	// TODO(item7b): persist closed pause windows (a habit_pause_windows table with
	// start/end, or paused-days in habit_logs) so previously-paused stretches are
	// also subtracted from streaks after resume. Requires a schema migration.
	pauseStart := ""
	if habit.IsPauze && habit.GepauzeerOm != nil {
		pauseStart = habit.GepauzeerOm.In(amsterdamLocationStore()).Format("2006-01-02")
	}
	isDue := func(date string) bool {
		if pauseStart != "" && date >= pauseStart && date <= today {
			return false
		}
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
	if _, err := q.Exec(ctx, `
		UPDATE habits
		SET huidige_streak = $1, langste_streak = $2, totaal_voltooid = $3,
		    totaal_xp = $4, gewijzigd = now()
		WHERE id = $5
	`, current, longest, total, xp, habitID); err != nil {
		return err
	}
	return awardHabitBadgesQ(ctx, q, habit.UserID, habitID, current, longest, total)
}

func calculateHabitProgress(habit model.Habit, logs []habitProgressLog, today string, isDue func(string) bool) (current, longest, total, xp int) {
	if habit.Type == "negatief" {
		return calculateNegativeHabitProgress(habit, logs, today)
	}
	// x_per_week / x_per_maand habits streak over PERIODS, not days: a week/month
	// counts when it reaches doel_aantal completions, and the current (partial)
	// period never breaks the run. Day-based streaks made weekly habits
	// practically unachievable (every skipped day reset the streak).
	switch habit.Frequentie {
	case "x_per_week":
		return calculatePeriodHabitProgress(habit, logs, today, true)
	case "x_per_maand":
		return calculatePeriodHabitProgress(habit, logs, today, false)
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

// habitPeriodGoal returns doel_aantal, defaulting to 1 when unset/invalid.
func habitPeriodGoal(habit model.Habit) int {
	if habit.DoelAantal != nil && *habit.DoelAantal > 0 {
		return *habit.DoelAantal
	}
	return 1
}

// habitPeriodKey buckets a parsed date into an ISO-week ("2026-W27") or month
// ("2026-07") key. Dates are Amsterdam calendar dates (date-only strings), so
// parsing without a zone is safe.
func habitPeriodKey(d time.Time, weekly bool) string {
	if weekly {
		year, week := d.ISOWeek()
		return fmt.Sprintf("%04d-W%02d", year, week)
	}
	return d.Format("2006-01")
}

// habitPeriodStart normalizes a date to the start of its period (ISO Monday or
// the 1st of the month).
func habitPeriodStart(d time.Time, weekly bool) time.Time {
	if weekly {
		offset := (int(d.Weekday()) + 6) % 7 // Monday=0 ... Sunday=6
		return d.AddDate(0, 0, -offset)
	}
	return time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, d.Location())
}

func habitNextPeriod(d time.Time, weekly bool) time.Time {
	if weekly {
		return d.AddDate(0, 0, 7)
	}
	return d.AddDate(0, 1, 0)
}

// calculatePeriodHabitProgress computes streaks for x_per_week/x_per_maand
// habits: consecutive periods (ISO weeks or months, Amsterdam calendar) with at
// least doel_aantal completions. The current partial period doesn't break the
// streak when it hasn't reached the goal yet.
func calculatePeriodHabitProgress(habit model.Habit, logs []habitProgressLog, today string, weekly bool) (current, longest, total, xp int) {
	goal := habitPeriodGoal(habit)
	counts := map[string]int{}
	earliest := today
	for _, log := range logs {
		if !log.Voltooid {
			continue
		}
		total++
		xp += log.XPVerdiend
		if d, err := time.Parse("2006-01-02", log.Datum); err == nil {
			counts[habitPeriodKey(d, weekly)]++
		}
		if log.Datum < earliest {
			earliest = log.Datum
		}
	}
	if total == 0 {
		return 0, habit.LangsteStreak, 0, 0
	}
	startD, err1 := time.Parse("2006-01-02", earliest)
	endD, err2 := time.Parse("2006-01-02", today)
	if err1 != nil || err2 != nil {
		return 0, habit.LangsteStreak, total, xp
	}
	var keys []string
	for d := habitPeriodStart(startD, weekly); !d.After(endD); d = habitNextPeriod(d, weekly) {
		keys = append(keys, habitPeriodKey(d, weekly))
	}
	run := 0
	for _, k := range keys {
		if counts[k] >= goal {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	currentKey := habitPeriodKey(endD, weekly)
	i := len(keys) - 1
	if i >= 0 && keys[i] == currentKey && counts[currentKey] < goal {
		i-- // the running period is still in progress — don't break on it
	}
	for ; i >= 0; i-- {
		if counts[keys[i]] >= goal {
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

// completionForDate computes completed/due counts for a single date.
//
// periodSatisfied(habitID) reports whether an x_per_week/x_per_maand habit has
// already met its period target as of this date (counting logs strictly BEFORE
// this date). When it has, and there is no completing log on this date, the
// habit is NOT due today — matching the frontend N5 rule so a period-satisfied
// weekly habit isn't shown as incomplete on its remaining days. Pass a nil
// predicate to disable period-awareness (treats every due frequency-day as due).
func completionForDate(habits []model.Habit, logs map[uuid.UUID]model.HabitLog, datum, today string, schedule habitScheduleContext, periodSatisfied func(uuid.UUID) bool) (completed, due int) {
	for _, habit := range habits {
		if !habitDueOnDate(habit, datum, today, schedule) {
			continue
		}
		log, hasLog := logs[habit.ID]
		// N5: a period-satisfied weekly/monthly habit is not due on a day without
		// its own completing log.
		if periodSatisfied != nil && (habit.Frequentie == "x_per_week" || habit.Frequentie == "x_per_maand") {
			completedToday := hasLog && log.Voltooid
			if !completedToday && periodSatisfied(habit.ID) {
				continue
			}
		}
		due++
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

func awardHabitBadgesQ(ctx context.Context, q pgQuerier, userID string, habitID uuid.UUID, current, longest, total int) error {
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
		_, err := q.Exec(ctx, `
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

// PeriodBoundsForDate returns the ISO-week (weekly=true) or month bounds
// containing the given date, as YYYY-MM-DD strings — used to count "X/Y deze
// week/maand" for x_per_week/x_per_maand habits in the for-date payload.
func PeriodBoundsForDate(datum string, weekly bool) (start, end string, err error) {
	d, err := time.Parse("2006-01-02", datum)
	if err != nil {
		return "", "", err
	}
	s := habitPeriodStart(d, weekly)
	var e time.Time
	if weekly {
		e = s.AddDate(0, 0, 6)
	} else {
		e = s.AddDate(0, 1, -1)
	}
	return s.Format("2006-01-02"), e.Format("2006-01-02"), nil
}

// currentPeriodLogs fetches logs spanning both the ISO-week and month containing
// datum, so a period index can decide N5 exclusion for TodayDue. Returns nil when
// no habit uses a periodic frequency (nothing to compute).
func (s *HabitStore) currentPeriodLogs(ctx context.Context, userID, datum string, habits []model.Habit) ([]model.HabitLog, error) {
	needsPeriod := false
	for _, h := range habits {
		if h.Frequentie == "x_per_week" || h.Frequentie == "x_per_maand" {
			needsPeriod = true
			break
		}
	}
	if !needsPeriod {
		return nil, nil
	}
	weekStart, weekEnd, err := PeriodBoundsForDate(datum, true)
	if err != nil {
		return nil, err
	}
	monthStart, monthEnd, err := PeriodBoundsForDate(datum, false)
	if err != nil {
		return nil, err
	}
	start, end := weekStart, weekEnd
	if monthStart < start {
		start = monthStart
	}
	if monthEnd > end {
		end = monthEnd
	}
	return s.ListLogsRange(ctx, userID, start, end)
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
	today := todayAmsterdam()
	// Index completed period-counts so the heatmap applies the same N5 rule as the
	// frontend: a weekly/monthly habit already at target earlier in its period is
	// not counted as due on its remaining empty days.
	periodIdx := newHabitPeriodIndex(habits, logs)
	out := make([]DayRow, 0, days)
	for i := 0; i < days; i++ {
		date := endDate.AddDate(0, 0, -days+1+i).Format("2006-01-02")
		count, due := completionForDate(habits, logMap[date], date, today, schedules[date], periodIdx.satisfiedBefore(date))
		rate := 0.0
		if due > 0 {
			rate = float64(count) / float64(due)
		}
		out = append(out, DayRow{Datum: date, Count: count, Due: due, Rate: rate})
	}
	return out, nil
}

// habitPeriodIndex answers, for a given date, whether an x_per_week/x_per_maand
// habit had already met its period target using only completing logs dated
// strictly BEFORE that date (same period). This mirrors the frontend N5 exclusion.
type habitPeriodIndex struct {
	weekly  map[uuid.UUID]bool // habit is x_per_week
	goal    map[uuid.UUID]int
	// completions[habitID][periodKey] = sorted-ascending list of completion dates
	completions map[uuid.UUID]map[string][]string
}

func newHabitPeriodIndex(habits []model.Habit, logs []model.HabitLog) *habitPeriodIndex {
	idx := &habitPeriodIndex{
		weekly:      map[uuid.UUID]bool{},
		goal:        map[uuid.UUID]int{},
		completions: map[uuid.UUID]map[string][]string{},
	}
	for _, h := range habits {
		if h.Frequentie == "x_per_week" || h.Frequentie == "x_per_maand" {
			idx.weekly[h.ID] = h.Frequentie == "x_per_week"
			idx.goal[h.ID] = habitPeriodGoal(h)
			idx.completions[h.ID] = map[string][]string{}
		}
	}
	for _, l := range logs {
		if !l.Voltooid {
			continue
		}
		if _, ok := idx.completions[l.HabitID]; !ok {
			continue
		}
		d, err := time.Parse("2006-01-02", l.Datum)
		if err != nil {
			continue
		}
		key := habitPeriodKey(d, idx.weekly[l.HabitID])
		idx.completions[l.HabitID][key] = append(idx.completions[l.HabitID][key], l.Datum)
	}
	return idx
}

// satisfiedBefore returns a predicate: for habitID, was the period target met by
// completions strictly before `date`?
func (idx *habitPeriodIndex) satisfiedBefore(date string) func(uuid.UUID) bool {
	return func(habitID uuid.UUID) bool {
		byPeriod, ok := idx.completions[habitID]
		if !ok {
			return false
		}
		d, err := time.Parse("2006-01-02", date)
		if err != nil {
			return false
		}
		key := habitPeriodKey(d, idx.weekly[habitID])
		count := 0
		for _, cd := range byPeriod[key] {
			if cd < date {
				count++
			}
		}
		return count >= idx.goal[habitID]
	}
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
	// N5: build a period index over the current week+month so TodayDue applies
	// the same period-satisfied exclusion the heatmap/frontend do. Fetch the
	// widest bound (month usually) covering today's period.
	periodLogs, perr := s.currentPeriodLogs(ctx, userID, today, habits)
	if perr != nil {
		periodLogs = nil
	}
	periodIdx := newHabitPeriodIndex(habits, periodLogs)
	stats.TodayCompleted, stats.TodayDue = completionForDate(habits, habitLogByID(logs), today, today, schedule, periodIdx.satisfiedBefore(today))

	_ = s.db.Pool.QueryRow(ctx, `
		SELECT COUNT(DISTINCT datum)
		FROM habit_logs
		WHERE user_id = $1 AND is_incident = true AND datum >= CURRENT_DATE - INTERVAL '30 days'
	`, userID).Scan(&stats.Incidents30d)

	// PerfectDays is computed from the SAME per-day due/completed logic the heatmap
	// and frontend day% use (creation-date + frequency/schedule + pause-only-today
	// + N5 period exclusion), so the three no longer disagree. The old SQL
	// heuristic (done >= active-habit-count) over-counted low-due days and mis-
	// counted weekly habits. Bounded to the heatmap's 365-day window.
	if perfect, perr := s.perfectDaysCount(ctx, userID, 365); perr == nil {
		stats.PerfectDays = perfect
	}
	return stats, nil
}

// perfectDaysCount counts days in the trailing window where every due habit was
// completed (Rate == 1, at least one habit due), using HeatmapData's accurate
// per-day computation so Stats.PerfectDays agrees with the heatmap.
func (s *HabitStore) perfectDaysCount(ctx context.Context, userID string, days int) (int, error) {
	rows, err := s.HeatmapData(ctx, userID, days)
	if err != nil {
		return 0, err
	}
	perfect := 0
	for _, r := range rows {
		if r.Due > 0 && r.Count >= r.Due {
			perfect++
		}
	}
	return perfect, nil
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
