package store

import (
	"reflect"
	"testing"
	"time"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

func TestExtractWikiLinkTitles(t *testing.T) {
	got := extractWikiLinkTitles("Bel [[Project A]] en daarna [[ Project A ]] of [[Agenda]]. Lege [[]] telt niet.")
	want := []string{"Project A", "Agenda"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestNoteRevisionFieldsChangedIgnoresNonContentFields(t *testing.T) {
	note := model.Note{Inhoud: "zelfde"}

	if noteRevisionFieldsChanged(note, map[string]any{
		"is_pinned":    true,
		"is_archived":  true,
		"is_completed": true,
		"completed_at": time.Now(),
	}) {
		t.Fatal("expected status-only fields to be ignored")
	}
}

func TestNoteRevisionFieldsChangedDetectsContentChange(t *testing.T) {
	note := model.Note{Inhoud: "oud"}

	if !noteRevisionFieldsChanged(note, map[string]any{"inhoud": "nieuw"}) {
		t.Fatal("expected changed content to create a revision")
	}
}

func TestNoteRevisionFieldsChangedSkipsEquivalentOptionalStrings(t *testing.T) {
	note := model.Note{}

	if noteRevisionFieldsChanged(note, map[string]any{"titel": "", "kleur": nil, "linked_event_id": ""}) {
		t.Fatal("expected nil and empty optional strings to be treated as equivalent")
	}
}

func TestNoteRevisionFieldsChangedComparesTagsAsSet(t *testing.T) {
	note := model.Note{Tags: []string{"dkl", "werk"}}

	if noteRevisionFieldsChanged(note, map[string]any{"tags": []string{"Werk", "dkl"}}) {
		t.Fatal("expected reordered/case-normalized tags to be equivalent")
	}

	if !noteRevisionFieldsChanged(note, map[string]any{"tags": []string{"dkl", "urgent"}}) {
		t.Fatal("expected changed tag set to create a revision")
	}
}

func TestNoteRevisionFieldsChangedComparesDeadlines(t *testing.T) {
	deadline := time.Date(2026, 6, 3, 17, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
	sameInstant := deadline.UTC()
	other := deadline.Add(time.Hour)
	note := model.Note{Deadline: &deadline}

	if noteRevisionFieldsChanged(note, map[string]any{"deadline": &sameInstant}) {
		t.Fatal("expected equal deadline instants to be equivalent")
	}

	if !noteRevisionFieldsChanged(note, map[string]any{"deadline": &other}) {
		t.Fatal("expected changed deadline to create a revision")
	}
}
