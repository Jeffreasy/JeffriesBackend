package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// RoomStore handles all room database operations.
type RoomStore struct {
	db *DB
}

// NewRoomStore creates a new RoomStore.
func NewRoomStore(db *DB) *RoomStore {
	return &RoomStore{db: db}
}

// GetAll returns all rooms ordered by floor and name.
func (s *RoomStore) GetAll(ctx context.Context, skip, limit int) ([]model.Room, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, icon, floor_number, created_at
		 FROM rooms
		 ORDER BY floor_number, name
		 OFFSET $1 LIMIT $2`,
		skip, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Room, error) {
		var r model.Room
		err := row.Scan(&r.ID, &r.Name, &r.Icon, &r.FloorNumber, &r.CreatedAt)
		return r, err
	})
}

// GetByID returns a single room or nil.
func (s *RoomStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Room, error) {
	var r model.Room
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, name, icon, floor_number, created_at
		 FROM rooms WHERE id = $1`,
		id,
	).Scan(&r.ID, &r.Name, &r.Icon, &r.FloorNumber, &r.CreatedAt)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// Create inserts a new room.
func (s *RoomStore) Create(ctx context.Context, input model.RoomCreate) (*model.Room, error) {
	id := uuid.New()
	now := time.Now().UTC()

	icon := input.Icon
	if icon == "" {
		icon = "room"
	}

	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO rooms (id, name, icon, floor_number, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		id, input.Name, icon, input.FloorNumber, now,
	)
	if err != nil {
		return nil, err
	}

	return &model.Room{
		ID:          id,
		Name:        input.Name,
		Icon:        icon,
		FloorNumber: input.FloorNumber,
		CreatedAt:   now,
	}, nil
}

// Update modifies a room's fields.
func (s *RoomStore) Update(ctx context.Context, id uuid.UUID, input model.RoomUpdate) (*model.Room, error) {
	room, err := s.GetByID(ctx, id)
	if err != nil || room == nil {
		return nil, err
	}

	if input.Name != nil {
		room.Name = *input.Name
	}
	if input.Icon != nil {
		room.Icon = *input.Icon
	}
	if input.FloorNumber != nil {
		room.FloorNumber = *input.FloorNumber
	}

	_, err = s.db.Pool.Exec(ctx,
		`UPDATE rooms SET name = $2, icon = $3, floor_number = $4 WHERE id = $1`,
		id, room.Name, room.Icon, room.FloorNumber,
	)
	if err != nil {
		return nil, err
	}
	return room, nil
}

// Delete removes a room by ID.
func (s *RoomStore) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM rooms WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
