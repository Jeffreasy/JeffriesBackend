package store

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestBeginPaymentRequestAttemptHasExactlyOneConcurrentWinner(t *testing.T) {
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

	userID := "bunq-reservation-test-" + uuid.NewString()
	invoiceID := uuid.New()
	if _, err := db.Pool.Exec(ctx, `
		INSERT INTO lc_invoices (id,user_id,invoice_number)
		VALUES ($1,$2,$3)`, invoiceID, userID, "TEST-"+invoiceID.String()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.Pool.Exec(context.Background(), `DELETE FROM lc_invoices WHERE id=$1 AND user_id=$2`, invoiceID, userID)
	})

	store := NewLaventeCareStore(db)
	start := make(chan struct{})
	var winners atomic.Int32
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, shouldCreate, err := store.BeginPaymentRequestAttempt(ctx, userID, invoiceID, uuid.NewString())
			if err != nil {
				errs <- err
				return
			}
			if shouldCreate {
				winners.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Errorf("reservation error: %v", err)
	}
	if got := winners.Load(); got != 1 {
		t.Fatalf("concurrent remote-create reservations = %d, want exactly 1", got)
	}
}
