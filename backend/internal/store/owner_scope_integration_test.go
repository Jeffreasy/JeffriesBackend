package store

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/Jeffreasy/JeffriesBackend/internal/model"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func TestHabitAndAutomationMutationsAreOwnerScoped(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}
	owner := "owner-scope-" + uuid.NewString()
	other := "other-scope-" + uuid.NewString()

	habits := NewHabitStore(db)
	habit, err := habits.Create(ctx, owner, model.Habit{
		Naam: "Scope test", Emoji: "x", Type: "positief", Frequentie: "dagelijks",
		XPPerVoltooiing: 10, Moeilijkheid: "normaal",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM habits WHERE id=$1`, habit.ID) })
	if _, err := habits.Get(ctx, other, habit.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("other owner Get error = %v", err)
	}
	if _, err := habits.Update(ctx, other, habit.ID, map[string]any{"naam": "stolen"}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("other owner Update error = %v", err)
	}
	if _, err := habits.UpsertLog(ctx, model.HabitLog{UserID: other, HabitID: habit.ID, Datum: "2026-07-17", Voltooid: true}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-owner log error = %v", err)
	}

	autos := NewAutomationStore(db)
	auto, err := autos.Create(ctx, model.AutomationRow{
		UserID: owner, Name: "scope-" + uuid.NewString(), Enabled: true,
		TriggerConfig: json.RawMessage(`{"triggerType":"time","time":"12:00"}`),
		ActionConfig:  json.RawMessage(`{"type":"noop"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.Pool.Exec(context.Background(), `DELETE FROM automations WHERE id=$1`, auto.ID) })
	if err := autos.Toggle(ctx, other, auto.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("other owner Toggle error = %v", err)
	}
	if _, err := autos.Update(ctx, other, auto.ID, auto); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("other owner automation Update error = %v", err)
	}
}

func TestContactListPaginationAndSearchContract(t *testing.T) {
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	db, err := New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatal(err)
	}

	owner := "contacts-contract-" + uuid.NewString()
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM contacts WHERE user_id=$1`, owner)
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM contact_labels WHERE user_id=$1`, owner)
	})
	contacts := NewContactStore(db)
	emailToken := "unique-email-token@example.test"
	noteToken := "unique notes token"
	first, err := contacts.Create(ctx, owner, model.Contact{DisplayName: "Alex", Email: &emailToken})
	if err != nil {
		t.Fatal(err)
	}
	second, err := contacts.Create(ctx, owner, model.Contact{DisplayName: "alex", Notes: &noteToken})
	if err != nil {
		t.Fatal(err)
	}
	third, err := contacts.Create(ctx, owner, model.Contact{DisplayName: "Alex"})
	if err != nil {
		t.Fatal(err)
	}
	label, err := contacts.CreateLabel(ctx, owner, "Unieke Zorglabel", "teal")
	if err != nil {
		t.Fatal(err)
	}
	if err := contacts.AssignLabel(ctx, owner, third.ID, label.ID); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		query string
		id    uuid.UUID
	}{
		{query: "unique-email-token", id: first.ID},
		{query: "notes token", id: second.ID},
		{query: "zorglabel", id: third.ID},
	} {
		got, err := contacts.List(ctx, owner, ListContactsOptions{Query: tc.query, Limit: 200})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].ID != tc.id {
			t.Fatalf("q=%q returned %+v, want contact %s", tc.query, got, tc.id)
		}
	}

	full, err := contacts.List(ctx, owner, ListContactsOptions{Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if len(full) != 3 {
		t.Fatalf("full list length = %d, want 3", len(full))
	}
	for offset := range full {
		page, err := contacts.List(ctx, owner, ListContactsOptions{Limit: 1, Offset: offset})
		if err != nil {
			t.Fatal(err)
		}
		if len(page) != 1 || page[0].ID != full[offset].ID {
			t.Fatalf("page offset %d = %+v, want %s", offset, page, full[offset].ID)
		}
	}
	empty, err := contacts.List(ctx, owner, ListContactsOptions{Query: "definitely-no-match", Limit: 200})
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty contact page must be a non-nil empty slice, got %#v", empty)
	}
}
