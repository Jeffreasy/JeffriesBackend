package store

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type HabitStore struct{ db *DB }

func NewHabitStore(db *DB) *HabitStore { return &HabitStore{db: db} }

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
		sets = append(sets, col+" = $"+strconv.Itoa(i))
		args = append(args, val)
		i++
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
	l.ID = uuid.New()
	l.Aangemaakt = time.Now()
	var out model.HabitLog
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO habit_logs (id, user_id, habit_id, datum, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (habit_id, datum) DO UPDATE SET
			voltooid = EXCLUDED.voltooid, waarde = EXCLUDED.waarde,
			is_incident = EXCLUDED.is_incident, trigger_cat = EXCLUDED.trigger_cat,
			notitie = EXCLUDED.notitie, xp_verdiend = EXCLUDED.xp_verdiend
		RETURNING id, user_id, habit_id, datum::text, voltooid, waarde, is_incident,
			trigger_cat, notitie, bron, xp_verdiend, aangemaakt
	`, l.ID, l.UserID, l.HabitID, l.Datum, l.Voltooid, l.Waarde,
		l.IsIncident, l.TriggerCat, l.Notitie, l.Bron, l.XPVerdiend, l.Aangemaakt,
	).Scan(
		&out.ID, &out.UserID, &out.HabitID, &out.Datum, &out.Voltooid, &out.Waarde,
		&out.IsIncident, &out.TriggerCat, &out.Notitie, &out.Bron, &out.XPVerdiend, &out.Aangemaakt,
	)
	return out, err
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

// DayRow represents a single day's completion data for the heatmap.
type DayRow struct {
	Datum string  `json:"datum"`
	Count int     `json:"count"`
	Rate  float64 `json:"rate"`
}

// HeatmapData returns daily completion rates for the last N days.
func (s *HabitStore) HeatmapData(ctx context.Context, userID string, days int) ([]DayRow, error) {
	rows, err := s.db.Pool.Query(ctx, `
		WITH dates AS (
			SELECT generate_series(
				CURRENT_DATE - ($2 || ' days')::interval,
				CURRENT_DATE,
				'1 day'
			)::date AS datum
		),
		daily AS (
			SELECT hl.datum, COUNT(*) AS done
			FROM habit_logs hl
			JOIN habits h ON h.id = hl.habit_id
			WHERE hl.user_id = $1 AND hl.voltooid = true AND h.is_actief = true
			GROUP BY hl.datum
		),
		total AS (
			SELECT COUNT(*) AS active_habits FROM habits WHERE user_id = $1 AND is_actief = true
		)
		SELECT d.datum::text, COALESCE(dl.done, 0) AS count,
			CASE WHEN t.active_habits > 0 THEN COALESCE(dl.done, 0)::float / t.active_habits ELSE 0 END AS rate
		FROM dates d
		LEFT JOIN daily dl ON dl.datum = d.datum
		CROSS JOIN total t
		ORDER BY d.datum
	`, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DayRow, error) {
		var d DayRow
		err := row.Scan(&d.Datum, &d.Count, &d.Rate)
		return d, err
	})
}

// Stats returns aggregate stats for a user.
func (s *HabitStore) Stats(ctx context.Context, userID string) (struct {
	TotaalXP       int `json:"totaalXP"`
	TotaalVoltooid int `json:"totaalVoltooid"`
	ActiveHabits   int `json:"activeHabits"`
}, error) {
	var stats struct {
		TotaalXP       int `json:"totaalXP"`
		TotaalVoltooid int `json:"totaalVoltooid"`
		ActiveHabits   int `json:"activeHabits"`
	}
	err := s.db.Pool.QueryRow(ctx, `
		SELECT COALESCE(SUM(totaal_xp), 0), COALESCE(SUM(totaal_voltooid), 0), COUNT(*)
		FROM habits WHERE user_id = $1 AND is_actief = true
	`, userID).Scan(&stats.TotaalXP, &stats.TotaalVoltooid, &stats.ActiveHabits)
	return stats, err
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
