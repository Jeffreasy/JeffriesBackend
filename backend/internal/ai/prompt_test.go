package ai

import (
	"strings"
	"testing"
)

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
