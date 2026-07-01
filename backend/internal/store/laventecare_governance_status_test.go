package store

import "testing"

// TestGovernanceStatusVocabularies locks the small, distinct status vocabularies
// for decisions, change requests and SLA incidents — each narrower than the
// shared lead/project/workstream vocabulary, confirmed against the actual
// frontend dropdowns/action buttons in LaventeCareOperationsView.tsx.
func TestGovernanceStatusVocabularies(t *testing.T) {
	for _, s := range []string{"genomen", "voorstel", "herzien"} {
		if !isKnownDecisionStatus(s) {
			t.Fatalf("decision status %q should be known", s)
		}
	}
	for _, s := range []string{"", "open", "afgerond", "typo"} {
		if isKnownDecisionStatus(s) {
			t.Fatalf("decision status %q should not be known", s)
		}
	}

	for _, s := range []string{"nieuw", "beoordeeld", "goedgekeurd", "afgewezen", "afgehandeld"} {
		if !isKnownChangeRequestStatus(s) {
			t.Fatalf("change request status %q should be known", s)
		}
	}
	for _, s := range []string{"", "genomen", "typo"} {
		if isKnownChangeRequestStatus(s) {
			t.Fatalf("change request status %q should not be known", s)
		}
	}

	for _, s := range []string{"open", "in_behandeling", "wacht_op_klant", "gesloten"} {
		if !isKnownSlaIncidentStatus(s) {
			t.Fatalf("sla incident status %q should be known", s)
		}
	}
	for _, s := range []string{"", "afgerond", "typo"} {
		if isKnownSlaIncidentStatus(s) {
			t.Fatalf("sla incident status %q should not be known", s)
		}
	}
}

// TestContainsWordBoundary locks the business-signal matcher so a short CRM
// term (e.g. a workstream literally named "wonen") can no longer produce a
// false-positive signal by matching as a substring of an unrelated word like
// "voorwonen" or "bewonen".
func TestContainsWordBoundary(t *testing.T) {
	cases := []struct {
		haystack string
		term     string
		want     bool
	}{
		{"we hebben het over wonen gehad", "wonen", true},
		{"voorwonen is geen onderwerp hier", "wonen", false},
		{"ze gaan daar graag bewonen", "wonen", false},
		{"henke wonen belt morgen", "henke wonen", true},
		{"3x3 anders project update", "3x3 anders", true},
		{"anderszins niets van toepassing", "anders", false},
		{"wonen", "wonen", true},
		{"", "wonen", false},
	}
	for _, c := range cases {
		got := containsWordBoundary(normalize(c.haystack), normalize(c.term))
		if got != c.want {
			t.Fatalf("containsWordBoundary(%q, %q) = %v, want %v", c.haystack, c.term, got, c.want)
		}
	}
}

// TestUpdateActionStatusRejectsUnknownStatus locks that UpdateActionStatus
// validates against the shared lcKnownStatus vocabulary before touching the
// database, so a typo'd status is rejected instead of silently persisted.
func TestUpdateActionStatusRejectsUnknownStatus(t *testing.T) {
	s := &LaventeCareStore{}
	for _, status := range []string{"open", "bezig", "wacht_op_klant", "done", "afgerond"} {
		if !lcKnownStatus(status) {
			t.Fatalf("action status %q should be a known status", status)
		}
	}
	err := s.UpdateActionStatus(nil, "user", [16]byte{}, "onbekend")
	if err != ErrInvalidStatus {
		t.Fatalf("expected ErrInvalidStatus for unknown action status, got %v", err)
	}
}
