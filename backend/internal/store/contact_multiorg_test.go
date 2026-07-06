package store

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestLaventeCareIdentityKey(t *testing.T) {
	// Same person at two companies (same name + email) → same key.
	a := laventeCareIdentityKey("Benjamin Verschoor", "benjamin@3x3anders.nl")
	b := laventeCareIdentityKey("  benjamin verschoor ", "Benjamin@3x3anders.nl")
	if a != b {
		t.Fatalf("expected same identity key, got %q vs %q", a, b)
	}
	// Same name, different email (e.g. per-company address) → different key (safe).
	if laventeCareIdentityKey("Jan Jansen", "jan@a.nl") == laventeCareIdentityKey("Jan Jansen", "jan@b.nl") {
		t.Fatal("different emails must not collapse into one person")
	}
	// Empty email falls back to name-only.
	if laventeCareIdentityKey("Cengiz", "") != "cengiz|" {
		t.Fatalf("unexpected key for empty email: %q", laventeCareIdentityKey("Cengiz", ""))
	}
}

func TestUnionStrings(t *testing.T) {
	got := unionStrings([]string{"business", "friend"}, []string{"friend", "family", ""})
	want := []string{"business", "friend", "family"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestPickPrimaryLC(t *testing.T) {
	older := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	id1, id2, id3 := uuid.New(), uuid.New(), uuid.New()

	// is_primary wins over a more recent non-primary.
	got := pickPrimaryLC([]lcMirrorRow{
		{id: id1, updatedAt: newer, isPrimary: false},
		{id: id2, updatedAt: older, isPrimary: true},
	})
	if got.id != id2 {
		t.Fatalf("expected primary row to win, got %v", got.id)
	}

	// Among non-primaries, the most recently updated wins.
	got = pickPrimaryLC([]lcMirrorRow{
		{id: id1, updatedAt: older, isPrimary: false},
		{id: id3, updatedAt: newer, isPrimary: false},
	})
	if got.id != id3 {
		t.Fatalf("expected most-recent row to win, got %v", got.id)
	}
}
