package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type ScheduleStore struct{ db *DB }

func NewScheduleStore(db *DB) *ScheduleStore { return &ScheduleStore{db: db} }

// List returns all diensten for a user ordered by start_datum.
func (s *ScheduleStore) List(ctx context.Context, userID string) ([]model.Schedule, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, event_id, titel, start_datum::text, start_tijd,
		        eind_datum::text, eind_tijd, werktijd, locatie, team, shift_type,
		        prioriteit, duur, weeknr, dag, status, beschrijving, heledag
		   FROM schedule
		  WHERE user_id = $1
		  ORDER BY start_datum, start_tijd`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanSchedule)
}

// ListByDate returns diensten for a specific date.
func (s *ScheduleStore) ListByDate(ctx context.Context, userID, date string) ([]model.Schedule, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, event_id, titel, start_datum::text, start_tijd,
		        eind_datum::text, eind_tijd, werktijd, locatie, team, shift_type,
		        prioriteit, duur, weeknr, dag, status, beschrijving, heledag
		   FROM schedule
		  WHERE user_id = $1 AND start_datum = $2
		  ORDER BY start_tijd`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanSchedule)
}

// ListUpcoming returns future diensten starting from today.
func (s *ScheduleStore) ListUpcoming(ctx context.Context, userID string, limit int) ([]model.Schedule, error) {
	today := time.Now().Format("2006-01-02")
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, event_id, titel, start_datum::text, start_tijd,
		        eind_datum::text, eind_tijd, werktijd, locatie, team, shift_type,
		        prioriteit, duur, weeknr, dag, status, beschrijving, heledag
		   FROM schedule
		  WHERE user_id = $1 AND start_datum >= $2
		  ORDER BY start_datum, start_tijd
		  LIMIT $3`, userID, today, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanSchedule)
}

// ListRange returns diensten between startIso and eindIso.
func (s *ScheduleStore) ListRange(ctx context.Context, userID, startIso, eindIso string) ([]model.Schedule, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, event_id, titel, start_datum::text, start_tijd,
		        eind_datum::text, eind_tijd, werktijd, locatie, team, shift_type,
		        prioriteit, duur, weeknr, dag, status, beschrijving, heledag
		   FROM schedule
		  WHERE user_id = $1 AND start_datum >= $2 AND start_datum <= $3
		  ORDER BY start_datum, start_tijd`, userID, startIso, eindIso)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanSchedule)
}

// BulkUpsert inserts or updates diensten using ON CONFLICT.
func (s *ScheduleStore) BulkUpsert(ctx context.Context, userID string, items []model.ScheduleImport) (int, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var count int
	for _, item := range items {
		tag, err := tx.Exec(ctx,
			`INSERT INTO schedule (id, user_id, event_id, titel, start_datum, start_tijd,
			    eind_datum, eind_tijd, werktijd, locatie, team, shift_type,
			    prioriteit, duur, weeknr, dag, status, beschrijving, heledag)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
			 ON CONFLICT (user_id, event_id) DO UPDATE SET
			    titel = EXCLUDED.titel, start_datum = EXCLUDED.start_datum,
			    start_tijd = EXCLUDED.start_tijd, eind_datum = EXCLUDED.eind_datum,
			    eind_tijd = EXCLUDED.eind_tijd, werktijd = EXCLUDED.werktijd,
			    locatie = EXCLUDED.locatie, team = EXCLUDED.team,
			    shift_type = EXCLUDED.shift_type, prioriteit = EXCLUDED.prioriteit,
			    duur = EXCLUDED.duur, weeknr = EXCLUDED.weeknr, dag = EXCLUDED.dag,
			    status = EXCLUDED.status, beschrijving = EXCLUDED.beschrijving,
			    heledag = EXCLUDED.heledag`,
			uuid.New(), userID, item.EventID, item.Titel,
			item.StartDatum, item.StartTijd, item.EindDatum, item.EindTijd,
			item.Werktijd, item.Locatie, item.Team, item.ShiftType,
			item.Prioriteit, item.Duur, item.Weeknr, item.Dag,
			item.Status, item.Beschrijving, item.Heledag,
		)
		if err != nil {
			return 0, err
		}
		count += int(tag.RowsAffected())
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return count, nil
}

// GetMeta returns the schedule import metadata for a user.
func (s *ScheduleStore) GetMeta(ctx context.Context, userID string) (*model.ScheduleMeta, error) {
	var m model.ScheduleMeta
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, user_id, imported_at, file_name, total_rows
		   FROM schedule_meta WHERE user_id = $1`, userID,
	).Scan(&m.ID, &m.UserID, &m.ImportedAt, &m.FileName, &m.TotalRows)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// UpsertMeta creates or updates schedule import metadata.
func (s *ScheduleStore) UpsertMeta(ctx context.Context, userID, fileName string, totalRows int) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO schedule_meta (id, user_id, imported_at, file_name, total_rows)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id) DO UPDATE SET
		    imported_at = EXCLUDED.imported_at,
		    file_name   = EXCLUDED.file_name,
		    total_rows  = EXCLUDED.total_rows`,
		uuid.New(), userID, time.Now().UTC(), fileName, totalRows,
	)
	return err
}

func scanSchedule(row pgx.CollectableRow) (model.Schedule, error) {
	var s model.Schedule
	err := row.Scan(
		&s.ID, &s.UserID, &s.EventID, &s.Titel, &s.StartDatum, &s.StartTijd,
		&s.EindDatum, &s.EindTijd, &s.Werktijd, &s.Locatie, &s.Team, &s.ShiftType,
		&s.Prioriteit, &s.Duur, &s.Weeknr, &s.Dag, &s.Status, &s.Beschrijving, &s.Heledag,
	)
	return s, err
}
