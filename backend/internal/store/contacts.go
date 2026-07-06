package store

import (
	"context"
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
	Query            string   // ILIKE match on name/email/notes
	RelationshipType string   // exact match against relationship_types array
	LabelNames       []string // filter on assigned label names (case-insensitive)
	LabelMatchAll    bool     // true = contact must have ALL LabelNames; false = ANY
	IncludeArchived  bool
	Limit            int
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
		q += fmt.Sprintf(` AND (display_name ILIKE $%d OR email ILIKE $%d OR notes ILIKE $%d)`, n, n, n)
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
	q += ` ORDER BY display_name ASC`
	if opts.Limit > 0 {
		args = append(args, opts.Limit)
		q += fmt.Sprintf(` LIMIT $%d`, len(args))
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
	// Rows are fully drained above; safe to run the label-hydration query on the
	// same pool connection now.
	if err := s.hydrateLabels(ctx, userID, out); err != nil {
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
	return c, nil
}

// Create inserts a new contact.
func (s *ContactStore) Create(ctx context.Context, userID string, c model.Contact) (model.Contact, error) {
	types := c.RelationshipTypes
	if types == nil {
		types = []string{}
	}
	q := fmt.Sprintf(`
		INSERT INTO contacts (user_id, display_name, relationship_types, notes, email, phone, address,
			organization_id, business_role)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING %s`, contactCols)
	return scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, c.DisplayName, types, c.Notes, c.Email,
		c.Phone, c.Address, c.OrganizationID, c.BusinessRole))
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
		add("display_name", strings.TrimSpace(*u.DisplayName))
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
	return scanContactRow(s.db.Pool.QueryRow(ctx, q, args...))
}

// getBare returns the contact row without nested dates/facts.
func (s *ContactStore) getBare(ctx context.Context, userID string, id uuid.UUID) (model.Contact, error) {
	q := fmt.Sprintf(`SELECT %s FROM contacts WHERE user_id = $1 AND id = $2`, contactCols)
	return scanContactRow(s.db.Pool.QueryRow(ctx, q, userID, id))
}

// Delete removes a contact (dates/facts cascade).
func (s *ContactStore) Delete(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM contacts WHERE user_id = $1 AND id = $2`, userID, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
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
func (s *ContactStore) DeleteImportantDate(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM contact_important_dates WHERE user_id = $1 AND id = $2`, userID, id)
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
func (s *ContactStore) DeleteFact(ctx context.Context, userID string, id uuid.UUID) error {
	tag, err := s.db.Pool.Exec(ctx, `DELETE FROM contact_facts WHERE user_id = $1 AND id = $2`, userID, id)
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

// SyncLaventeCareContactMirror upserts LaventeCare business contacts (lc_contacts)
// into the unified contacts table as relationship_type "business", linked to their
// organization and tagged source="laventecare". lc_contacts stays the source of
// truth: the mirrored display_name/email/phone/notes/business_role/organization_id
// converge to it, but relationship_types are NOT overwritten so a business contact
// can also be tagged e.g. "friend" in the module.
//
// onlyID scopes the sync to a single lc_contact (the write-through hooks in
// CreateContact/UpdateContact); nil syncs all of the user's business contacts and
// prunes mirrors whose LaventeCare contact was deleted. Returns the upserted count.
func SyncLaventeCareContactMirror(ctx context.Context, db *DB, userID string, onlyID *uuid.UUID) (int, error) {
	q := `
		INSERT INTO contacts
			(user_id, display_name, relationship_types, email, phone, notes, business_role, organization_id, source, lc_contact_id)
		SELECT user_id, naam, ARRAY['business']::text[], email, telefoon, notities, rol, company_id, 'laventecare', id
		FROM lc_contacts
		WHERE user_id = $1 AND naam IS NOT NULL AND btrim(naam) <> ''`
	args := []any{userID}
	if onlyID != nil {
		args = append(args, *onlyID)
		q += fmt.Sprintf(" AND id = $%d", len(args))
	}
	q += `
		ON CONFLICT (lc_contact_id) DO UPDATE SET
			display_name    = EXCLUDED.display_name,
			email           = EXCLUDED.email,
			phone           = EXCLUDED.phone,
			notes           = EXCLUDED.notes,
			business_role   = EXCLUDED.business_role,
			organization_id = EXCLUDED.organization_id,
			updated_at      = now()`
	tag, err := db.Pool.Exec(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n := int(tag.RowsAffected())
	if onlyID == nil {
		// Prune mirrors whose LaventeCare contact no longer exists.
		_, _ = db.Pool.Exec(ctx, `
			DELETE FROM contacts
			WHERE user_id = $1 AND source = 'laventecare' AND lc_contact_id IS NOT NULL
			  AND lc_contact_id NOT IN (SELECT id FROM lc_contacts WHERE user_id = $1)`, userID)
	}
	return n, nil
}

// BackfillLaventeCareContacts syncs all of the user's LaventeCare business
// contacts into the unified module (create + update + prune). Called by the
// contacts-laventecare-sync cron.
func (s *ContactStore) BackfillLaventeCareContacts(ctx context.Context, userID string) (int, error) {
	return SyncLaventeCareContactMirror(ctx, s.db, userID, nil)
}
