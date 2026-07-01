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

// RequeueOrFail increments the attempt counter and either requeues the command
// to 'pending' for another try, or marks it 'failed' once maxAttempts is reached.
// Returns whether the command was requeued plus the target device id (nil for
// broadcast commands), so a terminal failure can flip that device offline. Use
// for transient send failures so a one-off LAN/bridge blip does not become a
// permanent failure.
func (s *DeviceCommandStore) RequeueOrFail(ctx context.Context, id uuid.UUID, maxAttempts int) (bool, *uuid.UUID, error) {
	if maxAttempts < 1 {
		maxAttempts = 3
	}
	var requeued bool
	var deviceID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`UPDATE device_commands
		    SET attempts = attempts + 1,
		        status = CASE WHEN attempts + 1 < $2 THEN 'pending' ELSE 'failed' END,
		        claimed_at = NULL,
		        completed_at = CASE WHEN attempts + 1 < $2 THEN NULL ELSE now() END,
		        updated_at = now()
		  WHERE id = $1
		    AND status IN ('pending', 'processing')
		  RETURNING (status = 'pending'), device_id`,
		id, maxAttempts,
	).Scan(&requeued, &deviceID)
	if err == pgx.ErrNoRows {
		return false, nil, nil
	}
	return requeued, deviceID, err
}

// TouchBridge bumps the bridge liveness heartbeat. Called on every authenticated
// /bridge/* request so bridge liveness is independent of WiZ UDP reachability.
func (s *DeviceCommandStore) TouchBridge(ctx context.Context) error {
	_, err := s.db.Pool.Exec(ctx,
		`INSERT INTO bridge_heartbeat (id, last_seen) VALUES (1, now())
		 ON CONFLICT (id) DO UPDATE SET last_seen = now()`)
	return err
}

// BridgeLastSeen returns the last bridge heartbeat (zero time if never seen).
func (s *DeviceCommandStore) BridgeLastSeen(ctx context.Context) (time.Time, error) {
	var t time.Time
	err := s.db.Pool.QueryRow(ctx, `SELECT last_seen FROM bridge_heartbeat WHERE id = 1`).Scan(&t)
	if err == pgx.ErrNoRows {
		return time.Time{}, nil
	}
	return t, err
}

// DeleteOldCompleted removes terminal (done/failed) commands older than cutoff
// so historical failures stop accumulating (and stop inflating alert counts).
func (s *DeviceCommandStore) DeleteOldCompleted(ctx context.Context, olderThan time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-olderThan)
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM device_commands
		  WHERE status IN ('done', 'failed')
		    AND COALESCE(completed_at, updated_at) < $1`,
		cutoff)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
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
