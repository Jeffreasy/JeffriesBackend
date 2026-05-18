package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// DeviceCommand represents a pending WiZ command.
type DeviceCommand struct {
	ID          uuid.UUID       `json:"id"`
	UserID      string          `json:"user_id"`
	DeviceID    *uuid.UUID      `json:"device_id,omitempty"`
	Command     json.RawMessage `json:"command"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
}

type DeviceCommandStore struct{ db *DB }

func NewDeviceCommandStore(db *DB) *DeviceCommandStore {
	return &DeviceCommandStore{db: db}
}

// ListPending returns all pending device commands.
func (s *DeviceCommandStore) ListPending(ctx context.Context) ([]DeviceCommand, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, user_id, device_id, command, status, created_at, completed_at
		 FROM device_commands WHERE status = 'pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeviceCommand, error) {
		var c DeviceCommand
		err := row.Scan(&c.ID, &c.UserID, &c.DeviceID, &c.Command, &c.Status, &c.CreatedAt, &c.CompletedAt)
		return c, err
	})
}

// MarkDone marks a command as completed.
func (s *DeviceCommandStore) MarkDone(ctx context.Context, id uuid.UUID, status string) error {
	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE device_commands SET status = $2, completed_at = $3 WHERE id = $1`,
		id, status, now)
	return err
}

// Create inserts a new device command.
func (s *DeviceCommandStore) Create(ctx context.Context, userID string, deviceID *uuid.UUID, command json.RawMessage) (*DeviceCommand, error) {
	var c DeviceCommand
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO device_commands (user_id, device_id, command)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, device_id, command, status, created_at, completed_at`,
		userID, deviceID, command,
	).Scan(&c.ID, &c.UserID, &c.DeviceID, &c.Command, &c.Status, &c.CreatedAt, &c.CompletedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
