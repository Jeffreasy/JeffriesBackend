package store

import (
	"context"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

type PrivacyStore struct{ db *DB }

func NewPrivacyStore(db *DB) *PrivacyStore { return &PrivacyStore{db: db} }

// Get returns the privacy settings for a user, creating defaults if none exist.
func (s *PrivacyStore) Get(ctx context.Context, userID string) (model.PrivacySettings, error) {
	var p model.PrivacySettings
	err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO privacy_settings (user_id) VALUES ($1)
		ON CONFLICT (user_id) DO UPDATE SET user_id = EXCLUDED.user_id
		RETURNING id, user_id, finance, habits, notes, email, account, updated_at
	`, userID).Scan(&p.ID, &p.UserID, &p.Finance, &p.Habits, &p.Notes, &p.Email, &p.Account, &p.UpdatedAt)
	return p, err
}

// Update sets the privacy settings for a user.
func (s *PrivacyStore) Update(ctx context.Context, userID string, p model.PrivacySettings) error {
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO privacy_settings (user_id, finance, habits, notes, email, account, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id) DO UPDATE SET
			finance = EXCLUDED.finance, habits = EXCLUDED.habits,
			notes = EXCLUDED.notes, email = EXCLUDED.email,
			account = EXCLUDED.account, updated_at = EXCLUDED.updated_at
	`, userID, p.Finance, p.Habits, p.Notes, p.Email, p.Account, time.Now())
	return err
}
