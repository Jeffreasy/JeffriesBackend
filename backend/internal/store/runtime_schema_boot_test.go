package store

import (
	"context"
	"os"
	"testing"
)

// TestEnsureRuntimeSchema_FreshDB verifies the runtime schema is self-contained:
// applied against an EMPTY database it must succeed end-to-end (no "relation does
// not exist"), proving a fresh/restored DB — a new Render instance, a restore
// into an empty DB, local dev, a DR rebuild — can actually boot. It is then run a
// second time to confirm idempotency on a normal reboot.
//
// Gated on TEST_DATABASE_URL so it only runs where a throwaway empty Postgres is
// available (CI / local docker):
//
//	docker run -d -e POSTGRES_PASSWORD=test -e POSTGRES_DB=drtest -p 55432:5432 postgres:16-alpine
//	TEST_DATABASE_URL="postgres://postgres:test@localhost:55432/drtest" go test ./internal/store/ -run FreshDB
func TestEnsureRuntimeSchema_FreshDB(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping fresh-DB schema boot test")
	}
	ctx := context.Background()
	db, err := New(ctx, url)
	if err != nil {
		t.Fatalf("connect to test DB: %v", err)
	}
	defer db.Close()

	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatalf("EnsureRuntimeSchema on an empty DB failed (fresh-boot/DR is broken): %v", err)
	}
	if err := EnsureRuntimeSchema(ctx, db); err != nil {
		t.Fatalf("EnsureRuntimeSchema second run failed (not idempotent): %v", err)
	}

	// Smoke-test live store reads against the fresh schema so column drift between
	// the base CREATEs and what the stores actually query is caught here, not in
	// production after a DR rebuild.
	if _, err := NewAutomationStore(db).List(ctx, "boot-test-user"); err != nil {
		t.Fatalf("AutomationStore.List on fresh schema (column drift?): %v", err)
	}

	// Every ON CONFLICT upsert needs a matching UNIQUE index. These live only as
	// separate CREATE UNIQUE INDEX statements (not inline), so verify the fresh
	// schema actually has them — a missing one raises Postgres 42P10 on the first
	// upsert and silently rolls back (schedule/events/finance never populate).
	requiredUnique := []struct{ table, index string }{
		{"schedule", "idx_schedule_user_event"},
		{"personal_events", "idx_pe_user_event"},
		{"transactions", "idx_trx_user_rek_volgnr"},
		{"salary", "idx_salary_user_periode"},
		{"loonstroken", "idx_loon_user_jr_per"},
		{"lc_documents", "idx_lc_documents_user_key"},
	}
	for _, ru := range requiredUnique {
		var ok bool
		err := db.Pool.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM pg_indexes WHERE tablename = $1 AND indexname = $2 AND indexdef ILIKE '%unique%')`,
			ru.table, ru.index).Scan(&ok)
		if err != nil {
			t.Fatalf("checking unique index %s: %v", ru.index, err)
		}
		if !ok {
			t.Fatalf("missing UNIQUE index %s on %s — its ON CONFLICT upsert would 42P10 on a fresh DB", ru.index, ru.table)
		}
	}
}
