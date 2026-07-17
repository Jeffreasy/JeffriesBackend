package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	tg "github.com/Jeffreasy/JeffriesBackend/internal/telegram"
	"github.com/google/uuid"
)

func TestDetectLampCommandWholeWordOnly(t *testing.T) {
	// T4: "aanpassen" contains "aan" — must NOT trigger "alles aan" but go to AI.
	if cmd := detectLampCommand("lampen aanpassen naar blauw"); cmd != nil && cmd.beschrijving == "Lampen aanzetten" {
		t.Fatalf("detectLampCommand matched 'aan' inside 'aanpassen': %v", cmd.beschrijving)
	}
	if cmd := detectLampCommand("lampen aan"); cmd == nil || cmd.beschrijving != "Lampen aanzetten" {
		t.Fatalf("detectLampCommand('lampen aan') = %v, want Lampen aanzetten", cmd)
	}
	if cmd := detectLampCommand("doe de lampen uit"); cmd == nil || cmd.beschrijving != "Lampen uitzetten" {
		t.Fatalf("detectLampCommand('doe de lampen uit') = %v, want Lampen uitzetten", cmd)
	}
	// "buiten" contains "uit" — must not turn everything off.
	if cmd := detectLampCommand("lampen buitenshuis graag feller"); cmd != nil && cmd.beschrijving == "Lampen uitzetten" {
		t.Fatal("detectLampCommand matched 'uit' inside 'buitenshuis'")
	}
}

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

func TestRouteFreeTextLaventeCareAgendaAndNotesStayWithLaventeCare(t *testing.T) {
	tests := []string{
		"welke afspraken heb ik rond LaventeCare deze week?",
		"zoek notities over LaventeCare HenkeWonen",
		"welke offertes staan in het klantdossier?",
	}

	for _, input := range tests {
		if got := routeFreeText(input); got != "laventecare" {
			t.Fatalf("routeFreeText(%q) = %q, want laventecare", input, got)
		}
	}
}

func TestRouteFreeTextLaventeCareNoteCaptureStaysWithNotes(t *testing.T) {
	got := routeFreeText("noteer LaventeCare HenkeWonen morgen bellen")
	if got != "notes" {
		t.Fatalf("routeFreeText() = %q, want notes", got)
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
	// Test bold and link conversion
	got1 := stripTelegramPlainText("**Kop**\n[Bron](https://example.com)")
	want1 := "Kop\nBron (https://example.com)"
	if got1 != want1 {
		t.Errorf("stripTelegramPlainText() = %q, want %q", got1, want1)
	}

	// Test headers (ensuring no sub-hash bugs)
	got2 := stripTelegramPlainText("#### Sub-sectie\n# Hoofdkop")
	want2 := "Sub-sectie\nHoofdkop"
	if got2 != want2 {
		t.Errorf("stripTelegramPlainText() with headers = %q, want %q", got2, want2)
	}

	// Test bullet lists (asterisks should not be stripped if they are list items)
	got3 := stripTelegramPlainText("* Bullet 1\n* Bullet 2 met *italic* en _underscore_")
	want3 := "* Bullet 1\n* Bullet 2 met italic en underscore"
	if got3 != want3 {
		t.Errorf("stripTelegramPlainText() with bullets = %q, want %q", got3, want3)
	}
}

func TestRelativeDateLabelDST(t *testing.T) {
	// Mock a DST transition day in spring: Sunday 2026-03-29 clocks go forward.
	// We'll test from Saturday 2026-03-28 to Sunday 2026-03-29.
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	nowSaturday := time.Date(2026, 3, 28, 12, 0, 0, 0, loc)

	got := relativeDateLabel("2026-03-29", nowSaturday)
	if !strings.HasPrefix(got, "morgen") {
		t.Fatalf("relativeDateLabel on spring DST transition weekend got %q, want starting with 'morgen'", got)
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

	if period.Label != "juni 2026 tot nu (standaard)" {
		t.Fatalf("Label = %q", period.Label)
	}
	if period.DatumVan != "2026-06-01" || period.DatumTot != "2026-06-05" {
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

func TestClassifyUserFacingErrorMapsToDutch(t *testing.T) {
	cases := map[string]string{
		"context deadline exceeded":                      "De AI reageerde niet op tijd. Probeer het nog eens.",
		"Grok 429: rate limit exceeded":                  "Te veel aanvragen bij de AI. Wacht even en probeer het opnieuw.",
		"request error: dial tcp: i/o timeout":           "De AI reageerde niet op tijd. Probeer het nog eens.",
		"parse error: unexpected end of JSON input":      "Onverwacht antwoord van de AI-server. Probeer het opnieuw.",
		"some totally unrecognized go internal error :(": "Er ging iets mis bij het verwerken van je bericht. Probeer het opnieuw.",
	}
	for raw, want := range cases {
		if got := classifyUserFacingError(raw); got != want {
			t.Errorf("classifyUserFacingError(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestClassifyUserFacingErrorPassesThroughFriendlyDutchMessages(t *testing.T) {
	friendly := "De AI server is tijdelijk onbereikbaar wegens overbelasting. Probeer het later opnieuw."
	if got := classifyUserFacingError(friendly); got != friendly {
		t.Errorf("expected friendly Dutch message to pass through unchanged, got %q", got)
	}
	configMsg := "GROK_API_KEY niet geconfigureerd"
	if got := classifyUserFacingError(configMsg); got != configMsg {
		t.Errorf("expected config message to pass through unchanged, got %q", got)
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

func TestClosestKnownCommandSuggestsTypo(t *testing.T) {
	cases := map[string]string{
		"/aproove":  "/approve", // the exact reported typo class — missing/reordered letters, must still suggest
		"/aprove":   "/approve",
		"/rejct":    "/reject",
		"/breifing": "/briefing",
	}
	for typed, want := range cases {
		if got := closestKnownCommand(typed); got != want {
			t.Errorf("closestKnownCommand(%q) = %q, want %q", typed, got, want)
		}
	}
}

func TestClosestKnownCommandNoSuggestionWhenTooDifferent(t *testing.T) {
	// A genuinely unrelated slash-string shouldn't produce a misleading guess.
	if got := closestKnownCommand("/xyzxyzxyz"); got != "" {
		t.Errorf("closestKnownCommand(%q) = %q, want no suggestion", "/xyzxyzxyz", got)
	}
}

func TestLevenshteinDistance(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"approve", "approve", 0},
		{"approve", "aproove", 2},
		{"", "abc", 3},
		{"abc", "", 3},
		{"kitten", "sitting", 3},
	}
	for _, c := range cases {
		if got := levenshteinDistance(c.a, c.b); got != c.want {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestFormatNoteDeadlineDST(t *testing.T) {
	// Mock a DST transition day in spring: Sunday 2026-03-29 clocks go forward.
	// We'll test from Saturday 2026-03-28 to Sunday 2026-03-29.
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	nowSaturday := time.Date(2026, 3, 28, 12, 0, 0, 0, loc)
	deadlineSunday := time.Date(2026, 3, 29, 15, 0, 0, 0, loc)

	got := formatNoteDeadline(deadlineSunday, nowSaturday, loc)
	if got != "morgen" {
		t.Fatalf("formatNoteDeadline on spring DST transition weekend got %q, want 'morgen'", got)
	}
}
