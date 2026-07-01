package store

import "testing"

// TestValidateLCStatus locks the lead/project/workstream status vocabulary: the
// new terminal lost states are accepted, typos are rejected, and nil/empty
// (="unchanged") always passes.
func TestValidateLCStatus(t *testing.T) {
	ok := func(s string) {
		t.Helper()
		v := s
		if err := validateLCStatus(&v); err != nil {
			t.Fatalf("status %q should be valid, got %v", s, err)
		}
	}
	bad := func(s string) {
		t.Helper()
		v := s
		if err := validateLCStatus(&v); err != ErrInvalidStatus {
			t.Fatalf("status %q should be ErrInvalidStatus, got %v", s, err)
		}
	}

	if err := validateLCStatus(nil); err != nil {
		t.Fatalf("nil status should pass (unchanged): %v", err)
	}
	empty := "  "
	if err := validateLCStatus(&empty); err != nil {
		t.Fatalf("blank status should pass (unchanged): %v", err)
	}

	for _, s := range []string{
		"nieuw", "intake", "discovery", "voorstel", "actief", "uitvoering", "review",
		"afgerond", "gesloten", "gewonnen", "verloren", "gediskwalificeerd", "geannuleerd",
	} {
		ok(s)
	}
	for _, s := range []string{"verlroen", "gewnonen", "onbekend", "won", "lost", "qualified"} {
		bad(s)
	}

	// LAVENTECARE_PROJECT_STATUSES (frontend dropdown) offers "on_hold" and
	// "opgeleverd" as project statuses — they must be recognised (and stay
	// open, since a delivered project can still move through sla/evolution
	// phases) or every project update to either value 400s in the UI.
	for _, s := range []string{"on_hold", "opgeleverd"} {
		ok(s)
		if !isOpenStatus(s) {
			t.Fatalf("%q should be an open status", s)
		}
		if isClosedStatus(s) {
			t.Fatalf("%q should not be a closed status", s)
		}
	}

	// The won/lost terminal states must count as closed so they leave open lists.
	for _, s := range []string{"gewonnen", "verloren", "gediskwalificeerd"} {
		if !isClosedStatus(s) {
			t.Fatalf("%q should be a closed status", s)
		}
		if isOpenStatus(s) {
			t.Fatalf("%q should not be an open status", s)
		}
	}
}
