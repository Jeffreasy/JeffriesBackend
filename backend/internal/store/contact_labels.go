package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// ErrLabelNameTaken is returned when a create/rename collides with an existing
// label name for the same user (the unique index on (user_id, lower(name))).
var ErrLabelNameTaken = errors.New("label name already exists")

// labelColorPalette is the fixed set of palette keys a label colour may take.
// Storing a key (not raw CSS) keeps the UI and AI consistent and lets us restyle
// the whole app from one place. Unknown values normalize to "slate".
var labelColorPalette = map[string]bool{
	"slate": true, "amber": true, "sky": true, "emerald": true, "rose": true,
	"violet": true, "orange": true, "teal": true, "blue": true, "pink": true,
	"lime": true, "cyan": true, "red": true, "indigo": true, "fuchsia": true,
}

// NormalizeLabelColor lower-cases and validates a palette key, defaulting unknown
// or empty values to "slate".
func NormalizeLabelColor(color string) string {
	c := strings.ToLower(strings.TrimSpace(color))
	if c == "" || !labelColorPalette[c] {
		return "slate"
	}
	return c
}

// normalizeLabelName trims surrounding whitespace and collapses internal runs of
// whitespace to a single space so "  VIP  klant " and "VIP klant" are one label.
func normalizeLabelName(name string) string {
	return strings.Join(strings.Fields(name), " ")
}

func scanLabel(row pgx.Row) (model.ContactLabel, error) {
	var l model.ContactLabel
	err := row.Scan(&l.ID, &l.UserID, &l.Name, &l.Color, &l.CreatedAt, &l.UpdatedAt)
	return l, err
}

// ─── Catalog CRUD ────────────────────────────────────────────────────────────

// ListLabels returns the user's label catalog with per-label usage counts,
// alphabetically.
func (s *ContactStore) ListLabels(ctx context.Context, userID string) ([]model.ContactLabel, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT l.id, l.user_id, l.name, l.color, l.created_at, l.updated_at,
		       COUNT(a.contact_id) AS contact_count
		FROM contact_labels l
		LEFT JOIN contact_label_assignments a ON a.label_id = l.id
		WHERE l.user_id = $1
		GROUP BY l.id
		ORDER BY l.name ASC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactLabel{}
	for rows.Next() {
		var l model.ContactLabel
		if err := rows.Scan(&l.ID, &l.UserID, &l.Name, &l.Color, &l.CreatedAt, &l.UpdatedAt, &l.ContactCount); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// CreateLabel adds a label. If one with the same name (case-insensitively)
// already exists it returns that existing label unchanged (idempotent create),
// so callers never have to special-case "already there".
func (s *ContactStore) CreateLabel(ctx context.Context, userID, name, color string) (model.ContactLabel, error) {
	name = normalizeLabelName(name)
	if name == "" {
		return model.ContactLabel{}, fmt.Errorf("label name required")
	}
	color = NormalizeLabelColor(color)
	// Insert-or-get: ON CONFLICT DO NOTHING then read the row back either way.
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO contact_labels (user_id, name, color) VALUES ($1, $2, $3)
		ON CONFLICT (user_id, lower(name)) DO NOTHING`, userID, name, color)
	if err != nil {
		return model.ContactLabel{}, err
	}
	return scanLabel(s.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, name, color, created_at, updated_at
		FROM contact_labels WHERE user_id = $1 AND lower(name) = lower($2)`, userID, name))
}

// UpdateLabel renames and/or recolours a label. A name collision with another
// label returns ErrLabelNameTaken.
func (s *ContactStore) UpdateLabel(ctx context.Context, userID string, id uuid.UUID, name, color *string) (model.ContactLabel, error) {
	set := []string{}
	args := []any{}
	if name != nil {
		n := normalizeLabelName(*name)
		if n == "" {
			return model.ContactLabel{}, fmt.Errorf("label name required")
		}
		args = append(args, n)
		set = append(set, fmt.Sprintf("name = $%d", len(args)))
	}
	if color != nil {
		args = append(args, NormalizeLabelColor(*color))
		set = append(set, fmt.Sprintf("color = $%d", len(args)))
	}
	if len(set) == 0 {
		return scanLabel(s.db.Pool.QueryRow(ctx, `
			SELECT id, user_id, name, color, created_at, updated_at
			FROM contact_labels WHERE user_id = $1 AND id = $2`, userID, id))
	}
	set = append(set, "updated_at = now()")
	args = append(args, userID, id)
	q := fmt.Sprintf(`UPDATE contact_labels SET %s WHERE user_id = $%d AND id = $%d
		RETURNING id, user_id, name, color, created_at, updated_at`,
		strings.Join(set, ", "), len(args)-1, len(args))
	l, err := scanLabel(s.db.Pool.QueryRow(ctx, q, args...))
	if isUniqueViolation(err) {
		return model.ContactLabel{}, ErrLabelNameTaken
	}
	return l, err
}

// DeleteLabel removes a label; its assignments cascade away.
func (s *ContactStore) DeleteLabel(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM contact_labels WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// MergeLabels repoints every assignment of `fromID` onto `toID` and deletes
// `fromID` — so two labels that mean the same thing become one. Both must belong
// to the user.
func (s *ContactStore) MergeLabels(ctx context.Context, userID string, fromID, toID uuid.UUID) error {
	if fromID == toID {
		return nil
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var n int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM contact_labels WHERE user_id = $1 AND id = ANY($2)`,
		userID, []uuid.UUID{fromID, toID}).Scan(&n); err != nil {
		return err
	}
	if n < 2 {
		return pgx.ErrNoRows // one (or both) labels missing for this user
	}

	// Repoint assignments that the target doesn't already have.
	if _, err := tx.Exec(ctx, `
		UPDATE contact_label_assignments SET label_id = $3
		WHERE user_id = $1 AND label_id = $2
		  AND contact_id NOT IN (
		      SELECT contact_id FROM contact_label_assignments WHERE user_id = $1 AND label_id = $3)`,
		userID, fromID, toID); err != nil {
		return err
	}
	// Drop any leftover source assignments (contact already had the target label).
	if _, err := tx.Exec(ctx,
		`DELETE FROM contact_label_assignments WHERE user_id = $1 AND label_id = $2`, userID, fromID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM contact_labels WHERE user_id = $1 AND id = $2`, userID, fromID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ─── Assignment ──────────────────────────────────────────────────────────────

// assertOwnsLabel returns pgx.ErrNoRows if the label does not belong to the user.
func (s *ContactStore) assertOwnsLabel(ctx context.Context, userID string, labelID uuid.UUID) error {
	var exists bool
	if err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM contact_labels WHERE user_id = $1 AND id = $2)`, userID, labelID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

// AssignLabel tags a contact with a label (idempotent). Both must belong to the user.
func (s *ContactStore) AssignLabel(ctx context.Context, userID string, contactID, labelID uuid.UUID) error {
	if err := s.assertOwns(ctx, userID, contactID); err != nil {
		return err
	}
	if err := s.assertOwnsLabel(ctx, userID, labelID); err != nil {
		return err
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO contact_label_assignments (contact_id, label_id, user_id)
		VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, contactID, labelID, userID)
	return err
}

// RemoveLabel untags a contact. Returns pgx.ErrNoRows when no such assignment
// existed, so the handler reports 404 instead of a misleading success.
func (s *ContactStore) RemoveLabel(ctx context.Context, userID string, contactID, labelID uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM contact_label_assignments WHERE user_id = $1 AND contact_id = $2 AND label_id = $3`,
		userID, contactID, labelID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// AssignLabelByName tags a contact with a label identified by name, creating the
// label first if it doesn't exist yet — the convenient path for the AI. Ownership
// of the contact is checked BEFORE the label is created, so an AI misfire with a
// bad contact_id can't leave an orphan zero-count label polluting the catalog.
func (s *ContactStore) AssignLabelByName(ctx context.Context, userID string, contactID uuid.UUID, name, color string) (model.ContactLabel, error) {
	if err := s.assertOwns(ctx, userID, contactID); err != nil {
		return model.ContactLabel{}, err
	}
	label, err := s.CreateLabel(ctx, userID, name, color)
	if err != nil {
		return model.ContactLabel{}, err
	}
	if err := s.AssignLabel(ctx, userID, contactID, label.ID); err != nil {
		return model.ContactLabel{}, err
	}
	return label, nil
}

// SetContactLabels replaces a contact's entire label set with labelIDs (in a tx).
// Labels not owned by the user are silently skipped.
func (s *ContactStore) SetContactLabels(ctx context.Context, userID string, contactID uuid.UUID, labelIDs []uuid.UUID) error {
	if err := s.assertOwns(ctx, userID, contactID); err != nil {
		return err
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`DELETE FROM contact_label_assignments WHERE user_id = $1 AND contact_id = $2`, userID, contactID); err != nil {
		return err
	}
	for _, lid := range labelIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO contact_label_assignments (contact_id, label_id, user_id)
			SELECT $1, $2, $3 WHERE EXISTS (
				SELECT 1 FROM contact_labels WHERE user_id = $3 AND id = $2)
			ON CONFLICT DO NOTHING`, contactID, lid, userID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// BulkAssignLabel adds (or, when remove=true, removes) one label across many
// contacts in a single statement — the substrate for bulk tag/untag in the UI.
// Returns the number of contacts affected.
func (s *ContactStore) BulkAssignLabel(ctx context.Context, userID string, labelID uuid.UUID, contactIDs []uuid.UUID, remove bool) (int, error) {
	if len(contactIDs) == 0 {
		return 0, nil
	}
	if err := s.assertOwnsLabel(ctx, userID, labelID); err != nil {
		return 0, err
	}
	if remove {
		tag, err := s.db.Pool.Exec(ctx,
			`DELETE FROM contact_label_assignments WHERE user_id = $1 AND label_id = $2 AND contact_id = ANY($3)`,
			userID, labelID, contactIDs)
		if err != nil {
			return 0, err
		}
		return int(tag.RowsAffected()), nil
	}
	// Insert only for contacts that actually belong to the user.
	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO contact_label_assignments (contact_id, label_id, user_id)
		SELECT c.id, $2, $1 FROM contacts c
		WHERE c.user_id = $1 AND c.id = ANY($3)
		ON CONFLICT DO NOTHING`, userID, labelID, contactIDs)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

// ─── Hydration ───────────────────────────────────────────────────────────────

// labelsForContact returns a single contact's labels (alphabetically).
func (s *ContactStore) labelsForContact(ctx context.Context, userID string, contactID uuid.UUID) ([]model.ContactLabel, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT l.id, l.user_id, l.name, l.color, l.created_at, l.updated_at
		FROM contact_label_assignments a
		JOIN contact_labels l ON l.id = a.label_id
		WHERE a.user_id = $1 AND a.contact_id = $2
		ORDER BY l.name ASC`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactLabel{}
	for rows.Next() {
		l, err := scanLabel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// hydrateLabels bulk-loads labels for a slice of contacts in one query (avoids
// the N+1 of labelsForContact per row) and attaches them by contact id.
func (s *ContactStore) hydrateLabels(ctx context.Context, userID string, contacts []model.Contact) error {
	if len(contacts) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(contacts))
	for i, c := range contacts {
		ids[i] = c.ID
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT a.contact_id, l.id, l.user_id, l.name, l.color, l.created_at, l.updated_at
		FROM contact_label_assignments a
		JOIN contact_labels l ON l.id = a.label_id
		WHERE a.user_id = $1 AND a.contact_id = ANY($2)
		ORDER BY l.name ASC`, userID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byContact := map[uuid.UUID][]model.ContactLabel{}
	for rows.Next() {
		var cid uuid.UUID
		var l model.ContactLabel
		if err := rows.Scan(&cid, &l.ID, &l.UserID, &l.Name, &l.Color, &l.CreatedAt, &l.UpdatedAt); err != nil {
			return err
		}
		byContact[cid] = append(byContact[cid], l)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range contacts {
		contacts[i].Labels = byContact[contacts[i].ID]
	}
	return nil
}
