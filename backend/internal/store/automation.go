package store

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type AutomationStore struct{ db *DB }

func NewAutomationStore(db *DB) *AutomationStore { return &AutomationStore{db: db} }

const autoCols = `id, user_id, name, enabled, created_at, last_fired_at, group_name, trigger_config, action_config`

func collectAutomation(row pgx.CollectableRow) (model.AutomationRow, error) {
	var a model.AutomationRow
	err := row.Scan(&a.ID, &a.UserID, &a.Name, &a.Enabled, &a.CreatedAt,
		&a.LastFiredAt, &a.GroupName, &a.TriggerConfig, &a.ActionConfig)
	return a, err
}

// List returns all automations for a user.
func (s *AutomationStore) List(ctx context.Context, userID string) ([]model.AutomationRow, error) {
	rows, err := s.db.Pool.Query(ctx, `SELECT `+autoCols+` FROM automations WHERE user_id = $1 ORDER BY created_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return pgx.CollectRows(rows, collectAutomation)
}

// Create inserts a new automation.
func (s *AutomationStore) Create(ctx context.Context, a model.AutomationRow) (model.AutomationRow, error) {
	a.ID = uuid.New()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now()
	}
	var out model.AutomationRow
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO automations (id, user_id, name, enabled, created_at, group_name, trigger_config, action_config)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING `+autoCols,
		a.ID, a.UserID, a.Name, a.Enabled, a.CreatedAt, a.GroupName, a.TriggerConfig, a.ActionConfig,
	).Scan(&out.ID, &out.UserID, &out.Name, &out.Enabled, &out.CreatedAt,
		&out.LastFiredAt, &out.GroupName, &out.TriggerConfig, &out.ActionConfig)
	return out, err
}

// Toggle flips the enabled flag.
func (s *AutomationStore) Toggle(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `UPDATE automations SET enabled = NOT enabled WHERE id = $1`, id)
	return err
}

// Delete removes an automation.
func (s *AutomationStore) Delete(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM automations WHERE id = $1`, id)
	return err
}

// DeleteByGroup removes all automations for a user with a given group.
func (s *AutomationStore) DeleteByGroup(ctx context.Context, userID, group string) error {
	_, err := s.db.Pool.Exec(ctx, `DELETE FROM automations WHERE user_id = $1 AND group_name = $2`, userID, group)
	return err
}

// MarkFired sets last_fired_at to now.
func (s *AutomationStore) MarkFired(ctx context.Context, id uuid.UUID) error {
	_, err := s.db.Pool.Exec(ctx,
		`UPDATE automations SET last_fired_at = $2 WHERE id = $1`,
		id, time.Now().UTC())
	return err
}
