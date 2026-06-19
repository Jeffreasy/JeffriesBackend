package google

import (
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestNormalizeCalendarID(t *testing.T) {
	cases := map[string]string{
		"":           "primary",
		"  ":         "primary",
		"Main":       "primary",
		"main":       "primary",
		"AI":         "primary", // the bug: "AI" must resolve to primary, not 404
		"ai":         "primary",
		"work@x.com": "work@x.com",
	}
	for in, want := range cases {
		if got := NormalizeCalendarID(in); got != want {
			t.Errorf("NormalizeCalendarID(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveCalendarTarget(t *testing.T) {
	// AI-staged event resolves to primary, event id kept as-is (no prefix on primary).
	cal, id := ResolveCalendarTarget(model.PersonalEvent{Kalender: "AI", EventID: "ai-123"})
	if cal != "primary" || id != "ai-123" {
		t.Fatalf("AI event: got (%q,%q), want (primary, ai-123)", cal, id)
	}

	// Non-primary calendar strips its namespace prefix from the event id.
	cal, id = ResolveCalendarTarget(model.PersonalEvent{Kalender: "work@x.com", EventID: "work@x.com:evt9"})
	if cal != "work@x.com" || id != "evt9" {
		t.Fatalf("named cal: got (%q,%q), want (work@x.com, evt9)", cal, id)
	}
}

func TestStoredCalendarEventID(t *testing.T) {
	if got := StoredCalendarEventID("primary", "evt1"); got != "evt1" {
		t.Errorf("primary: got %q, want evt1", got)
	}
	if got := StoredCalendarEventID("work@x.com", "evt1"); got != "work@x.com:evt1" {
		t.Errorf("named: got %q, want work@x.com:evt1", got)
	}
	// Round-trips with ResolveCalendarTarget.
	stored := StoredCalendarEventID("work@x.com", "evt1")
	_, id := ResolveCalendarTarget(model.PersonalEvent{Kalender: "work@x.com", EventID: stored})
	if id != "evt1" {
		t.Errorf("round-trip: got %q, want evt1", id)
	}
}

func TestSplitCalendarIDs(t *testing.T) {
	if got := SplitCalendarIDs(""); len(got) != 1 || got[0] != "primary" {
		t.Errorf("empty: got %v, want [primary]", got)
	}
	got := SplitCalendarIDs(" a@x.com , , b@x.com ")
	if len(got) != 2 || got[0] != "a@x.com" || got[1] != "b@x.com" {
		t.Errorf("list: got %v, want [a@x.com b@x.com]", got)
	}
}
