package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// BrainPreferences holds user AI behavior preferences.
type BrainPreferences struct {
	ID              string   `json:"id"`
	UserID          string   `json:"user_id"`
	DetailLevel     string   `json:"detail_level"`
	Tone            string   `json:"tone"`
	ProactiveLevel  string   `json:"proactive_level"`
	FocusAreas      []string `json:"focus_areas"`
	BriefingTime    *string  `json:"briefing_time,omitempty"`
	QuietHoursStart *string  `json:"quiet_hours_start,omitempty"`
	QuietHoursEnd   *string  `json:"quiet_hours_end,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PreferencesStore handles brain preferences persistence.
type PreferencesStore struct {
	pool *pgxpool.Pool
}

func NewPreferencesStore(pool *pgxpool.Pool) *PreferencesStore {
	return &PreferencesStore{pool: pool}
}

// Get returns preferences for a user, creating defaults if not found.
func (s *PreferencesStore) Get(ctx context.Context, userID string) (*BrainPreferences, error) {
	var p BrainPreferences
	err := s.pool.QueryRow(ctx,
		`SELECT id, user_id, detail_level, tone, proactive_level, focus_areas,
		        briefing_time, quiet_hours_start, quiet_hours_end, created_at, updated_at
		 FROM brain_preferences WHERE user_id = $1`,
		userID,
	).Scan(&p.ID, &p.UserID, &p.DetailLevel, &p.Tone, &p.ProactiveLevel, &p.FocusAreas,
		&p.BriefingTime, &p.QuietHoursStart, &p.QuietHoursEnd, &p.CreatedAt, &p.UpdatedAt)

	if err != nil {
		// Create default preferences
		return s.createDefaults(ctx, userID)
	}
	return &p, nil
}

// Update merges partial updates into existing preferences.
func (s *PreferencesStore) Update(ctx context.Context, userID string, updates map[string]any) (*BrainPreferences, error) {
	// Ensure record exists
	current, err := s.Get(ctx, userID)
	if err != nil {
		return nil, err
	}

	// Apply updates
	dl := current.DetailLevel
	if v, ok := updates["detail_level"].(string); ok {
		dl = v
	}
	t := current.Tone
	if v, ok := updates["tone"].(string); ok {
		t = v
	}
	pl := current.ProactiveLevel
	if v, ok := updates["proactive_level"].(string); ok {
		pl = v
	}
	fa := current.FocusAreas
	if v, ok := updates["focus_areas"].([]string); ok {
		fa = v
	}
	bt := current.BriefingTime
	if v, ok := updates["briefing_time"].(*string); ok {
		bt = v
	}
	qhs := current.QuietHoursStart
	if v, ok := updates["quiet_hours_start"].(*string); ok {
		qhs = v
	}
	qhe := current.QuietHoursEnd
	if v, ok := updates["quiet_hours_end"].(*string); ok {
		qhe = v
	}

	var p BrainPreferences
	err = s.pool.QueryRow(ctx,
		`UPDATE brain_preferences
		 SET detail_level = $2, tone = $3, proactive_level = $4, focus_areas = $5,
		     briefing_time = $6, quiet_hours_start = $7, quiet_hours_end = $8, updated_at = now()
		 WHERE user_id = $1
		 RETURNING id, user_id, detail_level, tone, proactive_level, focus_areas,
		           briefing_time, quiet_hours_start, quiet_hours_end, created_at, updated_at`,
		userID, dl, t, pl, fa, bt, qhs, qhe,
	).Scan(&p.ID, &p.UserID, &p.DetailLevel, &p.Tone, &p.ProactiveLevel, &p.FocusAreas,
		&p.BriefingTime, &p.QuietHoursStart, &p.QuietHoursEnd, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *PreferencesStore) createDefaults(ctx context.Context, userID string) (*BrainPreferences, error) {
	var p BrainPreferences
	err := s.pool.QueryRow(ctx,
		`INSERT INTO brain_preferences (user_id) VALUES ($1)
		 ON CONFLICT (user_id) DO UPDATE SET updated_at = now()
		 RETURNING id, user_id, detail_level, tone, proactive_level, focus_areas,
		           briefing_time, quiet_hours_start, quiet_hours_end, created_at, updated_at`,
		userID,
	).Scan(&p.ID, &p.UserID, &p.DetailLevel, &p.Tone, &p.ProactiveLevel, &p.FocusAreas,
		&p.BriefingTime, &p.QuietHoursStart, &p.QuietHoursEnd, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
