package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// loweredNonEmpty trims, lower-cases and drops empty entries — used to normalize
// label-name filters before an =ANY() match.
func loweredNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.ToLower(strings.TrimSpace(s))
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// StaleContact is a contact you haven't been in touch with for a while, with the
// number of days since the last logged interaction (nil = never contacted).
type StaleContact struct {
	model.Contact
	DaysSince *int `json:"days_since"`
}

// StaleContacts returns non-archived contacts whose last_contacted_at is older
// than olderThanDays (or never set), oldest-contacted first — the substrate for
// "wie moet ik weer eens spreken" in the AI and the weekly nudge.
func (s *ContactStore) StaleContacts(ctx context.Context, userID string, olderThanDays, limit int) ([]StaleContact, error) {
	if olderThanDays <= 0 {
		olderThanDays = 60
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	q := fmt.Sprintf(`
		SELECT %s FROM contacts
		WHERE user_id = $1 AND archived = false
		  AND (last_contacted_at IS NULL OR last_contacted_at < now() - make_interval(days => $2))
		ORDER BY last_contacted_at ASC NULLS LAST, display_name ASC
		LIMIT $3`, contactCols)
	rows, err := s.db.Pool.Query(ctx, q, userID, olderThanDays, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StaleContact{}
	for rows.Next() {
		c, err := scanContactRow(rows)
		if err != nil {
			return nil, err
		}
		sc := StaleContact{Contact: c}
		if c.LastContactedAt != nil {
			days := int(time.Since(*c.LastContactedAt).Hours() / 24)
			sc.DaysSince = &days
		}
		out = append(out, sc)
	}
	return out, rows.Err()
}
