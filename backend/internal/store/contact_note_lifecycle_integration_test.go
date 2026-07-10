package store

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
)

// TestContactNoteLifecycle_FreshDB exercises the complete contact-context edge
// lifecycle against a real throwaway Postgres database. It deliberately starts
// from EnsureRuntimeSchema so schema drift and transactional lifecycle bugs are
// caught together.
//
// Gated on TEST_DATABASE_URL; see TestEnsureRuntimeSchema_FreshDB for a local
// Postgres command that provisions a suitable empty database.
func TestContactNoteLifecycle_FreshDB(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping contact-note lifecycle integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	t.Cleanup(db.Close)

	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatalf("EnsureRuntimeSchema on test DB: %v", err)
	}

	suffix := uuid.NewString()
	userID := "contact-note-lifecycle-" + suffix
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if _, err := db.Pool.Exec(cleanupCtx, `DELETE FROM notes WHERE user_id = $1`, userID); err != nil {
			t.Errorf("cleanup lifecycle notes: %v", err)
		}
		if _, err := db.Pool.Exec(cleanupCtx, `DELETE FROM contacts WHERE user_id = $1`, userID); err != nil {
			t.Errorf("cleanup lifecycle contacts: %v", err)
		}
	})

	contacts := NewContactStore(db)
	notes := NewNoteStore(db)
	createContact := func(label string) model.Contact {
		t.Helper()
		contact, err := contacts.Create(ctx, userID, model.Contact{
			DisplayName:       fmt.Sprintf("%s %s", label, suffix),
			RelationshipTypes: []string{"test"},
		})
		if err != nil {
			t.Fatalf("create %s contact: %v", label, err)
		}
		return contact
	}

	source := createContact("Source")
	survivor := createContact("Survivor")
	unrelated := createContact("Unrelated")

	contactType := "contact"
	sourceID := source.ID.String()
	forgedTitle := "door client vervalste titel"
	noteTitle := "Lifecycle note"
	linked, err := notes.Create(ctx, userID, model.Note{
		Titel:                &noteTitle,
		Inhoud:               "eerste versie",
		BusinessContextType:  &contactType,
		BusinessContextID:    &sourceID,
		BusinessContextTitle: &forgedTitle,
	})
	if err != nil {
		t.Fatalf("create contact-linked note: %v", err)
	}
	requireLifecycleContext(t, linked, contactType, source.ID.String(), source.DisplayName)
	if linked.BusinessContextTitle != nil && *linked.BusinessContextTitle == forgedTitle {
		t.Fatalf("NoteStore.Create trusted client title %q instead of canonical contact title", forgedTitle)
	}

	unrelatedID := unrelated.ID.String()
	unrelatedTitle := "also forged"
	unrelatedNote, err := notes.Create(ctx, userID, model.Note{
		Inhoud:               "hoort bij een andere contactpersoon",
		BusinessContextType:  &contactType,
		BusinessContextID:    &unrelatedID,
		BusinessContextTitle: &unrelatedTitle,
	})
	if err != nil {
		t.Fatalf("create unrelated contact note: %v", err)
	}
	if _, err := notes.Create(ctx, userID, model.Note{Inhoud: "zonder contactcontext"}); err != nil {
		t.Fatalf("create unlinked control note: %v", err)
	}

	assertLifecycleFilter := func(contactID uuid.UUID, wantIDs ...uuid.UUID) {
		t.Helper()
		got, err := notes.ListWithOptions(ctx, userID, NoteListOptions{
			ContextType: "contact",
			ContextID:   contactID.String(),
		})
		if err != nil {
			t.Fatalf("filter notes for contact %s: %v", contactID, err)
		}
		requireLifecycleNoteIDs(t, got, wantIDs...)
	}
	assertLifecycleFilter(source.ID, linked.ID)
	assertLifecycleFilter(unrelated.ID, unrelatedNote.ID)
	contactNotes, err := notes.ListWithOptions(ctx, userID, NoteListOptions{ContextType: "contact"})
	if err != nil {
		t.Fatalf("filter notes by contact context type: %v", err)
	}
	requireLifecycleNoteIDs(t, contactNotes, linked.ID, unrelatedNote.ID)

	// A content update records the original contact-bearing snapshot. All later
	// contact operations must update both this history row and the live note.
	linked, err = notes.UpdateForUser(ctx, userID, linked.ID, map[string]any{"inhoud": "tweede versie"})
	if err != nil {
		t.Fatalf("update note to create revision: %v", err)
	}
	revisions, err := notes.ListRevisions(ctx, userID, linked.ID, 20)
	if err != nil {
		t.Fatalf("list initial revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("expected one revision after content update, got %d", len(revisions))
	}
	requireLifecycleRevisionContext(t, revisions[0], contactType, source.ID.String(), source.DisplayName)

	renamed := "Renamed " + suffix
	if _, err := contacts.Update(ctx, userID, source.ID, ContactUpdate{DisplayName: &renamed}); err != nil {
		t.Fatalf("rename source contact: %v", err)
	}
	linked = lifecycleGetNote(t, ctx, notes, userID, linked.ID)
	requireLifecycleContext(t, linked, contactType, source.ID.String(), renamed)
	revisions = lifecycleGetRevisions(t, ctx, notes, userID, linked.ID)
	requireLifecycleRevisionContext(t, revisions[0], contactType, source.ID.String(), renamed)

	if _, err := contacts.MergeContacts(ctx, userID, source.ID, survivor.ID); err != nil {
		t.Fatalf("merge source into survivor: %v", err)
	}
	linked = lifecycleGetNote(t, ctx, notes, userID, linked.ID)
	requireLifecycleContext(t, linked, contactType, survivor.ID.String(), survivor.DisplayName)
	revisions = lifecycleGetRevisions(t, ctx, notes, userID, linked.ID)
	requireLifecycleRevisionContext(t, revisions[0], contactType, survivor.ID.String(), survivor.DisplayName)
	assertLifecycleFilter(survivor.ID, linked.ID)
	assertLifecycleFilter(unrelated.ID, unrelatedNote.ID)

	if err := contacts.Delete(ctx, userID, survivor.ID); err != nil {
		t.Fatalf("delete survivor contact: %v", err)
	}
	linked = lifecycleGetNote(t, ctx, notes, userID, linked.ID)
	requireLifecycleNoContext(t, linked.BusinessContextType, linked.BusinessContextID, linked.BusinessContextTitle)
	revisions = lifecycleGetRevisions(t, ctx, notes, userID, linked.ID)
	if len(revisions) != 1 {
		t.Fatalf("contact delete changed revision count: got %d, want 1", len(revisions))
	}
	requireLifecycleNoContext(t, revisions[0].BusinessContextType, revisions[0].BusinessContextID, revisions[0].BusinessContextTitle)

	// Deleting one contact must not detach another contact's note.
	unrelatedNote = lifecycleGetNote(t, ctx, notes, userID, unrelatedNote.ID)
	requireLifecycleContext(t, unrelatedNote, contactType, unrelated.ID.String(), unrelated.DisplayName)
}

func lifecycleGetNote(t *testing.T, ctx context.Context, notes *NoteStore, userID string, id uuid.UUID) model.Note {
	t.Helper()
	note, err := notes.GetForUser(ctx, userID, id)
	if err != nil {
		t.Fatalf("get lifecycle note %s: %v", id, err)
	}
	return note
}

func lifecycleGetRevisions(t *testing.T, ctx context.Context, notes *NoteStore, userID string, id uuid.UUID) []model.NoteRevision {
	t.Helper()
	revisions, err := notes.ListRevisions(ctx, userID, id, 20)
	if err != nil {
		t.Fatalf("list lifecycle revisions for %s: %v", id, err)
	}
	if len(revisions) == 0 {
		t.Fatalf("expected at least one lifecycle revision for %s", id)
	}
	return revisions
}

func requireLifecycleContext(t *testing.T, note model.Note, wantType, wantID, wantTitle string) {
	t.Helper()
	requireLifecycleContextValues(t, note.BusinessContextType, note.BusinessContextID, note.BusinessContextTitle, wantType, wantID, wantTitle)
}

func requireLifecycleRevisionContext(t *testing.T, revision model.NoteRevision, wantType, wantID, wantTitle string) {
	t.Helper()
	requireLifecycleContextValues(t, revision.BusinessContextType, revision.BusinessContextID, revision.BusinessContextTitle, wantType, wantID, wantTitle)
}

func requireLifecycleContextValues(t *testing.T, gotType, gotID, gotTitle *string, wantType, wantID, wantTitle string) {
	t.Helper()
	if gotType == nil || *gotType != wantType {
		t.Fatalf("context type = %v, want %q", gotType, wantType)
	}
	if gotID == nil || *gotID != wantID {
		t.Fatalf("context id = %v, want %q", gotID, wantID)
	}
	if gotTitle == nil || *gotTitle != wantTitle {
		t.Fatalf("context title = %v, want %q", gotTitle, wantTitle)
	}
}

func requireLifecycleNoContext(t *testing.T, gotType, gotID, gotTitle *string) {
	t.Helper()
	if gotType != nil || gotID != nil || gotTitle != nil {
		t.Fatalf("expected cleared context, got type=%v id=%v title=%v", gotType, gotID, gotTitle)
	}
}

func requireLifecycleNoteIDs(t *testing.T, got []model.Note, want ...uuid.UUID) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("filtered note count = %d, want %d (got=%v want=%v)", len(got), len(want), lifecycleNoteIDs(got), want)
	}
	wantSet := make(map[uuid.UUID]struct{}, len(want))
	for _, id := range want {
		wantSet[id] = struct{}{}
	}
	for _, note := range got {
		if _, ok := wantSet[note.ID]; !ok {
			t.Fatalf("filtered notes contain unexpected id %s (got=%v want=%v)", note.ID, lifecycleNoteIDs(got), want)
		}
	}
}

func lifecycleNoteIDs(notes []model.Note) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(notes))
	for _, note := range notes {
		ids = append(ids, note.ID)
	}
	return ids
}
