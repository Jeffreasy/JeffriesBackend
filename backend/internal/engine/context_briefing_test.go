package engine

import (
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func strp(s string) *string { return &s }

// TestRecommendedContextActionsRanksAcrossDomains pins down the fix for a
// real gap: a same-day, high-priority LaventeCare deadline must not be
// dropped from the aandachtspunten pool just because several low-priority
// notes happened to be appended first and filled the limit. Previously
// domain order (notes -> email -> laventecare -> agenda) plus a hard stop at
// `limit` meant this exact case was silently lost.
func TestRecommendedContextActionsRanksAcrossDomains(t *testing.T) {
	loc := amsterdamLocation()
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, loc)

	// 5 low-priority notes needing attention (an open checklist each), enough
	// to fill a limit of 5 on their own if domain order were still first-served.
	var notes []model.Note
	for i := 0; i < 5; i++ {
		notes = append(notes, model.Note{
			Titel:      strp("Losse todo"),
			Inhoud:     "- [ ] iets kleins\n- [x] gedaan",
			Prioriteit: strp("laag"),
			Aangemaakt: now.Add(-48 * time.Hour),
			Gewijzigd:  now.Add(-48 * time.Hour),
		})
	}

	cockpit := &model.LCCockpit{
		ActionItems: []model.LCActionItem{
			{
				Title:    "Offerte C&F Bouw controleren",
				Priority: "hoog",
				DueDate:  strp(now.Format("2006-01-02")), // due TODAY
			},
		},
	}

	actions := recommendedContextActions(notes, nil, cockpit, 0, now, loc, 3)

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions (limit), got %d: %+v", len(actions), actions)
	}
	found := false
	for _, a := range actions {
		if a["domain"] == "laventecare" && a["title"] == "Offerte C&F Bouw controleren" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the same-day high-priority LaventeCare action to survive truncation, got %+v", actions)
	}
}

func TestDomainUrgencyScoreOverdueBeatsFuture(t *testing.T) {
	loc := amsterdamLocation()
	now := time.Date(2026, 7, 1, 9, 0, 0, 0, loc)

	overdue := domainUrgencyScore("normaal", "2026-06-25", now, loc, 15)
	future := domainUrgencyScore("hoog", "2026-08-01", now, loc, 15)

	if overdue <= future {
		t.Fatalf("expected overdue normaal-priority item to outscore a far-future hoog-priority item: overdue=%d future=%d", overdue, future)
	}
}

func TestWeekdayLabel(t *testing.T) {
	// 2026-07-07 is a real-calendar Tuesday — the exact date from the
	// production weekday bug this defense-in-depth guards against.
	got := weekdayLabel("2026-07-07")
	if got != "dinsdag" {
		t.Fatalf("weekdayLabel(2026-07-07) = %q, want dinsdag", got)
	}
	if weekdayLabel("not-a-date") != "" {
		t.Fatalf("expected empty string for unparseable date")
	}
}
