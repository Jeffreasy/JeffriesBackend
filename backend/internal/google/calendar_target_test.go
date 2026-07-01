package google

import (
	"errors"
	"fmt"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestIsPermanentCalendarError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"birthday event type restriction", &APIError{Method: "PUT", URL: "x", Status: 400, Body: `{"error":{"errors":[{"reason":"eventTypeRestriction","message":"Event type cannot be changed."}]}}`}, true},
		{"human-readable message match", errors.New("Event type cannot be changed."), true},
		{"unrelated 404", &APIError{Method: "PUT", URL: "x", Status: 404, Body: "Not Found"}, false},
		{"transient 503", &APIError{Method: "PUT", URL: "x", Status: 503, Body: "Service Unavailable"}, false},
	}
	for _, c := range cases {
		if got := IsPermanentCalendarError(c.err); got != c.want {
			t.Errorf("%s: IsPermanentCalendarError() = %v, want %v", c.name, got, c.want)
		}
	}
}

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

func TestDeterministicEventID(t *testing.T) {
	in := "ai-550e8400-e29b-41d4-a716-446655440000"
	a := deterministicEventID(in)
	if a != deterministicEventID(in) {
		t.Fatal("not deterministic")
	}
	if len(a) < 5 || len(a) > 1024 {
		t.Fatalf("length %d outside Google's 5..1024 range", len(a))
	}
	for _, r := range a { // Google requires base32hex: 0-9 and a-v
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'v')) {
			t.Fatalf("invalid base32hex char %q in %q", r, a)
		}
	}
	if deterministicEventID("") != "" {
		t.Fatal("empty input must yield empty id")
	}
	if deterministicEventID("other-id") == a {
		t.Fatal("distinct inputs must not collide")
	}
}

func TestStatusCode(t *testing.T) {
	apiErr := &APIError{Method: "GET", URL: "u", Status: 404, Body: "x"}
	if StatusCode(apiErr) != 404 {
		t.Error("direct APIError status not extracted")
	}
	if StatusCode(fmt.Errorf("history list: %w", apiErr)) != 404 {
		t.Error("wrapped APIError status not extracted")
	}
	if StatusCode(errors.New("plain")) != 0 {
		t.Error("non-APIError should yield 0")
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
