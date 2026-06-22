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
}
