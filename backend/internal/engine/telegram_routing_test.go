package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
	"github.com/google/uuid"
)

func TestRouteFreeTextPlanningGoesToAgenda(t *testing.T) {
	got := routeFreeText("wat staat er vandaag op mijn planning?")
	if got != "agenda" {
		t.Fatalf("routeFreeText() = %q, want agenda", got)
	}
}

func TestRouteFreeTextNoteIntentGoesToNotes(t *testing.T) {
	got := routeFreeText("onthoud dat ik HenkeWonen morgen moet terugbellen")
	if got != "notes" {
		t.Fatalf("routeFreeText() = %q, want notes", got)
	}
}

func TestRouteFreeTextFinanceIntentGoesToFinance(t *testing.T) {
	got := routeFreeText("wat zijn mijn grootste uitgaven deze maand?")
	if got != "finance" {
		t.Fatalf("routeFreeText() = %q, want finance", got)
	}
}

func TestRouteFreeTextLaventeCareIntentGoesToLaventeCare(t *testing.T) {
	got := routeFreeText("geef mijn LaventeCare CRM cockpit")
	if got != "laventecare" {
		t.Fatalf("routeFreeText() = %q, want laventecare", got)
	}
}

func TestRouteFreeTextHabitIntentGoesToHabits(t *testing.T) {
	got := routeFreeText("ik heb mijn water habit afgevinkt")
	if got != "habits" {
		t.Fatalf("routeFreeText() = %q, want habits", got)
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
		{input: "/agenda", agentHint: "agenda", contains: "afsprakenopvragen"},
		{input: "/rooster", agentHint: "rooster", contains: "dienstenopvragen"},
		{input: "/finance", agentHint: "finance", contains: "uitgavenoverzicht"},
		{input: "/laventecare", agentHint: "laventecare", contains: "laventecarecockpit"},
		{input: "/habits", agentHint: "habits", contains: "habitrapport"},
		{input: "/check", agentHint: "habits", contains: "habitrapport"},
		{input: "/news", agentHint: "brain", contains: "nieuws"},
		{input: "/noteai", agentHint: "notes", contains: "notities"},
		{input: "/notetriage", agentHint: "notes", contains: "triage"},
		{input: "/notesamenvatting", agentHint: "notes", contains: "samen"},
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

func TestParseTelegramFinancePeriodDefaultsToCurrentMonth(t *testing.T) {
	now := time.Date(2026, time.June, 5, 12, 0, 0, 0, time.UTC)
	period := parseTelegramFinancePeriod("/finance", now)

	if period.Label != "juni 2026 (standaard maand)" {
		t.Fatalf("Label = %q", period.Label)
	}
	if period.DatumVan != "2026-06-01" || period.DatumTot != "2026-06-30" {
		t.Fatalf("range = %s..%s", period.DatumVan, period.DatumTot)
	}
	if period.AllTime {
		t.Fatal("default period should not be all-time")
	}
}

func TestParseTelegramFinancePeriodSupportsExplicitScopes(t *testing.T) {
	now := time.Date(2026, time.June, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		text string
		from string
		to   string
		all  bool
	}{
		{text: "/finance vorige maand", from: "2026-05-01", to: "2026-05-31"},
		{text: "/finance 2018", from: "2018-01-01", to: "2018-12-31"},
		{text: "/finance 2026", from: "2026-01-01", to: "2026-12-31"},
		{text: "/finance 2026-06", from: "2026-06-01", to: "2026-06-30"},
		{text: "/finance juni 2026", from: "2026-06-01", to: "2026-06-30"},
		{text: "/finance alles", all: true},
	}

	for _, tt := range tests {
		period := parseTelegramFinancePeriod(tt.text, now)
		if period.AllTime != tt.all {
			t.Fatalf("%s AllTime = %v, want %v", tt.text, period.AllTime, tt.all)
		}
		if tt.all {
			continue
		}
		if period.DatumVan != tt.from || period.DatumTot != tt.to {
			t.Fatalf("%s range = %s..%s, want %s..%s", tt.text, period.DatumVan, period.DatumTot, tt.from, tt.to)
		}
	}
}

func TestExpandNotesAICommandPrefersLiveNotesSnapshot(t *testing.T) {
	expanded, agentHint, ok := expandTelegramCommand("/noteai")
	if !ok {
		t.Fatal("expected /noteai to expand")
	}
	if agentHint != "notes" {
		t.Fatalf("agentHint = %q, want notes", agentHint)
	}

	lower := strings.ToLower(expanded)
	for _, needle := range []string{"live data.notes", "notitiesoverzicht", "notitiesvandaag alleen", "leeg vandaag betekent niet"} {
		if !strings.Contains(lower, needle) {
			t.Fatalf("/noteai expansion missing %q: %s", needle, expanded)
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
		{name: "note-dashboard", rows: buildNotesDashboardKeyboard([]model.Note{
			{ID: uuid.MustParse("65360cd0-0a6f-4a52-a5af-5486f6e2d1f7")},
		}).InlineKeyboard},
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

func TestChecklistProgress(t *testing.T) {
	checked, total := checklistProgress("- [x] klaar\n- [ ] open\n✅ gedaan\nlosse regel")
	if checked != 2 || total != 3 {
		t.Fatalf("checklistProgress() = %d/%d, want 2/3", checked, total)
	}
}

func TestParseNoteCaptureEnrichesTelegramNote(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	now := time.Date(2026, 6, 4, 10, 0, 0, 0, loc)
	capture := parseNoteCapture("Bel HenkeWonen morgen 11:00 #werk !hoog", now, loc)

	if capture.Title != "Bel HenkeWonen morgen 11:00" {
		t.Fatalf("Title = %q", capture.Title)
	}
	if capture.Priority == nil || *capture.Priority != "hoog" {
		t.Fatalf("Priority = %v, want hoog", capture.Priority)
	}
	if capture.Symbol == nil || *capture.Symbol != "warning" {
		t.Fatalf("Symbol = %v, want warning", capture.Symbol)
	}
	if capture.TriageFlag == nil || !*capture.TriageFlag {
		t.Fatal("expected triage flag")
	}
	if capture.Deadline == nil || capture.Deadline.In(loc).Format("2006-01-02 15:04") != "2026-06-05 11:00" {
		t.Fatalf("Deadline = %v, want 2026-06-05 11:00", capture.Deadline)
	}
	if !hasTag(capture.Tags, "werk") {
		t.Fatalf("Tags = %v, want werk", capture.Tags)
	}
}

func TestParseOptionalNoteDeadline(t *testing.T) {
	parsed, err := parseOptionalNoteDeadline("05-06-2026 11:45")
	if err != nil {
		t.Fatalf("parseOptionalNoteDeadline() error = %v", err)
	}
	if parsed == nil || parsed.Format("2006-01-02 15:04") != "2026-06-05 11:45" {
		t.Fatalf("parsed = %v, want 2026-06-05 11:45", parsed)
	}
}
