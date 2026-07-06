package store

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// ─── Channels (extra emails/phones) ──────────────────────────────────────────

func scanChannel(row pgx.Row) (model.ContactChannel, error) {
	var c model.ContactChannel
	err := row.Scan(&c.ID, &c.UserID, &c.ContactID, &c.Kind, &c.Value, &c.Label, &c.IsPrimary, &c.CreatedAt)
	return c, err
}

// ListChannels returns a contact's extra channels (primary first, then newest).
func (s *ContactStore) ListChannels(ctx context.Context, userID string, contactID uuid.UUID) ([]model.ContactChannel, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, contact_id, kind, value, label, is_primary, created_at
		FROM contact_channels WHERE user_id = $1 AND contact_id = $2
		ORDER BY is_primary DESC, created_at ASC`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactChannel{}
	for rows.Next() {
		c, err := scanChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// normalizeChannelKind maps free input to email|phone|other.
func normalizeChannelKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "email", "e-mail", "mail":
		return "email"
	case "phone", "tel", "telefoon", "mobile", "mobiel":
		return "phone"
	default:
		return "other"
	}
}

// AddChannel adds an extra channel. When is_primary is set, other channels of the
// same kind for that contact are demoted (in a tx) so there's one primary per kind.
func (s *ContactStore) AddChannel(ctx context.Context, userID string, c model.ContactChannel) (model.ContactChannel, error) {
	if err := s.assertOwns(ctx, userID, c.ContactID); err != nil {
		return model.ContactChannel{}, err
	}
	kind := normalizeChannelKind(c.Kind)
	value := strings.TrimSpace(c.Value)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.ContactChannel{}, err
	}
	defer tx.Rollback(ctx)

	if c.IsPrimary {
		if _, err := tx.Exec(ctx, `
			UPDATE contact_channels SET is_primary = false
			WHERE user_id = $1 AND contact_id = $2 AND kind = $3`, userID, c.ContactID, kind); err != nil {
			return model.ContactChannel{}, err
		}
	}
	created, err := scanChannel(tx.QueryRow(ctx, `
		INSERT INTO contact_channels (user_id, contact_id, kind, value, label, is_primary)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		RETURNING id, user_id, contact_id, kind, value, label, is_primary, created_at`,
		userID, c.ContactID, kind, value, strings.TrimSpace(deref(c.Label)), c.IsPrimary))
	if err != nil {
		return model.ContactChannel{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ContactChannel{}, err
	}
	return created, nil
}

// DeleteChannel removes a channel.
func (s *ContactStore) DeleteChannel(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM contact_channels WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Interactions (touchpoint timeline) ──────────────────────────────────────

func scanInteraction(row pgx.Row) (model.ContactInteraction, error) {
	var i model.ContactInteraction
	err := row.Scan(&i.ID, &i.UserID, &i.ContactID, &i.Kind, &i.Summary, &i.OccurredAt, &i.CreatedAt)
	return i, err
}

// normalizeInteractionKind maps free input to a known touchpoint kind.
func normalizeInteractionKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "call", "bellen", "gebeld", "phone":
		return "call"
	case "meeting", "afspraak", "meet":
		return "meeting"
	case "message", "bericht", "appen", "whatsapp", "sms":
		return "message"
	case "email", "mail", "e-mail":
		return "email"
	case "note", "notitie", "":
		return "note"
	default:
		return "other"
	}
}

// ListInteractions returns a contact's touchpoints, newest first.
func (s *ContactStore) ListInteractions(ctx context.Context, userID string, contactID uuid.UUID, limit int) ([]model.ContactInteraction, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, contact_id, kind, summary, occurred_at, created_at
		FROM contact_interactions WHERE user_id = $1 AND contact_id = $2
		ORDER BY occurred_at DESC, created_at DESC LIMIT $3`, userID, contactID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactInteraction{}
	for rows.Next() {
		i, err := scanInteraction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// AddInteraction logs a touchpoint and advances contacts.last_contacted_at to the
// later of its current value and this interaction's occurred_at (in a tx).
func (s *ContactStore) AddInteraction(ctx context.Context, userID string, in model.ContactInteraction) (model.ContactInteraction, error) {
	if err := s.assertOwns(ctx, userID, in.ContactID); err != nil {
		return model.ContactInteraction{}, err
	}
	occurred := in.OccurredAt
	if occurred.IsZero() {
		occurred = time.Now()
	}
	kind := normalizeInteractionKind(in.Kind)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.ContactInteraction{}, err
	}
	defer tx.Rollback(ctx)

	created, err := scanInteraction(tx.QueryRow(ctx, `
		INSERT INTO contact_interactions (user_id, contact_id, kind, summary, occurred_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5)
		RETURNING id, user_id, contact_id, kind, summary, occurred_at, created_at`,
		userID, in.ContactID, kind, strings.TrimSpace(deref(in.Summary)), occurred))
	if err != nil {
		return model.ContactInteraction{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE contacts SET last_contacted_at = GREATEST(COALESCE(last_contacted_at, $3), $3), updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, in.ContactID, occurred); err != nil {
		return model.ContactInteraction{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.ContactInteraction{}, err
	}
	return created, nil
}

// DeleteInteraction removes a touchpoint and recomputes last_contacted_at from the
// remaining interactions (NULL if none remain).
func (s *ContactStore) DeleteInteraction(ctx context.Context, userID string, id uuid.UUID) error {
	var contactID uuid.UUID
	err := s.db.Pool.QueryRow(ctx,
		`DELETE FROM contact_interactions WHERE user_id = $1 AND id = $2 RETURNING contact_id`, userID, id).Scan(&contactID)
	if err != nil {
		return err // pgx.ErrNoRows when not found
	}
	_, err = s.db.Pool.Exec(ctx, `
		UPDATE contacts SET last_contacted_at = (
			SELECT MAX(occurred_at) FROM contact_interactions WHERE user_id = $1 AND contact_id = $2
		), updated_at = now()
		WHERE user_id = $1 AND id = $2`, userID, contactID)
	return err
}
