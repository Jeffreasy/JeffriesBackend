package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	DeviceCommandStatusPending    = "pending"
	DeviceCommandStatusProcessing = "processing"
	DeviceCommandStatusDone       = "done"
	DeviceCommandStatusFailed     = "failed"

	defaultDeviceCommandClaimLimit = 25
	staleDeviceCommandAfter        = 2 * time.Minute
)

// DeviceCommand represents a pending WiZ command.
type DeviceCommand struct {
	ID          uuid.UUID       `json:"id"`
	UserID      string          `json:"user_id"`
	DeviceID    *uuid.UUID      `json:"device_id,omitempty"`
	Command     json.RawMessage `json:"command"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"created_at"`
	ClaimedAt   *time.Time      `json:"claimed_at,omitempty"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`
	UpdatedAt   time.Time       `json:"updated_at"`
}

type DeviceCommandStore struct{ db *DB }

func NewDeviceCommandStore(db *DB) *DeviceCommandStore {
	return &DeviceCommandStore{db: db}
}

// ClaimPending atomically claims pending commands for this worker.
func (s *DeviceCommandStore) ClaimPending(ctx context.Context, limit int) ([]DeviceCommand, error) {
	if limit <= 0 {
		limit = defaultDeviceCommandClaimLimit
	}

	staleBefore := time.Now().UTC().Add(-staleDeviceCommandAfter)
	if _, err := s.db.Pool.Exec(ctx,
		`UPDATE device_commands
		    SET status = 'pending', claimed_at = NULL, updated_at = now()
		  WHERE status = 'processing'
		    AND claimed_at IS NOT NULL
		    AND claimed_at < $1`,
		staleBefore,
	); err != nil {
		return nil, err
	}

	rows, err := s.db.Pool.Query(ctx,
		`WITH picked AS (
		     SELECT id
		       FROM device_commands
		      WHERE status = 'pending'
		      ORDER BY created_at
		      FOR UPDATE SKIP LOCKED
		      LIMIT $1
		 )
		 UPDATE device_commands dc
		    SET status = 'processing',
		        claimed_at = now(),
		        updated_at = now()
		   FROM picked
		  WHERE dc.id = picked.id
		  RETURNING dc.id, dc.user_id, dc.device_id, dc.command, dc.status,
		            dc.created_at, dc.claimed_at, dc.completed_at, dc.updated_at`,
		limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (DeviceCommand, error) {
		var c DeviceCommand
		err := row.Scan(&c.ID, &c.UserID, &c.DeviceID, &c.Command, &c.Status, &c.CreatedAt, &c.ClaimedAt, &c.CompletedAt, &c.UpdatedAt)
		return c, err
	})
}

// MarkDone marks a claimed command as completed.
func (s *DeviceCommandStore) MarkDone(ctx context.Context, id uuid.UUID, status string) error {
	if status != DeviceCommandStatusDone && status != DeviceCommandStatusFailed {
		return fmt.Errorf("invalid device command completion status: %s", status)
	}

	now := time.Now().UTC()
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE device_commands
		    SET status = $2,
		        completed_at = $3,
		        updated_at = $3
		  WHERE id = $1
		    AND status IN ('pending', 'processing')`,
		id, status, now)
	return err
}

// Create inserts a new device command.
func (s *DeviceCommandStore) Create(ctx context.Context, userID string, deviceID *uuid.UUID, command json.RawMessage) (*DeviceCommand, error) {
	var c DeviceCommand
	err := s.db.Pool.QueryRow(ctx,
		`INSERT INTO device_commands (user_id, device_id, command)
		 VALUES ($1, $2, $3)
		 RETURNING id, user_id, device_id, command, status, created_at, claimed_at, completed_at, updated_at`,
		userID, deviceID, command,
	).Scan(&c.ID, &c.UserID, &c.DeviceID, &c.Command, &c.Status, &c.CreatedAt, &c.ClaimedAt, &c.CompletedAt, &c.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}
