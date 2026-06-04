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
