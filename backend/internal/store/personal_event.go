package store

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type PersonalEventStore struct{ db *DB }

func NewPersonalEventStore(db *DB) *PersonalEventStore { return &PersonalEventStore{db: db} }

const peColumns = `id, user_id, event_id, titel, start_datum::text, start_tijd,
	eind_datum::text, eind_tijd, heledag, locatie, beschrijving,
	conflict_met_dienst, status, kalender`

func (s *PersonalEventStore) List(ctx context.Context, userID string) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events WHERE user_id = $1 ORDER BY start_datum DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanPE)
}

func (s *PersonalEventStore) ListByDate(ctx context.Context, userID, date string) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events WHERE user_id = $1 AND start_datum = $2 ORDER BY start_tijd`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanPE)
}

func (s *PersonalEventStore) ListUpcoming(ctx context.Context, userID string, limit int) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events WHERE user_id = $1 AND status != 'VERWIJDERD' AND start_datum >= CURRENT_DATE ORDER BY start_datum LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, scanPE)
}

func (s *PersonalEventStore) Upsert(ctx context.Context, e model.PersonalEvent) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO personal_events (id,user_id,event_id,titel,start_datum,start_tijd,eind_datum,eind_tijd,heledag,locatie,beschrijving,conflict_met_dienst,status,kalender)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		 ON CONFLICT (user_id, event_id) DO UPDATE SET
		    titel=EXCLUDED.titel, start_datum=EXCLUDED.start_datum, start_tijd=EXCLUDED.start_tijd,
		    eind_datum=EXCLUDED.eind_datum, eind_tijd=EXCLUDED.eind_tijd, heledag=EXCLUDED.heledag,
		    locatie=EXCLUDED.locatie, beschrijving=EXCLUDED.beschrijving,
		    conflict_met_dienst=EXCLUDED.conflict_met_dienst, status=EXCLUDED.status`,
		e.ID, e.UserID, e.EventID, e.Titel, e.StartDatum, e.StartTijd,
		e.EindDatum, e.EindTijd, e.Heledag, e.Locatie, e.Beschrijving,
		e.ConflictMetDienst, e.Status, e.Kalender)
	return err
}

func (s *PersonalEventStore) UpdateStatus(ctx context.Context, userID, eventID, status string) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE personal_events SET status=$3 WHERE user_id=$1 AND event_id=$2`, userID, eventID, status)
	return err
}

func scanPE(row pgx.CollectableRow) (model.PersonalEvent, error) {
	var e model.PersonalEvent
	err := row.Scan(&e.ID, &e.UserID, &e.EventID, &e.Titel, &e.StartDatum, &e.StartTijd,
		&e.EindDatum, &e.EindTijd, &e.Heledag, &e.Locatie, &e.Beschrijving,
		&e.ConflictMetDienst, &e.Status, &e.Kalender)
	return e, err
}
