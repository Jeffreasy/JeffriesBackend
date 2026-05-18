package store

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// SceneStore handles all scene + scene_action database operations.
type SceneStore struct {
	db *DB
}

// NewSceneStore creates a new SceneStore.
func NewSceneStore(db *DB) *SceneStore {
	return &SceneStore{db: db}
}

// GetAll returns all scenes with their actions.
func (s *SceneStore) GetAll(ctx context.Context, skip, limit int) ([]model.Scene, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, name, icon, color_hex, created_at
		 FROM scenes ORDER BY name OFFSET $1 LIMIT $2`,
		skip, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	scenes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.Scene, error) {
		var sc model.Scene
		err := row.Scan(&sc.ID, &sc.Name, &sc.Icon, &sc.ColorHex, &sc.CreatedAt)
		return sc, err
	})
	if err != nil {
		return nil, err
	}

	for i := range scenes {
		actions, err := s.getActions(ctx, scenes[i].ID)
		if err != nil {
			return nil, err
		}
		scenes[i].Actions = actions
	}
	return scenes, nil
}

// GetByID returns a single scene with actions or nil.
func (s *SceneStore) GetByID(ctx context.Context, id uuid.UUID) (*model.Scene, error) {
	var sc model.Scene
	err := s.db.Pool.QueryRow(ctx,
		`SELECT id, name, icon, color_hex, created_at FROM scenes WHERE id = $1`, id,
	).Scan(&sc.ID, &sc.Name, &sc.Icon, &sc.ColorHex, &sc.CreatedAt)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	actions, err := s.getActions(ctx, id)
	if err != nil {
		return nil, err
	}
	sc.Actions = actions
	return &sc, nil
}

// Create inserts a scene with its actions in a transaction.
func (s *SceneStore) Create(ctx context.Context, input model.SceneCreate) (*model.Scene, error) {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	sceneID := uuid.New()
	now := time.Now().UTC()

	icon := input.Icon
	if icon == "" {
		icon = "scene"
	}
	colorHex := input.ColorHex
	if colorHex == "" {
		colorHex = "#6366f1"
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO scenes (id, name, icon, color_hex, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		sceneID, input.Name, icon, colorHex, now,
	)
	if err != nil {
		return nil, err
	}

	for _, a := range input.Actions {
		stateJSON, err := json.Marshal(a.TargetState)
		if err != nil {
			return nil, err
		}
		transitionMs := a.TransitionMs
		if transitionMs == 0 {
			transitionMs = 1000
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO scene_actions (id, scene_id, device_id, target_state, execution_order, transition_ms)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.New(), sceneID, a.DeviceID, stateJSON, a.ExecutionOrder, transitionMs,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	return s.GetByID(ctx, sceneID)
}

// Delete removes a scene and its actions (cascade).
func (s *SceneStore) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM scenes WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// getActions returns all actions for a scene.
func (s *SceneStore) getActions(ctx context.Context, sceneID uuid.UUID) ([]model.SceneAction, error) {
	rows, err := s.db.Pool.Query(ctx,
		`SELECT id, scene_id, device_id, target_state, execution_order, transition_ms
		 FROM scene_actions WHERE scene_id = $1 ORDER BY execution_order`,
		sceneID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (model.SceneAction, error) {
		var a model.SceneAction
		var stateJSON []byte
		err := row.Scan(&a.ID, &a.SceneID, &a.DeviceID, &stateJSON, &a.ExecutionOrder, &a.TransitionMs)
		if err != nil {
			return a, err
		}
		if len(stateJSON) > 0 {
			_ = json.Unmarshal(stateJSON, &a.TargetState)
		}
		return a, nil
	})
}
