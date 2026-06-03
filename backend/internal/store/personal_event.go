package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type PersonalEventStore struct{ db *DB }

func NewPersonalEventStore(db *DB) *PersonalEventStore { return &PersonalEventStore{db: db} }

const (
	PersonalEventStatusUpcoming      = "Aankomend"
	PersonalEventStatusPast          = "Voorbij"
	PersonalEventStatusDeleted       = "VERWIJDERD"
	PersonalEventStatusPendingCreate = "PendingCreate"
	PersonalEventStatusPendingUpdate = "PendingUpdate"
	PersonalEventStatusPendingDelete = "PendingDelete"
)

const peColumns = `id, user_id, event_id, titel, start_datum::text, start_tijd,
	eind_datum::text, eind_tijd, heledag, locatie, beschrijving,
	conflict_met_dienst, status, kalender`

func (s *PersonalEventStore) List(ctx context.Context, userID string) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events
		  WHERE user_id = $1
		  ORDER BY start_datum, COALESCE(start_tijd, '00:00'), titel`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := pgx.CollectRows(rows, scanPE)
	if err != nil {
		return nil, err
	}
	normalizePersonalEventStatuses(events)
	return events, nil
}

func (s *PersonalEventStore) ListByDate(ctx context.Context, userID, date string) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events WHERE user_id = $1 AND start_datum = $2 ORDER BY start_tijd`, userID, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := pgx.CollectRows(rows, scanPE)
	if err != nil {
		return nil, err
	}
	normalizePersonalEventStatuses(events)
	return events, nil
}

func (s *PersonalEventStore) ListUpcoming(ctx context.Context, userID string, limit int) ([]model.PersonalEvent, error) {
	events, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}

	upcoming := make([]model.PersonalEvent, 0, len(events))
	now := personalEventNow()
	for _, event := range events {
		if event.Status == PersonalEventStatusDeleted || event.Status == PersonalEventStatusPendingDelete {
			continue
		}
		if personalEventIsPast(event, now) {
			continue
		}
		upcoming = append(upcoming, event)
		if len(upcoming) >= limit {
			break
		}
	}
	return upcoming, nil
}

func (s *PersonalEventStore) GetByUserEventID(ctx context.Context, userID, eventID string) (model.PersonalEvent, error) {
	var event model.PersonalEvent
	err := s.db.Pool.QueryRow(ctx,
		`SELECT `+peColumns+` FROM personal_events WHERE user_id = $1 AND event_id = $2`,
		userID, eventID,
	).Scan(&event.ID, &event.UserID, &event.EventID, &event.Titel, &event.StartDatum, &event.StartTijd,
		&event.EindDatum, &event.EindTijd, &event.Heledag, &event.Locatie, &event.Beschrijving,
		&event.ConflictMetDienst, &event.Status, &event.Kalender)
	if err != nil {
		return model.PersonalEvent{}, err
	}
	normalizePersonalEventStatus(&event, personalEventNow())
	return event, nil
}

func (s *PersonalEventStore) ListPendingCalendar(ctx context.Context, userID string, limit int) ([]model.PersonalEvent, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT `+peColumns+` FROM personal_events
		  WHERE user_id = $1
		    AND status IN ($3, $4, $5)
		  ORDER BY created_at, start_datum, COALESCE(start_tijd, '00:00')
		  LIMIT $2`,
		userID, limit,
		PersonalEventStatusPendingCreate,
		PersonalEventStatusPendingUpdate,
		PersonalEventStatusPendingDelete,
	)
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
		    conflict_met_dienst=EXCLUDED.conflict_met_dienst, status=EXCLUDED.status,
		    kalender=EXCLUDED.kalender`,
		e.ID, e.UserID, e.EventID, e.Titel, e.StartDatum, e.StartTijd,
		e.EindDatum, e.EindTijd, e.Heledag, e.Locatie, e.Beschrijving,
		e.ConflictMetDienst, e.Status, e.Kalender)
	return err
}

func (s *PersonalEventStore) UpsertSynced(ctx context.Context, e model.PersonalEvent) error {
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
		    conflict_met_dienst=EXCLUDED.conflict_met_dienst, status=EXCLUDED.status,
		    kalender=EXCLUDED.kalender
		  WHERE personal_events.status NOT IN ($15, $16, $17)`,
		e.ID, e.UserID, e.EventID, e.Titel, e.StartDatum, e.StartTijd,
		e.EindDatum, e.EindTijd, e.Heledag, e.Locatie, e.Beschrijving,
		e.ConflictMetDienst, e.Status, e.Kalender,
		PersonalEventStatusPendingCreate,
		PersonalEventStatusPendingUpdate,
		PersonalEventStatusPendingDelete)
	return err
}

func (s *PersonalEventStore) UpdateStatus(ctx context.Context, userID, eventID, status string) error {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE personal_events SET status=$3 WHERE user_id=$1 AND event_id=$2`, userID, eventID, status)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("personal event not found: %s", eventID)
	}
	return err
}

func (s *PersonalEventStore) ReplaceEventIDAndStatus(ctx context.Context, userID, oldEventID, newEventID, status string) error {
	tag, err := s.db.Pool.Exec(ctx,
		`UPDATE personal_events
		    SET event_id=$3, status=$4
		  WHERE user_id=$1 AND event_id=$2`,
		userID, oldEventID, newEventID, status)
	if isUniqueViolation(err) {
		return s.mergePendingIntoExistingEvent(ctx, userID, oldEventID, newEventID, status)
	}
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("personal event not found: %s", oldEventID)
	}
	return err
}

func (s *PersonalEventStore) mergePendingIntoExistingEvent(ctx context.Context, userID, pendingEventID, existingEventID, status string) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx,
		`UPDATE personal_events
		    SET status=$3
		  WHERE user_id=$1 AND event_id=$2`,
		userID, existingEventID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("existing personal event not found after id conflict: %s", existingEventID)
	}

	_, err = tx.Exec(ctx,
		`UPDATE notes
		    SET linked_event_id=$3, gewijzigd=now()
		  WHERE user_id=$1 AND linked_event_id=$2`,
		userID, pendingEventID, existingEventID)
	if err != nil {
		return err
	}

	tag, err = tx.Exec(ctx,
		`DELETE FROM personal_events
		  WHERE user_id=$1 AND event_id=$2`,
		userID, pendingEventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("pending personal event not found after id conflict: %s", pendingEventID)
	}

	return tx.Commit(ctx)
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func scanPE(row pgx.CollectableRow) (model.PersonalEvent, error) {
	var e model.PersonalEvent
	err := row.Scan(&e.ID, &e.UserID, &e.EventID, &e.Titel, &e.StartDatum, &e.StartTijd,
		&e.EindDatum, &e.EindTijd, &e.Heledag, &e.Locatie, &e.Beschrijving,
		&e.ConflictMetDienst, &e.Status, &e.Kalender)
	return e, err
}

type personalEventClock struct {
	date string
	time string
}

func personalEventNow() personalEventClock {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	return personalEventClock{
		date: now.Format("2006-01-02"),
		time: now.Format("15:04"),
	}
}

func normalizePersonalEventStatuses(events []model.PersonalEvent) {
	now := personalEventNow()
	for i := range events {
		normalizePersonalEventStatus(&events[i], now)
	}
}

func normalizePersonalEventStatus(event *model.PersonalEvent, now personalEventClock) {
	switch event.Status {
	case PersonalEventStatusPendingCreate, PersonalEventStatusPendingUpdate,
		PersonalEventStatusPendingDelete, PersonalEventStatusDeleted:
		return
	}

	if personalEventIsPast(*event, now) {
		event.Status = PersonalEventStatusPast
	} else if event.Status == PersonalEventStatusPast {
		event.Status = PersonalEventStatusUpcoming
	}
}

func personalEventIsPast(event model.PersonalEvent, now personalEventClock) bool {
	endDate := event.EindDatum
	if endDate == "" {
		endDate = event.StartDatum
	}

	if event.Heledag {
		return endDate < now.date
	}

	endTime := "23:59"
	if event.EindTijd != nil && *event.EindTijd != "" {
		endTime = *event.EindTijd
	}
	return endDate < now.date || (endDate == now.date && endTime <= now.time)
}
