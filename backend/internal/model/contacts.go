package model

import (
	"time"

	"github.com/google/uuid"
)

// Contact is the unified relationship record — one row per person, business or
// personal (family, friends, colleagues, LaventeCare). RelationshipTypes holds
// the (possibly multiple) kinds a person is to you. OrganizationID is a soft
// reference to lc_companies(id) for business contacts (no FK in phase 0 to keep
// the module decoupled from LaventeCare).
type Contact struct {
	ID                uuid.UUID  `json:"id" db:"id"`
	UserID            string     `json:"user_id" db:"user_id"`
	DisplayName       string     `json:"display_name" db:"display_name"`
	RelationshipTypes []string   `json:"relationship_types" db:"relationship_types"`
	Notes             *string    `json:"notes" db:"notes"`
	Email             *string    `json:"email" db:"email"`
	Phone             *string    `json:"phone" db:"phone"`
	Address           *string    `json:"address" db:"address"`
	OrganizationID    *uuid.UUID `json:"organization_id" db:"organization_id"`
	BusinessRole      *string    `json:"business_role" db:"business_role"`
	LastContactedAt   *time.Time `json:"last_contacted_at" db:"last_contacted_at"`
	Archived          bool       `json:"archived" db:"archived"`
	Source            string     `json:"source" db:"source"` // manual | laventecare
	CreatedAt         time.Time  `json:"created_at" db:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at"`

	// Nested detail, populated by Get (not stored on the contacts row).
	ImportantDates []ContactImportantDate `json:"important_dates,omitempty" db:"-"`
	Facts          []ContactFact          `json:"facts,omitempty" db:"-"`
	// Labels are the free-form, colour-coded tags assigned to this contact from
	// the per-user catalog. Hydrated by List/Get; never overwritten by the
	// LaventeCare mirror sync, so a business contact can also be tagged e.g. VIP.
	Labels []ContactLabel `json:"labels,omitempty" db:"-"`
}

// ContactLabel is a free-form, colour-coded tag in the per-user catalog — the
// rich labelling layer above the fixed relationship_types (family/friend/…).
// Examples: "hardloopmaatje", "investeerder", "VIP", "kerstkaart". Labels are
// first-class (rename/merge/recolour) and feed AI context. Color is a palette
// key (see store.NormalizeLabelColor), not raw CSS, so the UI/AI stay consistent.
type ContactLabel struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	Name      string    `json:"name" db:"name"`
	Color     string    `json:"color" db:"color"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`

	// ContactCount is populated only by ListLabels (usage count); omitted elsewhere.
	ContactCount int `json:"contact_count,omitempty" db:"-"`
}

// ContactImportantDate is a recurring (or one-off) date for a contact —
// birthdays, anniversaries. Year is optional (a birthday may have an unknown
// year); month/day drive the "upcoming"/reminder logic.
type ContactImportantDate struct {
	ID        uuid.UUID `json:"id" db:"id"`
	UserID    string    `json:"user_id" db:"user_id"`
	ContactID uuid.UUID `json:"contact_id" db:"contact_id"`
	Kind      string    `json:"kind" db:"kind"` // birthday | anniversary | other
	Label     *string   `json:"label" db:"label"`
	Month     int       `json:"month" db:"month"`
	Day       int       `json:"day" db:"day"`
	Year      *int      `json:"year" db:"year"`
	Recurring bool      `json:"recurring" db:"recurring"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
}

// ContactFact is a discrete fact tied to a contact ("houdt van hardlopen",
// "verhuisd naar Amsterdam") — the substrate for the AI's "remember facts"
// capability. Source records where it came from.
type ContactFact struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	UserID     string     `json:"user_id" db:"user_id"`
	ContactID  uuid.UUID  `json:"contact_id" db:"contact_id"`
	Fact       string     `json:"fact" db:"fact"`
	Source     string     `json:"source" db:"source"` // manual | telegram | whatsapp_summary
	OccurredAt *time.Time `json:"occurred_at" db:"occurred_at"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
}
