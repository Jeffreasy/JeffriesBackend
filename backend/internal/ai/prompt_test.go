package ai

import (
	"strings"
	"testing"
	"time"
)

// TestDateContextBlockWeekdays pins down the exact bug reported in production:
// the Telegram assistant told the user "vandaag is 30 juni 2026 (maandag)" and
// then proposed wrong dates for "volgende week", because the prompt only ever
// gave the model a bare ISO date ("2026-06-30") and let it guess the weekday.
// 2026-06-30 is in fact a Tuesday — verified against the real calendar.
func TestDateContextBlockWeekdays(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Amsterdam")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Date(2026, time.June, 30, 9, 0, 0, 0, loc)

	block := dateContextBlockAt(now)

	if !strings.Contains(block, "Vandaag is dinsdag 30 juni 2026 (2026-06-30)") {
		t.Fatalf("expected today's line to say dinsdag 30 juni 2026, got:\n%s", block)
	}

	// The dates Salih proposed in the reported conversation.
	for date, weekday := range map[string]string{
		"2026-07-07": "dinsdag",
		"2026-07-08": "woensdag",
		"2026-07-09": "donderdag",
	} {
		needle := date + ": " + weekday
		if !strings.Contains(block, needle) {
			t.Fatalf("expected table to contain %q, got:\n%s", needle, block)
		}
	}
}

func TestBuildSystemPromptAddsNotesGuardrails(t *testing.T) {
	agent := GetAgent("notes")
	if agent == nil {
		t.Fatal("notes agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{
		"notes": map[string]any{
			"stats": map[string]any{
				"active": 3,
			},
		},
	}, nil)

	for _, needle := range []string{
		"NOTES ORCHESTRATIE",
		"Lees eerst Live Data.notes",
		"Zeg nooit \"geen actieve notities\"",
		"notes.stats.active",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestBuildSystemPromptAddsAgendaGuardrails(t *testing.T) {
	agent := GetAgent("agenda")
	if agent == nil {
		t.Fatal("agenda agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{}, nil)
	for _, needle := range []string{
		"AGENDA ORCHESTRATIE",
		"planningOpvragen",
		"afsprakenOpvragen",
		"verzin geen datums",
		"werkdiensten en persoonlijke afspraken",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestBuildSystemPromptAddsRoosterGuardrails(t *testing.T) {
	agent := GetAgent("rooster")
	if agent == nil {
		t.Fatal("rooster agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{}, nil)
	for _, needle := range []string{
		"ROOSTER ORCHESTRATIE",
		"dienstenOpvragen",
		"totaalUur",
		"contractAnalyseOpvragen",
		"verzin geen datums",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestBuildSystemPromptAddsFinanceGuardrails(t *testing.T) {
	agent := GetAgent("finance")
	if agent == nil {
		t.Fatal("finance agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{}, nil)
	for _, needle := range []string{
		"FINANCE ORCHESTRATIE",
		"saldoOpvragen",
		"transactiesZoeken",
		"specifieke analyse-tools",
		"Verzin nooit bedragen",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestBuildSystemPromptAddsLaventeCareGuardrails(t *testing.T) {
	agent := GetAgent("laventecare")
	if agent == nil {
		t.Fatal("laventecare agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{}, nil)
	for _, needle := range []string{
		"LAVENTECARE ORCHESTRATIE",
		"laventecareCockpit",
		"planningOpvragen",
		"notitiesZoeken",
		"laventecareDossierDocumentenOpvragen",
		"PDF Studio",
		"documentbasis leeg",
		"Nederlandse status- en prioriteitswaarden",
		"Verzin nooit leads",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}

func TestBuildSystemPromptAddsHabitsGuardrails(t *testing.T) {
	agent := GetAgent("habits")
	if agent == nil {
		t.Fatal("habits agent not found")
	}

	prompt := BuildSystemPrompt(agent, map[string]any{}, nil)
	for _, needle := range []string{
		"HABITS ORCHESTRATIE",
		"habitRapport",
		"habitVoltooien",
		"habitIncident",
		"vandaagDue",
		"Verzin nooit habits",
	} {
		if !strings.Contains(prompt, needle) {
			t.Fatalf("prompt missing %q", needle)
		}
	}
}
