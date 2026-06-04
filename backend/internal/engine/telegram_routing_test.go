package engine

import "testing"

func TestRouteFreeTextPlanningGoesToAgenda(t *testing.T) {
	got := routeFreeText("wat staat er vandaag op mijn planning?")
	if got != "agenda" {
		t.Fatalf("routeFreeText() = %q, want agenda", got)
	}
}

func TestExternalNewsIntent(t *testing.T) {
	if !hasExternalNewsIntent("wat was het laatste nieuws de afgelopen 24 uur?") {
		t.Fatal("expected news intent")
	}
	if hasExternalNewsIntent("doorzoek mijn mail op nieuwsbrieven") {
		t.Fatal("newsletter/mail request should not use external news search")
	}
}

func TestStripTelegramPlainText(t *testing.T) {
	got := stripTelegramPlainText("**Kop**\n[Bron](https://example.com)")
	want := "Kop\nBron (https://example.com)"
	if got != want {
		t.Fatalf("stripTelegramPlainText() = %q, want %q", got, want)
	}
}

func TestParseToolDateRangeDefaultsToToday(t *testing.T) {
	start, end, hasRange, err := parseToolDateRange(`{}`, true)
	if err != nil {
		t.Fatalf("parseToolDateRange() error = %v", err)
	}
	if !hasRange {
		t.Fatal("expected fallback today range")
	}
	if start == "" || end == "" || start != end {
		t.Fatalf("unexpected fallback range: start=%q end=%q", start, end)
	}
}
