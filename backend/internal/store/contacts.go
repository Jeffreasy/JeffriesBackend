package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// amsterdamLoc pins reminder/birthday math to the Europe/Amsterdam calendar so a
// date lands on the right day regardless of the server timezone.
var amsterdamLoc = func() *time.Location {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		return time.UTC
	}
	return loc
}()

// ContactStore is the data layer for the unified Contacts/Relationships module.
type ContactStore struct{ db *DB }

func NewContactStore(db *DB) *ContactStore { return &ContactStore{db: db} }

// ensureContactsSchema creates the contacts tables idempotently. Called from
// EnsureRuntimeSchema after the LaventeCare tables exist (organization_id is a
// soft reference to lc_companies(id); no FK in phase 0 to keep this module
// decoupled and independently deployable).
func ensureContactsSchema(ctx context.Context, db *DB) error {
	_, err := db.Pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS contacts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id            TEXT NOT NULL,
    display_name       TEXT NOT NULL,
    relationship_types TEXT[] NOT NULL DEFAULT '{}',
    notes              TEXT,
    email              TEXT,
    phone              TEXT,
    address            TEXT,
    organization_id    UUID,
    business_role      TEXT,
    last_contacted_at  TIMESTAMPTZ,
    archived           BOOLEAN NOT NULL DEFAULT false,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_contacts_user ON contacts (user_id);
CREATE INDEX IF NOT EXISTS idx_contacts_user_org ON contacts (user_id, organization_id);
CREATE INDEX IF NOT EXISTS idx_contacts_reltypes ON contacts USING GIN (relationship_types);
-- Phase 3: provenance for contacts mirrored from LaventeCare (lc_contacts).
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS lc_contact_id UUID;
CREATE UNIQUE INDEX IF NOT EXISTS idx_contacts_lc_contact ON contacts (lc_contact_id);

CREATE TABLE IF NOT EXISTS contact_important_dates (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT 'other',
    label      TEXT,
    month      INTEGER NOT NULL,
    day        INTEGER NOT NULL,
    year       INTEGER,
    recurring  BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_contact_dates_contact ON contact_important_dates (contact_id);
CREATE INDEX IF NOT EXISTS idx_contact_dates_user_md ON contact_important_dates (user_id, month, day);

CREATE TABLE IF NOT EXISTS contact_facts (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT NOT NULL,
    contact_id  UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    fact        TEXT NOT NULL,
    source      TEXT NOT NULL DEFAULT 'manual',
    occurred_at TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_contact_facts_contact ON contact_facts (contact_id);

-- Enterprise labelling: a per-user catalog of colour-coded tags (the rich layer
-- above relationship_types) + a join to contacts. First-class so labels can be
-- renamed/merged/recoloured in one place.
CREATE TABLE IF NOT EXISTS contact_labels (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    color      TEXT NOT NULL DEFAULT 'slate',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contact_labels_user_name ON contact_labels (user_id, lower(name));

CREATE TABLE IF NOT EXISTS contact_label_assignments (
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    label_id   UUID NOT NULL REFERENCES contact_labels(id) ON DELETE CASCADE,
    user_id    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (contact_id, label_id)
);
CREATE INDEX IF NOT EXISTS idx_contact_label_assign_label ON contact_label_assignments (label_id);
CREATE INDEX IF NOT EXISTS idx_contact_label_assign_user ON contact_label_assignments (user_id);

-- Additional contact channels (extra emails/phones beyond the primary on the row).
CREATE TABLE IF NOT EXISTS contact_channels (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    TEXT NOT NULL,
    contact_id UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL DEFAULT 'email',
    value      TEXT NOT NULL,
    label      TEXT,
    is_primary BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_contact_channels_contact ON contact_channels (contact_id);

-- Interaction timeline (logged touchpoints). Advances contacts.last_contacted_at.
CREATE TABLE IF NOT EXISTS contact_interactions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     TEXT NOT NULL,
    contact_id  UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    kind        TEXT NOT NULL DEFAULT 'note',
    summary     TEXT,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_contact_interactions_contact ON contact_interactions (contact_id, occurred_at DESC);

-- One person = one contact, affiliated with (possibly) multiple organizations.
-- A LaventeCare contact who works with two customers becomes ONE contact with two
-- org links here, deduped by identity_key (see SyncLaventeCareContactMirror).
ALTER TABLE contacts ADD COLUMN IF NOT EXISTS identity_key TEXT;

CREATE TABLE IF NOT EXISTS contact_organizations (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         TEXT NOT NULL,
    contact_id      UUID NOT NULL REFERENCES contacts(id) ON DELETE CASCADE,
    organization_id UUID,
    role            TEXT,
    source          TEXT NOT NULL DEFAULT 'manual',
    lc_contact_id   UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_contact_orgs_lc ON contact_organizations (lc_contact_id) WHERE lc_contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_contact_orgs_contact ON contact_organizations (contact_id);

-- Migration to the person/multi-org model (idempotent):
-- 1) tag existing LaventeCare mirrors with their person-identity key,
UPDATE contacts
   SET identity_key = lower(btrim(display_name)) || '|' || lower(btrim(coalesce(email, '')))
 WHERE source = 'laventecare' AND identity_key IS NULL;
-- 2) move the old 1:1 lc_contact_id link onto the association table,
INSERT INTO contact_organizations (user_id, contact_id, organization_id, role, source, lc_contact_id)
SELECT user_id, id, organization_id, business_role, 'laventecare', lc_contact_id
  FROM contacts
 WHERE source = 'laventecare' AND lc_contact_id IS NOT NULL
ON CONFLICT (lc_contact_id) WHERE lc_contact_id IS NOT NULL DO NOTHING;
-- 3) retire the legacy 1:1 column so the sync's dedup can collapse duplicates.
UPDATE contacts SET lc_contact_id = NULL WHERE source = 'laventecare' AND lc_contact_id IS NOT NULL;
`)
	return err
}

const contactCols = `id, user_id, display_name, relationship_types, notes, email, phone, address,
	organization_id, business_role, last_contacted_at, archived, created_at, updated_at, source`

func scanContactRow(row pgx.Row) (model.Contact, error) {
	var c model.Contact
	err := row.Scan(&c.ID, &c.UserID, &c.DisplayName, &c.RelationshipTypes, &c.Notes, &c.Email,
		&c.Phone, &c.Address, &c.OrganizationID, &c.BusinessRole, &c.LastContactedAt,
		&c.Archived, &c.CreatedAt, &c.UpdatedAt, &c.Source)
	if c.RelationshipTypes == nil {
		c.RelationshipTypes = []string{}
	}
	return c, err
}

// ListContactsOptions filters the contact list.
type ListContactsOptions struct {
	Query            string   // ILIKE match on name/email/notes/assigned-label
	RelationshipType string   // exact match against relationship_types array
	LabelNames       []string // filter on assigned label names (case-insensitive)
	LabelMatchAll    bool     // true = contact must have ALL LabelNames; false = ANY
	IncludeArchived  bool
	Limit            int
	Offset           int
}

// List returns contacts for a user, filtered and sorted by name.
func (s *ContactStore) List(ctx context.Context, userID string, opts ListContactsOptions) ([]model.Contact, error) {
	q := fmt.Sprintf(`SELECT %s FROM contacts WHERE user_id = $1`, contactCols)
	args := []any{userID}
	if !opts.IncludeArchived {
		q += ` AND archived = false`
	}
	if rt := strings.TrimSpace(opts.RelationshipType); rt != "" {
		args = append(args, rt)
		q += fmt.Sprintf(` AND $%d = ANY(relationship_types)`, len(args))
	}
	if query := strings.TrimSpace(opts.Query); query != "" {
		args = append(args, "%"+query+"%")
		n := len(args)
		q += fmt.Sprintf(` AND (
			display_name ILIKE $%d OR email ILIKE $%d OR notes ILIKE $%d
			OR EXISTS (
				SELECT 1
				FROM contact_label_assignments a
				JOIN contact_labels l ON l.id = a.label_id
				WHERE a.user_id = contacts.user_id
				  AND a.contact_id = contacts.id
				  AND l.name ILIKE $%d
			)
		)`, n, n, n, n)
	}
	if names := loweredNonEmpty(opts.LabelNames); len(names) > 0 {
		args = append(args, names)
		namesPh := len(args)
		if opts.LabelMatchAll {
			args = append(args, len(names))
			cntPh := len(args)
			q += fmt.Sprintf(` AND (SELECT COUNT(DISTINCT lower(l.name))
				FROM contact_label_assignments a JOIN contact_labels l ON l.id = a.label_id
				WHERE a.contact_id = contacts.id AND lower(l.name) = ANY($%d)) = $%d`, namesPh, cntPh)
		} else {
			q += fmt.Sprintf(` AND EXISTS (SELECT 1
				FROM contact_label_assignments a JOIN contact_labels l ON l.id = a.label_id
				WHERE a.contact_id = contacts.id AND lower(l.name) = ANY($%d))`, namesPh)
		}
	}
	// Case-insensitive name ordering with explicit display-name and UUID tie-breakers
	// keeps offset pagination deterministic when names differ only by case or repeat.
	q += ` ORDER BY lower(display_name) ASC, display_name ASC, id ASC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit)
		q += fmt.Sprintf(` LIMIT $%d`, len(args))
	}
	if opts.Offset > 0 {
		args = append(args, opts.Offset)
		q += fmt.Sprintf(` OFFSET $%d`, len(args))
	}

	rows, err := s.db.Pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Contact{}
	for rows.Next() {
		c, err := scanContactRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Rows are fully drained above; safe to run the hydration queries now.
	if err := s.hydrateLabels(ctx, userID, out); err != nil {
		return nil, err
	}
	if err := s.hydrateOrganizations(ctx, userID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get returns a single contact with its important dates and facts.
func (s *ContactStore) Get(ctx context.Context, userID string, id uuid.UUID) (model.Contact, error) {
	q := fmt.Sprintf(`SELECT %s FROM contacts WHERE user_id = $1 AND id = $2`, contactCols)
	c, err := scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, id))
	if err != nil {
		return model.Contact{}, err
	}
	if c.ImportantDates, err = s.ListImportantDates(ctx, userID, id); err != nil {
		return model.Contact{}, err
	}
	if c.Facts, err = s.ListFacts(ctx, userID, id); err != nil {
		return model.Contact{}, err
	}
	if c.Labels, err = s.labelsForContact(ctx, userID, id); err != nil {
		return model.Contact{}, err
	}
	if c.Channels, err = s.ListChannels(ctx, userID, id); err != nil {
		return model.Contact{}, err
	}
	if c.Interactions, err = s.ListInteractions(ctx, userID, id, 50); err != nil {
		return model.Contact{}, err
	}
	if c.Organizations, err = s.ListOrganizations(ctx, userID, id); err != nil {
		return model.Contact{}, err
	}
	return c, nil
}

// Create inserts a new contact. Text fields are trimmed and empty strings stored
// as NULL, matching Update's addText so `phone IS NULL` behaves consistently
// regardless of which path wrote the row.
func (s *ContactStore) Create(ctx context.Context, userID string, c model.Contact) (model.Contact, error) {
	types := c.RelationshipTypes
	if types == nil {
		types = []string{}
	}
	q := fmt.Sprintf(`
		INSERT INTO contacts (user_id, display_name, relationship_types, notes, email, phone, address,
			organization_id, business_role)
		VALUES ($1, $2, $3, NULLIF(btrim($4), ''), NULLIF(btrim($5), ''), NULLIF(btrim($6), ''),
			NULLIF(btrim($7), ''), $8, NULLIF(btrim($9), ''))
		RETURNING %s`, contactCols)
	return scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, strings.TrimSpace(c.DisplayName), types,
		derefTrim(c.Notes), derefTrim(c.Email), derefTrim(c.Phone), derefTrim(c.Address), c.OrganizationID, derefTrim(c.BusinessRole)))
}

// derefTrim returns the trimmed pointed-to string, or "" for nil (paired with a
// NULLIF(btrim(...),”) on the SQL side so empty/whitespace becomes NULL).
func derefTrim(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// ContactUpdate holds partial-update fields. A nil pointer means "leave
// unchanged"; a non-nil pointer to an empty string clears a nullable text field.
type ContactUpdate struct {
	DisplayName       *string
	RelationshipTypes *[]string
	Notes             *string
	Email             *string
	Phone             *string
	Address           *string
	BusinessRole      *string
	OrganizationID    *uuid.UUID // set organization
	ClearOrganization bool       // null out organization
	Archived          *bool
	TouchLastContact  bool // set last_contacted_at = now()
}

var ErrInvalidContactName = errors.New("contact display name is required")

// Update applies a partial update and returns the fresh contact.
func (s *ContactStore) Update(ctx context.Context, userID string, id uuid.UUID, u ContactUpdate) (model.Contact, error) {
	set := []string{}
	args := []any{}
	add := func(expr string, val any) {
		args = append(args, val)
		set = append(set, fmt.Sprintf("%s = $%d", expr, len(args)))
	}
	// Nullable text fields: an empty string clears (NULLIF at write time).
	addText := func(col string, val *string) {
		if val == nil {
			return
		}
		args = append(args, strings.TrimSpace(*val))
		set = append(set, fmt.Sprintf("%s = NULLIF($%d, '')", col, len(args)))
	}

	if u.DisplayName != nil {
		name := strings.TrimSpace(*u.DisplayName)
		if name == "" {
			return model.Contact{}, ErrInvalidContactName
		}
		add("display_name", name)
	}
	if u.RelationshipTypes != nil {
		types := *u.RelationshipTypes
		if types == nil {
			types = []string{}
		}
		add("relationship_types", types)
	}
	addText("notes", u.Notes)
	addText("email", u.Email)
	addText("phone", u.Phone)
	addText("address", u.Address)
	addText("business_role", u.BusinessRole)
	if u.ClearOrganization {
		set = append(set, "organization_id = NULL")
	} else if u.OrganizationID != nil {
		add("organization_id", *u.OrganizationID)
	}
	if u.Archived != nil {
		add("archived", *u.Archived)
	}
	if u.TouchLastContact {
		set = append(set, "last_contacted_at = now()")
	}

	if len(set) == 0 {
		// Nothing to change — return the current row.
		return s.getBare(ctx, userID, id)
	}
	set = append(set, "updated_at = now()")

	args = append(args, userID, id)
	q := fmt.Sprintf(`UPDATE contacts SET %s WHERE user_id = $%d AND id = $%d RETURNING %s`,
		strings.Join(set, ", "), len(args)-1, len(args), contactCols)

	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.Contact{}, err
	}
	defer tx.Rollback(ctx)
	if u.DisplayName != nil {
		if err := lockBusinessContextGraph(ctx, tx, userID); err != nil {
			return model.Contact{}, err
		}
	}
	updated, err := scanContactRow(tx.QueryRow(ctx, q, args...))
	if err != nil {
		return model.Contact{}, err
	}
	if u.DisplayName != nil {
		if err := renameContactBusinessContexts(ctx, tx, userID, id, updated.DisplayName); err != nil {
			return model.Contact{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Contact{}, err
	}
	return updated, nil
}

// getBare returns the contact row without nested dates/facts.
func (s *ContactStore) getBare(ctx context.Context, userID string, id uuid.UUID) (model.Contact, error) {
	q := fmt.Sprintf(`SELECT %s FROM contacts WHERE user_id = $1 AND id = $2`, contactCols)
	return scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, id))
}

// ErrManagedContact is returned when trying to hard-delete a LaventeCare-managed
// contact; deleting it would only lose local enrichment and get resurrected by the
// mirror sync. Manage such contacts in LaventeCare (or merge them).
var ErrManagedContact = errors.New("contact is managed in LaventeCare")

// Delete removes a contact (dates/facts/labels/etc. cascade). Refuses
// LaventeCare-sourced contacts: they'd be re-created by the mirror sync, so a
// delete would only silently drop the user's local enrichment.
func (s *ContactStore) Delete(ctx context.Context, userID string, id uuid.UUID) error {
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := lockBusinessContextGraph(ctx, tx, userID); err != nil {
		return err
	}

	var source string
	if err := tx.QueryRow(ctx, `SELECT source FROM contacts WHERE user_id = $1 AND id = $2 FOR UPDATE`, userID, id).Scan(&source); err != nil {
		return err // pgx.ErrNoRows when not found
	}
	if source == "laventecare" {
		return ErrManagedContact
	}
	// Notes are user-authored records and must survive a contact deletion. Clear
	// their soft context link (including history) in the same transaction so a
	// restore can never resurrect the deleted contact id.
	if err := clearContactBusinessContexts(ctx, tx, userID, id); err != nil {
		return err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM contacts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return tx.Commit(ctx)
}

// ─── Important dates ─────────────────────────────────────────────────────────

func scanImportantDate(row pgx.Row) (model.ContactImportantDate, error) {
	var d model.ContactImportantDate
	err := row.Scan(&d.ID, &d.UserID, &d.ContactID, &d.Kind, &d.Label, &d.Month, &d.Day, &d.Year, &d.Recurring, &d.CreatedAt)
	return d, err
}

// ListImportantDates returns a contact's dates.
func (s *ContactStore) ListImportantDates(ctx context.Context, userID string, contactID uuid.UUID) ([]model.ContactImportantDate, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, contact_id, kind, label, month, day, year, recurring, created_at
		FROM contact_important_dates WHERE user_id = $1 AND contact_id = $2
		ORDER BY month, day`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactImportantDate{}
	for rows.Next() {
		d, err := scanImportantDate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// AddImportantDate inserts a date for a contact (validating the contact belongs
// to the user via the FK + an explicit ownership check).
func (s *ContactStore) AddImportantDate(ctx context.Context, userID string, d model.ContactImportantDate) (model.ContactImportantDate, error) {
	if err := s.assertOwns(ctx, userID, d.ContactID); err != nil {
		return model.ContactImportantDate{}, err
	}
	kind := strings.TrimSpace(d.Kind)
	if kind == "" {
		kind = "other"
	}
	return scanImportantDate(s.db.Pool.QueryRow(ctx, `
		INSERT INTO contact_important_dates (user_id, contact_id, kind, label, month, day, year, recurring)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id, user_id, contact_id, kind, label, month, day, year, recurring, created_at`,
		userID, d.ContactID, kind, d.Label, d.Month, d.Day, d.Year, d.Recurring))
}

// DeleteImportantDate removes a date.
func (s *ContactStore) DeleteImportantDate(ctx context.Context, userID string, contactID, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM contact_important_dates WHERE user_id = $1 AND contact_id = $2 AND id = $3`,
		userID, contactID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Facts ───────────────────────────────────────────────────────────────────

func scanFact(row pgx.Row) (model.ContactFact, error) {
	var f model.ContactFact
	err := row.Scan(&f.ID, &f.UserID, &f.ContactID, &f.Fact, &f.Source, &f.OccurredAt, &f.CreatedAt)
	return f, err
}

// ListFacts returns a contact's facts (newest first).
func (s *ContactStore) ListFacts(ctx context.Context, userID string, contactID uuid.UUID) ([]model.ContactFact, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id, contact_id, fact, source, occurred_at, created_at
		FROM contact_facts WHERE user_id = $1 AND contact_id = $2
		ORDER BY created_at DESC`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactFact{}
	for rows.Next() {
		f, err := scanFact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// AddFact records a fact about a contact.
func (s *ContactStore) AddFact(ctx context.Context, userID string, f model.ContactFact) (model.ContactFact, error) {
	if err := s.assertOwns(ctx, userID, f.ContactID); err != nil {
		return model.ContactFact{}, err
	}
	source := strings.TrimSpace(f.Source)
	if source == "" {
		source = "manual"
	}
	return scanFact(s.db.Pool.QueryRow(ctx, `
		INSERT INTO contact_facts (user_id, contact_id, fact, source, occurred_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, user_id, contact_id, fact, source, occurred_at, created_at`,
		userID, f.ContactID, strings.TrimSpace(f.Fact), source, f.OccurredAt))
}

// DeleteFact removes a fact.
func (s *ContactStore) DeleteFact(ctx context.Context, userID string, contactID, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM contact_facts WHERE user_id = $1 AND contact_id = $2 AND id = $3`,
		userID, contactID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// assertOwns returns pgx.ErrNoRows if the contact does not exist for this user,
// so a caller can't attach dates/facts to someone else's (or a missing) contact.
func (s *ContactStore) assertOwns(ctx context.Context, userID string, contactID uuid.UUID) error {
	var exists bool
	err := s.db.Pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM contacts WHERE user_id = $1 AND id = $2)`, userID, contactID).Scan(&exists)
	if err != nil {
		return err
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

// ─── Upcoming dates (reminders / AI) ─────────────────────────────────────────

// UpcomingDate is a computed view of an important date's next occurrence.
type UpcomingDate struct {
	ContactID   uuid.UUID `json:"contact_id"`
	ContactName string    `json:"contact_name"`
	Kind        string    `json:"kind"`
	Label       *string   `json:"label"`
	Month       int       `json:"month"`
	Day         int       `json:"day"`
	NextDate    string    `json:"next_date"` // YYYY-MM-DD of the next occurrence
	DaysUntil   int       `json:"days_until"`
	TurningAge  *int      `json:"turning_age,omitempty"`
}

// UpcomingImportantDates returns important dates whose next occurrence falls
// within `withinDays` days (Amsterdam calendar), soonest first. Recurring dates
// roll to next year once this year's has passed; a non-recurring past date is
// skipped. Archived contacts are excluded.
func (s *ContactStore) UpcomingImportantDates(ctx context.Context, userID string, withinDays int) ([]UpcomingDate, error) {
	if withinDays <= 0 {
		withinDays = 30
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT d.contact_id, c.display_name, d.kind, d.label, d.month, d.day, d.year, d.recurring
		FROM contact_important_dates d
		JOIN contacts c ON c.id = d.contact_id AND c.user_id = d.user_id
		WHERE d.user_id = $1 AND c.archived = false`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now().In(amsterdamLoc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, amsterdamLoc)
	out := []UpcomingDate{}
	for rows.Next() {
		var (
			contactID uuid.UUID
			name      string
			kind      string
			label     *string
			month     int
			day       int
			year      *int
			recurring bool
		)
		if err := rows.Scan(&contactID, &name, &kind, &label, &month, &day, &year, &recurring); err != nil {
			return nil, err
		}
		if month < 1 || month > 12 || day < 1 || day > 31 {
			continue
		}
		next := nextOccurrence(today, month, day, recurring)
		if next == nil {
			continue
		}
		daysUntil := int(next.Sub(today).Hours()/24 + 0.5)
		if daysUntil < 0 || daysUntil > withinDays {
			continue
		}
		u := UpcomingDate{
			ContactID:   contactID,
			ContactName: name,
			Kind:        kind,
			Label:       label,
			Month:       month,
			Day:         day,
			NextDate:    next.Format("2006-01-02"),
			DaysUntil:   daysUntil,
		}
		if year != nil && kind == "birthday" && *year > 0 {
			age := next.Year() - *year
			u.TurningAge = &age
		}
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DaysUntil < out[j].DaysUntil })
	return out, nil
}

// nextOccurrence returns the next date on/after `today` for month/day. Recurring
// dates roll to next year when this year's has passed; non-recurring past dates
// return nil.
func nextOccurrence(today time.Time, month, day int, recurring bool) *time.Time {
	candidate := clampDate(today.Year(), month, day, today.Location())
	if !candidate.Before(today) {
		return &candidate
	}
	if !recurring {
		return nil
	}
	next := clampDate(today.Year()+1, month, day, today.Location())
	return &next
}

// clampDate builds a date, clamping an out-of-range day (e.g. Feb 29 in a common
// year) down to the month's last valid day so it never rolls into the next month.
func clampDate(year, month, day int, loc *time.Location) time.Time {
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, loc)
	lastDay := first.AddDate(0, 1, -1).Day()
	if day > lastDay {
		day = lastDay
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, loc)
}

// laventeCareIdentityKey is the person-identity used to dedup lc_contacts across
// companies: same name AND same email (or both empty) → one unified contact. Must
// stay in sync with the SQL backfill in ensureContactsSchema.
func laventeCareIdentityKey(naam, email string) string {
	return strings.ToLower(strings.TrimSpace(naam)) + "|" + strings.ToLower(strings.TrimSpace(email))
}

// lcMirrorRow is a lc_contacts row loaded for the mirror sync.
type lcMirrorRow struct {
	id        uuid.UUID
	companyID *uuid.UUID
	naam      string
	email     string
	telefoon  string
	rol       string
	notities  string
	isPrimary bool
	updatedAt time.Time
}

// SyncLaventeCareContactMirror reconciles the unified contacts mirror from
// lc_contacts. lc_contacts stays the source of truth, but a person who appears at
// multiple companies (one lc_contacts row per company) becomes ONE unified contact
// with one contact_organizations link per company.
//
// Identity is resolved by the STABLE lc_contact_id link first (so renaming a
// LaventeCare person keeps them on the same unified contact), falling back to the
// person-identity key (name|email) only for lc_contacts not yet linked. A manual
// merge stays durable because it repoints the org links onto the survivor. Core
// fields converge to the person's primary lc_contact only when the resolved contact
// is still LaventeCare-sourced; relationship_types, labels, dates, facts, channels,
// interactions, WhatsApp and a manual survivor's core fields are never overwritten.
// People left with no LaventeCare link are demoted to source='manual' if they carry
// local enrichment, else deleted. A per-user advisory lock serializes concurrent
// syncs (cron + write-through) so they neither race nor deadlock.
func SyncLaventeCareContactMirror(ctx context.Context, db *DB, userID string) (int, error) {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	// Serialize per-user: two overlapping syncs would otherwise race to create the
	// same person and could deadlock acquiring row locks in different orders.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID); err != nil {
		return 0, err
	}

	// Fold any pre-existing same-identity duplicate LaventeCare contacts into the
	// oldest, preserving all their enrichment (defensive; steady state has none).
	if err := collapseAllLegacyDuplicates(ctx, tx, userID); err != nil {
		return 0, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id, company_id, naam, COALESCE(email, ''), COALESCE(telefoon, ''), COALESCE(rol, ''),
		       COALESCE(notities, ''), is_primary, updated_at
		FROM lc_contacts
		WHERE user_id = $1 AND naam IS NOT NULL AND btrim(naam) <> ''
		ORDER BY id`, userID) // deterministic order → consistent lock acquisition
	if err != nil {
		return 0, err
	}
	var lcRows []lcMirrorRow
	for rows.Next() {
		var r lcMirrorRow
		if err := rows.Scan(&r.id, &r.companyID, &r.naam, &r.email, &r.telefoon, &r.rol,
			&r.notities, &r.isPrimary, &r.updatedAt); err != nil {
			rows.Close()
			return 0, err
		}
		lcRows = append(lcRows, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// Existing links: which unified contact each lc_contact already belongs to.
	linkRows, err := tx.Query(ctx, `
		SELECT lc_contact_id, contact_id FROM contact_organizations
		WHERE user_id = $1 AND source = 'laventecare' AND lc_contact_id IS NOT NULL`, userID)
	if err != nil {
		return 0, err
	}
	linked := map[uuid.UUID]uuid.UUID{}
	for linkRows.Next() {
		var lcID, cID uuid.UUID
		if err := linkRows.Scan(&lcID, &cID); err != nil {
			linkRows.Close()
			return 0, err
		}
		linked[lcID] = cID
	}
	linkRows.Close()
	if err := linkRows.Err(); err != nil {
		return 0, err
	}

	// Assign each lc_contact to a target unified contact, grouping by target.
	keyToContact := map[string]uuid.UUID{} // run-local: identity_key → chosen contact
	groups := map[uuid.UUID][]lcMirrorRow{}
	groupOrder := []uuid.UUID{}
	for _, r := range lcRows {
		key := laventeCareIdentityKey(r.naam, r.email)
		var target uuid.UUID
		if c, ok := linked[r.id]; ok {
			target = c // stable: already linked (survives rename/merge)
		} else if c, ok := keyToContact[key]; ok {
			target = c // a sibling this run already resolved the person
		} else {
			// New lc_contact: join an existing LaventeCare person with this identity,
			// else create one.
			err := tx.QueryRow(ctx, `
				SELECT id FROM contacts
				WHERE user_id = $1 AND source = 'laventecare' AND identity_key = $2
				ORDER BY created_at ASC LIMIT 1`, userID, key).Scan(&target)
			if errors.Is(err, pgx.ErrNoRows) {
				if err := tx.QueryRow(ctx, `
					INSERT INTO contacts
						(user_id, display_name, relationship_types, email, phone, notes, business_role, organization_id, source, identity_key)
					VALUES ($1, $2, ARRAY['business']::text[], NULLIF($3,''), NULLIF($4,''), NULLIF($5,''), NULLIF($6,''), $7, 'laventecare', $8)
					RETURNING id`,
					userID, r.naam, r.email, r.telefoon, r.notities, r.rol, r.companyID, key).Scan(&target); err != nil {
					return 0, err
				}
			} else if err != nil {
				return 0, err
			}
		}
		keyToContact[key] = target
		if _, ok := groups[target]; !ok {
			groupOrder = append(groupOrder, target)
		}
		groups[target] = append(groups[target], r)
	}

	for _, target := range groupOrder {
		group := groups[target]
		best := pickPrimaryLC(group)

		// Converge core fields + refresh the identity key only when the target is
		// still a LaventeCare contact (a manual merge survivor keeps its own core).
		var source string
		if err := tx.QueryRow(ctx, `SELECT source FROM contacts WHERE id = $1`, target).Scan(&source); err != nil {
			return 0, err
		}
		if source == "laventecare" {
			if _, err := tx.Exec(ctx, `
				UPDATE contacts SET display_name = $2, email = NULLIF($3,''), phone = NULLIF($4,''),
					notes = NULLIF($5,''), business_role = NULLIF($6,''), organization_id = $7,
					identity_key = $8, updated_at = now()
				WHERE id = $1`,
				target, best.naam, best.email, best.telefoon, best.notities, best.rol, best.companyID,
				laventeCareIdentityKey(best.naam, best.email)); err != nil {
				return 0, err
			}
			if err := renameContactBusinessContexts(ctx, tx, userID, target, best.naam); err != nil {
				return 0, err
			}
		}

		for _, r := range group {
			if _, err := tx.Exec(ctx, `
				INSERT INTO contact_organizations (user_id, contact_id, organization_id, role, source, lc_contact_id)
				VALUES ($1, $2, $3, NULLIF($4,''), 'laventecare', $5)
				ON CONFLICT (lc_contact_id) WHERE lc_contact_id IS NOT NULL
				DO UPDATE SET contact_id = EXCLUDED.contact_id, organization_id = EXCLUDED.organization_id,
					role = EXCLUDED.role, updated_at = now()`,
				userID, target, r.companyID, r.rol, r.id); err != nil {
				return 0, err
			}
		}
	}

	// Prune LaventeCare org links whose lc_contact vanished.
	if _, err := tx.Exec(ctx, `
		DELETE FROM contact_organizations
		WHERE user_id = $1 AND source = 'laventecare'
		  AND lc_contact_id NOT IN (SELECT id FROM lc_contacts WHERE user_id = $1)`, userID); err != nil {
		return 0, err
	}
	// People left with no LaventeCare link: keep (demote to manual) if they carry
	// local enrichment, otherwise delete the bare mirror.
	orphanRows, err := tx.Query(ctx, `
		SELECT id FROM contacts
		WHERE user_id = $1 AND source = 'laventecare'
		  AND id NOT IN (SELECT contact_id FROM contact_organizations WHERE user_id = $1 AND source = 'laventecare')`, userID)
	if err != nil {
		return 0, err
	}
	var orphans []uuid.UUID
	for orphanRows.Next() {
		var id uuid.UUID
		if err := orphanRows.Scan(&id); err != nil {
			orphanRows.Close()
			return 0, err
		}
		orphans = append(orphans, id)
	}
	orphanRows.Close()
	if err := orphanRows.Err(); err != nil {
		return 0, err
	}
	for _, id := range orphans {
		enriched, err := contactHasEnrichment(ctx, tx, id)
		if err != nil {
			return 0, err
		}
		if enriched {
			// organization_id was a scalar pointer to the now-gone company; clear it
			// (affiliations live in contact_organizations, already pruned).
			if _, err := tx.Exec(ctx, `
				UPDATE contacts SET source = 'manual', identity_key = NULL, organization_id = NULL, updated_at = now() WHERE id = $1`, id); err != nil {
				return 0, err
			}
		} else {
			if err := clearContactBusinessContexts(ctx, tx, userID, id); err != nil {
				return 0, err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, id); err != nil {
				return 0, err
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return len(groupOrder), nil
}

// collapseAllLegacyDuplicates folds any LaventeCare contacts that share an
// identity_key into the oldest of the group (re-pointing all enrichment first).
// Steady state has none; this repairs any residual pre-migration duplicates safely.
func collapseAllLegacyDuplicates(ctx context.Context, tx pgx.Tx, userID string) error {
	rows, err := tx.Query(ctx, `
		SELECT identity_key FROM contacts
		WHERE user_id = $1 AND source = 'laventecare' AND identity_key IS NOT NULL
		GROUP BY identity_key HAVING COUNT(*) > 1`, userID)
	if err != nil {
		return err
	}
	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, key := range keys {
		var canonical uuid.UUID
		if err := tx.QueryRow(ctx, `
			SELECT id FROM contacts
			WHERE user_id = $1 AND source = 'laventecare' AND identity_key = $2
			ORDER BY created_at ASC LIMIT 1`, userID, key).Scan(&canonical); err != nil {
			return err
		}
		if err := collapseDuplicateContacts(ctx, tx, userID, key, canonical); err != nil {
			return err
		}
	}
	return nil
}

// unionStrings returns first followed by any entries of second not already
// present (case-sensitive), dropping empties — preserving order.
func unionStrings(first, second []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, t := range first {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	for _, t := range second {
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// pickPrimaryLC chooses the lc_contacts row whose fields represent the person:
// the primary one, else the most recently updated.
func pickPrimaryLC(group []lcMirrorRow) lcMirrorRow {
	best := group[0]
	for _, r := range group[1:] {
		if r.isPrimary && !best.isPrimary {
			best = r
		} else if r.isPrimary == best.isPrimary && r.updatedAt.After(best.updatedAt) {
			best = r
		}
	}
	return best
}

// contactChildTables are the child tables (besides label assignments, handled
// specially) that reference contacts(id) and must be RE-POINTED — not
// cascade-deleted — when folding one contact into another. whatsapp_messages
// follows its conversation FK, so re-pointing whatsapp_conversations covers it.
var contactChildTables = []string{
	"contact_important_dates",
	"contact_facts",
	"contact_channels",
	"contact_interactions",
	"contact_organizations",
	"whatsapp_conversations",
	"whatsapp_summaries",
}

// repointContactChildren moves every child row from fromID onto toID within tx.
// The caller deletes fromID afterwards. Labels use a composite PK so are merged
// (copy-missing then drop the source's), preventing a PK collision. Notes and
// events use a soft TEXT context reference rather than a FK, so they are moved
// explicitly and their denormalized title is refreshed from the survivor.
func repointContactChildren(ctx context.Context, tx pgx.Tx, userID string, fromID, toID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `
		INSERT INTO contact_label_assignments (contact_id, label_id, user_id)
		SELECT $2, label_id, user_id FROM contact_label_assignments WHERE contact_id = $1
		ON CONFLICT DO NOTHING`, fromID, toID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM contact_label_assignments WHERE contact_id = $1`, fromID); err != nil {
		return err
	}
	for _, table := range contactChildTables {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`UPDATE %s SET contact_id = $2 WHERE contact_id = $1`, table), fromID, toID); err != nil {
			return err
		}
	}
	return repointContactBusinessContexts(ctx, tx, userID, fromID, toID)
}

func repointContactBusinessContexts(ctx context.Context, tx pgx.Tx, userID string, fromID, toID uuid.UUID) error {
	var survivorTitle string
	if err := tx.QueryRow(ctx, `SELECT display_name FROM contacts WHERE user_id = $1 AND id = $2`, userID, toID).Scan(&survivorTitle); err != nil {
		return err
	}
	for _, table := range []string{"notes", "note_revisions", "personal_events"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s
			   SET business_context_id = $3::text, business_context_title = $4
			 WHERE user_id = $1
			   AND business_context_type = 'contact'
			   AND business_context_id = $2::text`, table), userID, fromID, toID, survivorTitle); err != nil {
			return err
		}
	}
	return nil
}

func clearContactBusinessContexts(ctx context.Context, tx pgx.Tx, userID string, id uuid.UUID) error {
	for _, table := range []string{"notes", "note_revisions", "personal_events"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s
			   SET business_context_type = NULL, business_context_id = NULL, business_context_title = NULL
			 WHERE user_id = $1
			   AND business_context_type = 'contact'
			   AND business_context_id = $2::text`, table), userID, id); err != nil {
			return err
		}
	}
	return nil
}

func renameContactBusinessContexts(ctx context.Context, tx pgx.Tx, userID string, id uuid.UUID, title string) error {
	for _, table := range []string{"notes", "note_revisions", "personal_events"} {
		if _, err := tx.Exec(ctx, fmt.Sprintf(`
			UPDATE %s SET business_context_title = $3
			 WHERE user_id = $1
			   AND business_context_type = 'contact'
			   AND business_context_id = $2::text`, table), userID, id, strings.TrimSpace(title)); err != nil {
			return err
		}
	}
	return nil
}

// contactHasEnrichment reports whether a contact carries local data worth keeping
// (labels, dates, facts, channels, interactions, imported WhatsApp, or notes).
func contactHasEnrichment(ctx context.Context, tx pgx.Tx, id uuid.UUID) (bool, error) {
	var has bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM contact_label_assignments WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM contact_important_dates   WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM contact_facts             WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM contact_channels          WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM contact_interactions      WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM whatsapp_conversations    WHERE contact_id = $1)
		    OR EXISTS(SELECT 1 FROM notes WHERE business_context_type = 'contact' AND business_context_id = $1::text)
		    OR EXISTS(SELECT 1 FROM contact_organizations     WHERE contact_id = $1 AND source <> 'laventecare')`, id).Scan(&has)
	return has, err
}

// collapseDuplicateContacts folds any other LaventeCare contacts sharing this
// identity into canonicalID (re-pointing all their enrichment first), then deletes
// them — so legacy duplicates converge to one person without data loss.
func collapseDuplicateContacts(ctx context.Context, tx pgx.Tx, userID, identityKey string, canonicalID uuid.UUID) error {
	rows, err := tx.Query(ctx, `
		SELECT id FROM contacts
		WHERE user_id = $1 AND source = 'laventecare' AND identity_key = $2 AND id <> $3`,
		userID, identityKey, canonicalID)
	if err != nil {
		return err
	}
	var dups []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		dups = append(dups, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, dup := range dups {
		if err := repointContactChildren(ctx, tx, userID, dup, canonicalID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM contacts WHERE id = $1`, dup); err != nil {
			return err
		}
	}
	return nil
}

// ErrCannotMergeSelf is returned when a merge names the same contact twice.
var ErrCannotMergeSelf = errors.New("cannot merge a contact into itself")

// MergeContacts folds fromID into toID (the survivor): all enrichment (labels,
// dates, facts, channels, interactions, org links, WhatsApp) moves onto toID,
// relationship_types are unioned, and fromID is deleted. Durable against the
// LaventeCare sync because the org links are repointed onto the survivor — a person
// split across differing emails stays merged. Blank core fields on the survivor are
// filled from the source ONLY when the survivor is manual; a LaventeCare survivor's
// core fields stay owned by the mirror (they would otherwise be reverted next sync).
func (s *ContactStore) MergeContacts(ctx context.Context, userID string, fromID, toID uuid.UUID) (model.Contact, error) {
	if fromID == toID {
		return model.Contact{}, ErrCannotMergeSelf
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return model.Contact{}, err
	}
	defer tx.Rollback(ctx)

	// Serialize against the LaventeCare mirror sync (same per-user lock) so a merge
	// and a concurrent reconcile can't race on the shared contact_organizations rows.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, userID); err != nil {
		return model.Contact{}, err
	}

	// Load + ownership-check both sides (source of the survivor decides core-fill).
	var fromTypes, toTypes []string
	var toSource string
	if err := tx.QueryRow(ctx, `SELECT relationship_types FROM contacts WHERE user_id = $1 AND id = $2`, userID, fromID).Scan(&fromTypes); err != nil {
		return model.Contact{}, err // pgx.ErrNoRows if not owned/found
	}
	if err := tx.QueryRow(ctx, `SELECT relationship_types, source FROM contacts WHERE user_id = $1 AND id = $2`, userID, toID).Scan(&toTypes, &toSource); err != nil {
		return model.Contact{}, err
	}

	if err := repointContactChildren(ctx, tx, userID, fromID, toID); err != nil {
		return model.Contact{}, err
	}

	// Union relationship_types (preserve target order, append new).
	merged := unionStrings(toTypes, fromTypes)
	if toSource == "laventecare" {
		// Survivor's core is mirror-owned; only union types + advance last-contacted.
		if _, err := tx.Exec(ctx, `
			UPDATE contacts t SET relationship_types = $3,
				last_contacted_at = GREATEST(t.last_contacted_at, f.last_contacted_at), updated_at = now()
			FROM contacts f WHERE t.id = $1 AND f.id = $2`, toID, fromID, merged); err != nil {
			return model.Contact{}, err
		}
	} else if _, err := tx.Exec(ctx, `
		UPDATE contacts t SET
			relationship_types = $3,
			email          = COALESCE(t.email, f.email),
			phone          = COALESCE(t.phone, f.phone),
			address        = COALESCE(t.address, f.address),
			notes          = COALESCE(t.notes, f.notes),
			business_role  = COALESCE(t.business_role, f.business_role),
			organization_id = COALESCE(t.organization_id, f.organization_id),
			last_contacted_at = GREATEST(t.last_contacted_at, f.last_contacted_at),
			updated_at     = now()
		FROM contacts f
		WHERE t.id = $1 AND f.id = $2`, toID, fromID, merged); err != nil {
		return model.Contact{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM contacts WHERE user_id = $1 AND id = $2`, userID, fromID); err != nil {
		return model.Contact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return model.Contact{}, err
	}
	return s.Get(ctx, userID, toID)
}

// BackfillLaventeCareContacts reconciles all of the user's LaventeCare business
// contacts into the unified module (dedup + org links + collapse + prune). Called
// by the contacts-laventecare-sync cron.
func (s *ContactStore) BackfillLaventeCareContacts(ctx context.Context, userID string) (int, error) {
	return SyncLaventeCareContactMirror(ctx, s.db, userID)
}

// ─── Organizations (person ↔ companies) ──────────────────────────────────────

// ListOrganizations returns a contact's organization affiliations with company
// names resolved from lc_companies.
func (s *ContactStore) ListOrganizations(ctx context.Context, userID string, contactID uuid.UUID) ([]model.ContactOrganization, error) {
	rows, err := s.db.Pool.Query(ctx, `
		SELECT o.id, o.user_id, o.contact_id, o.organization_id, o.role, o.source, o.created_at, co.naam
		FROM contact_organizations o
		LEFT JOIN lc_companies co ON co.id = o.organization_id AND co.user_id = o.user_id
		WHERE o.user_id = $1 AND o.contact_id = $2
		ORDER BY co.naam ASC NULLS LAST, o.created_at ASC`, userID, contactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ContactOrganization{}
	for rows.Next() {
		var o model.ContactOrganization
		if err := rows.Scan(&o.ID, &o.UserID, &o.ContactID, &o.OrganizationID, &o.Role, &o.Source, &o.CreatedAt, &o.OrganizationName); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// getOrganizationLink returns a single org link (with resolved company name).
func (s *ContactStore) getOrganizationLink(ctx context.Context, userID string, id uuid.UUID) (model.ContactOrganization, error) {
	var o model.ContactOrganization
	err := s.db.Pool.QueryRow(ctx, `
		SELECT o.id, o.user_id, o.contact_id, o.organization_id, o.role, o.source, o.created_at, co.naam
		FROM contact_organizations o
		LEFT JOIN lc_companies co ON co.id = o.organization_id AND co.user_id = o.user_id
		WHERE o.user_id = $1 AND o.id = $2`, userID, id).
		Scan(&o.ID, &o.UserID, &o.ContactID, &o.OrganizationID, &o.Role, &o.Source, &o.CreatedAt, &o.OrganizationName)
	return o, err
}

// AddManualOrganization links a contact to a company (source='manual', so the
// LaventeCare sync leaves it alone). organizationID is a soft ref to lc_companies.
func (s *ContactStore) AddManualOrganization(ctx context.Context, userID string, contactID uuid.UUID, organizationID *uuid.UUID, role string) (model.ContactOrganization, error) {
	if err := s.assertOwns(ctx, userID, contactID); err != nil {
		return model.ContactOrganization{}, err
	}
	var id uuid.UUID
	if err := s.db.Pool.QueryRow(ctx, `
		INSERT INTO contact_organizations (user_id, contact_id, organization_id, role, source)
		VALUES ($1, $2, $3, NULLIF(btrim($4), ''), 'manual')
		RETURNING id`, userID, contactID, organizationID, role).Scan(&id); err != nil {
		return model.ContactOrganization{}, err
	}
	return s.getOrganizationLink(ctx, userID, id)
}

// UpdateManualOrganization edits a manual org link's company/role. LaventeCare
// links are mirror-managed and return pgx.ErrNoRows here.
func (s *ContactStore) UpdateManualOrganization(ctx context.Context, userID string, contactID, id uuid.UUID, organizationID *uuid.UUID, clearOrg bool, role *string) (model.ContactOrganization, error) {
	set := []string{}
	args := []any{}
	if clearOrg {
		set = append(set, "organization_id = NULL")
	} else if organizationID != nil {
		args = append(args, *organizationID)
		set = append(set, fmt.Sprintf("organization_id = $%d", len(args)))
	}
	if role != nil {
		args = append(args, strings.TrimSpace(*role))
		set = append(set, fmt.Sprintf("role = NULLIF($%d, '')", len(args)))
	}
	if len(set) == 0 {
		link, err := s.getOrganizationLink(ctx, userID, id)
		if err != nil || link.ContactID != contactID {
			return model.ContactOrganization{}, pgx.ErrNoRows
		}
		return link, nil
	}
	set = append(set, "updated_at = now()")
	args = append(args, userID, contactID, id)
	tag, err := s.db.Pool.Exec(ctx, fmt.Sprintf(`
		UPDATE contact_organizations SET %s
		WHERE user_id = $%d AND contact_id = $%d AND id = $%d AND source = 'manual'`,
		strings.Join(set, ", "), len(args)-2, len(args)-1, len(args)), args...)
	if err != nil {
		return model.ContactOrganization{}, err
	}
	if tag.RowsAffected() == 0 {
		return model.ContactOrganization{}, pgx.ErrNoRows
	}
	return s.getOrganizationLink(ctx, userID, id)
}

// RemoveManualOrganization deletes a manual org link. Refuses LaventeCare links
// (they'd be re-created by the sync) via the source='manual' guard.
func (s *ContactStore) RemoveManualOrganization(ctx context.Context, userID string, contactID, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx,
		`DELETE FROM contact_organizations WHERE user_id = $1 AND contact_id = $2 AND id = $3 AND source = 'manual'`,
		userID, contactID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

// FindPossibleDuplicate returns an existing non-archived contact that matches the
// given email (case-insensitive) or, failing that, the exact display name — the
// substrate for warn-on-create duplicate detection. Empty email is ignored.
func (s *ContactStore) FindPossibleDuplicate(ctx context.Context, userID, displayName, email string) (*model.Contact, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name := strings.ToLower(strings.TrimSpace(displayName))
	q := fmt.Sprintf(`SELECT %s FROM contacts
		WHERE user_id = $1 AND archived = false
		  AND ( ($2 <> '' AND lower(email) = $2) OR lower(display_name) = $3 )
		ORDER BY (($2 <> '' AND lower(email) = $2)) DESC
		LIMIT 1`, contactCols)
	c, err := scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, email, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// hydrateOrganizations bulk-loads org affiliations for a slice of contacts in one
// query (no N+1) and attaches them by contact id.
func (s *ContactStore) hydrateOrganizations(ctx context.Context, userID string, contacts []model.Contact) error {
	if len(contacts) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(contacts))
	for i, c := range contacts {
		ids[i] = c.ID
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT o.id, o.user_id, o.contact_id, o.organization_id, o.role, o.source, o.created_at, co.naam
		FROM contact_organizations o
		LEFT JOIN lc_companies co ON co.id = o.organization_id AND co.user_id = o.user_id
		WHERE o.user_id = $1 AND o.contact_id = ANY($2)
		ORDER BY co.naam ASC NULLS LAST, o.created_at ASC`, userID, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	byContact := map[uuid.UUID][]model.ContactOrganization{}
	for rows.Next() {
		var o model.ContactOrganization
		if err := rows.Scan(&o.ID, &o.UserID, &o.ContactID, &o.OrganizationID, &o.Role, &o.Source, &o.CreatedAt, &o.OrganizationName); err != nil {
			return err
		}
		byContact[o.ContactID] = append(byContact[o.ContactID], o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range contacts {
		contacts[i].Organizations = byContact[contacts[i].ID]
	}
	return nil
}
