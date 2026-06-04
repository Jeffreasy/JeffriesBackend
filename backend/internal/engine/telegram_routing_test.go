package engine

import (
	"strings"
	"testing"

	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
)

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

func TestExpandTelegramCommand(t *testing.T) {
	tests := []struct {
		input     string
		agentHint string
		contains  string
	}{
		{input: "/briefing", agentHint: "brain", contains: "dagbriefing"},
		{input: "/planning", agentHint: "agenda", contains: "planning"},
		{input: "/news", agentHint: "brain", contains: "nieuws"},
	}

	for _, tt := range tests {
		expanded, agentHint, ok := expandTelegramCommand(tt.input)
		if !ok {
			t.Fatalf("expandTelegramCommand(%q) did not expand", tt.input)
		}
		if agentHint != tt.agentHint {
			t.Fatalf("expandTelegramCommand(%q) agent = %q, want %q", tt.input, agentHint, tt.agentHint)
		}
		if !strings.Contains(strings.ToLower(expanded), tt.contains) {
			t.Fatalf("expandTelegramCommand(%q) = %q, want text containing %q", tt.input, expanded, tt.contains)
		}
	}
}

func TestTelegramMenusUseShortCallbackData(t *testing.T) {
	for _, menu := range []struct {
		name string
		rows [][]tg.InlineKeyboardButton
	}{
		{name: "main", rows: buildMainMenu().InlineKeyboard},
		{name: "lamp", rows: buildLampMenu().InlineKeyboard},
		{name: "notes", rows: buildNotesMenu().InlineKeyboard},
	} {
		for _, row := range menu.rows {
			for _, button := range row {
				if len(button.CallbackData) > 64 {
					t.Fatalf("%s menu callback %q is %d bytes, Telegram max is 64", menu.name, button.CallbackData, len(button.CallbackData))
				}
			}
		}
	}
}
